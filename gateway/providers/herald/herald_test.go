// herald 单元测试:分页参数校验(limit/cursor)与 fail-closed。
// 公告可见期/定向过滤契约行为由 e2e/announcements_conformance_test.go 对两个实现跑同一场景。
package herald

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
)

var baseTime = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// passThrough 认证中间件放行(list 本身不查身份,由上游链裁决)。
func passThrough(next http.Handler) http.Handler { return next }

func newP(auth func(http.Handler) http.Handler) *AnnouncementProvider {
	opts := Options{Clock: func() time.Time { return baseTime }}
	if auth != nil {
		opts.RequireAuth = auth
	}
	return New(opts)
}

func call(t *testing.T, p *AnnouncementProvider, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

func errCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("信封解析失败: %v body=%s", err, rec.Body.String())
	}
	return env.Error.Code
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := newP(nil) // 未注入 RequireAuth:fail-closed
	if rec := call(t, p, http.MethodGet, "/v1/announcements/", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestInvalidPageParamsRejected(t *testing.T) {
	p := newP(passThrough)
	for _, q := range []string{"limit=0", "limit=abc", "limit=101", "cursor=-1", "cursor=abc"} {
		rec := call(t, p, http.MethodGet, "/v1/announcements/?"+q, "")
		if rec.Code != http.StatusBadRequest || errCode(t, rec) != aggregation.CodeInvalidArgument {
			t.Fatalf("?%s: %d/%s, want 400/%s", q, rec.Code, errCode(t, rec), aggregation.CodeInvalidArgument)
		}
	}
}

func TestListPublished(t *testing.T) {
	p := newP(passThrough)
	if _, err := p.Publish("hello", "body", "INFO", nil, nil, nil, "", ""); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	rec := call(t, p, http.MethodGet, "/v1/announcements/?limit=1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "hello") {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
}
