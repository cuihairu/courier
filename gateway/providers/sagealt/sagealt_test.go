// sagealt 单元测试:wire 面与 sage 同形状(命中快照、未命中引导、评分、
// 统计、校验、fail-closed、路由形状)+ 预处理异构面(写时规范化:大小写/
// 首尾空白条目与 sage 读时规范化逐条等价)。
package sagealt

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
	// 两条都含「活动」;问题命中应优于答案命中(权重 3/2/1 与 sage 同源)。
	p.AddEntry("活动怎么参加", "见公告", []string{"活动"}) // 问题含活动 → 3 分
	p.AddEntry("充值比例", "活动期间双倍", nil)           // 答案含活动 → 1 分

	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"活动"}`)
	body := rec.Body.String()
	if !strings.Contains(body, `"question":"活动怎么参加"`) {
		t.Fatalf("应命中问题条目(高分): %s", body)
	}
}

func TestQueryStats_HitRateCountable(t *testing.T) {
	p := newP()
	p.AddEntry("怎么充值", "商店-充值页", []string{"充值"})

	call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"怎么充值"}`)  // 命中
	call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"怎么充值?"}`) // 命中
	call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"天气如何"}`)  // 未命中
	call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"  "}`)    // 参数错不计

	q, h := p.QueryStats()
	if q != 3 || h != 2 {
		t.Fatalf("stats = %d/%d, want 3 queries 2 hits", q, h)
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

// TestWriteTimeNormalization 异构面:AddEntry 时预计算规范化字段(大小写/
// 首尾空白折叠),查询路径读预计算值;匹配行为与 sage 读时规范化逐条等价。
func TestWriteTimeNormalization(t *testing.T) {
	p := newP()
	p.AddEntry("  How To Refund  ", " CLICK OK Button ", []string{" REFUND ", "", "  "})

	p.mu.RLock()
	e := p.entries[0]
	p.mu.RUnlock()
	// 写时预计算:规范化字段折叠大小写与首尾空白。
	if e.normQuestion != "how to refund" || e.normAnswer != "click ok button" {
		t.Fatalf("normQuestion/normAnswer = %q/%q", e.normQuestion, e.normAnswer)
	}
	if len(e.normKeywords) != 3 || e.normKeywords[0] != "refund" ||
		e.normKeywords[1] != "" || e.normKeywords[2] != "" {
		t.Fatalf("normKeywords = %q", e.normKeywords)
	}
	// wire 快照仍是原始文本(契约:原样返回,不改写)。
	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"how to refund"}`)
	body := rec.Body.String()
	if !strings.Contains(body, `"matched":true`) ||
		!strings.Contains(body, `"question":"  How To Refund  "`) {
		t.Fatalf("大小写不敏感命中 + 原样快照: %s", body)
	}

	// 规范化幂等:对已规范化值再规范化不变(等价性根据)。
	if normalize(normalize("  MiXeD  ")) != normalize("  MiXeD  ") {
		t.Fatal("normalize 应幂等")
	}
}

// TestKeywordEmptySemanticsSameAsSage 空关键词不参与评分:sage 对原值判
// k != "",本实现对规范化值判 k != ""——纯空白关键词规范化后为空,两边都
// 不贡献分数(行为等价)。
func TestKeywordEmptySemanticsSameAsSage(t *testing.T) {
	p := newP()
	// 唯一可命中路径 = 问题子串;关键词全为空/纯空白。
	p.AddEntry("绑定手机", "答案文本", []string{"", "  "})
	rec := call(t, p, http.MethodPost, "/v1/assistant/query", `{"text":"绑定手机"}`)
	if !strings.Contains(rec.Body.String(), `"matched":true`) {
		t.Fatalf("问题命中不受空关键词影响: %s", rec.Body.String())
	}
	// 反向:只有空白关键词可"命中"时,不应误报。
	p2 := newP()
	p2.AddEntry("无关问题", "无关答案", []string{"   "})
	rec = call(t, p2, http.MethodPost, "/v1/assistant/query", `{"text":"  "}`)
	if rec.Code != http.StatusBadRequest { // 全空白 text 先被校验拦下
		t.Fatalf("空白 text = %d", rec.Code)
	}
	rec = call(t, p2, http.MethodPost, "/v1/assistant/query", `{"text":"x"}`)
	if !strings.Contains(rec.Body.String(), `"matched":false`) {
		t.Fatalf("纯空白关键词不应命中: %s", rec.Body.String())
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
