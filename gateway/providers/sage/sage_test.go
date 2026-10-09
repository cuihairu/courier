// sage 单元测试:检索命中/未命中语义、条目原样快照、校验、fail-closed、路由形状。
package sage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
)

func withAccount(accountID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: accountID, SessionID: "ses_1"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newP() *AssistantProvider {
	return New(Options{RequireAuth: withAccount("acc_1")})
}

func call(t *testing.T, p *AssistantProvider, method, path, body string) *httptest.ResponseRecorder {
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
	p := newP()
	if p.Name() != DefaultName || p.Capability() != providers.CapAssistant {
		t.Fatalf("name/capability = %s/%v", p.Name(), p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestQueryHit_ReturnsSnapshotVerbatim(t *testing.T) {
	p := newP()
	p.AddEntry("怎么找回账号", "在登录页点击『忘记密码』,按提示验证手机号即可。", []string{"找回", "账号"})

	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"账号怎么找回"}`)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 命中:matched=true + suggestTransfer=false + answer 原样
	if !strings.Contains(body, `"matched":true`) || !strings.Contains(body, `"suggestTransfer":false`) {
		t.Fatalf("hit flags = %s", body)
	}
	if !strings.Contains(body, `"question":"怎么找回账号"`) ||
		!strings.Contains(body, `"answer":"在登录页点击『忘记密码』,按提示验证手机号即可。"`) {
		t.Fatalf("answer 非原样快照: %s", body)
	}
	if !strings.Contains(body, `"id":"faq_`) {
		t.Fatalf("id 前缀应为 faq_: %s", body)
	}
}

func TestQueryMiss_NotAnError_SuggestTransfer(t *testing.T) {
	p := newP()
	p.AddEntry("怎么找回账号", "答案", []string{"找回"})

	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"如何下载游戏"}`)
	if rec.Code != 200 {
		t.Fatalf("未命中应 200,得 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"matched":false`) || !strings.Contains(body, `"suggestTransfer":true`) {
		t.Fatalf("miss flags = %s", body)
	}
	if strings.Contains(body, `"answer"`) {
		t.Fatalf("未命中不应带 answer: %s", body)
	}
}

func TestQueryEmptyKB_AlwaysMiss(t *testing.T) {
	p := newP()
	if p.EntryCount() != 0 {
		t.Fatalf("空库 count = %d", p.EntryCount())
	}
	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"任意问题"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"matched":false`) {
		t.Fatalf("空库 = %d %s", rec.Code, rec.Body.String())
	}
}

func TestQueryScoring_QuestionBeatsAnswer(t *testing.T) {
	p := newP()
	// 两条都含「活动」;问题命中应优于答案命中。
	p.AddEntry("活动怎么参加", "见公告", []string{"活动"}) // 问题含活动 → 3 分
	p.AddEntry("充值比例", "活动期间双倍", nil)           // 答案含活动 → 1 分

	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"活动"}`)
	body := rec.Body.String()
	if !strings.Contains(body, `"question":"活动怎么参加"`) {
		t.Fatalf("应命中问题条目(高分): %s", body)
	}
}

func TestQueryTrimAndValidation(t *testing.T) {
	p := newP()
	p.AddEntry("q", "a", nil)

	bad := func(name, body string) {
		t.Helper()
		if rec := call(t, p, http.MethodPost, "/v1/assistant/query", body); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d body=%s", name, rec.Code, rec.Body.String())
		}
	}
	bad("空 text", `{"text":""}`)
	bad("全空白", `{"text":"   "}`)
	bad("超 500 字", `{"text":"`+strings.Repeat("字", 501)+`"}`)
	bad("非 JSON", `oops`)

	// 修剪首尾:两侧空白不参与判定。
	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"  q  "}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"matched":true`) {
		t.Fatalf("trim = %d %s", rec.Code, rec.Body.String())
	}
	// 边界 500 字合法。
	rec = call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"`+strings.Repeat("字", 500)+`"}`)
	if rec.Code != 200 {
		t.Fatalf("500 字应合法: %d", rec.Code)
	}
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{}) // 未注入 RequireAuth:fail-closed
	if rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"q"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestRoutingShape(t *testing.T) {
	p := newP()
	if rec := call(t, p, http.MethodGet, "/v1/assistant/query", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("GET query: %d", rec.Code)
	}
	if rec := call(t, p, http.MethodPost, "/v1/assistant/chat", `{"text":"q"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("未知路径: %d", rec.Code)
	}
}
