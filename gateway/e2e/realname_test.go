// 批次 7(M2 后段)实名验收:默认关闭 501;开启后游客 → 实名 → 状态/时段/
// 额度/S2S 上报全链;降级链经 HTTP 生效(主挂走备,全挂 PENDING_REVIEW 安全态);
// 路由表热重载不中断服务。对齐 docs/contract/realname.md Frozen v1。
package e2e

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/audit"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/router"
	"github.com/cuihairu/courier/gateway/providers/warden"
	"github.com/cuihairu/courier/gateway/providers/wardenalt"
	"github.com/cuihairu/courier/gateway/routing"
)

// 2026-10-08 周四 15:00(未成年不可玩,下一窗口周五 20:00)。
var rnClock = func() time.Time { return time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC) }

const rnS2SToken = "e2e-s2s-secret"

// rnHarness 实名场景装配:mode 决定 realname 能力形态。
type rnHarness struct {
	srv       *httptest.Server
	warden    *warden.Warden
	wardenAlt *wardenalt.Provider
	fake      *fakeRealName
}

const (
	rnDisabled  = iota // 不配置 → 501 降级
	rnWarden           // warden(自建核验)
	rnFake             // fakeRealName(治理链演示:可整体替换的 Provider)
	rnWardenAlt        // warden-alt(托管核验,批次 64 第二实现)
	// consent 开启模式(realname.md v1.1,F31):同上两实现 + Consent 配置
	// (nil = 能力关闭 501)。一致性场景见 realname_conformance_test.go。
	rnWardenConsent
	rnWardenAltConsent
)

// rnConsentCfg consent 测试夹具(纯缺省阈值:数字同意 13 / 成年 18 / VPC 72h;
// 契约 v1.1「法域中立」——缺省口径即验收面,minimumAge 分档由两实现单测覆盖)。
func rnConsentCfg(mode int) *warden.ConsentConfig {
	if mode == rnWardenConsent {
		return &warden.ConsentConfig{}
	}
	return nil
}

func rnConsentAltCfg(mode int) *wardenalt.ConsentConfig {
	if mode == rnWardenAltConsent {
		return &wardenalt.ConsentConfig{}
	}
	return nil
}

func newRealNameHarness(t *testing.T, mode int, sinks ...audit.AuditSink) *rnHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{providers.CapIdentity: {Primary: account.DefaultName}}
	h := &rnHarness{}
	switch mode {
	case rnWarden, rnWardenConsent:
		var auditSink audit.AuditSink
		if len(sinks) > 0 {
			auditSink = sinks[0]
		}
		h.warden = warden.New(warden.Options{
			RequireAuth: reqAuth, Clock: rnClock, S2SToken: rnS2SToken, AuditSink: auditSink,
			Consent: rnConsentCfg(mode)})
		if err := reg.Register(h.warden); err != nil {
			t.Fatalf("register warden: %v", err)
		}
		cfg[providers.CapRealname] = providers.CapabilityConfig{Primary: warden.DefaultName}
	case rnFake:
		h.fake = newFakeRealName(reqAuth)
		if err := reg.Register(h.fake); err != nil {
			t.Fatalf("register fake: %v", err)
		}
		cfg[providers.CapRealname] = providers.CapabilityConfig{Primary: fakeRealNameName}
	case rnWardenAlt, rnWardenAltConsent:
		// 托管核验脚本:与 warden 本地解析同判定(成人/未成年),判定源不同
		// (脚本注入 vs 证件号解析)——一致性 e2e 的同断言基础;未登记 = 拒绝。
		var auditSink audit.AuditSink
		if len(sinks) > 0 {
			auditSink = sinks[0]
		}
		h.wardenAlt = wardenalt.New(wardenalt.Options{
			RequireAuth: reqAuth, Clock: rnClock, S2SToken: rnS2SToken,
			AuditSink: auditSink, Consent: rnConsentAltCfg(mode),
			Cloud: wardenalt.VerifierFunc(func(_, _ string, idNumber string) (wardenalt.Verdict, error) {
				switch idNumber {
				case "110101199001011234":
					return wardenalt.Verdict{State: "VERIFIED", AgeBand: wardenalt.BandAdult}, nil
				case "110101201506201234":
					return wardenalt.Verdict{State: "VERIFIED", IsMinor: true, AgeBand: wardenalt.Band8To16}, nil
				}
				return wardenalt.Verdict{State: "REJECTED"}, nil
			}),
		})
		if err := reg.Register(h.wardenAlt); err != nil {
			t.Fatalf("register wardenalt: %v", err)
		}
		cfg[providers.CapRealname] = providers.CapabilityConfig{Primary: wardenalt.DefaultName}
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	h.srv = httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(h.srv.Close)
	return h
}

// login 游客登录取 token(按设备幂等,同 harness 内重复登录同账号)。
func (h *rnHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"e2e-dev","platform":"linux"}`))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: status=%d err=%v", resp.StatusCode, err)
	}
	return out.Data.AccessToken
}

// call 带玩家凭证的 JSON 请求;返回状态码、错误码(无错误为空)与 data 反序列化目标。
func (h *rnHarness) call(t *testing.T, method, path, token, body string, out any) (int, string) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, reader)
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	errCode := ""
	if env.Error != nil {
		errCode = env.Error.Code
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			t.Fatalf("unmarshal %s data: %v", path, err)
		}
	}
	return resp.StatusCode, errCode
}

func TestRealName_DisabledByDefault_501(t *testing.T) {
	h := newRealNameHarness(t, rnDisabled)
	// 匿名即断:能力关闭是路由级降级,不进认证/业务。
	code, errCode := h.call(t, http.MethodPost, "/v1/realname/verify", "",
		`{"name":"张三","idNumber":"110101199001011234"}`, nil)
	if code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501(默认关闭)", code)
	}
	if errCode != "COMMON_CAPABILITY_DISABLED" {
		t.Errorf("code = %q, want COMMON_CAPABILITY_DISABLED", errCode)
	}
}

func TestRealName_VerifyStatusCurfewCharge_S2SAccumulation(t *testing.T) {
	h := newRealNameHarness(t, rnWarden)
	token := h.login(t)

	// 未提交:UNVERIFIED。
	var st struct {
		State string `json:"state"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", &st); code != 200 ||
		st.State != "UNVERIFIED" {
		t.Fatalf("initial status = %d %q", code, st.State)
	}

	// 成年提交 → VERIFIED, isMinor=false。
	var verify struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 {
		t.Fatalf("verify status = %d", code)
	}
	if verify.State != "VERIFIED" || verify.IsMinor == nil || *verify.IsMinor {
		t.Fatalf("verify = %+v", verify)
	}

	// 成年:恒可玩、充值无限额。
	var curfew struct {
		Playable bool `json:"playable"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", &curfew); code != 200 ||
		!curfew.Playable {
		t.Fatalf("adult curfew = %d %+v", code, curfew)
	}
	var charge struct {
		Allowed bool `json:"allowed"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":999999}`, &charge); code != 200 || !charge.Allowed {
		t.Fatalf("adult charge = %d %+v", code, charge)
	}
}

func TestRealName_Minor_CurfewBlocked_WithNextWindow(t *testing.T) {
	h := newRealNameHarness(t, rnWarden)
	token := h.login(t)
	var verify struct {
		IsMinor *bool `json:"isMinor"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"李四","idNumber":"110101201506201234"}`, &verify); code != 200 ||
		verify.IsMinor == nil || !*verify.IsMinor {
		t.Fatalf("minor verify = %d %+v", code, verify)
	}

	// 周四 15:00:不可玩,下一窗口周五 20:00(F28)。
	var curfew struct {
		Playable     bool    `json:"playable"`
		NextWindowAt *string `json:"nextWindowAt"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", &curfew); code != 200 {
		t.Fatalf("curfew status = %d", code)
	}
	if curfew.Playable || curfew.NextWindowAt == nil ||
		*curfew.NextWindowAt != "2026-10-09T20:00:00.000Z" {
		t.Fatalf("curfew = %+v, want playable=false nextWindow=周五20:00", curfew)
	}
}

func TestRealName_S2SReport_ChargeAccumulates(t *testing.T) {
	h := newRealNameHarness(t, rnWarden)
	token := h.login(t)
	h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"小明","idNumber":"110101201506201234"}`, nil) // 11 岁

	// 玩家 token 走 S2S:401。
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/playtime-report", token,
		`{"accountId":"x","date":"2026-10-07","playMinutes":1,"chargeAmountCents":0}`, nil); code != 401 {
		t.Fatalf("player token on s2s = %d, want 401", code)
	}
	// 接入方凭证上报月消费 180 元。
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/playtime-report", rnS2SToken,
		`{"accountId":"`+rnAccountID(t, h, token)+`","date":"2026-10-07","playMinutes":90,"chargeAmountCents":18000}`,
		nil); code != 200 {
		t.Fatalf("s2s report = %d, want 200", code)
	}

	// 月累计 180 → 再充 30 元超月上限 200:拒绝并回带额度字段(F29)。
	var charge struct {
		Allowed           bool `json:"allowed"`
		MonthlyUsedCents  *int `json:"monthlyUsedCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":3000}`, &charge); code != 200 || charge.Allowed ||
		charge.MonthlyUsedCents == nil || *charge.MonthlyUsedCents != 18000 ||
		charge.MonthlyLimitCents == nil || *charge.MonthlyLimitCents != 20000 {
		t.Fatalf("charge = %d %+v, want blocked 月已用18000/限20000", code, charge)
	}
}

// rnAccountID 经 identity 会话解析账号 id(S2S 上报键)。
func rnAccountID(t *testing.T, h *rnHarness, token string) string {
	t.Helper()
	// warden 核验记录按 AccountID 键;guest 登录响应 account.id 即账号 ID——重登一次拿。
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"e2e-dev","platform":"linux"}`))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.Account.ID == "" {
		t.Fatalf("account id decode: %v", err)
	}
	return out.Data.Account.ID
}

func TestRealName_FallbackChain_OverHTTP(t *testing.T) {
	h := newRealNameHarness(t, rnFake)
	token := h.login(t)

	// 主挂 → 走备:VERIFIED(降级链对玩家透明)。
	var verify struct {
		State string `json:"state"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 ||
		verify.State != "VERIFIED" {
		t.Fatalf("fallback verify = %d %q", code, verify.State)
	}

	// 热重载:链换成全挂 → 安全态 PENDING_REVIEW(契约:不通过也不拒绝放量)。
	h.fake.reloadAll(rnFail)
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 ||
		verify.State != "PENDING_REVIEW" {
		t.Fatalf("all-down verify = %d %q, want PENDING_REVIEW", code, verify.State)
	}
}

func TestRealName_HotReloadFallbacks_ServiceContinues(t *testing.T) {
	h := newRealNameHarness(t, rnWarden)
	token := h.login(t)

	var verify struct {
		State string `json:"state"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 ||
		verify.State != "VERIFIED" {
		t.Fatalf("before reload = %d %q", code, verify.State)
	}

	// 热重载降级链(路由表原子替换,不重启;契约「热切换」)。
	h.warden.ReloadFallbacks([]router.RealNameProvider{&rnStub{Name_: "aliyun"}})
	if chain := h.warden.Chain(); len(chain) != 2 || chain[1] != "aliyun" {
		t.Fatalf("chain after reload = %v, want [warden aliyun]", chain)
	}
	// 重载后服务不中断:状态查询与核验照常。
	var st struct {
		State string `json:"state"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", &st); code != 200 ||
		st.State != "VERIFIED" {
		t.Fatalf("status after reload = %d %q", code, st.State)
	}
}

// --- 测试用 Provider(fakeRealName:治理链经 HTTP 的演示替换件)---

const fakeRealNameName = "rnfake"

// rnMode fake 供应商行为。
type rnMode int32

const (
	rnOK   rnMode = iota // 健康:VERIFIED
	rnFail               // 调用失败(计熔断)
)

type rnStub struct {
	Name_ string
	Mode  atomic.Int32
}

func (s *rnStub) Name() string { return s.Name_ }
func (s *rnStub) Verify(string, router.IdentityInput) (router.VerifyOutcome, error) {
	if rnMode(s.Mode.Load()) == rnFail {
		return router.VerifyOutcome{}, errRNDown
	}
	return router.VerifyOutcome{State: "VERIFIED"}, nil
}
func (s *rnStub) Query(string) (router.StatusOutcome, error) {
	if rnMode(s.Mode.Load()) == rnFail {
		return router.StatusOutcome{}, errRNDown
	}
	return router.StatusOutcome{State: "VERIFIED", HasVerified: true}, nil
}
func (s *rnStub) Curfew(string, time.Time) (router.CurfewOutcome, error) {
	return router.CurfewOutcome{Playable: true}, nil
}
func (s *rnStub) ChargeCheck(string, int) (router.ChargeOutcome, error) {
	return router.ChargeOutcome{Allowed: true}, nil
}
func (s *rnStub) HealthCheck() bool { return rnMode(s.Mode.Load()) == rnOK }
func (s *rnStub) ConsentStatus(string) (router.ConsentStatus, error) {
	return router.ConsentStatus{State: "PENDING_PARENT"}, nil
}
func (s *rnStub) DeclareAge(string, string) (router.ConsentStatus, error) {
	return router.ConsentStatus{State: "PENDING_PARENT"}, nil
}
func (s *rnStub) RequestParentConsent(string) (router.ConsentRequestOutcome, error) {
	return router.ConsentRequestOutcome{}, nil
}

var errRNDown = errRN{}

type errRN struct{}

func (errRN) Error() string { return "rn provider down" }

// fakeRealName 可整体替换的实名 Provider(演示:接入方可自建同名替换默认供应商)。
type fakeRealName struct {
	auth func(http.Handler) http.Handler
	rt   *router.Router
	pri  *rnStub
	fb   *rnStub
}

func newFakeRealName(reqAuth func(http.Handler) http.Handler) *fakeRealName {
	f := &fakeRealName{auth: reqAuth,
		pri: &rnStub{Name_: "rn-primary"}, fb: &rnStub{Name_: "rn-fallback"}}
	f.pri.Mode.Store(int32(rnFail)) // 主恒挂:验证降级链
	f.rt = router.New(router.Config{Primary: f.pri, Fallbacks: []router.RealNameProvider{f.fb}})
	return f
}

// reloadAll 热重载为全挂链(PENDING_REVIEW 演示)。
func (f *fakeRealName) reloadAll(mode rnMode) {
	f.fb.Mode.Store(int32(mode))
	f.rt.Reload(router.Config{Primary: f.pri, Fallbacks: []router.RealNameProvider{f.fb}})
}

func (f *fakeRealName) Name() string                        { return fakeRealNameName }
func (f *fakeRealName) Capability() providers.Capability    { return providers.CapRealname }
func (f *fakeRealName) HealthCheck(_ context.Context) error { return nil }

func (f *fakeRealName) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.FromContext(r.Context())
		if !ok {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnauthenticated, "not authenticated")
			return
		}
		switch r.URL.Path {
		case "/v1/realname/verify":
			out := f.rt.Verify(id.AccountID, router.IdentityInput{})
			aggregation.WriteData(w, map[string]any{"state": out.State})
		case "/v1/realname/status":
			out := f.rt.Query(id.AccountID)
			aggregation.WriteData(w, map[string]any{"state": out.State})
		default:
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeNotFound, "no route")
		}
	})).ServeHTTP(w, r)
}

// captureAudit 事件捕获(测试装配面;生产 main.go 同源注入,经
// COURIER_REALNAME_AUDIT=log 时为 LogSink)。
type captureAudit struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (s *captureAudit) Record(e audit.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *captureAudit) snapshot() []audit.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]audit.AuditEvent(nil), s.events...)
}

// 批次 62 F33 事件面 e2e:审计事件经完整中间件链(trace → 结构化错误 → 限流 →
// scope)与真实装配面(registry → capability → provider)产出,TraceID 取自链上
// 上下文;身份输入走真实请求体后仍不进事件(realname.md 红线)。单测(audit_test.go)
// 直调 handler,本测补装配路径实证。
func TestRealName_AuditEventsThroughChain_Redaction(t *testing.T) {
	sink := &captureAudit{}
	h := newRealNameHarness(t, rnWarden, sink)
	token := h.login(t)

	var st struct {
		State string `json:"state"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101201506201234"}`, &st); code != 200 || st.State != "VERIFIED" {
		t.Fatalf("verify: %d %v", code, st)
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", nil); code != 200 {
		t.Fatalf("curfew: %d", code)
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":100}`, nil); code != 200 {
		t.Fatalf("charge-check: %d", code)
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/playtime-report", rnS2SToken,
		`{"accountId":"acc_e2e","date":"2026-10-08","playMinutes":30,"chargeAmountCents":0}`, nil); code != 200 {
		t.Fatalf("playtime-report: %d", code)
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", nil); code != 200 {
		t.Fatalf("status: %d", code)
	}

	events := sink.snapshot()
	want := []struct{ action, outcome string }{
		{audit.AuditActionVerify, "VERIFIED"},
		{audit.AuditActionCurfew, audit.AuditOutcomeDenied}, // 周四 15:00 未成年 → 不可玩
		{audit.AuditActionChargeCheck, audit.AuditOutcomeAllowed},
		{audit.AuditActionPlaytimeReport, audit.AuditOutcomeOK},
	}
	if len(events) != len(want) {
		t.Fatalf("事件数 = %d, want %d: %+v", len(events), len(want), events)
	}
	for i, e := range events {
		if e.Action != want[i].action || e.Outcome != want[i].outcome {
			t.Errorf("事件[%d] = %s/%s, want %s/%s", i, e.Action, e.Outcome, want[i].action, want[i].outcome)
		}
		if e.At.IsZero() {
			t.Errorf("事件[%d] At 零值", i)
		}
		if !hex32(e.TraceID) {
			t.Errorf("事件[%d] TraceID = %q, want 32-hex", i, e.TraceID)
		}
	}
	// 玩家侧三事件同账号;S2S 上报账号来自请求体。
	for i := 0; i < 2; i++ {
		if events[i].AccountID == "" || events[i].AccountID != events[0].AccountID {
			t.Errorf("事件[%d] 账号 %q 与事件[0] %q 不一致", i, events[i].AccountID, events[0].AccountID)
		}
	}
	if events[3].AccountID != "acc_e2e" {
		t.Errorf("S2S 事件账号 = %q, want acc_e2e", events[3].AccountID)
	}

	// 红线:身份输入经真实请求体进出后,事件序列化形状仍不含姓名/证件号/指纹。
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, banned := range []string{"张三", "110101201506201234", "idNumber", "fingerprint"} {
		if strings.Contains(string(encoded), banned) {
			t.Errorf("事件序列化含违禁内容 %q: %s", banned, encoded)
		}
	}
}

// 批次 65 F33 直写半边 e2e:HTTPSink 作为生产类型经完整装配面(registry →
// capability → provider → 审计接缝)产出,SIEM 摄取端(此处测试 HTTP 服务器)
// 收到事件 POST 与 Bearer 凭证;网关不持有任何存储。红线同上一测:身份输入
// 经真实请求体进出后仍不进事件体。单测(audit_test.go)直调 sink,本测补装配路径实证。
func TestRealName_AuditHTTPSinkThroughChain(t *testing.T) {
	type siemCapture struct {
		mu       sync.Mutex
		actions  []string
		authHits int
		bodies   []string
	}
	cap_ := &siemCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var e audit.AuditEvent
		if err := json.Unmarshal(body, &e); err != nil {
			t.Errorf("事件非 JSON: %v (%s)", err, body)
		}
		cap_.mu.Lock()
		defer cap_.mu.Unlock()
		cap_.actions = append(cap_.actions, e.Action)
		if r.Header.Get("Authorization") == "Bearer siem_token" {
			cap_.authHits++
		}
		cap_.bodies = append(cap_.bodies, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	sink := audit.HTTPSink{
		Endpoint: srv.URL, Token: "siem_token", Logger: log.New(io.Discard, "", 0),
	}
	h := newRealNameHarness(t, rnWarden, sink)
	token := h.login(t)

	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101201506201234"}`, nil); code != 200 {
		t.Fatalf("verify: %d", code)
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", nil); code != 200 {
		t.Fatalf("status: %d", code)
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":100}`, nil); code != 200 {
		t.Fatalf("charge-check: %d", code)
	}

	cap_.mu.Lock()
	defer cap_.mu.Unlock()
	// status 为纯读,不留痕。
	if len(cap_.actions) != 2 || cap_.actions[0] != audit.AuditActionVerify ||
		cap_.actions[1] != audit.AuditActionChargeCheck {
		t.Fatalf("SIEM 收到事件 = %v", cap_.actions)
	}
	if cap_.authHits != 2 {
		t.Errorf("Bearer 凭证命中 = %d, want 2", cap_.authHits)
	}
	for _, body := range cap_.bodies {
		if strings.Contains(body, "张三") || strings.Contains(body, "idNumber") {
			t.Errorf("SIEM 收到身份输入明文: %s", body)
		}
	}
}

// 批次 66 F33 续:审计事件面是「同契约可换」的行为面之一——两实现(warden 自建
// / warden-alt 托管)经同一共享接缝(gateway/audit)产出同一事件序列。切换
// Provider 不得静默丢合规留痕(否则接入方为合规切到 warden-alt 即失举证面)。
func TestRealName_AuditParity_BothImplementations(t *testing.T) {
	// 未成年链路:verify → curfew(denied) → charge-check(allowed) →
	// playtime-report(ok);status 纯读不留痕。
	auditTrail := func(t *testing.T, mode int) []audit.AuditEvent {
		t.Helper()
		sink := &captureAudit{}
		h := newRealNameHarness(t, mode, sink)
		token := h.login(t)
		if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
			`{"name":"张三","idNumber":"110101201506201234"}`, nil); code != 200 {
			t.Fatalf("verify: %d", code)
		}
		if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", nil); code != 200 {
			t.Fatalf("curfew: %d", code)
		}
		if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
			`{"amountCents":100}`, nil); code != 200 {
			t.Fatalf("charge-check: %d", code)
		}
		if code, _ := h.call(t, http.MethodPost, "/v1/realname/playtime-report", rnS2SToken,
			`{"accountId":"acc_e2e","date":"2026-10-08","playMinutes":30,"chargeAmountCents":0}`, nil); code != 200 {
			t.Fatalf("playtime-report: %d", code)
		}
		if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", nil); code != 200 {
			t.Fatalf("status: %d", code)
		}
		return sink.snapshot()
	}

	want := []struct{ action, outcome string }{
		{audit.AuditActionVerify, "VERIFIED"},
		{audit.AuditActionCurfew, audit.AuditOutcomeDenied},
		{audit.AuditActionChargeCheck, audit.AuditOutcomeAllowed},
		{audit.AuditActionPlaytimeReport, audit.AuditOutcomeOK},
	}
	for _, impl := range []struct {
		name string
		mode int
	}{
		{"warden", rnWarden},
		{"warden-alt", rnWardenAlt},
	} {
		events := auditTrail(t, impl.mode)
		if len(events) != len(want) {
			t.Fatalf("%s 事件数 = %d, want %d: %+v", impl.name, len(events), len(want), events)
		}
		for i, e := range events {
			if e.Action != want[i].action || e.Outcome != want[i].outcome {
				t.Errorf("%s 事件[%d] = %s/%s, want %s/%s",
					impl.name, i, e.Action, e.Outcome, want[i].action, want[i].outcome)
			}
			if e.AccountID == "" {
				t.Errorf("%s 事件[%d] 账号空", impl.name, i)
			}
			if !hex32(e.TraceID) {
				t.Errorf("%s 事件[%d] TraceID = %q, want 32-hex", impl.name, i, e.TraceID)
			}
		}
	}
}

// hex32 32 位小写 hex(primitives.md「TraceID」口径)。
func hex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
