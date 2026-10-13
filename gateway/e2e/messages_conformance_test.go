// 批次 68 推送域第二实现一致性 e2e(批次 18 identity / 64 realname / 67 player 先例同构):
// 同一 wire 场景对 chirp(扇出推送)与 chirp-alt(共享环形日志+游标拉取)各跑一遍、
// 同一断言——「推送通道 Provider 可换」有行为证明而非仅接口冻结。
// 两实现异构面(独立缓冲 channel vs 环形日志淘汰)不在本文件断言,
// 由各自单测承载(chirp_test.go 由 e2e/m2 承载 / providers/chirpalt)。
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/chirpalt"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/routing"
)

// messagesHarness 装配 account + herald + croupier + 一个推送实现
// (经路由,与 main 同构;域 Provider 的 Notify 钩子指到该实现的发布口)。
type messagesHarness struct {
	srv      *httptest.Server
	announce *herald.AnnouncementProvider
	croupier *croupier.SupportProvider
}

func newMessagesHarness(t *testing.T, useAlt bool) *messagesHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var messageP providers.Handler
	var publish func(eventType string, data any)
	if useAlt {
		alt := chirpalt.New(chirpalt.Options{RequireAuth: reqAuth})
		messageP = alt
		publish = alt.Publish
	} else {
		chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth})
		messageP = chirpP
		publish = chirpP.Hub().Publish
	}
	if err := reg.Register(messageP); err != nil {
		t.Fatalf("register message provider: %v", err)
	}
	heraldP := herald.New(herald.Options{RequireAuth: reqAuth, Notify: publish})
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth, Notify: publish})
	for _, h := range []providers.Handler{acc, heraldP, croupierP} {
		if err := reg.Register(h); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity:      {Primary: account.DefaultName},
		providers.CapAnnouncements: {Primary: herald.DefaultName},
		providers.CapSupport:       {Primary: croupier.DefaultName},
		providers.CapMessages:      {Primary: messageP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &messagesHarness{srv: srv, announce: heraldP, croupier: croupierP}
}

func (h *messagesHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

func (h *messagesHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"messages-e2e","platform":"linux"}`))
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

// msgEvent SSE 单事件(从帧解析)。
type msgEvent struct {
	ID   int
	Type string
	Data string
}

// openStream 打开 SSE 流;返回事件通道(起手 `: connected` 已消费)。
func (h *messagesHarness) openStream(t *testing.T, token string) (<-chan msgEvent, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, body)
	}
	events := make(chan msgEvent, 16)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var cur msgEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if cur.Type != "" {
					events <- cur
				}
				cur = msgEvent{}
			case strings.HasPrefix(line, "id: "):
				cur.ID, _ = strconv.Atoi(strings.TrimPrefix(line, "id: "))
			case strings.HasPrefix(line, "event: "):
				cur.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events, cancel
}

// awaitMsgEvent 等指定类型的事件(5s 上限,契约验收「5s 内收到」);断言 id 单调递增。
func awaitMsgEvent(t *testing.T, events <-chan msgEvent, lastID int, typ string) msgEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed waiting %s", typ)
			}
			if ev.Type != typ {
				continue
			}
			if ev.ID <= lastID {
				t.Fatalf("id = %d, want > %d(连接内单调)", ev.ID, lastID)
			}
			return ev
		case <-deadline:
			t.Fatalf("超时未收到 %s(5s)", typ)
		}
	}
}

func (h *messagesHarness) post(t *testing.T, path, body, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+path, strings.NewReader(body))
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

// runMessagesContract 推送通道契约链:认证(匿名 401)→ 形状(POST 400 /
// 未知路径 404)→ 订阅起手 → 域事件经 Notify 送达(公告、工单回复)→ id 单调。
func runMessagesContract(t *testing.T, h *messagesHarness) {
	t.Helper()
	token := h.login(t)

	// 匿名 → 401 COMMON_UNAUTHENTICATED(契约 messages.md)。
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("anon stream: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", resp.StatusCode, body)
	}

	// 形状:非 GET → 400;未知路径 → 404。
	if code, _ := h.post(t, "/v1/messages/stream", "", token); code != http.StatusBadRequest {
		t.Fatalf("POST stream = %d, want 400", code)
	}
	req2, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/messages/other", nil)
	h.scopeHeaders(req2)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("GET other: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("GET other = %d, want 404", resp2.StatusCode)
	}

	// 订阅建立 + 域事件送达。
	events, cancel := h.openStream(t, token)
	defer cancel()

	a, err := h.announce.Publish("版本 1.2 已发布", "新增公会战玩法", "INFO", nil, nil, nil, "", "")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	ev := awaitMsgEvent(t, events, 0, "announcement.published")
	var data struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &data); err != nil || data.ID != a.ID || data.Title != "版本 1.2 已发布" {
		t.Fatalf("announcement event data = %+v (%s)", data, ev.Data)
	}

	// 工单回复事件(经 croupier.Notify → 同一发布口)。
	status, bodyStr := h.post(t, "/v1/support/tickets",
		`{"title":"充值未到账","body":"订单 123 未发货","category":"PAYMENT"}`, token)
	if status != http.StatusOK {
		t.Fatalf("create ticket = %d %s", status, bodyStr)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(bodyStr), &created); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	if err := h.croupier.AgentReply(created.Data.ID, "已补发,请重新登录查收"); err != nil {
		t.Fatalf("agent reply: %v", err)
	}
	ev = awaitMsgEvent(t, events, ev.ID, "support.ticket_replied")
	var reply struct {
		TicketID string `json:"ticketId"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &reply); err != nil || reply.TicketID != created.Data.ID {
		t.Fatalf("reply event data = %+v (%s)", reply, ev.Data)
	}
}

// TestMessagesContractConformance 两实现 × 同一契约,同一断言。
func TestMessagesContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"chirp", false},
		{"chirp-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runMessagesContract(t, newMessagesHarness(t, impl.useAlt))
		})
	}
}
