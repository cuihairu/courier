// 批次 64 实名域第二实现一致性 e2e(批次 18 identity 先例同构):
// 同一 wire 场景对 warden(自建核验)与 warden-alt(托管核验)各跑一遍、
// 同一断言——「实名 Provider 可换」有行为证明而非仅接口冻结。
// 两实现异构面(判定来源/证件号格式政策/落库内容)不在本文件断言,
// 由各自单测承载(warden_test.go / providers/wardenalt)。
package e2e

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/wardenalt"
	"github.com/cuihairu/courier/gateway/routing"
)

// runRealNameAdultContract 成年链:UNVERIFIED → verify VERIFIED → 幂等重验 →
// 恒可玩 → 充值无限额 → status 全字段;认证/入参边缘(401/400)。
func runRealNameAdultContract(t *testing.T, h *rnHarness) {
	t.Helper()
	token := h.login(t)

	var st struct {
		State      string  `json:"state"`
		IsMinor    *bool   `json:"isMinor"`
		VerifiedAt *string `json:"verifiedAt"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", &st); code != 200 ||
		st.State != "UNVERIFIED" {
		t.Fatalf("initial status = %d %q", code, st.State)
	}

	// 未认证 → 401(通用码,两实现一致)。
	if code, errCode := h.call(t, http.MethodPost, "/v1/realname/verify", "",
		`{"name":"张三","idNumber":"110101199001011234"}`, nil); code != http.StatusUnauthorized ||
		errCode != "COMMON_UNAUTHENTICATED" {
		t.Fatalf("anon verify = %d %q, want 401 COMMON_UNAUTHENTICATED", code, errCode)
	}

	// 成年提交 → VERIFIED, isMinor=false。
	var verify struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 ||
		verify.State != "VERIFIED" || verify.IsMinor == nil || *verify.IsMinor {
		t.Fatalf("verify = %d %+v", code, verify)
	}

	// 幂等:重复提交返回既有判定(warden 返回本地记录 / alt 不再调核验服务)。
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"张三","idNumber":"110101199001011234"}`, &verify); code != 200 ||
		verify.State != "VERIFIED" || verify.IsMinor == nil || *verify.IsMinor {
		t.Fatalf("re-verify = %d %+v, want 幂等 VERIFIED", code, verify)
	}

	// 成年:恒可玩、充值无限额(F28/F29 成年口径)。
	var curfew struct {
		Playable bool `json:"playable"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", &curfew); code != 200 ||
		!curfew.Playable {
		t.Fatalf("curfew = %d %+v", code, curfew)
	}
	var charge struct {
		Allowed bool `json:"allowed"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":999999}`, &charge); code != 200 || !charge.Allowed {
		t.Fatalf("charge = %d %+v", code, charge)
	}

	// status 全字段:VERIFIED + isMinor + verifiedAt。
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/status", token, "", &st); code != 200 ||
		st.State != "VERIFIED" || st.IsMinor == nil || *st.IsMinor || st.VerifiedAt == nil {
		t.Fatalf("status = %d %+v", code, st)
	}

	// 入参边缘:name 空 → 400 COMMON_INVALID_ARGUMENT。
	if code, errCode := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"","idNumber":"110101199001011234"}`, nil); code != http.StatusBadRequest ||
		errCode != "COMMON_INVALID_ARGUMENT" {
		t.Fatalf("empty name = %d %q, want 400 COMMON_INVALID_ARGUMENT", code, errCode)
	}
}

// runRealNameMinorContract 未成年链:VERIFIED isMinor=true → F28 不可玩带
// 下一窗口 → F29 单次限额 → S2S 上报(玩家 token 401 / 月累计拦截)。
func runRealNameMinorContract(t *testing.T, h *rnHarness) {
	t.Helper()
	token := h.login(t)

	var verify struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/verify", token,
		`{"name":"小明","idNumber":"110101201506201234"}`, &verify); code != 200 ||
		verify.State != "VERIFIED" || verify.IsMinor == nil || !*verify.IsMinor {
		t.Fatalf("minor verify = %d %+v", code, verify)
	}

	// 周四 15:00:不可玩,下一窗口周五 20:00(F28)。
	var curfew struct {
		Playable     bool    `json:"playable"`
		NextWindowAt *string `json:"nextWindowAt"`
	}
	if code, _ := h.call(t, http.MethodGet, "/v1/realname/curfew", token, "", &curfew); code != 200 ||
		curfew.Playable || curfew.NextWindowAt == nil ||
		*curfew.NextWindowAt != "2026-10-09T20:00:00.000Z" {
		t.Fatalf("curfew = %d %+v, want blocked 周五20:00", code, curfew)
	}

	// 单次超限(8-16 岁档 50 元):拒绝并回带单次限额(F29)。
	var charge struct {
		Allowed          bool `json:"allowed"`
		SingleLimitCents *int `json:"singleLimitCents"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":6000}`, &charge); code != 200 || charge.Allowed ||
		charge.SingleLimitCents == nil || *charge.SingleLimitCents != 5000 {
		t.Fatalf("charge = %d %+v, want blocked 单次限 5000", code, charge)
	}

	// S2S:玩家凭证 401(玩家端上报会造成重复统计,统一服务端汇总)。
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

	// 月累计 180 → 再充 30 元超月上限 200:拒绝并回带月累计/月上限(F29+F30)。
	var monthly struct {
		Allowed           bool `json:"allowed"`
		MonthlyUsedCents  *int `json:"monthlyUsedCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	if code, _ := h.call(t, http.MethodPost, "/v1/realname/charge-check", token,
		`{"amountCents":3000}`, &monthly); code != 200 || monthly.Allowed ||
		monthly.MonthlyUsedCents == nil || *monthly.MonthlyUsedCents != 18000 ||
		monthly.MonthlyLimitCents == nil || *monthly.MonthlyLimitCents != 20000 {
		t.Fatalf("charge = %d %+v, want blocked 月已用 18000/限 20000", code, monthly)
	}
}

// TestRealNameContractConformance 两实现 × 两链路,同一断言。
func TestRealNameContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name string
		mode int
	}{
		{"warden", rnWarden},
		{"warden-alt", rnWardenAlt},
	} {
		t.Run(impl.name+"/adult", func(t *testing.T) {
			runRealNameAdultContract(t, newRealNameHarness(t, impl.mode))
		})
		t.Run(impl.name+"/minor", func(t *testing.T) {
			runRealNameMinorContract(t, newRealNameHarness(t, impl.mode))
		})
	}
}

// 未配核验服务的 warden-alt = 未就绪:路由 HealthCheck 直接跳过 → 503
// (fail-closed 不静默半可用;与「默认关闭 501」不同码,提示配置缺失而非能力关闭)。
func TestRealNameWardenAlt_NoCloud_NotReady(t *testing.T) {
	h := newRealNameHarnessNoCloud(t)
	if code, errCode := h.call(t, http.MethodPost, "/v1/realname/verify", "",
		`{"name":"张三","idNumber":"110101199001011234"}`, nil); code != http.StatusServiceUnavailable ||
		errCode != "COMMON_UNAVAILABLE" {
		t.Fatalf("no-cloud verify = %d %q, want 503 COMMON_UNAVAILABLE", code, errCode)
	}
}

// newRealNameHarnessNoCloud 装配仅注册「零 Cloud」的 warden-alt(直接构造,
// 绕过 rnWardenAlt 脚本):registry/account/routing 与主装配同构。
func newRealNameHarnessNoCloud(t *testing.T) *rnHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	alt := wardenalt.New(wardenalt.Options{RequireAuth: reqAuth}) // 无 Cloud = 核验服务未配置
	if err := reg.Register(alt); err != nil {
		t.Fatalf("register wardenalt: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapRealname: {Primary: wardenalt.DefaultName},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &rnHarness{srv: srv}
}
