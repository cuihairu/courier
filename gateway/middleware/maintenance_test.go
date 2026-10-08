package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMaintenanceGate 维护门白名单契约(app.md):维护开启 → /v1/* 除 /v1/app/*
// 一律 503 APP_MAINTENANCE;本域端点与 /healthz 放行;维护关闭全放行。
func TestMaintenanceGate(t *testing.T) {
	var on bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	})
	handler := Maintenance(func() bool { return on })(next)

	cases := []struct {
		name string
		path string
		on   bool
		want int
	}{
		{"维护开拦登录", "/v1/identity/guest", true, http.StatusServiceUnavailable},
		{"维护开拦业务", "/v1/support/tickets", true, http.StatusServiceUnavailable},
		{"维护开拦推送新连", "/v1/messages/stream", true, http.StatusServiceUnavailable},
		{"维护开放行本域状态", "/v1/app/maintenance", true, http.StatusOK},
		{"维护开放行本域配置", "/v1/app/config", true, http.StatusOK},
		{"维护开放行探活", "/healthz", true, http.StatusOK},
		{"维护关全放行", "/v1/identity/guest", false, http.StatusOK},
	}
	for _, c := range cases {
		on = c.on
		req := httptest.NewRequest(http.MethodGet, "http://gateway"+c.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Fatalf("%s: status = %d, want %d", c.name, rec.Code, c.want)
		}
	}

	// 503 信封形状:code=APP_MAINTENANCE,traceId 必带(32-hex)。
	on = true
	req := httptest.NewRequest(http.MethodPost, "http://gateway/v1/identity/guest", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		TraceID string `json:"traceId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	if env.Error.Code != "APP_MAINTENANCE" {
		t.Fatalf("code = %s", env.Error.Code)
	}
	if !traceIDRe.MatchString(env.TraceID) {
		t.Fatalf("traceId = %q, want 32-hex(未过 Trace 中间件时现场生成)", env.TraceID)
	}

	// enabled = nil(未装配)→ 永不拦截。
	req2 := httptest.NewRequest(http.MethodGet, "http://gateway/v1/identity/guest", nil)
	rec2 := httptest.NewRecorder()
	Maintenance(nil)(next).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("nil enabled: status = %d", rec2.Code)
	}
}
