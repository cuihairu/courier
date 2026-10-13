// wardenalt 验收:托管核验语义(判定来自注入服务/幂等/REJECTED 业务结果/
// 判定非法 fail-closed)、年龄段限额(服务下发口径)、S2S 上报幂等、HTTP 投影、
// Webhook 适配 wire、红线(错误信息不含输入原文)。
// 对齐 docs/contract/realname.md Frozen v1;一致性 e2e 见 e2e/realname_conformance_test.go。
package wardenalt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers/router"
)

const (
	adultID = "110101199001011234" // 脚本判定:成年
	minorID = "110101201506201234" // 脚本判定:未成年 8-16
)

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func post(t *testing.T, h http.Handler, path, token, body string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return do(t, h, req)
}

func get(t *testing.T, h http.Handler, path, token string) (int, envelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return do(t, h, req)
}

func do(t *testing.T, h http.Handler, req *http.Request) (int, envelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env
}

// newAuth 测试认证中间件:固定账号 acc_1(模拟 auth.RequireAuth 已完成注入)。
func newAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
			auth.Identity{AccountID: "acc_1", GameID: "game_demo", Env: "prod"})))
	})
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// 2026-10-08 是周四:未成年不可玩,下一窗口周五 20:00。
var nowThursday = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

// scriptedVerdicts 常规脚本:成人/未成年判定(键 = idNumber,零解析)。
func scriptedVerdicts() VerifierFunc {
	return VerifierFunc(func(_, _ string, idNumber string) (Verdict, error) {
		switch idNumber {
		case adultID:
			return Verdict{State: stateVerified, AgeBand: BandAdult}, nil
		case minorID:
			return Verdict{State: stateVerified, IsMinor: true, AgeBand: Band8To16}, nil
		}
		return Verdict{State: stateRejected}, nil
	})
}

func newProvider(cloud CloudVerifier, fallbacks ...router.RealNameProvider) *Provider {
	return New(Options{Clock: fixedClock(nowThursday), RequireAuth: newAuth,
		Cloud: cloud, Fallbacks: fallbacks, S2SToken: "s2s-secret"})
}

func verifyBody(idNumber string) string {
	return `{"name":"张三","idNumber":"` + idNumber + `"}`
}

func decodeStatus(t *testing.T, env envelope) (state string, isMinor *bool) {
	t.Helper()
	var d struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("decode: %v (data=%s)", err, env.Data)
	}
	return d.State, d.IsMinor
}

func TestVerify_ScriptedVerdicts_AdultAndMinor(t *testing.T) {
	t.Run("adult", func(t *testing.T) {
		p := newProvider(scriptedVerdicts())
		code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
		if code != http.StatusOK {
			t.Fatalf("adult verify status = %d", code)
		}
		state, isMinor := decodeStatus(t, env)
		if state != stateVerified || isMinor == nil || *isMinor {
			t.Fatalf("adult = %q isMinor=%v, want VERIFIED false", state, isMinor)
		}
	})
	t.Run("minor", func(t *testing.T) {
		// 独立实例:同账号已 VERIFIED 即幂等锁判定(不换人重验,与 warden 同语义)。
		p := newProvider(scriptedVerdicts())
		code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(minorID))
		if code != http.StatusOK {
			t.Fatalf("minor verify status = %d", code)
		}
		state, isMinor := decodeStatus(t, env)
		if state != stateVerified || isMinor == nil || !*isMinor {
			t.Fatalf("minor = %q isMinor=%v, want VERIFIED true", state, isMinor)
		}
	})
}

// 异构点:证件号格式不在本地校验(自建 18 位口径不复制)——非身份证形态
// 交由核验服务判定。
func TestVerify_IDNumberFormatNotValidatedLocally(t *testing.T) {
	calls := 0
	p := newProvider(VerifierFunc(func(_, _ string, id string) (Verdict, error) {
		calls++
		return Verdict{State: stateVerified, AgeBand: BandAdult}, nil
	}))
	code, env := post(t, p, "/v1/realname/verify", "player", `{"name":"张三","idNumber":"E12345678"}`)
	if code != http.StatusOK {
		t.Fatalf("passport-style verify status = %d", code)
	}
	state, _ := decodeStatus(t, env)
	if state != stateVerified || calls != 1 {
		t.Fatalf("state=%q calls=%d, want VERIFIED 1", state, calls)
	}
}

// REJECTED 是有效业务结果(非调用失败,不计熔断):落库、status 可见、
// charge-check 实名前置拦。
func TestVerify_Rejected_IsBusinessResult(t *testing.T) {
	p := newProvider(VerifierFunc(func(_, _, _ string) (Verdict, error) {
		return Verdict{State: stateRejected}, nil
	}))
	code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	if code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200(业务结果非错误)", code)
	}
	state, isMinor := decodeStatus(t, env)
	if state != stateRejected || isMinor != nil {
		t.Fatalf("verify = %q isMinor=%v, want REJECTED 无 isMinor", state, isMinor)
	}
	_, env = get(t, p, "/v1/realname/status", "player")
	if state, _ = decodeStatus(t, env); state != stateRejected {
		t.Fatalf("status = %q, want REJECTED", state)
	}
	code, env = post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":100}`)
	var charge struct {
		Allowed bool `json:"allowed"`
	}
	_ = json.Unmarshal(env.Data, &charge)
	if code != http.StatusOK || charge.Allowed {
		t.Fatalf("charge = %d %+v, want 200 blocked(实名前置)", code, charge)
	}
}

// 幂等:已 VERIFIED 的账号重复提交返回既有判定,不再调服务(契约/治理接口双重要求)。
func TestVerify_Idempotent_AfterVerified_NoSecondCall(t *testing.T) {
	calls := 0
	p := newProvider(VerifierFunc(func(_, _ string, id string) (Verdict, error) {
		calls++
		return Verdict{State: stateVerified, AgeBand: BandAdult}, nil
	}))
	post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	if code != http.StatusOK {
		t.Fatalf("re-verify status = %d", code)
	}
	state, isMinor := decodeStatus(t, env)
	if state != stateVerified || isMinor == nil || *isMinor || calls != 1 {
		t.Fatalf("re-verify = %q calls=%d, want VERIFIED false 1(幂等不二调)", state, calls)
	}
}

// REJECTED 后重验允许再调服务(服务判定可变;VERIFIED 才锁判定)。
func TestVerify_Rejected_Resubmit_CallsServiceAgain(t *testing.T) {
	verdict := stateRejected
	p := newProvider(VerifierFunc(func(_, _, _ string) (Verdict, error) {
		return Verdict{State: verdict, AgeBand: BandAdult}, nil
	}))
	post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	verdict = stateVerified
	code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	if code != http.StatusOK {
		t.Fatalf("resubmit status = %d", code)
	}
	if state, _ := decodeStatus(t, env); state != stateVerified {
		t.Fatalf("resubmit = %q, want VERIFIED(判定更新)", state)
	}
}

// 判定非法 = fail-closed:进治理链按调用失败计,无 fallback → 安全态。
func TestVerify_InvalidVerdictState_SafeState(t *testing.T) {
	p := newProvider(VerifierFunc(func(_, _, _ string) (Verdict, error) {
		return Verdict{State: "SURE_OK"}, nil
	}))
	code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	if code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200(安全态非错误)", code)
	}
	if state, _ := decodeStatus(t, env); state != statePendingReview {
		t.Fatalf("state = %q, want PENDING_REVIEW(不通过也不拒绝放量)", state)
	}
}

// 服务调用失败 = 治理链调用失败:fallback 接管(熔断/降级复用批次 7 引擎)。
func TestVerify_CloudError_FallbackTakesOver(t *testing.T) {
	p := newProvider(VerifierFunc(func(_, _, _ string) (Verdict, error) {
		return Verdict{}, errCloudUnavail
	}), stubFallback{verdict: Verdict{State: stateVerified, AgeBand: BandAdult}})
	code, env := post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	if code != http.StatusOK {
		t.Fatalf("verify status = %d", code)
	}
	if state, _ := decodeStatus(t, env); state != stateVerified {
		t.Fatalf("state = %q, want VERIFIED(fallback 接管对玩家透明)", state)
	}
	if got := p.Chain(); len(got) != 2 || got[0] != DefaultName || got[1] != "stub-fallback" {
		t.Fatalf("chain = %v", got)
	}
}

type stubFallback struct{ verdict Verdict }

func (s stubFallback) Name() string { return "stub-fallback" }
func (s stubFallback) Verify(accountID string, in router.IdentityInput) (router.VerifyOutcome, error) {
	return router.VerifyOutcome{State: s.verdict.State, IsMinor: s.verdict.IsMinor}, nil
}
func (s stubFallback) Query(accountID string) (router.StatusOutcome, error) {
	return router.StatusOutcome{State: s.verdict.State}, nil
}
func (s stubFallback) Curfew(accountID string, now time.Time) (router.CurfewOutcome, error) {
	return router.CurfewOutcome{Playable: true}, nil
}
func (s stubFallback) ChargeCheck(accountID string, amountCents int) (router.ChargeOutcome, error) {
	return router.ChargeOutcome{Allowed: true}, nil
}
func (s stubFallback) HealthCheck() bool { return true }

// 未配核验服务 = 未就绪(HealthCheck 不过,路由分发不进)。
func TestNilCloud_HealthCheckFails(t *testing.T) {
	p := newProvider(nil)
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("HealthCheck = nil, want error(fail-closed 不进路由)")
	}
}

func TestStatus_UnverifiedThenVerified_WithVerifiedAt(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	_, env := get(t, p, "/v1/realname/status", "player")
	var st struct {
		State      string  `json:"state"`
		IsMinor    *bool   `json:"isMinor"`
		VerifiedAt *string `json:"verifiedAt"`
	}
	if err := json.Unmarshal(env.Data, &st); err != nil || st.State != stateUnverified {
		t.Fatalf("initial = %s %v, want UNVERIFIED", env.Data, err)
	}
	post(t, p, "/v1/realname/verify", "player", verifyBody(adultID))
	_, env = get(t, p, "/v1/realname/status", "player")
	if err := json.Unmarshal(env.Data, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != stateVerified || st.IsMinor == nil || *st.IsMinor ||
		st.VerifiedAt == nil || *st.VerifiedAt != "2026-10-08T15:00:00.000Z" {
		t.Fatalf("status = %+v, want VERIFIED isMinor=false verifiedAt=2026-10-08T15:00:00.000Z", st)
	}
}

// F28:未成年周四 15:00 不可玩,下一窗口周五 20:00(与 warden 同窗口缺省)。
func TestCurfew_Minor_Blocked_WithNextWindow(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	post(t, p, "/v1/realname/verify", "player", verifyBody(minorID))
	code, env := get(t, p, "/v1/realname/curfew", "player")
	var curfew struct {
		Playable     bool    `json:"playable"`
		NextWindowAt *string `json:"nextWindowAt"`
	}
	if err := json.Unmarshal(env.Data, &curfew); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusOK || curfew.Playable || curfew.NextWindowAt == nil ||
		*curfew.NextWindowAt != "2026-10-09T20:00:00.000Z" {
		t.Fatalf("curfew = %d %+v, want blocked nextWindow=周五20:00", code, curfew)
	}
}

// F29:限额按服务下发的年龄段(非本地解析);单次超限回带限额字段。
func TestCharge_Minor_SingleLimitByBand(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	post(t, p, "/v1/realname/verify", "player", verifyBody(minorID))
	code, env := post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":6000}`)
	var charge struct {
		Allowed          bool `json:"allowed"`
		SingleLimitCents *int `json:"singleLimitCents"`
	}
	_ = json.Unmarshal(env.Data, &charge)
	if code != http.StatusOK || charge.Allowed || charge.SingleLimitCents == nil ||
		*charge.SingleLimitCents != 5000 {
		t.Fatalf("charge = %d %+v, want blocked single=5000(8-16 档)", code, charge)
	}
	code, env = post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":3000}`)
	_ = json.Unmarshal(env.Data, &charge)
	if code != http.StatusOK || !charge.Allowed {
		t.Fatalf("charge = %d %+v, want allowed(3000 ≤ 单次 5000)", code, charge)
	}
}

// 未成年但年龄段未下发 / <8 岁:保守禁充,不带限额字段。
func TestCharge_Minor_UnknownOrUnder8Band_BlockedConservative(t *testing.T) {
	for _, band := range []string{"", BandUnder8} {
		p := newProvider(VerifierFunc(func(_, _, _ string) (Verdict, error) {
			return Verdict{State: stateVerified, IsMinor: true, AgeBand: band}, nil
		}))
		post(t, p, "/v1/realname/verify", "player", verifyBody(minorID))
		code, env := post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":100}`)
		var charge struct {
			Allowed           bool `json:"allowed"`
			SingleLimitCents  *int `json:"singleLimitCents"`
			MonthlyLimitCents *int `json:"monthlyLimitCents"`
		}
		_ = json.Unmarshal(env.Data, &charge)
		if code != http.StatusOK || charge.Allowed || charge.SingleLimitCents != nil {
			t.Fatalf("band=%q charge = %d %+v, want blocked 无限额字段", band, code, charge)
		}
	}
}

// F30:玩家 token 走 S2S 401;接入方凭证上报;月累计拦截(幂等键 accountId+date)。
func TestS2S_Report_MonthlyAccumulation(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	post(t, p, "/v1/realname/verify", "player", verifyBody(minorID))
	if code, _ := post(t, p, "/v1/realname/playtime-report", "player",
		`{"accountId":"acc_1","date":"2026-10-07","playMinutes":1,"chargeAmountCents":0}`); code != http.StatusUnauthorized {
		t.Fatalf("player token on s2s = %d, want 401", code)
	}
	report := `{"accountId":"acc_1","date":"2026-10-07","playMinutes":90,"chargeAmountCents":18000}`
	if code, _ := post(t, p, "/v1/realname/playtime-report", "s2s-secret", report); code != http.StatusOK {
		t.Fatalf("s2s report = %d, want 200", code)
	}
	// 同日重报取最新(幂等):金额被覆盖不累计。
	if code, _ := post(t, p, "/v1/realname/playtime-report", "s2s-secret", report); code != http.StatusOK {
		t.Fatalf("s2s re-report = %d, want 200", code)
	}
	code, env := post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":3000}`)
	var charge struct {
		Allowed           bool `json:"allowed"`
		MonthlyUsedCents  *int `json:"monthlyUsedCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	_ = json.Unmarshal(env.Data, &charge)
	if code != http.StatusOK || charge.Allowed || charge.MonthlyUsedCents == nil ||
		*charge.MonthlyUsedCents != 18000 || charge.MonthlyLimitCents == nil ||
		*charge.MonthlyLimitCents != 20000 {
		t.Fatalf("charge = %d %+v, want blocked 月已用 18000/限 20000(重报覆盖不累计)", code, charge)
	}
}

// Webhook 适配 wire:请求体形状/判定解析/非 2xx/判定非法 = 调用失败。
func TestWebhookVerifier_Wire(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"VERIFIED","isMinor":true,"ageBand":"8-16"}`))
	}))
	defer srv.Close()

	v, err := WebhookVerifier{Endpoint: srv.URL}.Verify("acc_9", "张三", "X123")
	if err != nil || v.State != stateVerified || !v.IsMinor || v.AgeBand != Band8To16 {
		t.Fatalf("verdict = %+v err=%v", v, err)
	}
	mu.Lock()
	body := gotBody
	mu.Unlock()
	for _, key := range []string{`"accountId":"acc_9"`, `"name":"张三"`, `"idNumber":"X123"`} {
		if !strings.Contains(body, key) {
			t.Fatalf("webhook body %q missing %s", body, key)
		}
	}

	// 非 2xx = 调用失败。
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	if _, err := (WebhookVerifier{Endpoint: srv500.URL}).Verify("a", "n", "i"); err == nil {
		t.Fatal("webhook 500: want error(进治理熔断)")
	}
	// 判定非法 = 调用失败(fail-closed,不落库)。
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"MAYBE"}`))
	}))
	defer srvBad.Close()
	if _, err := (WebhookVerifier{Endpoint: srvBad.URL}).Verify("a", "n", "i"); err == nil {
		t.Fatal("webhook bad verdict: want error")
	}
	// 连接失败 = 调用失败。
	if _, err := (WebhookVerifier{Endpoint: "http://127.0.0.1:1/nope"}).Verify("a", "n", "i"); err == nil {
		t.Fatal("webhook unreachable: want error")
	}
}

// 红线:边缘 400 的错误信息不含输入原文(姓名/证件号不进响应/日志面)。
func TestVerify_RedLine_NoInputEchoInErrors(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	for _, body := range []string{
		`{"name":"","idNumber":"110101199001011234"}`,
		`{"name":"verylongnameverylongnameverylongname","idNumber":"110101199001011234"}`,
		`{"name":"张三","idNumber":""}`,
		`{"name":"张三","idNumber":"` + strings.Repeat("x", 65) + `"}`,
		`not json`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/realname/verify", strings.NewReader(body))
		req = req.WithContext(auth.WithIdentity(req.Context(),
			auth.Identity{AccountID: "acc_1", GameID: "g", Env: "prod"}))
		newAuth(p).ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%q status = %d, want 400", body, rec.Code)
		}
		respBody := rec.Body.String()
		if strings.Contains(respBody, "verylongname") || strings.Contains(respBody, "110101199001011234") ||
			strings.Contains(respBody, strings.Repeat("x", 65)) {
			t.Fatalf("error body echoes input: %s", respBody)
		}
	}
}

// 边界:amountCents ≤ 0 → 400;未知路径 → 404。
func TestHTTP_EdgeValidation(t *testing.T) {
	p := newProvider(scriptedVerdicts())
	if code, _ := post(t, p, "/v1/realname/charge-check", "player", `{"amountCents":0}`); code != http.StatusBadRequest {
		t.Fatalf("charge 0 = %d, want 400", code)
	}
	if code, _ := post(t, p, "/v1/realname/nope", "player", `{}`); code != http.StatusNotFound {
		t.Fatalf("no route = %d, want 404", code)
	}
	if code, _ := post(t, p, "/v1/realname/playtime-report", "", `{"accountId":"a","date":"2026-10-07","playMinutes":1,"chargeAmountCents":0}`); code != http.StatusUnauthorized {
		t.Fatalf("s2s no token = %d, want 401(fail-closed)", code)
	}
}
