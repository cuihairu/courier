// archivistalt 单元测试:与 archivist 同一 wire 面(懒建/修剪/校验、绑定幂等与
// scope 隔离、上限、fail-closed)+ 事件日志模型异构面(追加只读、投影重放、
// 注销弃历史)。
package archivistalt

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

var baseTime = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// withAccountScope 测试用认证中间件:注入身份与 scope(替代 auth.RequireAuth + Scope 链)。
func withAccountScope(accountID, gameID, env string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: accountID, SessionID: "ses_1"})
			ctx = scope.WithContext(ctx, scope.Scope{GameID: gameID, Env: env})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newP(accountID string) *Provider {
	return New(Options{
		Clock:       func() time.Time { return baseTime },
		RequireAuth: withAccountScope(accountID, "game_demo", "prod"),
	})
}

func call(t *testing.T, p *Provider, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

func TestCapabilityAndName(t *testing.T) {
	p := newP("acc_1")
	if p.Name() != DefaultName || p.Capability() != providers.CapPlayer {
		t.Fatalf("name/capability = %s/%v", p.Name(), p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestProfileLazyCreateAndPatch(t *testing.T) {
	p := newP("acc_1")

	// 首次 GET 即开档:默认展示名 Player,createdAt=updatedAt(开档事件时刻)。
	rec := call(t, p, http.MethodGet, "/v1/player/profile", "")
	if rec.Code != 200 {
		t.Fatalf("get: %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"displayName":"Player"`) ||
		!strings.Contains(body, `"createdAt":"2026-10-09T12:00:00.000Z"`) {
		t.Fatalf("lazy profile = %s", body)
	}

	// PATCH 修剪首尾空白,响应体为准(契约客户端要求)。
	rec = call(t, p, http.MethodPatch, "/v1/player/profile",
		`{"displayName":"  新名字  ","avatarUrl":"https://cdn.example/a.png"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"displayName":"新名字"`) {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"avatarUrl":"https://cdn.example/a.png"`) {
		t.Fatalf("avatarUrl = %s", rec.Body.String())
	}

	// GET 回读一致(投影=最后一次改写)。
	rec = call(t, p, http.MethodGet, "/v1/player/profile", "")
	if !strings.Contains(rec.Body.String(), `"displayName":"新名字"`) {
		t.Fatalf("get after patch = %s", rec.Body.String())
	}

	// 多次 PATCH:最后一次胜出(投影重放语义)。
	rec = call(t, p, http.MethodPatch, "/v1/player/profile", `{"displayName":"三号"}`)
	if !strings.Contains(rec.Body.String(), `"displayName":"三号"`) ||
		strings.Contains(rec.Body.String(), `"avatarUrl":""`) {
		t.Fatalf("second patch = %s", rec.Body.String())
	}

	// 跨账号隔离:另一账号各自开档。
	p2 := New(Options{Clock: func() time.Time { return baseTime },
		RequireAuth: withAccountScope("acc_2", "game_demo", "prod")})
	rec = call(t, p2, http.MethodGet, "/v1/player/profile", "")
	if !strings.Contains(rec.Body.String(), `"displayName":"Player"`) {
		t.Fatalf("acc_2 = %s(应各自开档)", rec.Body.String())
	}
}

func TestProfileValidation(t *testing.T) {
	p := newP("acc_1")
	bad := func(name, body string) {
		t.Helper()
		if rec := call(t, p, http.MethodPatch, "/v1/player/profile", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	bad("空名(修剪后)", `{"displayName":"   "}`)
	bad("超长", `{"displayName":"`+strings.Repeat("字", 31)+`"}`)
	bad("非 JSON", `oops`)
	// avatarUrl 超长。
	if rec := call(t, p, http.MethodPatch, "/v1/player/profile",
		`{"avatarUrl":"`+strings.Repeat("a", 2049)+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("avatarUrl: %d", rec.Code)
	}
	// 边界内合法:30 字符 + 2048 URL。
	if rec := call(t, p, http.MethodPatch, "/v1/player/profile",
		`{"displayName":"`+strings.Repeat("字", 30)+`"}`); rec.Code != 200 {
		t.Fatalf("30 字符应合法: %d", rec.Code)
	}
	if rec := call(t, p, http.MethodPatch, "/v1/player/profile",
		`{"avatarUrl":"`+strings.Repeat("a", 2048)+`"}`); rec.Code != 200 {
		t.Fatalf("2048 URL 应合法: %d", rec.Code)
	}
}

func TestCharactersBindIdempotentAndScopeIsolated(t *testing.T) {
	p := newP("acc_1")

	// 绑定 → 绑定同 ID 幂等(相同 boundAt,列表不重复)。
	rec := call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"char_001"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"playerId":"char_001"`) {
		t.Fatalf("bind = %d %s", rec.Code, rec.Body.String())
	}
	rec = call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"char_001"}`)
	if rec.Code != 200 {
		t.Fatalf("idempotent bind = %d", rec.Code)
	}
	rec = call(t, p, http.MethodGet, "/v1/player/characters", "")
	if strings.Count(rec.Body.String(), "char_001") != 1 {
		t.Fatalf("重复绑定不应产生第二条: %s", rec.Body.String())
	}

	// scope 隔离:同账号另一 env 各自成表。
	pOther := New(Options{Clock: func() time.Time { return baseTime },
		RequireAuth: withAccountScope("acc_1", "game_demo", "staging")})
	rec = call(t, pOther, http.MethodGet, "/v1/player/characters", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("跨 scope 应为空: %s", rec.Body.String())
	}
}

func TestCharactersLimitAndValidation(t *testing.T) {
	p := newP("acc_1")
	for i := 0; i < maxCharacters; i++ {
		pid := fmt.Sprintf("c%03d", i)
		if rec := call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"`+pid+`"}`); rec.Code != 200 {
			t.Fatalf("bind %d: %d", i, rec.Code)
		}
	}
	// 第 51 条 → 400(契约:≤50/账号/scope)。
	if rec := call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"overflow"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("overflow: %d", rec.Code)
	}
	// 非法 playerId。
	if rec := call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty playerId: %d", rec.Code)
	}
	if rec := call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"`+strings.Repeat("p", 65)+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("long playerId: %d", rec.Code)
	}
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{}) // 未注入 RequireAuth:fail-closed
	if rec := call(t, p, http.MethodGet, "/v1/player/profile", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestRoutingShape(t *testing.T) {
	p := newP("acc_1")
	if rec := call(t, p, http.MethodPost, "/v1/player/profile", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("POST profile: %d", rec.Code)
	}
	if rec := call(t, p, http.MethodDelete, "/v1/player/characters", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE characters: %d", rec.Code)
	}
}

// TestEventLogAppendOnly 异构面:日志只追加不改写——多轮请求后事件数单调增长,
// 且历史时刻不因新写被改(投影重放不回写)。
func TestEventLogAppendOnly(t *testing.T) {
	t0 := baseTime
	step := 0
	p := New(Options{Clock: func() time.Time { step++; return t0.Add(time.Duration(step) * time.Second) },
		RequireAuth: withAccountScope("acc_1", "game_demo", "prod")})

	call(t, p, http.MethodPatch, "/v1/player/profile", `{"displayName":"一"}`)
	call(t, p, http.MethodPatch, "/v1/player/profile", `{"displayName":"二"}`)
	call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"c1"}`)
	call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"c1"}`) // 幂等不加事件
	nAfterFour := len(p.logs["acc_1"])

	call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"c2"}`)
	if len(p.logs["acc_1"]) != nAfterFour+1 {
		t.Fatalf("日志应只追加: %d → %d", nAfterFour, len(p.logs["acc_1"]))
	}
	// 首事件时刻不变(开档时刻即 createdAt,不被后续写改写)。
	if first := p.logs["acc_1"][0]; first.at != t0.Add(time.Second) {
		t.Fatalf("首事件时刻被改写: %v", first.at)
	}
}

// TestProjectionDerivesFromHistory 异构面:档案字段完全由历史推导——
// createdAt=开档时刻、updatedAt=最后档案事件时刻、displayName=最后改写。
func TestProjectionDerivesFromHistory(t *testing.T) {
	t0 := baseTime
	step := 0
	p := New(Options{Clock: func() time.Time { step++; return t0.Add(time.Duration(step) * time.Minute) },
		RequireAuth: withAccountScope("acc_1", "game_demo", "prod")})

	rec := call(t, p, http.MethodGet, "/v1/player/profile", "")                     // T1 开档
	rec = call(t, p, http.MethodPatch, "/v1/player/profile", `{"displayName":"二"}`) // T2
	if !strings.Contains(rec.Body.String(), `"createdAt":"2026-10-09T12:01:00.000Z"`) ||
		!strings.Contains(rec.Body.String(), `"updatedAt":"2026-10-09T12:02:00.000Z"`) {
		t.Fatalf("时间戳应来自事件历史: %s", rec.Body.String())
	}

	// 再 GET:createdAt 恒为开档时刻(重放不产生新事件)。
	rec = call(t, p, http.MethodGet, "/v1/player/profile", "")
	if !strings.Contains(rec.Body.String(), `"createdAt":"2026-10-09T12:01:00.000Z"`) {
		t.Fatalf("GET 不应改变 createdAt: %s", rec.Body.String())
	}
	if len(p.logs["acc_1"]) != 2 { // opened + patched,GET 不追加
		t.Fatalf("GET 不应追加事件: %d", len(p.logs["acc_1"]))
	}
}

// TestDeleteAccountDropsLog 注销级联:整条日志丢弃,历史即状态(与状态表实现
// 的 delete prof+chars 同语义);之后再访问=重新开档。
func TestDeleteAccountDropsLog(t *testing.T) {
	p := newP("acc_1")
	call(t, p, http.MethodPatch, "/v1/player/profile", `{"displayName":"旧名"}`)
	call(t, p, http.MethodPost, "/v1/player/characters", `{"playerId":"c1"}`)

	p.DeleteAccount("acc_1")
	if len(p.logs) != 0 {
		t.Fatalf("注销后日志应清空: %d", len(p.logs))
	}

	// 重新访问=重新开档(默认名,绑定清零)。
	rec := call(t, p, http.MethodGet, "/v1/player/profile", "")
	if !strings.Contains(rec.Body.String(), `"displayName":"Player"`) {
		t.Fatalf("注销后重开档 = %s", rec.Body.String())
	}
	rec = call(t, p, http.MethodGet, "/v1/player/characters", "")
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("注销后绑定应清零: %s", rec.Body.String())
	}
}
