package aggregation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteData(rec, map[string]any{"items": []string{"a"}})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if _, hasData := body["data"]; !hasData {
		t.Fatalf("成功信封应只有 data: %s", rec.Body.String())
	}
	if _, hasErr := body["error"]; hasErr {
		t.Fatalf("成功信封不得含 error: %s", rec.Body.String())
	}
}

func TestWriteError(t *testing.T) {
	cases := []struct {
		code      string
		wantStat  int
		wantRetry bool
	}{
		{CodeCapabilityDisabled, http.StatusNotImplemented, false},
		{CodeRateLimited, http.StatusTooManyRequests, true},
		{CodeInternal, http.StatusInternalServerError, true},
		{CodeInvalidArgument, http.StatusBadRequest, false},
		{CodeNotFound, http.StatusNotFound, false},
		{CodeUnavailable, http.StatusServiceUnavailable, true},
		{CodeTokenExpired, http.StatusUnauthorized, false},
		{CodeMaintenance, http.StatusServiceUnavailable, false},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			WriteError(rec, "4bf92f3577b34da6a3ce929d0e0e4736", c.code, "msg")
			if rec.Code != c.wantStat {
				t.Fatalf("status = %d, 期望 %d", rec.Code, c.wantStat)
			}
			var env struct {
				Error struct {
					Code      string `json:"code"`
					Message   string `json:"message"`
					Retryable bool   `json:"retryable"`
				} `json:"error"`
				TraceID string `json:"traceId"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if env.Error.Code != c.code || env.Error.Retryable != c.wantRetry {
				t.Fatalf("error = %+v, 期望 code=%s retryable=%v", env.Error, c.code, c.wantRetry)
			}
			if env.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
				t.Fatalf("traceId = %q", env.TraceID)
			}
			if env.Error.Message == "" {
				t.Fatal("message 不得为空")
			}
		})
	}
}

func TestWriteErrorUnknownCode(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, "tid", "NOT_A_REGISTERED_CODE", "boom")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("未登记 code 应保守 500, got %d", rec.Code)
	}
	var env struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "NOT_A_REGISTERED_CODE" || !env.Error.Retryable {
		t.Fatalf("error = %+v, 期望保留原 code + retryable=true", env.Error)
	}
}
