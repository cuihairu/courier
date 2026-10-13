// chirpalt 单元测试:wire 面与 chirp 同形状(订阅点后送达、id 连接内单调、
// 心跳注释帧、认证、路由形状)+ 枢纽模型异构面(环形日志淘汰、慢消费者
// 跳窗 = at-most-once 缺失段不补发)。
package chirpalt

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

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

// sseClient 读 SSE 原始行(空行帧分隔跳过)。
type sseClient struct {
	lines  chan string
	errs   chan error
	cancel context.CancelFunc
}

func openSSE(t *testing.T, url string) *sseClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		t.Fatalf("stream status = %d", resp.StatusCode)
	}
	c := &sseClient{lines: make(chan string, 64), errs: make(chan error, 1), cancel: cancel}
	go func() {
		defer close(c.lines)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			c.lines <- scanner.Text()
		}
		if err := scanner.Err(); err != nil {
			c.errs <- err
		}
	}()
	// 订阅建立:首个注释行(: connected)。
	if line := c.next(t); line != ": connected" {
		t.Fatalf("起手帧 = %q, want : connected", line)
	}
	return c
}

// next 读下一非空行(空行为帧分隔,跳过)。
func (c *sseClient) next(t *testing.T) string {
	t.Helper()
	for {
		select {
		case line, ok := <-c.lines:
			if !ok {
				t.Fatal("stream closed")
			}
			if line != "" {
				return line
			}
		case err := <-c.errs:
			t.Fatalf("stream read: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("超时未读到帧(5s)")
		}
	}
}

func (c *sseClient) close() { c.cancel() }

func TestCapabilityAndName(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod")})
	if p.Name() != DefaultName || p.Capability() != providers.CapMessages {
		t.Fatalf("name/capability = %s/%v", p.Name(), p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

// TestStreamDeliversEventsInOrder 先开流(空日志)再发布:事件按发布序送达,
// id 连接内单调(空日志订阅点回归:游标 0 起,首条事件序号 1)。
func TestStreamDeliversEventsInOrder(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod"),
		Heartbeat: time.Second, Poll: 5 * time.Millisecond})
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	c := openSSE(t, srv.URL+"/v1/messages/stream") // 空日志开流(订阅点回归)
	defer c.close()

	p.Publish("announcement.published", map[string]string{"id": "a1"})
	p.Publish("support.ticket_replied", map[string]string{"ticketId": "t1"})

	want := []struct {
		typ  string
		data string
	}{
		{"announcement.published", `"id":"a1"`},
		{"support.ticket_replied", `"ticketId":"t1"`},
	}
	for i, w := range want {
		if idLine := c.next(t); idLine != "id: "+strconv.Itoa(i+1) {
			t.Fatalf("id 帧 = %q, want id: %d(连接内单调)", idLine, i+1)
		}
		if ev := c.next(t); ev != "event: "+w.typ {
			t.Fatalf("event = %q, want %s", ev, w.typ)
		}
		if data := c.next(t); !strings.Contains(data, w.data) {
			t.Fatalf("data = %q, want 含 %s", data, w.data)
		}
	}
}

// TestSubscribePointSemantics 订阅点前发布的不送(与 chirp 同语义:只送订阅后
// 发生的事件)。
func TestSubscribePointSemantics(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod"),
		Heartbeat: time.Second, Poll: 5 * time.Millisecond})
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	p.Publish("evt.before", map[string]int{"n": 0}) // 订阅前:不送
	c := openSSE(t, srv.URL+"/v1/messages/stream")
	defer c.close()

	p.Publish("evt.after", map[string]int{"n": 1})
	if idLine := c.next(t); idLine != "id: 1" {
		t.Fatalf("id 帧 = %q, want id: 1(订阅点前事件不送)", idLine)
	}
	if ev := c.next(t); ev != "event: evt.after" {
		t.Fatalf("event = %q, want evt.after", ev)
	}
}

// TestSlowConsumerSkipsGap 异构面:环形日志容量 4,订阅后连发 10 条(拉取
// 间隔内发布完毕)→ 环里只剩最后 4 条;游标落后出窗 = 跳过缺失段,送达的是
// 窗口内 4 条、顺序不乱、id 连续(at-most-once,不补发)。
func TestSlowConsumerSkipsGap(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod"),
		Heartbeat: time.Second, Poll: 200 * time.Millisecond, RingSize: 4})
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	c := openSSE(t, srv.URL+"/v1/messages/stream")
	defer c.close()

	for i := 1; i <= 10; i++ { // 200ms 拉取间隔内发完:首拍只剩窗口 4 条
		p.Publish("evt.n", map[string]int{"n": i})
	}

	for want := 7; want <= 10; want++ {
		if idLine := c.next(t); idLine != "id: "+strconv.Itoa(want-6) {
			t.Fatalf("id 帧 = %q, want id: %d(窗口内连续)", idLine, want-6)
		}
		if ev := c.next(t); ev != "event: evt.n" {
			t.Fatalf("event = %q, want evt.n", ev)
		}
		if data := c.next(t); !strings.Contains(data, `"n":`+strconv.Itoa(want)) {
			t.Fatalf("应送达窗口内 n=%d, got %q", want, data)
		}
	}
	// 窗口外不应再有事件帧(缺失段不补发)。
	deadline := time.After(600 * time.Millisecond)
	extra := 0
loop:
	for {
		select {
		case line := <-c.lines:
			if strings.HasPrefix(line, "event: ") {
				extra++
			}
		case <-deadline:
			break loop
		}
	}
	if extra != 0 {
		t.Fatalf("窗口外不应有事件帧: %d", extra)
	}
}

// TestHeartbeatCommentFrame 心跳为注释行 `: ping`(契约 25s;测试调短验证)。
func TestHeartbeatCommentFrame(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod"),
		Heartbeat: 30 * time.Millisecond, Poll: time.Second})
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	c := openSSE(t, srv.URL+"/v1/messages/stream")
	defer c.close()
	for {
		if line := c.next(t); line == ": ping" {
			return
		}
	}
}

func TestFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{}) // 未注入 RequireAuth:fail-closed
	req, _ := http.NewRequest(http.MethodGet, "/v1/messages/stream", nil)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestRoutingShape(t *testing.T) {
	p := New(Options{RequireAuth: withAccountScope("acc_1", "game_demo", "prod")})
	post := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/messages/stream", nil)
	p.ServeHTTP(post, req)
	if post.Code != http.StatusBadRequest {
		t.Fatalf("POST stream: %d, want 400(stream requires GET)", post.Code)
	}
	other := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodGet, "/v1/messages/other", nil)
	p.ServeHTTP(other, req2)
	if other.Code != http.StatusNotFound {
		t.Fatalf("GET other: %d, want 404", other.Code)
	}
}
