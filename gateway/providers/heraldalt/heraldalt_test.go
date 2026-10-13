// heraldalt 单元测试:wire 面与 herald 同形状(发布校验、可见期、定向过滤、
// 分页、详情同码不泄露、多语回显、认证、路由形状)+ 存储模型异构面(写时
// 有序索引:同刻发布按 ID 升序插入,倒序输出 ID 降序)。
package heraldalt

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

// withAccountScope 测试用认证中间件(与 archivistalt 同构)。
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
	if p.Name() != DefaultName || p.Capability() != providers.CapAnnouncements {
		t.Fatalf("name/capability = %s/%v", p.Name(), p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestPublishValidation(t *testing.T) {
	p := newP("acc_1")
	bad := func(name, title, body, severity string) {
		t.Helper()
		if _, err := p.Publish(title, body, severity, nil, nil, nil, "", ""); err == nil {
			t.Fatalf("%s: 应被拒", name)
		}
	}
	bad("空标题", "", "b", "INFO")
	bad("超长标题", strings.Repeat("标", 121), "b", "INFO")
	bad("空正文", "t", "", "INFO")
	bad("超长正文", "t", strings.Repeat("正", 4001), "INFO")
	bad("非法 severity", "t", "b", "DEBUG")
	// 边界内合法:120 字符标题 + 4000 字符正文。
	if _, err := p.Publish(strings.Repeat("标", 120), strings.Repeat("正", 4000), "INFO", nil, nil, nil, "", ""); err != nil {
		t.Fatalf("边界内应合法: %v", err)
	}
	// endAt 早于 startAt。
	start := baseTime.Add(time.Hour)
	end := baseTime.Add(-time.Hour)
	if _, err := p.Publish("t", "b", "INFO", &start, &end, nil, "", ""); err == nil {
		t.Fatal("endAt 早于 startAt 应被拒")
	}
}

// TestListOrderingAndVisibility 发布序与输出序:publishedAt 倒序;可见期
// (startAt 未来 / endAt 过去)排除。
func TestListOrderingAndVisibility(t *testing.T) {
	p := newP("acc_1")
	step := 0
	p.clock = func() time.Time { step++; return baseTime.Add(time.Duration(step) * time.Minute) }
	future := baseTime.Add(time.Hour)
	expiredEnd := baseTime.Add(-time.Minute)

	if _, err := p.Publish("甲", "b", "INFO", nil, nil, nil, "", ""); err != nil {
		t.Fatalf("publish 甲: %v", err)
	}
	if _, err := p.Publish("乙", "b", "INFO", nil, nil, nil, "", ""); err != nil {
		t.Fatalf("publish 乙: %v", err)
	}
	if _, err := p.Publish("未开始", "b", "INFO", &future, nil, nil, "", ""); err != nil {
		t.Fatalf("publish 未开始: %v", err)
	}
	if _, err := p.Publish("已结束", "b", "INFO", nil, &expiredEnd, nil, "", ""); err != nil {
		t.Fatalf("publish 已结束: %v", err)
	}

	rec := call(t, p, http.MethodGet, "/v1/announcements/", "")
	if rec.Code != 200 {
		t.Fatalf("list: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 倒序:乙在甲前;未开始/已结束不出现。
	if !strings.Contains(body, `"title":"乙"`) || !strings.Contains(body, `"title":"甲"`) {
		t.Fatalf("列表缺可见公告: %s", body)
	}
	if strings.Contains(body, "未开始") || strings.Contains(body, "已结束") {
		t.Fatalf("可见期外不应出现: %s", body)
	}
	if strings.Index(body, `"title":"乙"`) > strings.Index(body, `"title":"甲"`) {
		t.Fatalf("应 publishedAt 倒序: %s", body)
	}
}

// TestListFiltersMatching 定向过滤(v1.1):维度全命中才可见;请求缺维度 =
// 不排除;空集合维度不启用;未命中详情同码 404 不泄露存在性。
func TestListFiltersMatching(t *testing.T) {
	p := newP("acc_1")
	targeted, err := p.Publish("iOS 专属", "b", "INFO", nil, nil,
		map[string][]string{"platform": {"ios"}, "region": {"CN"}}, "", "")
	if err != nil {
		t.Fatalf("publish targeted: %v", err)
	}
	if _, err := p.Publish("全量", "b", "INFO", nil, nil, nil, "", ""); err != nil {
		t.Fatalf("publish open: %v", err)
	}
	if _, err := p.Publish("空维度", "b", "INFO", nil, nil,
		map[string][]string{"platform": {}}, "", ""); err != nil {
		t.Fatalf("publish emptyDim: %v", err)
	}

	list := func(query string) string {
		t.Helper()
		rec := call(t, p, http.MethodGet, "/v1/announcements/"+query, "")
		if rec.Code != 200 {
			t.Fatalf("list %s: %d", query, rec.Code)
		}
		return rec.Body.String()
	}
	has := func(body string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(body, w) {
				t.Fatalf("应含 %s: %s", w, body)
			}
		}
	}
	has(list(""), "iOS 专属", "全量", "空维度")
	has(list("?platform=ios&region=CN"), "iOS 专属", "全量", "空维度")
	has(list("?platform=android"), "全量", "空维度")    // 定向公告被排除
	has(list("?region=CN"), "iOS 专属", "全量", "空维度") // 缺 platform 不排除

	// 详情:未命中维度 → 404 同码;命中 → 200。
	detail := func(id, query string) int {
		t.Helper()
		return call(t, p, http.MethodGet, "/v1/announcements/"+id+query, "").Code
	}
	if detail(targeted.ID, "?platform=android") != http.StatusNotFound {
		t.Fatalf("未命中详情应 404")
	}
	if detail(targeted.ID, "?platform=ios&region=CN") != http.StatusOK {
		t.Fatalf("命中详情应 200")
	}
	if detail("ann_missing", "") != http.StatusNotFound {
		t.Fatalf("不存在详情应 404")
	}
}

// TestListPagination limit/cursor/next cursor 语义(与 herald 逐条一致)。
func TestListPagination(t *testing.T) {
	p := newP("acc_1")
	step := 0
	p.clock = func() time.Time { step++; return baseTime.Add(time.Duration(step) * time.Minute) }
	for i := 0; i < 5; i++ {
		if _, err := p.Publish(fmt.Sprintf("公告%d", i), "b", "INFO", nil, nil, nil, "", ""); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	page := func(query string) (int, string) {
		t.Helper()
		rec := call(t, p, http.MethodGet, "/v1/announcements/"+query, "")
		if rec.Code != 200 {
			t.Fatalf("list %s: %d", query, rec.Code)
		}
		return rec.Code, rec.Body.String()
	}
	_, first := page("?limit=2")
	if !strings.Contains(first, "公告4") || !strings.Contains(first, "公告3") {
		t.Fatalf("首页应为最新两条: %s", first)
	}
	if !strings.Contains(first, `"nextCursor":"2"`) {
		t.Fatalf("首页应有 nextCursor=2: %s", first)
	}
	_, second := page("?limit=2&cursor=2")
	if !strings.Contains(second, "公告2") || !strings.Contains(second, "公告1") {
		t.Fatalf("第二页应为 公告2/公告1: %s", second)
	}
	if !strings.Contains(second, `"nextCursor":"4"`) {
		t.Fatalf("第二页应有 nextCursor=4: %s", second)
	}
	_, last := page("?limit=2&cursor=4")
	if !strings.Contains(last, "公告0") {
		t.Fatalf("末页应为 公告0: %s", last)
	}
	if !strings.Contains(last, `"nextCursor":""`) {
		t.Fatalf("末页 nextCursor 应为空串: %s", last)
	}
	// cursor 越界 → 空列表。
	_, over := page("?limit=2&cursor=99")
	if !strings.Contains(over, `"items":[]`) {
		t.Fatalf("越界 cursor 应空列表: %s", over)
	}
	// 非法 limit/cursor → 400。
	if code := call(t, p, http.MethodGet, "/v1/announcements/?limit=0", "").Code; code != http.StatusBadRequest {
		t.Fatalf("limit=0 应 400: %d", code)
	}
	if code := call(t, p, http.MethodGet, "/v1/announcements/?cursor=-1", "").Code; code != http.StatusBadRequest {
		t.Fatalf("cursor=-1 应 400: %d", code)
	}
}

// TestDetailLanguageEcho 多语声明回显(F76):命中详情回显 language/groupKey;
// 无声明字段不出现(omitempty)。
func TestDetailLanguageEcho(t *testing.T) {
	p := newP("acc_1")
	zh, err := p.Publish("维护公告", "b", "WARNING", nil, nil,
		map[string][]string{"language": {"zh-CN"}}, "zh-CN", "maint")
	if err != nil {
		t.Fatalf("publish zh: %v", err)
	}
	plain, err := p.Publish("全量", "b", "INFO", nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("publish plain: %v", err)
	}
	rec := call(t, p, http.MethodGet, "/v1/announcements/"+zh.ID+"?language=zh-CN", "")
	body := rec.Body.String()
	if !strings.Contains(body, `"language":"zh-CN"`) || !strings.Contains(body, `"groupKey":"maint"`) {
		t.Fatalf("应回显 language/groupKey: %s", body)
	}
	rec = call(t, p, http.MethodGet, "/v1/announcements/"+plain.ID, "")
	if strings.Contains(rec.Body.String(), `"language"`) || strings.Contains(rec.Body.String(), `"groupKey"`) {
		t.Fatalf("无声明不应出现: %s", rec.Body.String())
	}
}

// TestWriteTimeIndex 异构面:同刻发布多条,写时按 ID 升序插入有序切片,
// 倒序输出 = ID 降序(与 herald 读时排序同输出)。
func TestWriteTimeIndex(t *testing.T) {
	p := newP("acc_1")
	// 同刻(同一 clock)发布三条:ID 由 UUIDv7 时间序 + 随机位决定,插入序
	// 按 ID 升序维护;倒序遍历输出 ID 降序。
	var ids [3]string
	for i := range ids {
		a, err := p.Publish(fmt.Sprintf("同刻%d", i), "b", "INFO", nil, nil, nil, "", "")
		if err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
		ids[i] = a.ID
	}
	p.mu.RLock()
	asc := make([]string, len(p.ordered))
	for i, a := range p.ordered {
		asc[i] = a.ID
	}
	p.mu.RUnlock()
	if !(asc[0] < asc[1] && asc[1] < asc[2]) {
		t.Fatalf("写时索引应 ID 升序: %v", asc)
	}
	rec := call(t, p, http.MethodGet, "/v1/announcements/?limit=3", "")
	body := rec.Body.String()
	// 倒序输出:ID 降序 = 与升序相反。
	if strings.Index(body, asc[2]) > strings.Index(body, asc[1]) ||
		strings.Index(body, asc[1]) > strings.Index(body, asc[0]) {
		t.Fatalf("输出应 ID 降序: %s", body)
	}
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{Clock: func() time.Time { return baseTime }}) // 未注入 RequireAuth
	if rec := call(t, p, http.MethodGet, "/v1/announcements/", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestRoutingShape(t *testing.T) {
	p := newP("acc_1")
	if rec := call(t, p, http.MethodPost, "/v1/announcements/", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST list: %d, want 400", rec.Code)
	}
	if rec := call(t, p, http.MethodPost, "/v1/announcements/ann_x", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("POST detail: %d, want 400", rec.Code)
	}
	if rec := call(t, p, http.MethodGet, "/v1/announcements/ann_x/extra", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("未知路径: %d, want 404", rec.Code)
	}
}
