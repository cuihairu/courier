// croupier 单元测试:HTTP 面 fail-closed、无身份写路径、CreateTicket 运维入口。
// 工单全生命周期契约行为由 e2e/support_conformance_test.go 对两个实现跑同一场景。
package croupier

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

// passThrough 认证中间件放行但不注入身份(模拟上游认证漏过)。
func passThrough(next http.Handler) http.Handler { return next }

func newP(auth func(http.Handler) http.Handler) *SupportProvider {
	opts := Options{Clock: func() time.Time { return baseTime }}
	if auth != nil {
		opts.RequireAuth = auth
	}
	return New(opts)
}

func call(t *testing.T, p *SupportProvider, method, path, body string) *httptest.ResponseRecorder {
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
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/v1/support/tickets"},
		{http.MethodGet, "/v1/support/tickets"},
		{http.MethodGet, "/v1/support/faq"},
	} {
		if rec := call(t, p, tc.method, tc.path, "{}"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: status = %d, want 401(fail-closed)", tc.method, tc.path, rec.Code)
		}
	}
}

func TestUnauthenticatedPaths(t *testing.T) {
	p := newP(passThrough) // 放行但无身份:提单/追加/列表必须 401
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/support/tickets", `{"title":"t","body":"b"}`},
		{http.MethodPost, "/v1/support/tickets/tkt_x/messages", `{"body":"b"}`},
		{http.MethodGet, "/v1/support/tickets", ""},
	} {
		rec := call(t, p, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusUnauthorized || errCode(t, rec) != aggregation.CodeUnauthenticated {
			t.Fatalf("%s %s: %d/%s, want 401/%s", tc.method, tc.path, rec.Code, errCode(t, rec),
				aggregation.CodeUnauthenticated)
		}
	}
}

func TestCreateTicketOpsEntry(t *testing.T) {
	p := newP(passThrough)
	tk, err := p.CreateTicket("acc_1", "title", "body", "OTHER")
	if err != nil || tk.ID == "" || tk.Status != "OPEN" {
		t.Fatalf("CreateTicket = %+v err=%v", tk, err)
	}
	if _, err := p.CreateTicket("acc_1", "", "b", ""); err == nil {
		t.Fatal("空 title 应拒")
	}
	if _, err := p.CreateTicket("acc_1", strings.Repeat("t", 121), "b", ""); err == nil {
		t.Fatal("超长 title 应拒")
	}

	if err := p.AgentReply(tk.ID, "reply"); err != nil {
		t.Fatalf("AgentReply: %v", err)
	}
	if err := p.AgentClose(tk.ID); err != nil {
		t.Fatalf("AgentClose: %v", err)
	}
	p.mu.RLock()
	status := p.tickets[tk.ID].status
	p.mu.RUnlock()
	if status != "CLOSED" {
		t.Fatalf("回单后 status = %s, want CLOSED", status)
	}

	if err := p.AgentClose("tkt_missing"); err == nil {
		t.Fatal("不存在工单应拒")
	}
}
