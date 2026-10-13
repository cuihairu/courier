// Package wardenalt 实名域第二实现(批次 64,契约 docs/contract/realname.md):
// 托管核验形状——判定全部来自注入的核验服务(CloudVerifier:接入方对接
// 阿里云/腾讯云慧眼/易盾/自有 Webhook 的适配,契约「自定义 Webhook」行),
// 本包不解析证件号(连出生段都不提取)、不存指纹(本地只留判定结果),与
// warden(自建核验:本地格式判定恒 VERIFIED + SHA-256 指纹落库)同契约异构:
// 自建恒通过 vs 托管可 REJECTED/PENDING_REVIEW,是可观察的 wire 差异面。
//
// 一致性证明:e2e/realname_conformance_test.go——两实现同一 wire 场景同断言
// (批次 18 accountalt 先例)。接入方切换 = {"realname":{"primary":"warden-alt"}}
// 一行配置;核验服务经 Options.Cloud 注入(Webhook 形状可用环境变量装配,见 main.go)。
//
// 治理:cloudCore 作为 primary 挂 router.Router(降级链/熔断/热重载复用批次 7
// 治理引擎);未配核验服务 = fail-closed(HealthCheck 不过,不进路由)。
//
// 红线(realname.md「字段脱敏与数据红线」):姓名/证件号只在调用栈内存活并
// 仅透传给核验服务;本地持久化零身份派生数据(比 warden 的指纹口径更小);
// 日志与 trace 禁明文——错误信息不含输入原文。
package wardenalt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/audit"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/router"
)

// DefaultName 注册名;路由表 {"realname":{"primary":"warden-alt"}} 即换。
const DefaultName = "warden-alt"

const prefixRealname = "/v1/realname/"

// 实名状态(契约 realname.md;与 warden 同表)。
const (
	stateUnverified    = "UNVERIFIED"
	statePendingReview = "PENDING_REVIEW"
	stateVerified      = "VERIFIED"
	stateRejected      = "REJECTED"
)

// Verdict 托管核验服务返回的判定(契约不描述核验细节,判定语义归服务)。
type Verdict struct {
	State   string `json:"state"` // VERIFIED / REJECTED / PENDING_REVIEW
	IsMinor bool   `json:"isMinor"`
	AgeBand string `json:"ageBand,omitempty"` // "<8"/"8-16"/"16-18"/"18+";VERIFIED 时有效
}

// CloudVerifier 托管核验服务接缝:接入方实现本接口对接云核验 API
// (Webhook 形状已内置,云 API 适配同形状)。返回 error = 调用失败,
// 进治理熔断;业务判定(含 REJECTED)一律经 Verdict 返回。
type CloudVerifier interface {
	Verify(accountID, name, idNumber string) (Verdict, error)
}

// VerifierFunc 函数适配器(测试/联调脚本化)。
type VerifierFunc func(accountID, name, idNumber string) (Verdict, error)

// Verify 实现 CloudVerifier。
func (f VerifierFunc) Verify(accountID, name, idNumber string) (Verdict, error) {
	return f(accountID, name, idNumber)
}

// WebhookVerifier 自定义 Webhook 适配(契约 realname.md Provider 表末行):
// POST JSON {accountId,name,idNumber} 到接入方核验服务,2xx 响应体即 Verdict;
// 非 2xx / 非法判定 = 调用失败(进治理熔断,fallback 接管)。
type WebhookVerifier struct {
	// Endpoint 接入方核验服务 URL(HTTPS,契约传输红线)。
	Endpoint string
	// Client HTTP 客户端;nil = 缺省 5s 超时(核验调用必须有界,不拖垮降级链)。
	Client *http.Client
}

// Verify 实现 CloudVerifier。
func (w WebhookVerifier) Verify(accountID, name, idNumber string) (Verdict, error) {
	body, err := json.Marshal(map[string]string{
		"accountId": accountID, "name": name, "idNumber": idNumber,
	})
	if err != nil {
		return Verdict{}, err
	}
	client := w.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Post(w.Endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return Verdict{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Verdict{}, fmt.Errorf("wardenalt: webhook status %d", resp.StatusCode)
	}
	var v Verdict
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return Verdict{}, err
	}
	if !validVerdict(v) {
		return Verdict{}, fmt.Errorf("wardenalt: webhook verdict state %q invalid", v.State)
	}
	return v, nil
}

func validVerdict(v Verdict) bool {
	switch v.State {
	case stateVerified, stateRejected, statePendingReview:
		return true
	}
	return false
}

// Window 可玩时段窗口(F28;策略配置归 Provider——契约明示,缺省同监管口径)。
type Window struct {
	Weekday   time.Weekday
	StartHour int
	EndHour   int
}

// ChargeLimits 分年龄段充值限额(分;F29 监管口径缺省,与 warden 同表)。
type ChargeLimits struct {
	Single8To16Cents   int
	Monthly8To16Cents  int
	Single16To18Cents  int
	Monthly16To18Cents int
}

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(默认 time.Now)。
	Clock func() time.Time
	// RequireAuth 玩家侧认证中间件;nil = fail-closed(一律 401)。
	RequireAuth func(http.Handler) http.Handler
	// Cloud 托管核验服务;nil = fail-closed(HealthCheck 不过,不进路由)。
	Cloud CloudVerifier
	// Fallbacks 降级链(依序;托管核验恒为 primary)。
	Fallbacks []router.RealNameProvider
	// Breaker 熔断参数(零值 = 契约默认 5 次/30 调用)。
	Breaker router.BreakerConfig
	// CurfewWindows 未成年可玩窗口(缺省 = 周五/六/日 20:00–21:00)。
	CurfewWindows []Window
	// Limits 分龄限额(缺省 = F29 监管口径)。
	Limits ChargeLimits
	// S2SToken 接入方服务端凭证(playtime-report 专用);空 = S2S 端点 fail-closed 401。
	S2SToken string
	// AuditSink 审计留痕接收面(F33;nil = 不产出审计事件)。与 warden 同形状
	// (gateway/audit 共享接缝)——「同契约可换」要求切换实现不静默丢合规留痕。
	AuditSink audit.AuditSink
}

func defaultWindows() []Window {
	return []Window{
		{time.Friday, 20, 21}, {time.Saturday, 20, 21}, {time.Sunday, 20, 21},
	}
}

func defaultLimits() ChargeLimits {
	return ChargeLimits{
		Single8To16Cents: 5000, Monthly8To16Cents: 20000,
		Single16To18Cents: 10000, Monthly16To18Cents: 40000,
	}
}

// Provider 实名域第二实现:HTTP 投影(/v1/realname/*)+ 治理路由(托管核验为主)。
type Provider struct {
	name   string
	clock  func() time.Time
	authMW func(http.Handler) http.Handler
	rt     *router.Router
	core   *cloudCore
	s2s    string
	// auditSink 审计留痕接收面(F33;nil = 不产出)。
	auditSink audit.AuditSink
}

// New 装配;Fallbacks 注入即降级链(托管核验恒为 primary)。
func New(opts Options) *Provider {
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.CurfewWindows == nil {
		opts.CurfewWindows = defaultWindows()
	}
	if opts.Limits == (ChargeLimits{}) {
		opts.Limits = defaultLimits()
	}
	c := &cloudCore{cloud: opts.Cloud, clock: opts.Clock, windows: opts.CurfewWindows,
		limits: opts.Limits, records: make(map[string]*bandRecord), plays: make(map[playKey]playReport)}
	rt := router.New(router.Config{Primary: c, Fallbacks: opts.Fallbacks, Breaker: opts.Breaker})
	return &Provider{name: opts.Name, clock: opts.Clock,
		authMW: opts.RequireAuth, rt: rt, core: c, s2s: opts.S2SToken,
		auditSink: opts.AuditSink}
}

// emitAudit 关键动作留痕(F33):nil sink 零开销;事件只含账号维度元数据,
// 身份输入(姓名/证件号)到不了这里——红线在 handleVerify 处即已隔离。
func (p *Provider) emitAudit(action, accountID, outcome, traceID string) {
	if p.auditSink == nil {
		return
	}
	p.auditSink.Record(audit.AuditEvent{TraceID: traceID, AccountID: accountID,
		Action: action, Outcome: outcome, At: p.clock()})
}

// ReloadFallbacks 热重载降级链(契约「热切换」:不重启,路由表原子替换)。
func (p *Provider) ReloadFallbacks(fallbacks []router.RealNameProvider) {
	p.rt.Reload(router.Config{Primary: p.core, Fallbacks: fallbacks})
}

// Chain 当前链供应商名(诊断)。
func (p *Provider) Chain() []string { return p.rt.Snapshot() }

// --- providers.Handler ---

func (p *Provider) Name() string                     { return p.name }
func (p *Provider) Capability() providers.Capability { return providers.CapRealname }

// HealthCheck 未配核验服务 = 未就绪(路由分发直接跳过,不静默半可用)。
func (p *Provider) HealthCheck(_ context.Context) error {
	if p.core.cloud == nil {
		return errors.New("wardenalt: no cloud verifier configured")
	}
	return nil
}

func (p *Provider) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// S2S 端点不套玩家认证(同 warden):凭证自校验,玩家 Bearer 不是合法凭证。
	if strings.TrimPrefix(req.URL.Path, prefixRealname) == "playtime-report" {
		p.route(resp, req)
		return
	}
	authMW := p.authMW
	if authMW == nil {
		authMW = func(next http.Handler) http.Handler { // fail-closed
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				aggregation.WriteError(w, middleware.TraceID(r.Context()),
					aggregation.CodeUnauthenticated, "not authenticated")
			})
		}
	}
	authMW(http.HandlerFunc(p.route)).ServeHTTP(resp, req)
}

func (p *Provider) route(resp http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, prefixRealname)
	switch {
	case path == "verify" && req.Method == http.MethodPost:
		p.handleVerify(resp, req)
	case path == "status" && req.Method == http.MethodGet:
		p.handleStatus(resp, req)
	case path == "curfew" && req.Method == http.MethodGet:
		p.handleCurfew(resp, req)
	case path == "charge-check" && http.MethodPost == req.Method:
		p.handleChargeCheck(resp, req)
	case path == "playtime-report" && req.Method == http.MethodPost:
		p.handlePlaytimeReport(resp, req)
	default:
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeNotFound, "no route for "+req.URL.Path)
	}
}

// --- 玩家侧端点 ---

func (p *Provider) handleVerify(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		Name     string `json:"name"`
		IDNumber string `json:"idNumber"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		p.writeInvalid(resp, req, "invalid json body")
		return
	}
	// 入参形状校验挡在边缘(400)。与 warden 的口径差异(异构点):证件号格式
	// 不在本地校验——18 位大陆身份证是自建库的比对口径,托管核验的格式政策归
	// 核验服务(护照/港澳台证件等由服务端判定),本地只要求非空且有界。
	if body.Name == "" || len([]rune(body.Name)) > 30 {
		p.writeInvalid(resp, req, "name required (1-30 chars)")
		return
	}
	if body.IDNumber == "" || len([]rune(body.IDNumber)) > 64 {
		p.writeInvalid(resp, req, "idNumber required (1-64 chars)")
		return
	}
	out := p.rt.Verify(id.AccountID, router.IdentityInput{Name: body.Name, IDNumber: body.IDNumber})
	p.emitAudit(audit.AuditActionVerify, id.AccountID, out.State, middleware.TraceID(req.Context()))
	d := statusDTO{State: out.State}
	if out.State == stateVerified {
		d.IsMinor = &out.IsMinor
	}
	aggregation.WriteData(resp, d)
}

func (p *Provider) handleStatus(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	out := p.rt.Query(id.AccountID)
	d := statusDTO{State: out.State}
	if out.HasVerified {
		at := out.VerifiedAt.UTC().Format("2006-01-02T15:04:05.000Z")
		d.VerifiedAt = &at
		if out.State == stateVerified {
			d.IsMinor = &out.IsMinor
		}
	}
	aggregation.WriteData(resp, d)
}

func (p *Provider) handleCurfew(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	out := p.rt.Curfew(id.AccountID, p.clock())
	outcome := audit.AuditOutcomeDenied
	if out.Playable {
		outcome = audit.AuditOutcomePlayable
	}
	p.emitAudit(audit.AuditActionCurfew, id.AccountID, outcome, middleware.TraceID(req.Context()))
	d := curfewDTO{Playable: out.Playable}
	if out.HasNextWindow {
		at := out.NextWindowAt.UTC().Format("2006-01-02T15:04:05.000Z")
		d.NextWindowAt = &at
	}
	aggregation.WriteData(resp, d)
}

func (p *Provider) handleChargeCheck(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		AmountCents int64 `json:"amountCents"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.AmountCents <= 0 {
		p.writeInvalid(resp, req, "amountCents must be a positive integer")
		return
	}
	out := p.rt.ChargeCheck(id.AccountID, int(body.AmountCents))
	outcome := audit.AuditOutcomeDenied
	if out.Allowed {
		outcome = audit.AuditOutcomeAllowed
	}
	p.emitAudit(audit.AuditActionChargeCheck, id.AccountID, outcome, middleware.TraceID(req.Context()))
	d := chargeDTO{Allowed: out.Allowed}
	if out.SingleLimitCents > 0 {
		d.SingleLimitCents = &out.SingleLimitCents
	}
	if out.MonthlyLimitCents > 0 {
		d.MonthlyLimitCents = &out.MonthlyLimitCents
	}
	if out.MonthlyUsedCents > 0 {
		d.MonthlyUsedCents = &out.MonthlyUsedCents
	}
	aggregation.WriteData(resp, d)
}

// --- S2S 端点(F30:防沉迷数据上报,接入方服务端专用,SDK 不得调用)---

func (p *Provider) handlePlaytimeReport(resp http.ResponseWriter, req *http.Request) {
	if p.s2s == "" || req.Header.Get("Authorization") != "Bearer "+p.s2s {
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeUnauthenticated, "s2s token required")
		return
	}
	var body struct {
		AccountID         string `json:"accountId"`
		Date              string `json:"date"`
		PlayMinutes       int64  `json:"playMinutes"`
		ChargeAmountCents int64  `json:"chargeAmountCents"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.AccountID == "" ||
		!datePattern(body.Date) || body.PlayMinutes < 0 || body.ChargeAmountCents < 0 {
		p.writeInvalid(resp, req, "accountId/date/playMinutes/chargeAmountCents required")
		return
	}
	p.core.report(body.AccountID, body.Date, int(body.PlayMinutes), int(body.ChargeAmountCents))
	p.emitAudit(audit.AuditActionPlaytimeReport, body.AccountID, audit.AuditOutcomeOK, middleware.TraceID(req.Context()))
	aggregation.WriteData(resp, struct{}{})
}

func datePattern(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return false
	}
	return true
}

// --- wire DTO(与 warden 同形,一致性 e2e 的同断言基础)---

type statusDTO struct {
	State      string  `json:"state"`
	IsMinor    *bool   `json:"isMinor,omitempty"`
	VerifiedAt *string `json:"verifiedAt,omitempty"`
}

type curfewDTO struct {
	Playable     bool    `json:"playable"`
	NextWindowAt *string `json:"nextWindowAt,omitempty"`
}

type chargeDTO struct {
	Allowed           bool `json:"allowed"`
	SingleLimitCents  *int `json:"singleLimitCents,omitempty"`
	MonthlyLimitCents *int `json:"monthlyLimitCents,omitempty"`
	MonthlyUsedCents  *int `json:"monthlyUsedCents,omitempty"`
}

func (p *Provider) writeUnauthenticated(resp http.ResponseWriter, req *http.Request) {
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeUnauthenticated, "not authenticated")
}

func (p *Provider) writeInvalid(resp http.ResponseWriter, req *http.Request, msg string) {
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// --- cloudCore:托管核验(router.RealNameProvider primary)---

// bandRecord 判定记录:只存服务判定结果(state/isMinor/年龄段),零身份派生
// 数据——与 warden 的指纹口径互为异构面(本实现连出生段都不落地)。
type bandRecord struct {
	state      string
	isMinor    bool
	band       string
	verifiedAt time.Time
}

type playKey struct {
	account string
	date    string // YYYY-MM-DD(幂等键 accountId+date,同日重报取最新)
}

type playReport struct {
	playMinutes       int
	chargeAmountCents int
}

type cloudCore struct {
	cloud   CloudVerifier
	clock   func() time.Time
	windows []Window
	limits  ChargeLimits

	mu      sync.Mutex
	records map[string]*bandRecord
	plays   map[playKey]playReport
}

var (
	errInvalid      = errors.New("wardenalt: invalid input")
	errCloudMissing = errors.New("wardenalt: no cloud verifier")
	errCloudVerdict = errors.New("wardenalt: invalid verdict")
	errCloudUnavail = errors.New("wardenalt: cloud verifier unavailable")
)

func (c *cloudCore) Name() string { return DefaultName }

// Verify 判定全部来自核验服务:幂等(已 VERIFIED 不再调服务,契约与治理接口
// 双重要求);服务调用失败/判定非法 = 调用失败(进治理熔断,fallback 接管);
// REJECTED 是有效业务结果,照常落库,重验允许(服务判定可变)。
func (c *cloudCore) Verify(accountID string, in router.IdentityInput) (router.VerifyOutcome, error) {
	if c.cloud == nil {
		return router.VerifyOutcome{}, errCloudMissing
	}
	if in.Name == "" || len([]rune(in.Name)) > 30 || in.IDNumber == "" || len([]rune(in.IDNumber)) > 64 {
		return router.VerifyOutcome{}, errInvalid
	}
	c.mu.Lock()
	if rec := c.records[accountID]; rec != nil && rec.state == stateVerified {
		out := router.VerifyOutcome{State: rec.state, IsMinor: rec.isMinor}
		c.mu.Unlock()
		return out, nil // 幂等:已通过的账号重复提交返回既有判定(不换人重验)
	}
	c.mu.Unlock()
	v, err := c.cloud.Verify(accountID, in.Name, in.IDNumber)
	if err != nil {
		return router.VerifyOutcome{}, errCloudUnavail // 调用失败语义(熔断计数)
	}
	if !validVerdict(v) {
		return router.VerifyOutcome{}, errCloudVerdict
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records[accountID] = &bandRecord{state: v.State, isMinor: v.IsMinor,
		band: v.AgeBand, verifiedAt: c.clock()}
	return router.VerifyOutcome{State: v.State, IsMinor: v.IsMinor}, nil
}

func (c *cloudCore) Query(accountID string) (router.StatusOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := c.records[accountID]
	if rec == nil {
		return router.StatusOutcome{State: stateUnverified}, nil
	}
	return router.StatusOutcome{State: rec.state, IsMinor: rec.isMinor,
		VerifiedAt: rec.verifiedAt, HasVerified: rec.state == stateVerified}, nil
}

func (c *cloudCore) Curfew(accountID string, now time.Time) (router.CurfewOutcome, error) {
	rec := c.lookup(accountID)
	if rec == nil || rec.state != stateVerified || !rec.isMinor {
		return router.CurfewOutcome{Playable: true}, nil // 成年/未判定:恒可玩
	}
	windows := c.sortedWindows()
	for _, win := range windows {
		if now.Weekday() == win.Weekday &&
			now.Hour() >= win.StartHour && now.Hour() < win.EndHour {
			return router.CurfewOutcome{Playable: true}, nil
		}
	}
	for i := 1; i <= 8; i++ {
		next := now.AddDate(0, 0, i)
		for _, win := range windows {
			if next.Weekday() == win.Weekday {
				at := time.Date(next.Year(), next.Month(), next.Day(),
					win.StartHour, 0, 0, 0, next.Location())
				return router.CurfewOutcome{Playable: false,
					NextWindowAt: at, HasNextWindow: true}, nil
			}
		}
	}
	return router.CurfewOutcome{Playable: false}, nil
}

// ChargeCheck 限额按服务下发的年龄段判定(非本地解析):未成年人年龄段缺失
// = 保守禁充(不通过也不放量,F29 安全面)。
func (c *cloudCore) ChargeCheck(accountID string, amountCents int) (router.ChargeOutcome, error) {
	if amountCents <= 0 {
		return router.ChargeOutcome{}, errInvalid
	}
	rec := c.lookup(accountID)
	if rec == nil || rec.state != stateVerified {
		return router.ChargeOutcome{Allowed: false}, nil // 实名前置
	}
	if !rec.isMinor {
		return router.ChargeOutcome{Allowed: true}, nil // 成年:无限额
	}
	single, monthly := c.limitsFor(rec.band)
	if single <= 0 { // <8 岁禁充;年龄段未下发 = 保守禁充
		return router.ChargeOutcome{Allowed: false}, nil
	}
	used := c.monthlyUsed(accountID)
	if amountCents > single || used+amountCents > monthly {
		return router.ChargeOutcome{Allowed: false,
			SingleLimitCents: single, MonthlyLimitCents: monthly, MonthlyUsedCents: used}, nil
	}
	return router.ChargeOutcome{Allowed: true,
		SingleLimitCents: single, MonthlyLimitCents: monthly, MonthlyUsedCents: used}, nil
}

func (c *cloudCore) HealthCheck() bool { return c.cloud != nil }

// report S2S 上报落库(幂等:同账号同日取最新;金额累计按报告口径)。
func (c *cloudCore) report(accountID, date string, playMinutes, chargeCents int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.plays[playKey{accountID, date}] = playReport{playMinutes: playMinutes, chargeAmountCents: chargeCents}
}

func (c *cloudCore) lookup(accountID string) *bandRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records[accountID]
}

func (c *cloudCore) monthlyUsed(accountID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	month := c.clock().Format("2006-01")
	total := 0
	for k, v := range c.plays {
		if k.account == accountID && strings.HasPrefix(k.date, month) {
			total += v.chargeAmountCents
		}
	}
	return total
}

func (c *cloudCore) sortedWindows() []Window {
	out := append([]Window(nil), c.windows...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weekday != out[j].Weekday {
			return out[i].Weekday < out[j].Weekday
		}
		return out[i].StartHour < out[j].StartHour
	})
	return out
}

// AgeBand 服务下发口径:VERIFIED 时有效;未成年缺失 = 保守禁充。
const (
	BandUnder8 = "<8"
	Band8To16  = "8-16"
	Band16To18 = "16-18"
	BandAdult  = "18+"
)

func (c *cloudCore) limitsFor(band string) (single, monthly int) {
	l := c.limits
	switch band {
	case Band8To16:
		return l.Single8To16Cents, l.Monthly8To16Cents
	case Band16To18:
		return l.Single16To18Cents, l.Monthly16To18Cents
	default:
		return 0, 0
	}
}

var _ router.RealNameProvider = (*cloudCore)(nil)
var _ providers.Handler = (*Provider)(nil)
