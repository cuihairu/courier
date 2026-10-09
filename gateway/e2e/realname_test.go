// 批次 7(M2 后段)实名验收:默认关闭 501;开启后游客 → 实名 → 状态/时段/
// 额度/S2S 上报全链;降级链经 HTTP 生效(主挂走备,全挂 PENDING_REVIEW 安全态);
// 路由表热重载不中断服务。对齐 docs/contract/realname.md Frozen v1。
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/router"
	"github.com/cuihairu/courier/gateway/providers/warden"
	"github.com/cuihairu/courier/gateway/routing"
)

// 2026-10-08 周四 15:00(未成年不可玩,下一窗口周五 20:00)。
var rnClock = func() time.Time { return time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC) }

const rnS2SToken = "e2e-s2s-secret"

// rnHarness 实名场景装配:mode 决定 realname 能力形态。
type rnHarness struct {
	srv    *httptest.Server
	warden *warden.Warden
	fake   *fakeRealName
}

const (
	rnDisabled = iota // 不配置 → 501 降级
	rnWarden          // warden(自建核验)
	rnFake            // fakeRealName(治理链演示:可整体替换的 Provider)
)

func newRealNameHarness(t *testing.T, mode int) *rnHarness {
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
	case rnWarden:
		h.warden = warden.New(warden.Options{
			RequireAuth: reqAuth, Clock: rnClock, S2SToken: rnS2SToken})
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
	// warden 核验记录按 AccountID 键;guest 登录响应即含 accountId——重登一次拿。
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
