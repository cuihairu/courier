// 批次 69 公告域第二实现一致性 e2e(批次 18/64/67/68 先例同构):
// 同一 wire 场景对 herald(读时计算:线性扫描+读时排序)与 herald-alt(写时索引:
// map 详情 + 发布时维护有序切片)各跑一遍、同一断言——「公告 Provider 可换」
// 有行为证明而非仅接口冻结。两实现异构面不在本文件断言,由各自单测承载
// (herald 由 e2e/m2 承载 / providers/heraldalt)。
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/providers/heraldalt"
	"github.com/cuihairu/courier/gateway/routing"
)

// announcementsHarness 装配 account + 一个公告实现(经路由,与 main 同构)。
type announcementsHarness struct {
	srv      *httptest.Server
	announce *herald.AnnouncementProvider
	annAlt   *heraldalt.Provider
}

func newAnnouncementsHarness(t *testing.T, useAlt bool) *announcementsHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var annP providers.Handler
	var announce *herald.AnnouncementProvider
	var annAlt *heraldalt.Provider
	if useAlt {
		annAlt = heraldalt.New(heraldalt.Options{RequireAuth: reqAuth})
		annP = annAlt
	} else {
		announce = herald.New(herald.Options{RequireAuth: reqAuth})
		annP = announce
	}
	if err := reg.Register(annP); err != nil {
		t.Fatalf("register announcement provider: %v", err)
	}
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity:      {Primary: account.DefaultName},
		providers.CapAnnouncements: {Primary: annP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &announcementsHarness{srv: srv, announce: announce, annAlt: annAlt}
}

func (h *announcementsHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

func (h *announcementsHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"ann-e2e","platform":"linux"}`))
	h.scopeHeaders(req)
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

// publish 管理面发布(两实现同签名);alt 实现经 harness 暴露的发布口。
func (h *announcementsHarness) publish(t *testing.T, useAlt bool, title, body, severity string,
	startAt, endAt *time.Time, filters map[string][]string, language, groupKey string) (id string) {
	t.Helper()
	if useAlt {
		a, err := h.annAlt.Publish(title, body, severity, startAt, endAt, filters, language, groupKey)
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		return a.ID
	}
	a, err := h.announce.Publish(title, body, severity, startAt, endAt, filters, language, groupKey)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return a.ID
}

func (h *announcementsHarness) get(t *testing.T, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+path, nil)
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func (h *announcementsHarness) post(t *testing.T, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(""))
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// runAnnouncementsContract 公告契约链:认证 → 形状 → 定向过滤 → 分页 →
// 详情同码不泄露 → 多语回显 → 可见期。
func runAnnouncementsContract(t *testing.T, h *announcementsHarness, useAlt bool) {
	t.Helper()
	token := h.login(t)

	// 匿名 → 401 COMMON_UNAUTHENTICATED(契约 announcement.md)。
	if code, body := h.get(t, "/v1/announcements/", ""); code != http.StatusUnauthorized ||
		!strings.Contains(body, "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", code, body)
	}
	// 形状:非 GET → 400;未知路径 → 404。
	if code, _ := h.post(t, "/v1/announcements/", token); code != http.StatusBadRequest {
		t.Fatalf("POST list = %d, want 400", code)
	}
	if code, body := h.get(t, "/v1/announcements/ann_missing", token); code != http.StatusNotFound ||
		!strings.Contains(body, "ANNOUNCEMENT_NOT_FOUND") {
		t.Fatalf("missing detail = %d %s, want 404 ANNOUNCEMENT_NOT_FOUND", code, body)
	}

	// v1.1 定向过滤:维度全命中才可见;缺维度不排除;空集合不启用。
	targeted := h.publish(t, useAlt, "iOS 专属活动", "仅 iOS 中国区可见", "INFO", nil, nil,
		map[string][]string{"platform": {"ios"}, "region": {"CN"}}, "", "")
	open := h.publish(t, useAlt, "全量公告", "所有人均可见", "INFO", nil, nil, nil, "", "")
	emptyDim := h.publish(t, useAlt, "空维度公告", "platform 空集合 = 不过滤", "INFO", nil, nil,
		map[string][]string{"platform": {}}, "", "")

	assertIDs := func(query string, want ...string) {
		t.Helper()
		status, body := h.get(t, "/v1/announcements/"+query, token)
		if status != http.StatusOK {
			t.Fatalf("list %s = %d %s", query, status, body)
		}
		var page struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("list decode: %v (%s)", err, body)
		}
		got := map[string]bool{}
		for _, it := range page.Data.Items {
			got[it.ID] = true
		}
		for _, id := range want {
			if !got[id] {
				t.Fatalf("list %s 应含 %s, got %v", query, id, got)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("list %s 条数 = %v, want %d", query, got, len(want))
		}
	}
	assertIDs("", targeted, open, emptyDim)
	assertIDs("?platform=ios&region=CN", targeted, open, emptyDim)
	assertIDs("?platform=android", open, emptyDim)
	assertIDs("?region=CN", targeted, open, emptyDim)

	// 详情:未命中维度 → 404 同码;命中 → 200。
	if code, _ := h.get(t, "/v1/announcements/"+targeted+"?platform=android", token); code != http.StatusNotFound {
		t.Fatalf("未命中详情 = %d, want 404", code)
	}
	if code, _ := h.get(t, "/v1/announcements/"+targeted+"?platform=ios&region=CN", token); code != http.StatusOK {
		t.Fatalf("命中详情 = %d, want 200", code)
	}

	// 分页:再发 5 条(含前置 3 条共 8 条可见)→ limit=2 翻完;每页 ≤2、
	// next cursor 连续、末页 next 空、翻页并集 = 全量且不重复。
	for i := 0; i < 5; i++ {
		h.publish(t, useAlt, fmt.Sprintf("分页%d", i), "b", "INFO", nil, nil, nil, "", "")
	}
	totalVisible := 8
	ids := func(query string) ([]string, string) {
		t.Helper()
		status, body := h.get(t, "/v1/announcements/"+query, token)
		if status != http.StatusOK {
			t.Fatalf("page %s = %d %s", query, status, body)
		}
		var page struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
				NextCursor string `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("page decode: %v", err)
		}
		var got []string
		for _, it := range page.Data.Items {
			got = append(got, it.ID)
		}
		return got, page.Data.NextCursor
	}
	var collected []string
	cursor := 0
	for {
		pageIDs, next := ids(fmt.Sprintf("?limit=2&cursor=%d", cursor))
		if len(pageIDs) == 0 && cursor > 0 {
			t.Fatalf("cursor=%d 不应有空页", cursor)
		}
		if len(pageIDs) > 2 {
			t.Fatalf("页大小 = %d, want <=2", len(pageIDs))
		}
		collected = append(collected, pageIDs...)
		wantNext := fmt.Sprintf("%d", cursor+2)
		if cursor+2 < totalVisible {
			if next != wantNext {
				t.Fatalf("cursor=%d next = %q, want %q", cursor, next, wantNext)
			}
		} else {
			if next != "" {
				t.Fatalf("末页 next = %q, want 空", next)
			}
			break
		}
		cursor += 2
	}
	if len(collected) != totalVisible {
		t.Fatalf("翻页并集 = %d 条, want %d", len(collected), totalVisible)
	}
	seen := map[string]bool{}
	for _, id := range collected {
		if seen[id] {
			t.Fatalf("翻页出现重复 %s", id)
		}
		seen[id] = true
	}
	if !seen[targeted] || !seen[open] || !seen[emptyDim] {
		t.Fatalf("翻页并集缺前置公告")
	}

	// 多语回显(F76):声明 language/groupKey 回显;无声明不出现(omitempty)。
	zh := h.publish(t, useAlt, "维护公告", "今晚 03:00 停机", "WARNING", nil, nil,
		map[string][]string{"language": {"zh-CN"}}, "zh-CN", "maint")
	plain := h.publish(t, useAlt, "全量", "所有语种可见", "INFO", nil, nil, nil, "", "")
	status, body := h.get(t, "/v1/announcements/"+zh+"?language=zh-CN", token)
	if status != http.StatusOK || !strings.Contains(body, `"language":"zh-CN"`) ||
		!strings.Contains(body, `"groupKey":"maint"`) {
		t.Fatalf("多语详情 = %d %s", status, body)
	}
	_, body = h.get(t, "/v1/announcements/"+plain, token)
	if strings.Contains(body, `"language"`) || strings.Contains(body, `"groupKey"`) {
		t.Fatalf("无声明不应出现: %s", body)
	}

	// 可见期:startAt 未来 → 列表不含 + 详情 404(不泄露存在性)。
	future := time.Now().Add(time.Hour)
	scheduled := h.publish(t, useAlt, "未开始", "b", "INFO", &future, nil, nil, "", "")
	status, body = h.get(t, "/v1/announcements/", token)
	if strings.Contains(body, scheduled) {
		t.Fatalf("未到 startAt 不应出现在列表: %s", body)
	}
	if code, _ := h.get(t, "/v1/announcements/"+scheduled, token); code != http.StatusNotFound {
		t.Fatalf("未到 startAt 详情 = %d, want 404", code)
	}
}

// TestAnnouncementsContractConformance 两实现 × 同一契约,同一断言。
func TestAnnouncementsContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"herald", false},
		{"herald-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runAnnouncementsContract(t, newAnnouncementsHarness(t, impl.useAlt), impl.useAlt)
		})
	}
}
