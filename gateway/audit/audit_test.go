// 审计接收面单测:LogSink 每事件一行 JSON;HTTPSink 直写形状(POST/凭证/体)与
// 尽力交付语义(非 2xx / 网络失败 / 超时只记日志不 panic 不阻断);红线——事件
// 体永不携带姓名 / 证件号明文或指纹。留痕触发行为由各 Provider 的测试覆盖
// (warden audit_test.go / wardenalt audit_test.go)。
package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// verifiedOutcome 实名 VERIFIED 状态(契约 realname.md;verify 的 outcome 直接
// 回传状态值,本包不定义实名状态常量,测试用字面量)。
const verifiedOutcome = "VERIFIED"

var auditAt = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

func TestLogSink_OneJSONLinePerEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := LogSink{Logger: log.New(buf, "", 0)}
	sink.Record(AuditEvent{TraceID: "abc", AccountID: "acc_1",
		Action: AuditActionVerify, Outcome: verifiedOutcome, At: auditAt})

	line := strings.TrimSpace(buf.String())
	idx := strings.Index(line, "{")
	if idx < 0 {
		t.Fatalf("缺 JSON 事件体: %q", line)
	}
	var e AuditEvent
	if err := json.Unmarshal([]byte(line[idx:]), &e); err != nil {
		t.Fatalf("事件行非 JSON: %v (%q)", err, line)
	}
	if e.AccountID != "acc_1" || e.Action != AuditActionVerify || e.Outcome != verifiedOutcome {
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
		Action: AuditActionVerify, Outcome: verifiedOutcome, At: auditAt}
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
	sink.Record(httpsinkTestEvent())

	for _, banned := range []string{"张敏", "110101201006201234", "idNumber", "name", "fingerprint"} {
		if strings.Contains(f.body, banned) {
			t.Errorf("请求体携带违禁内容 %q: %s", banned, f.body)
		}
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
