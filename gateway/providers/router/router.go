// Package router Provider 治理引擎(批次 7):降级链 + 熔断 + 路由表热重载。
//
// 规则事实源:docs/architecture.md「Provider 治理」与 docs/contract/realname.md
// 「Provider 热插拔」——RealName 是第一个完整适用方,后续域(配置/品牌/诊断)复用。
// 调用失败(网络/超时/5xx,即 error)才计入熔断;业务结果(REJECTED 等)是有效
// 输出,不计。全链失败按域定义安全态:Verify/Query → PENDING_REVIEW,
// Curfew → 不可玩,ChargeCheck → 不放行(不通过也不拒绝放量,保守面)。
package router

import (
	"sync"
	"sync/atomic"
	"time"
)

// IdentityInput 提交核验的实名输入。
//
// 红线(docs/contract/realname.md):本结构只在调用栈内存活——实现方不得落日志、
// 不得进 trace、不得进诊断采集;持久化只允许存指纹(SHA-256),不存明文。
type IdentityInput struct {
	Name     string
	IDNumber string
}

// VerifyOutcome 核验结果。State ∈ UNVERIFIED / PENDING_REVIEW / VERIFIED / REJECTED。
type VerifyOutcome struct {
	State   string
	IsMinor bool
}

// StatusOutcome 状态查询结果(契约 realname.md status 数据模型)。
type StatusOutcome struct {
	State       string
	IsMinor     bool
	VerifiedAt  time.Time
	HasVerified bool
}

// CurfewOutcome 可玩时段判定(契约 F28)。
type CurfewOutcome struct {
	Playable      bool
	NextWindowAt  time.Time
	HasNextWindow bool
}

// ChargeOutcome 充值额度判定(契约 F29;限额字段 0 = 无限制/未下发)。
type ChargeOutcome struct {
	Allowed           bool
	SingleLimitCents  int
	MonthlyLimitCents int
	MonthlyUsedCents  int
}

// RealNameProvider 实名域供应商接口(自建核验/阿里云/腾讯云慧眼/易盾/Webhook 同一形状)。
// 返回 error = 调用失败(计入熔断);业务判定一律经 Outcome 返回。
type RealNameProvider interface {
	Name() string
	// Verify 提交核验;实现方保证幂等(已 VERIFIED 的账号重复提交返回既有状态)。
	Verify(accountID string, id IdentityInput) (VerifyOutcome, error)
	// Query 状态查询;未提交 → UNVERIFIED。
	Query(accountID string) (StatusOutcome, error)
	// Curfew 可玩时段判定;now 由调用方注入(可测)。
	Curfew(accountID string, now time.Time) (CurfewOutcome, error)
	// ChargeCheck 充值额度校验。
	ChargeCheck(accountID string, amountCents int) (ChargeOutcome, error)
	// HealthCheck 路由表剔除不健康实例用。
	HealthCheck() bool
}

// BreakerConfig 熔断参数(零值 = 默认:连续 5 次失败开路,开路 30 次调用后半开探测)。
type BreakerConfig struct {
	Failures  int // 连续失败开路阈值(契约默认 5)
	OpenCalls int // 开路期间经过的调用数达到该值后放行一次半开探测(契约默认 30)
}

func (c BreakerConfig) withDefaults() BreakerConfig {
	if c.Failures <= 0 {
		c.Failures = 5
	}
	if c.OpenCalls <= 0 {
		c.OpenCalls = 30
	}
	return c
}

// Config 路由表配置:primary + fallbacks[](契约「热切换」节)。
type Config struct {
	Primary   RealNameProvider
	Fallbacks []RealNameProvider // 保序、去重(primary 名重复的静默丢弃)
	Breaker   BreakerConfig
}

// Router 实名域治理路由器:每次调用开头原子加载路由表快照——
// Reload 替换表不影响在途请求(契约:在途按旧表完成,新请求走新表)。
type Router struct {
	table atomic.Pointer[routingTable]
}

type routingTable struct {
	chain    []RealNameProvider
	breakers map[string]*Breaker
}

// New 装配路由器;Primary 为 nil 视为空链(所有调用落安全态)。
func New(cfg Config) *Router {
	r := &Router{}
	r.Reload(cfg)
	return r
}

// Reload 原子替换路由表;熔断器随之重建(新供应商新熔断,旧状态不跨表携带)。
func (r *Router) Reload(cfg Config) {
	t := &routingTable{breakers: make(map[string]*Breaker)}
	seen := make(map[string]bool)
	add := func(p RealNameProvider) {
		if p == nil || seen[p.Name()] {
			return
		}
		seen[p.Name()] = true
		t.chain = append(t.chain, p)
	}
	add(cfg.Primary)
	for _, f := range cfg.Fallbacks {
		add(f)
	}
	for _, p := range t.chain {
		t.breakers[p.Name()] = NewBreaker(cfg.Breaker)
	}
	r.table.Store(t)
}

// Snapshot 当前链的供应商名(诊断/测试;primary 在首位)。
func (r *Router) Snapshot() []string {
	t := r.table.Load()
	names := make([]string, 0, len(t.chain))
	for _, p := range t.chain {
		names = append(names, p.Name())
	}
	return names
}

// Verify 治理链核验;全链失败 → PENDING_REVIEW(安全态)。
func (r *Router) Verify(accountID string, id IdentityInput) VerifyOutcome {
	return route(r, func(p RealNameProvider) (VerifyOutcome, error) {
		return p.Verify(accountID, id)
	}, VerifyOutcome{State: "PENDING_REVIEW"})
}

// Query 治理链状态查询;全链失败 → PENDING_REVIEW。
func (r *Router) Query(accountID string) StatusOutcome {
	return route(r, func(p RealNameProvider) (StatusOutcome, error) {
		return p.Query(accountID)
	}, StatusOutcome{State: "PENDING_REVIEW"})
}

// Curfew 治理链时段判定;全链失败 → 不可玩(保守面,契约 F28 安全态)。
func (r *Router) Curfew(accountID string, now time.Time) CurfewOutcome {
	return route(r, func(p RealNameProvider) (CurfewOutcome, error) {
		return p.Curfew(accountID, now)
	}, CurfewOutcome{Playable: false})
}

// ChargeCheck 治理链额度校验;全链失败 → 不放行(契约 F29 安全态)。
func (r *Router) ChargeCheck(accountID string, amountCents int) ChargeOutcome {
	return route(r, func(p RealNameProvider) (ChargeOutcome, error) {
		return p.ChargeCheck(accountID, amountCents)
	}, ChargeOutcome{Allowed: false})
}

// route 单次调用走链:熔断开路的供应商直接跳过;调用失败计熔断并落下一家;
// 全链失败返回 safe(域安全态)。业务结果(error == nil)即成功,不计熔断。
func route[T any](r *Router, fn func(RealNameProvider) (T, error), safe T) T {
	t := r.table.Load() // 在途请求按本快照完成
	for _, p := range t.chain {
		b := t.breakers[p.Name()]
		allowed, _ := b.Allow()
		if !allowed {
			continue // 开路中:熔断期直接走 fallback(契约)
		}
		out, err := fn(p)
		if err == nil {
			b.OnSuccess()
			return out
		}
		b.OnFailure()
	}
	return safe
}

// Breaker 按调用次数驱动的熔断器(无后台定时,平台无关,可测):
// closed →(连续 Failures 次失败)→ open →(OpenCalls 次调用后)→ 半开放行一次探测
// → 成功回 closed / 失败回 open。
type Breaker struct {
	mu        sync.Mutex
	threshold int
	openCalls int
	failures  int
	open      bool
	ticks     int
	probing   bool
}

// NewBreaker 零值 BreakerConfig 走默认(5 次开路 / 30 次调用后半开)。
func NewBreaker(cfg BreakerConfig) *Breaker {
	c := cfg.withDefaults()
	return &Breaker{threshold: c.Failures, openCalls: c.OpenCalls}
}

// Allow 本次调用是否放行;probe = 本次是半开探测。
func (b *Breaker) Allow() (allowed, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return true, false
	}
	b.ticks++
	if b.ticks >= b.openCalls {
		b.ticks = 0
		b.probing = true
		return true, true
	}
	return false, false
}

// OnSuccess 调用成功:连续失败清零;半开探测成功 → 关闭熔断。
func (b *Breaker) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	if b.probing {
		b.open = false
		b.probing = false
	}
}

// OnFailure 调用失败:计数;达到阈值开路;半开探测失败 → 维持开路。
func (b *Breaker) OnFailure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open {
		b.probing = false
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.open = true
		b.probing = false
		b.ticks = 0
	}
}

// IsOpen 诊断/测试用。
func (b *Breaker) IsOpen() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.open
}
