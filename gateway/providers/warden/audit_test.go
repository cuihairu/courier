// 审计留痕半边(F33)验收:关键动作产出事件、nil sink 零产出、S2S 鉴权失败与
// 入参非法不留痕,以及 realname.md 红线——事件永不携带姓名/证件号明文或指纹。
package warden

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureSink 捕获事件留断言用。
type captureSink struct{ events []AuditEvent }

func (s *captureSink) Record(e AuditEvent) { s.events = append(s.events, e) }

func newWardenWithSink(clock func() time.Time, sink AuditSink) *Warden {
	return New(Options{Clock: clock, RequireAuth: newAuthW, S2SToken: "s2s", AuditSink: sink})
}

// hitAll 走一遍四个留痕端点(verify 用未成年证件触发 curfew/charge 的 denied 面)。
func hitAll(t *testing.T, w *Warden) {
	t.Helper()
	code, _ := post(t, w, "/v1/realname/verify", "tok", `{"name":"张敏","idNumber":"`+minorID+`"}`)
	if code != 200 {
		t.Fatalf("verify: %d", code)
	}
	if code, _ := get(t, w, "/v1/realname/curfew", "tok"); code != 200 {
		t.Fatalf("curfew: %d", code)
	}
	if code, _ := post(t, w, "/v1/realname/charge-check", "tok", `{"amountCents":100}`); code != 200 {
		t.Fatalf("charge-check: %d", code)
	}
	if code, _ := post(t, w, "/v1/realname/playtime-report", "s2s",
		`{"accountId":"acc_1","date":"2026-10-08","playMinutes":30,"chargeAmountCents":0}`); code != 200 {
		t.Fatalf("playtime-report: %d", code)
	}
}

func TestAudit_KeyActionsEmitted(t *testing.T) {
	sink := &captureSink{}
	w := newWardenWithSink(fixedClock(nowThursday), sink)
	hitAll(t, w)

	want := []struct{ action, outcome string }{
		{AuditActionVerify, stateVerified},
		{AuditActionCurfew, AuditOutcomeDenied}, // 未成年 @周四 15:00 → 不可玩
		{AuditActionChargeCheck, AuditOutcomeAllowed},
		{AuditActionPlaytimeReport, AuditOutcomeOK},
	}
	if len(sink.events) != len(want) {
		t.Fatalf("事件数: %d, want %d (%v)", len(sink.events), len(want), sink.events)
	}
	for i, e := range sink.events {
		if e.Action != want[i].action || e.Outcome != want[i].outcome {
			t.Errorf("事件[%d] = %s/%s, want %s/%s", i, e.Action, e.Outcome, want[i].action, want[i].outcome)
		}
		if e.AccountID != "acc_1" {
			t.Errorf("事件[%d] 账号 = %q", i, e.AccountID)
		}
		if !e.At.Equal(nowThursday) {
			t.Errorf("事件[%d] At = %v, want Clock 口径 %v", i, e.At, nowThursday)
		}
		// TraceID 恒非空(未经中间件时现场生成,32 位 hex)。
		if b, err := hex.DecodeString(e.TraceID); err != nil || len(b) != 16 {
			t.Errorf("事件[%d] TraceID = %q", i, e.TraceID)
		}
	}
}

func TestAudit_StatusIsPureRead_NoEvent(t *testing.T) {
	sink := &captureSink{}
	w := newWardenWithSink(fixedClock(nowThursday), sink)
	if code, _ := get(t, w, "/v1/realname/status", "tok"); code != 200 {
		t.Fatalf("status: %d", code)
	}
	if len(sink.events) != 0 {
		t.Fatalf("纯读不应留痕: %v", sink.events)
	}
}

func TestAudit_NilSink_NoPanicNoEvents(t *testing.T) {
	w := New(Options{Clock: fixedClock(nowThursday), RequireAuth: newAuthW, S2SToken: "s2s"})
	hitAll(t, w) // nil sink:不产出也不 panic
}

func TestAudit_AuthFailureAndInvalidInput_NoEvent(t *testing.T) {
	sink := &captureSink{}
	w := newWardenWithSink(fixedClock(nowThursday), sink)
	// S2S 凭证错误 → 401,动作未发生,不留痕。
	if code, _ := post(t, w, "/v1/realname/playtime-report", "wrong",
		`{"accountId":"acc_1","date":"2026-10-08","playMinutes":1}`); code != 401 {
		t.Fatalf("s2s 401: %d", code)
	}
	// 入参非法挡在边缘(400):结构化错误 + trace 已覆盖,不产生审计事件。
	if code, _ := post(t, w, "/v1/realname/verify", "tok", `{"name":"","idNumber":"x"}`); code != 400 {
		t.Fatalf("verify 400: %d", code)
	}
	if code, _ := post(t, w, "/v1/realname/charge-check", "tok", `{"amountCents":-1}`); code != 400 {
		t.Fatalf("charge-check 400: %d", code)
	}
	if len(sink.events) != 0 {
		t.Fatalf("失败路径不应留痕: %v", sink.events)
	}
}

// 红线(realname.md):审计事件只含账号维度元数据——姓名/证件号明文与
// SHA-256 指纹都不得出现在事件及其序列化形状里。
func TestAudit_RedLine_NeverCarriesIdentityInput(t *testing.T) {
	sink := &captureSink{}
	w := newWardenWithSink(fixedClock(nowThursday), sink)
	if code, _ := post(t, w, "/v1/realname/verify", "tok",
		`{"name":"张敏","idNumber":"`+minorID+`"}`); code != 200 {
		t.Fatalf("verify: %d", code)
	}
	if len(sink.events) != 1 {
		t.Fatalf("事件数: %d", len(sink.events))
	}
	encoded, err := json.Marshal(sink.events[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	shape := string(encoded)
	for _, banned := range []string{"张敏", minorID, "idNumber", "name", "fingerprint"} {
		if strings.Contains(shape, banned) {
			t.Errorf("事件携带违禁内容 %q: %s", banned, shape)
		}
	}
}

func TestLogSink_OneJSONLinePerEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := LogSink{Logger: log.New(buf, "", 0)}
	sink.Record(AuditEvent{TraceID: "abc", AccountID: "acc_1",
		Action: AuditActionVerify, Outcome: stateVerified, At: nowThursday})

	line := strings.TrimSpace(buf.String())
	idx := strings.Index(line, "{")
	if idx < 0 {
		t.Fatalf("缺 JSON 事件体: %q", line)
	}
	var e AuditEvent
	if err := json.Unmarshal([]byte(line[idx:]), &e); err != nil {
		t.Fatalf("事件行非 JSON: %v (%q)", err, line)
	}
	if e.AccountID != "acc_1" || e.Action != AuditActionVerify || e.Outcome != stateVerified {
		t.Fatalf("事件回读: %+v", e)
	}
	if !strings.Contains(line, `"traceId":"abc"`) {
		t.Fatalf("缺 traceId: %q", line)
	}
}

// httpsinkFixture 记录 HTTPSink 收到的请求。
type httpsinkFixture struct {
	method, path, auth, contentType string
	event                           AuditEvent
	body                            string
	hits                            int
}

func newHTTPSinkServer(t *testing.T, f *httpsinkFixture, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		f.method = r.Method
		f.path = r.URL.Path
		f.auth = r.Header.Get("Authorization")
		f.contentType = r.Header.Get("Content-Type")
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		f.body = string(b)
		var e AuditEvent
		if err := json.Unmarshal(b, &e); err == nil {
			f.event = e
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func httpsinkTestEvent() AuditEvent {
	return AuditEvent{TraceID: "abc", AccountID: "acc_1",
		Action: AuditActionVerify, Outcome: stateVerified, At: nowThursday}
}

func TestHTTPSink_PostsJSONWithBearer(t *testing.T) {
	f := &httpsinkFixture{}
	srv := newHTTPSinkServer(t, f, http.StatusNoContent)

	buf := &bytes.Buffer{}
	sink := HTTPSink{
		Endpoint: srv.URL + "/events", Token: "tok_123",
		Logger: log.New(buf, "", 0),
	}
	want := httpsinkTestEvent()
	sink.Record(want)

	if f.hits != 1 {
		t.Fatalf("命中数: %d", f.hits)
	}
	if f.method != http.MethodPost || f.path != "/events" {
		t.Errorf("请求: %s %s", f.method, f.path)
	}
	if f.contentType != "application/json" {
		t.Errorf("Content-Type = %q", f.contentType)
	}
	if f.auth != "Bearer tok_123" {
		t.Errorf("Authorization = %q", f.auth)
	}
	if f.event != want {
		t.Errorf("事件回读: %+v, want %+v", f.event, want)
	}
	if strings.TrimSpace(f.body) != string(mustJSON(t, want)) {
		t.Errorf("请求体 = %q", f.body)
	}
}

func TestHTTPSink_BestEffort_ServerErrorStillNoPanic(t *testing.T) {
	f := &httpsinkFixture{}
	srv := newHTTPSinkServer(t, f, http.StatusInternalServerError)

	buf := &bytes.Buffer{}
	sink := HTTPSink{Endpoint: srv.URL, Logger: log.New(buf, "", 0)}
	sink.Record(httpsinkTestEvent()) // 非 2xx:不 panic、不阻断

	if f.hits != 1 {
		t.Fatalf("命中数: %d", f.hits)
	}
	if !strings.Contains(buf.String(), "status 500") {
		t.Errorf("失败应记日志: %q", buf.String())
	}
}

func TestHTTPSink_BestEffort_NetworkFailureStillNoPanic(t *testing.T) {
	buf := &bytes.Buffer{}
	// 端口未监听 → 连接被拒;Record 必须安静返回,不 panic。
	sink := HTTPSink{Endpoint: "http://127.0.0.1:1/x", Logger: log.New(buf, "", 0)}
	sink.Record(httpsinkTestEvent())
	if !strings.Contains(buf.String(), "courier realname audit sink") {
		t.Errorf("失败应记日志: %q", buf.String())
	}
}

func TestHTTPSink_EmptyEndpoint_NoOp(t *testing.T) {
	sink := HTTPSink{Endpoint: ""}
	sink.Record(httpsinkTestEvent()) // 无端点 = 不产出,零 HTTP
}

func TestHTTPSink_TimeoutBounded(t *testing.T) {
	// 慢服务器 + 50ms 客户端超时:Record 必须快速返回(不拖垮请求路径)。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	sink := HTTPSink{Endpoint: srv.URL, Client: &http.Client{Timeout: 50 * time.Millisecond}}
	start := time.Now()
	sink.Record(httpsinkTestEvent())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时未生效: %v", elapsed)
	}
}

// 红线同 LogSink:HTTPSink 的 POST 体永不携带姓名/证件号明文或指纹。
func TestHTTPSink_RedLine_NeverCarriesIdentityInput(t *testing.T) {
	f := &httpsinkFixture{}
	srv := newHTTPSinkServer(t, f, http.StatusNoContent)

	sink := HTTPSink{Endpoint: srv.URL, Logger: log.New(io.Discard, "", 0)}
	sink.Record(AuditEvent{TraceID: "abc", AccountID: "acc_1",
		Action: AuditActionVerify, Outcome: stateVerified, At: nowThursday})

	for _, banned := range []string{"张敏", minorID, "idNumber", "name", "fingerprint"} {
		if strings.Contains(f.body, banned) {
			t.Errorf("请求体携带违禁内容 %q: %s", banned, f.body)
		}
	}
}

// sink 失败不得反向打断合规动作:verify 在 sink 报错时仍 200。
func TestAudit_FailingSink_DoesNotBreakVerifyPath(t *testing.T) {
	w := New(Options{
		Clock: fixedClock(nowThursday), RequireAuth: newAuthW, S2SToken: "s2s",
		AuditSink: HTTPSink{Endpoint: "http://127.0.0.1:1/x", Logger: log.New(io.Discard, "", 0)},
	})
	if code, _ := post(t, w, "/v1/realname/verify", "tok",
		`{"name":"张敏","idNumber":"`+minorID+`"}`); code != 200 {
		t.Fatalf("sink 失败不应影响 verify: %d", code)
	}
}

func mustJSON(t *testing.T, e AuditEvent) []byte {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
