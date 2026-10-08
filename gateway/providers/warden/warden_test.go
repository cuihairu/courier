// warden 验收:自建核验语义(状态/幂等/未成年判定)、curfew 窗口、分龄限额、
// S2S 上报幂等、HTTP 投影(verify/status/curfew/charge-check/playtime-report)。
// 对齐 docs/contract/realname.md Frozen v1。
package warden

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
)

const (
	adultID = "110101199001011234" // 1990 出生 → 成年
	minorID = "110101201506201234" // 2015 出生 → 未成年(<18 @2026)
	under8  = "110101202201011234" // 2022 出生 → <8 岁禁充
)

type envelope struct {
	Data json.RawMessage `json:"data"`
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

// newAuthW 测试认证中间件:固定账号 acc_1(模拟 auth.RequireAuth 已完成注入)。
func newAuthW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
			auth.Identity{AccountID: "acc_1", GameID: "game_demo", Env: "prod"})))
	})
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// 2026-10-08 是周四:未成年不可玩,下一窗口周五 20:00。
var nowThursday = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

func newWarden(clock func() time.Time, s2s string) *Warden {
	return New(Options{Clock: clock, RequireAuth: newAuthW, S2SToken: s2s})
}

func TestVerifyAdult_VerifiedNotMinor(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	code, env := post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"`+adultID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	var got struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != "VERIFIED" || got.IsMinor == nil || *got.IsMinor {
		t.Errorf("got %+v, want VERIFIED isMinor=false(仅 VERIFIED 下发)", got)
	}
}

func TestVerifyMinor_Flagged(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	_, env := post(t, w, "/v1/realname/verify", "player",
		`{"name":"李四","idNumber":"`+minorID+`"}`)
	var got struct {
		State   string `json:"state"`
		IsMinor *bool  `json:"isMinor"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.State != "VERIFIED" || got.IsMinor == nil || !*got.IsMinor {
		t.Errorf("got %+v, want VERIFIED isMinor=true", got)
	}
}

func TestStatusUnverified_ThenVerified(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	_, env := get(t, w, "/v1/realname/status", "player")
	var st struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(env.Data, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != "UNVERIFIED" {
		t.Fatalf("初始 state = %q, want UNVERIFIED", st.State)
	}

	post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"`+adultID+`"}`)
	_, env = get(t, w, "/v1/realname/status", "player")
	var after struct {
		State      string  `json:"state"`
		VerifiedAt *string `json:"verifiedAt"`
		IsMinor    *bool   `json:"isMinor"`
	}
	if err := json.Unmarshal(env.Data, &after); err != nil {
		t.Fatal(err)
	}
	if after.State != "VERIFIED" || after.VerifiedAt == nil || after.IsMinor == nil {
		t.Errorf("got %+v, want VERIFIED + verifiedAt + isMinor", after)
	}
	if *after.VerifiedAt != "2026-10-08T15:00:00.000Z" {
		t.Errorf("verifiedAt = %q, want 毫秒 UTC 原文", *after.VerifiedAt)
	}
}

func TestVerifyIdempotent_AlreadyVerified(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"`+adultID+`"}`)
	// 换身份重复提交:不重验,返回既有判定。
	_, env := post(t, w, "/v1/realname/verify", "player",
		`{"name":"王五","idNumber":"110101199501012345"}`)
	var got struct {
		IsMinor *bool `json:"isMinor"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.IsMinor == nil || *got.IsMinor {
		t.Errorf("幂等应返回首验判定 isMinor=false, got %+v", got)
	}
}

func TestVerifyInvalidIdNumber_400(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	code, _ := post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"123"}`)
	if code != http.StatusBadRequest {
		t.Errorf("格式非法应 400, got %d", code)
	}
}

func TestCurfewAdult_AlwaysPlayable(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"`+adultID+`"}`)
	_, env := get(t, w, "/v1/realname/curfew", "player")
	var got struct {
		Playable bool `json:"playable"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Playable {
		t.Error("成年人恒可玩")
	}
}

func TestCurfewMinor_OutOfWindow_BlockedWithNextWindow(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "") // 周四 15:00
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"李四","idNumber":"`+minorID+`"}`)
	_, env := get(t, w, "/v1/realname/curfew", "player")
	var got struct {
		Playable     bool    `json:"playable"`
		NextWindowAt *string `json:"nextWindowAt"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Playable || got.NextWindowAt == nil {
		t.Fatalf("got %+v, want playable=false + nextWindowAt", got)
	}
	if *got.NextWindowAt != "2026-10-09T20:00:00.000Z" { // 周五 20:00
		t.Errorf("nextWindowAt = %q, want 2026-10-09T20:00:00.000Z(周五窗口)", *got.NextWindowAt)
	}
}

func TestCurfewMinor_InWindow_Playable(t *testing.T) {
	// 2026-10-09 是周五 20:30:窗口内。
	w := newWarden(fixedClock(time.Date(2026, 10, 9, 20, 30, 0, 0, time.UTC)), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"李四","idNumber":"`+minorID+`"}`)
	_, env := get(t, w, "/v1/realname/curfew", "player")
	var got struct {
		Playable bool `json:"playable"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Playable {
		t.Error("周五 20:30 未成年应在窗口内可玩")
	}
}

func TestChargeAdult_Unlimited(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"张三","idNumber":"`+adultID+`"}`)
	_, env := post(t, w, "/v1/realname/charge-check", "player",
		`{"amountCents":1000000}`)
	var got struct {
		Allowed           bool `json:"allowed"`
		SingleLimitCents  *int `json:"singleLimitCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Allowed || got.SingleLimitCents != nil || got.MonthlyLimitCents != nil {
		t.Errorf("got %+v, want allowed=true 无限额字段", got)
	}
}

func TestChargeMinor8To16_WithinAndOverSingle(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"小明","idNumber":"110101201506201234"}`) // 11 岁

	_, env := post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":5000}`)
	var ok struct {
		Allowed           bool `json:"allowed"`
		SingleLimitCents  *int `json:"singleLimitCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	if err := json.Unmarshal(env.Data, &ok); err != nil {
		t.Fatal(err)
	}
	if !ok.Allowed || ok.SingleLimitCents == nil || *ok.SingleLimitCents != 5000 ||
		ok.MonthlyLimitCents == nil || *ok.MonthlyLimitCents != 20000 {
		t.Errorf("got %+v, want allowed=true 单次 5000/月 20000(F29 口径)", ok)
	}

	_, env = post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":5001}`)
	var over struct {
		Allowed          bool `json:"allowed"`
		MonthlyUsedCents *int `json:"monthlyUsedCents"`
	}
	if err := json.Unmarshal(env.Data, &over); err != nil {
		t.Fatal(err)
	}
	if over.Allowed {
		t.Errorf("单次超 50 元应拒绝: %+v", over)
	}
}

func TestChargeUnder8_Blocked(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"小豆","idNumber":"`+under8+`"}`)
	_, env := post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":100}`)
	var got struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Allowed {
		t.Error("<8 岁应禁充")
	}
}

func TestChargeUnverified_Blocked(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	_, env := post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":100}`)
	var got struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Allowed {
		t.Error("未实名应前置拒绝(REALNAME_REQUIRED 语义)")
	}
}

func TestMonthlyAccumulationViaS2SReport(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "s2s-secret")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"小明","idNumber":"110101201506201234"}`)
	// 月内已上报消费 180 元(18000 分),再充 30 元(3000)→ 月累计 210 超 200 拒绝。
	if code, _ := post(t, w, "/v1/realname/playtime-report", "s2s-secret",
		`{"accountId":"acc_1","date":"2026-10-07","playMinutes":90,"chargeAmountCents":18000}`); code != http.StatusOK {
		t.Fatalf("s2s report status = %d", code)
	}

	_, env := post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":3000}`)
	var got struct {
		Allowed           bool `json:"allowed"`
		MonthlyUsedCents  *int `json:"monthlyUsedCents"`
		MonthlyLimitCents *int `json:"monthlyLimitCents"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Allowed || got.MonthlyUsedCents == nil || *got.MonthlyUsedCents != 18000 {
		t.Errorf("got %+v, want allowed=false 月已用 18000", got)
	}
	// 单次 20 元(2000)在剩余额度内:放行。
	_, env = post(t, w, "/v1/realname/charge-check", "player", `{"amountCents":2000}`)
	var ok struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.Unmarshal(env.Data, &ok); err != nil {
		t.Fatal(err)
	}
	if !ok.Allowed {
		t.Error("200+2000=200 边界内应放行")
	}
}

func TestS2SAuthAndValidation(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "s2s-secret")
	// 玩家 token 不认(即使 RequireAuth 放行,S2S 面单独校验)。
	if code, _ := post(t, w, "/v1/realname/playtime-report", "player",
		`{"accountId":"acc_1","date":"2026-10-07","playMinutes":1,"chargeAmountCents":0}`); code != http.StatusUnauthorized {
		t.Errorf("玩家凭证走 S2S 应 401, got %d", code)
	}
	// 未配置 token 的实例:fail-closed。
	w2 := newWarden(fixedClock(nowThursday), "")
	if code, _ := post(t, w2, "/v1/realname/playtime-report", "any",
		`{"accountId":"acc_1","date":"2026-10-07","playMinutes":1,"chargeAmountCents":0}`); code != http.StatusUnauthorized {
		t.Errorf("空 token 应 fail-closed 401, got %d", code)
	}
	// 日期非法。
	if code, _ := post(t, w, "/v1/realname/playtime-report", "s2s-secret",
		`{"accountId":"acc_1","date":"2026/10/07","playMinutes":1,"chargeAmountCents":0}`); code != http.StatusBadRequest {
		t.Errorf("非法日期应 400, got %d", code)
	}
}

func TestS2SReportIdempotent_LastWins(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "s2s-secret")
	post(t, w, "/v1/realname/verify", "player",
		`{"name":"小明","idNumber":"110101201506201234"}`)
	for _, amount := range []int{18000, 9000} { // 同日重报取最新
		post(t, w, "/v1/realname/playtime-report", "s2s-secret",
			`{"accountId":"acc_1","date":"2026-10-07","playMinutes":90,"chargeAmountCents":`+
				strconv.Itoa(amount)+`}`)
	}
	w.core.mu.Lock()
	got := w.core.plays[playKey{"acc_1", "2026-10-07"}].chargeAmountCents
	w.core.mu.Unlock()
	if got != 9000 {
		t.Errorf("同日重报应取最新: got %d, want 9000", got)
	}
}

func TestNoAuthMiddleware_401FailClosed(t *testing.T) {
	w := New(Options{Clock: fixedClock(nowThursday)}) // RequireAuth nil
	code, _ := post(t, w, "/v1/realname/verify", "player", `{"name":"张三","idNumber":"`+adultID+`"}`)
	if code != http.StatusUnauthorized {
		t.Errorf("忘注入认证应 fail-closed 401, got %d", code)
	}
}

func TestHandlerIdentity(t *testing.T) {
	w := newWarden(fixedClock(nowThursday), "")
	if w.Name() != DefaultName || w.Capability() != "realname" {
		t.Errorf("name/capability = %q/%q", w.Name(), w.Capability())
	}
	if err := w.HealthCheck(context.Background()); err != nil {
		t.Errorf("health: %v", err)
	}
	if chain := w.Chain(); len(chain) != 1 || chain[0] != DefaultName {
		t.Errorf("chain = %v, want [warden](core 为 primary)", chain)
	}
}
