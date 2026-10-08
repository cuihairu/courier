// Package warden 实名域默认 Provider(M2 后段,契约 docs/contract/realname.md):
// 自建核验——本地库比对,数据不出自建环境(首个实现;阿里云/腾讯云慧眼/易盾/
// Webhook 按接入方需要另行适配同一 router.RealNameProvider 形状)。
//
// 治理:本包把核验核心(core)作为 primary 挂进 router.Router;Fallbacks 经
// Options 注入(其他供应商实现),热重载经 ReloadFallbacks(路由表原子替换)。
//
// 红线(realname.md「字段脱敏与数据红线」):姓名/证件号只在调用栈内存活;
// 持久化只存 SHA-256 指纹(比对用),不存明文;日志与 trace 禁明文。
package warden

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/router"
)

// DefaultName 注册名(main.go 默认路由表不指向它——实名默认关闭,接入方显式开启)。
const DefaultName = "warden"

const prefixRealname = "/v1/realname/"

// 实名状态(契约 realname.md)。
const (
	stateUnverified    = "UNVERIFIED"
	statePendingReview = "PENDING_REVIEW"
	stateVerified      = "VERIFIED"
	stateRejected      = "REJECTED"
)

// Window 可玩时段窗口(F28:默认周五/六/日 20:00–21:00,未成年人)。
type Window struct {
	Weekday   time.Weekday
	StartHour int
	EndHour   int
}

// ChargeLimits 分年龄段充值限额(分;契约 F29 口径:8–16 岁单次 ≤50/月 ≤200 元,
// 16–18 岁单次 ≤100/月 ≤400 元;<8 岁禁充)。
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
	// Fallbacks 降级链(依序;primary 恒为本核验核心)。
	Fallbacks []router.RealNameProvider
	// Breaker 熔断参数(零值 = 契约默认 5 次/30 调用)。
	Breaker router.BreakerConfig
	// CurfewWindows 未成年可玩窗口(缺省 = 周五/六/日 20:00–21:00)。
	CurfewWindows []Window
	// Limits 分龄限额(缺省 = F29 监管口径)。
	Limits ChargeLimits
	// S2SToken 接入方服务端凭证(playtime-report 专用);空 = S2S 端点 fail-closed 401。
	S2SToken string
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

// Warden 实名域 Provider:HTTP 投影(/v1/realname/*)+ 治理路由(core 为主)。
type Warden struct {
	name   string
	clock  func() time.Time
	authMW func(http.Handler) http.Handler
	rt     *router.Router
	core   *core
	s2s    string
}

// New 装配;Fallbacks 注入即降级链(core 恒为 primary)。
func New(opts Options) *Warden {
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
	c := &core{clock: opts.Clock, windows: opts.CurfewWindows, limits: opts.Limits,
		records: make(map[string]*rnRecord), plays: make(map[playKey]playReport)}
	rt := router.New(router.Config{Primary: c, Fallbacks: opts.Fallbacks, Breaker: opts.Breaker})
	return &Warden{name: opts.Name, clock: opts.Clock,
		authMW: opts.RequireAuth, rt: rt, core: c, s2s: opts.S2SToken}
}

// ReloadFallbacks 热重载降级链(契约「热切换」:不重启,路由表原子替换)。
func (w *Warden) ReloadFallbacks(fallbacks []router.RealNameProvider) {
	w.rt.Reload(router.Config{Primary: w.core, Fallbacks: fallbacks})
}

// Chain 当前链供应商名(诊断)。
func (w *Warden) Chain() []string { return w.rt.Snapshot() }

// --- providers.Handler ---

func (w *Warden) Name() string                     { return w.name }
func (w *Warden) Capability() providers.Capability { return providers.CapRealname }
func (w *Warden) HealthCheck(_ context.Context) error {
	if len(w.rt.Snapshot()) == 0 {
		return errors.New("warden: empty chain")
	}
	return nil
}

func (w *Warden) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	// S2S 端点不套玩家认证:playtime-report 是接入方服务端上报,
	// 凭证自校验(配置 token);玩家 Bearer 在此不是合法凭证。
	if strings.TrimPrefix(req.URL.Path, prefixRealname) == "playtime-report" {
		w.route(resp, req)
		return
	}
	authMW := w.authMW
	if authMW == nil {
		authMW = func(next http.Handler) http.Handler { // fail-closed
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				aggregation.WriteError(w, middleware.TraceID(r.Context()),
					aggregation.CodeUnauthenticated, "not authenticated")
			})
		}
	}
	authMW(http.HandlerFunc(w.route)).ServeHTTP(resp, req)
}

func (w *Warden) route(resp http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, prefixRealname)
	switch {
	case path == "verify" && req.Method == http.MethodPost:
		w.handleVerify(resp, req)
	case path == "status" && req.Method == http.MethodGet:
		w.handleStatus(resp, req)
	case path == "curfew" && req.Method == http.MethodGet:
		w.handleCurfew(resp, req)
	case path == "charge-check" && req.Method == http.MethodPost:
		w.handleChargeCheck(resp, req)
	case path == "playtime-report" && req.Method == http.MethodPost:
		w.handlePlaytimeReport(resp, req)
	default:
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeNotFound, "no route for "+req.URL.Path)
	}
}

// --- 玩家侧端点 ---

func (w *Warden) handleVerify(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		Name     string `json:"name"`
		IDNumber string `json:"idNumber"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		w.writeInvalid(resp, req, "invalid json body")
		return
	}
	// 入参形状校验挡在边缘(400):核验链只接业务,调用失败语义留给熔断。
	if body.Name == "" || len([]rune(body.Name)) > 30 {
		w.writeInvalid(resp, req, "name required (1-30 chars)")
		return
	}
	if _, err := parseIDNumber(body.IDNumber); err != nil {
		w.writeInvalid(resp, req, "idNumber malformed (18-digit CN resident ID)")
		return
	}
	out := w.rt.Verify(id.AccountID, router.IdentityInput{Name: body.Name, IDNumber: body.IDNumber})
	d := statusDTO{State: out.State}
	if out.State == stateVerified {
		d.IsMinor = &out.IsMinor
	}
	aggregation.WriteData(resp, d)
}

func (w *Warden) handleStatus(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	out := w.rt.Query(id.AccountID)
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

func (w *Warden) handleCurfew(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	out := w.rt.Curfew(id.AccountID, w.clock())
	d := curfewDTO{Playable: out.Playable}
	if out.HasNextWindow {
		at := out.NextWindowAt.UTC().Format("2006-01-02T15:04:05.000Z")
		d.NextWindowAt = &at
	}
	aggregation.WriteData(resp, d)
}

func (w *Warden) handleChargeCheck(resp http.ResponseWriter, req *http.Request) {
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		AmountCents int64 `json:"amountCents"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.AmountCents <= 0 {
		w.writeInvalid(resp, req, "amountCents must be a positive integer")
		return
	}
	out := w.rt.ChargeCheck(id.AccountID, int(body.AmountCents))
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

func (w *Warden) handlePlaytimeReport(resp http.ResponseWriter, req *http.Request) {
	// 玩家 Bearer 不是合法凭证:本端点只认配置的接入方服务端 token。
	if w.s2s == "" || req.Header.Get("Authorization") != "Bearer "+w.s2s {
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
		w.writeInvalid(resp, req, "accountId/date/playMinutes/chargeAmountCents required")
		return
	}
	w.core.report(body.AccountID, body.Date, int(body.PlayMinutes), int(body.ChargeAmountCents))
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

// --- wire DTO ---

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

func (w *Warden) writeUnauthenticated(resp http.ResponseWriter, req *http.Request) {
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeUnauthenticated, "not authenticated")
}

func (w *Warden) writeInvalid(resp http.ResponseWriter, req *http.Request, msg string) {
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// --- core:自建核验逻辑(router.RealNameProvider primary)---

// rnRecord 账号实名记录(指纹比对,不存明文——红线;出生段是脱敏口径
// 保留的生日信息,供年龄分档,非证件号本身)。
type rnRecord struct {
	fingerprint string // SHA-256(name + "|" + idNumber)
	state       string // UNVERIFIED/PENDING_REVIEW/VERIFIED/REJECTED
	isMinor     bool
	birth       time.Time
	verifiedAt  time.Time
}

type playKey struct {
	account string
	date    string // YYYY-MM-DD(幂等键 accountId+date,同日重报取最新)
}

type playReport struct {
	playMinutes       int
	chargeAmountCents int
}

type core struct {
	clock   func() time.Time
	windows []Window
	limits  ChargeLimits

	mu      sync.Mutex
	records map[string]*rnRecord
	plays   map[playKey]playReport
}

// errInvalid 入参非法(HTTP 层已挡格式;此为核验核心二次防线)。
var errInvalid = errors.New("warden: invalid identity")

func (c *core) Name() string { return DefaultName }

func (c *core) Verify(accountID string, in router.IdentityInput) (router.VerifyOutcome, error) {
	birth, err := parseIDNumber(in.IDNumber)
	if err != nil || in.Name == "" || len([]rune(in.Name)) > 30 {
		return router.VerifyOutcome{}, errInvalid
	}
	minor := ageAt(birth, c.clock()) < 18 // 未成年判定只认服务端口径
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := c.records[accountID]
	if rec != nil && rec.state == stateVerified {
		// 幂等:已通过的账号重复提交返回既有判定,不重算(也不允许「换人重验」)。
		return router.VerifyOutcome{State: rec.state, IsMinor: rec.isMinor}, nil
	}
	c.records[accountID] = &rnRecord{
		fingerprint: fingerprint(in),
		state:       stateVerified, // 自建核验:格式合法即库内通过
		isMinor:     minor,
		birth:       birth,
		verifiedAt:  c.clock(),
	}
	return router.VerifyOutcome{State: stateVerified, IsMinor: minor}, nil
}

func (c *core) Query(accountID string) (router.StatusOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := c.records[accountID]
	if rec == nil {
		return router.StatusOutcome{State: stateUnverified}, nil
	}
	return router.StatusOutcome{State: rec.state, IsMinor: rec.isMinor,
		VerifiedAt: rec.verifiedAt, HasVerified: rec.state == stateVerified}, nil
}

func (c *core) Curfew(accountID string, now time.Time) (router.CurfewOutcome, error) {
	rec := c.lookup(accountID)
	if rec == nil || rec.state != stateVerified || !rec.isMinor {
		return router.CurfewOutcome{Playable: true}, nil // 成年/未判定:恒可玩
	}
	windows := c.sortedWindows()
	// 当前时刻在窗口内 → 可玩。
	for _, win := range windows {
		if now.Weekday() == win.Weekday &&
			now.Hour() >= win.StartHour && now.Hour() < win.EndHour {
			return router.CurfewOutcome{Playable: true}, nil
		}
	}
	// 否则找下一窗口起点(未来 8 天内)。
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

func (c *core) ChargeCheck(accountID string, amountCents int) (router.ChargeOutcome, error) {
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
	age := ageAt(rec.birth, c.clock())
	single, monthly := c.limitsFor(age)
	if single <= 0 { // 8 岁以下禁充
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

func (c *core) HealthCheck() bool { return true }

// report S2S 上报落库(幂等:同账号同日取最新;金额累计按报告口径)。
func (c *core) report(accountID, date string, playMinutes, chargeCents int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.plays[playKey{accountID, date}] = playReport{playMinutes: playMinutes, chargeAmountCents: chargeCents}
}

func (c *core) lookup(accountID string) *rnRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.records[accountID]
}

func (c *core) monthlyUsed(accountID string) int {
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

func (c *core) sortedWindows() []Window {
	out := append([]Window(nil), c.windows...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weekday != out[j].Weekday {
			return out[i].Weekday < out[j].Weekday
		}
		return out[i].StartHour < out[j].StartHour
	})
	return out
}

func (c *core) limitsFor(age int) (single, monthly int) {
	l := c.limits
	switch {
	case age < 8:
		return 0, 0 // 禁充
	case age < 16:
		return l.Single8To16Cents, l.Monthly8To16Cents
	default:
		return l.Single16To18Cents, l.Monthly16To18Cents
	}
}

// fingerprint 身份指纹(SHA-256;红线:不存明文)。
func fingerprint(in router.IdentityInput) string {
	h := sha256.Sum256([]byte(in.Name + "|" + in.IDNumber))
	return hex.EncodeToString(h[:])
}

// parseIDNumber 大陆居民身份证号(18 位):提取出生段;不做 GB 11643 校验位
// 验算(自建核验按库内口径;格式非法 → err)。
func parseIDNumber(s string) (time.Time, error) {
	if len(s) != 18 {
		return time.Time{}, errInvalid
	}
	for i := 0; i < 17; i++ {
		if s[i] < '0' || s[i] > '9' {
			return time.Time{}, errInvalid
		}
	}
	last := s[17]
	if (last < '0' || last > '9') && last != 'X' && last != 'x' {
		return time.Time{}, errInvalid
	}
	birth, err := time.Parse("20060102", s[6:14])
	if err != nil {
		return time.Time{}, errInvalid
	}
	if birth.Year() < 1900 || birth.After(time.Now().AddDate(0, 0, 1)) {
		return time.Time{}, errInvalid
	}
	return birth, nil
}

// ageAt 周岁。
func ageAt(birth, now time.Time) int {
	age := now.Year() - birth.Year()
	if now.Month() < birth.Month() ||
		(now.Month() == birth.Month() && now.Day() < birth.Day()) {
		age--
	}
	return age
}

var _ router.RealNameProvider = (*core)(nil)
var _ providers.Handler = (*Warden)(nil)
