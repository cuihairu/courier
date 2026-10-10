// 批次 6(M2)网关级验收:运营发布公告 → SSE 5s 内送达 + 拉取历史可用;
// 推送通道关闭 → 501 降级且拉取兜底;提单 → 坐席回复 → 玩家实时可见。
// 装配与 cmd/gateway/main.go 同构(account + herald/croupier/chirp 共享会话校验)。
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/croupier"
	"github.com/cuihairu/courier/gateway/providers/herald"
	"github.com/cuihairu/courier/gateway/routing"
)

// harness 全链路装配(messagesEnabled=false 时路由表不含 messages)。
type harness struct {
	srv      *httptest.Server
	announce *herald.AnnouncementProvider
	croupier *croupier.SupportProvider
}

func newHarness(t *testing.T, messagesEnabled bool) *harness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth, Heartbeat: 25 * time.Second})
	heraldP := herald.New(herald.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	croupierP := croupier.New(croupier.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	for _, h := range []providers.Handler{acc, heraldP, croupierP, chirpP} {
		if err := reg.Register(h); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity:      {Primary: account.DefaultName},
		providers.CapAnnouncements: {Primary: herald.DefaultName},
		providers.CapSupport:       {Primary: croupier.DefaultName},
	}
	if messagesEnabled {
		cfg[providers.CapMessages] = providers.CapabilityConfig{Primary: chirp.DefaultName}
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &harness{srv: srv, croupier: croupierP, announce: heraldP}
}

// login 游客登录取 token。
func (h *harness) login(t *testing.T) string {
	t.Helper()
	body := `{"deviceId":"e2e-dev","platform":"linux"}`
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(body))
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

func (h *harness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

// sseEvent SSE 单事件(从帧解析)。
type sseEvent struct {
	Type string
	Data string
}

// openStream 打开 SSE 流;返回事件通道(读到首个帧后调用方可确认订阅建立)。
func (h *harness) openStream(t *testing.T, token string) (<-chan sseEvent, <-chan error, context.CancelFunc) {
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
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, body)
	}
	events := make(chan sseEvent, 16)
	errs := make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errs)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var cur sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if cur.Type != "" {
					events <- cur
				}
				cur = sseEvent{}
			case strings.HasPrefix(line, "event: "):
				cur.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
		if err := scanner.Err(); err != nil {
			errs <- err
		}
	}()
	return events, errs, cancel
}

// awaitEvent 等指定类型的事件(5s 上限,契约验收「5s 内收到」)。
func awaitEvent(t *testing.T, events <-chan sseEvent, typ string) sseEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed waiting %s", typ)
			}
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("超时未收到 %s(5s)", typ)
		}
	}
}

// scopeGet 带认证的 GET;返回状态码与 body。
func (h *harness) scopeGet(t *testing.T, path, token string) (int, string) {
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
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// M2 验收一:运营发布公告 → 玩家端 5s 内收到推送并可拉取历史。
func TestAnnouncementPublished_PushesWithin5s_ThenPullHistory(t *testing.T) {
	h := newHarness(t, true)
	token := h.login(t)
	events, _, cancel := h.openStream(t, token)
	defer cancel()

	// 运营侧发布(管理面入口)。chirp handleStream 在 WriteHeader 前同步注册
	// 订阅者,openStream 返回 200 即订阅已建立,无需额外等待。
	a, err := h.announce.Publish("版本 1.2 已发布", "新增公会战玩法", "INFO", nil, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	ev := awaitEvent(t, events, "announcement.published")
	var data struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &data); err != nil {
		t.Fatalf("event data: %v (%s)", err, ev.Data)
	}
	if data.ID != a.ID || data.Title != "版本 1.2 已发布" {
		t.Fatalf("event data mismatch: %+v", data)
	}

	// 拉取历史(通道只是优化,列表是权威)。
	status, body := h.scopeGet(t, "/v1/announcements?limit=20", token)
	if status != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", status, body)
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
		t.Fatalf("list decode: %v", err)
	}
	if len(page.Data.Items) == 0 || page.Data.Items[0].ID != a.ID {
		t.Fatalf("拉取列表未含已发布公告: %s", body)
	}
}

// M2 验收一(反面):推送通道关闭 → stream 501 降级,公告拉取兜底可用。
func TestMessagesDisabled_StreamDegrades_PullFallbackWorks(t *testing.T) {
	h := newHarness(t, false)
	token := h.login(t)

	// 通道降级:501 COMMON_CAPABILITY_DISABLED(结构化信封)。
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request stream: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, body)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
		t.Fatalf("stream body = %s, err = %v", body, err)
	}

	// 拉取兜底:公告列表照常。
	status, pullBody := h.scopeGet(t, "/v1/announcements", token)
	if status != http.StatusOK {
		t.Fatalf("pull fallback status = %d, body = %s", status, pullBody)
	}
}

// M2 验收二:玩家提单 → 坐席回复 → 玩家端实时可见(SSE)+ 详情含回复。
func TestTicketCreated_AgentReply_LiveVisibleInStreamAndDetail(t *testing.T) {
	h := newHarness(t, true)
	token := h.login(t)
	events, _, cancel := h.openStream(t, token)
	defer cancel()

	// 玩家提单。
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/support/tickets",
		strings.NewReader(`{"title":"充值未到账","body":"订单 123 未发货","category":"PAYMENT"}`))
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}
	var created struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("create decode: %v", err)
	}
	resp.Body.Close()
	if created.Data.Status != "OPEN" {
		t.Fatalf("提单后应为 OPEN, got %s", created.Data.Status)
	}

	// 坐席回复(管理面)。
	if err := h.croupier.AgentReply(created.Data.ID, "已补发,请重新登录查收"); err != nil {
		t.Fatalf("agent reply: %v", err)
	}

	// 玩家端实时收到事件。
	ev := awaitEvent(t, events, "support.ticket_replied")
	var data struct {
		TicketID string `json:"ticketId"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &data); err != nil || data.TicketID != created.Data.ID {
		t.Fatalf("reply event data = %s, err = %v", ev.Data, err)
	}

	// 详情:状态 REPLIED + 坐席消息在列。
	status, body := h.scopeGet(t, "/v1/support/tickets/"+created.Data.ID, token)
	if status != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", status, body)
	}
	var detail struct {
		Data struct {
			Ticket struct {
				Status string `json:"status"`
			} `json:"ticket"`
			Messages []struct {
				SenderType string `json:"senderType"`
			} `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("detail decode: %v", err)
	}
	if detail.Data.Ticket.Status != "REPLIED" {
		t.Fatalf("回复后应为 REPLIED, got %s", detail.Data.Ticket.Status)
	}
	last := detail.Data.Messages[len(detail.Data.Messages)-1]
	if last.SenderType != "AGENT" {
		t.Fatalf("最后消息应为 AGENT, got %s", last.SenderType)
	}
}

// 契约端点覆盖:公告详情(GET /v1/announcements/{id})与客服 FAQ(GET /v1/support/faq)。
// 两路由此前既无 provider 单测也不在 e2e 内(herald/croupier 无 *_test.go),此处补齐。
func TestAnnouncementDetailAndSupportFAQ_ContractEndpoints(t *testing.T) {
	h := newHarness(t, true)
	token := h.login(t)

	// 公告详情:命中返回 DTO,缺失/过期同码不泄露存在性(announcement.md)。
	a, err := h.announce.Publish("维护通知", "周三 03:00-05:00 停服", "WARNING", nil, nil)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	status, body := h.scopeGet(t, "/v1/announcements/"+a.ID, token)
	if status != http.StatusOK {
		t.Fatalf("detail status = %d, body = %s", status, body)
	}
	var detail struct {
		Data struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("detail decode: %v", err)
	}
	if detail.Data.ID != a.ID || detail.Data.Title != "维护通知" {
		t.Fatalf("detail mismatch: %+v", detail.Data)
	}

	// 详情反面:不存在的 id → 404 ANNOUNCEMENT_NOT_FOUND。
	status, body = h.scopeGet(t, "/v1/announcements/ann_missing", token)
	if status != http.StatusNotFound || !strings.Contains(body, "ANNOUNCEMENT_NOT_FOUND") {
		t.Fatalf("missing detail: status = %d, body = %s", status, body)
	}

	// 客服 FAQ:关键词检索命中 AddFAQ 注入项。
	h.croupier.AddFAQ("如何充值", "在商城页点击充值按钮", []string{"充值", "商城"})
	status, body = h.scopeGet(t, "/v1/support/faq?keyword=%E5%85%85%E5%80%BC", token)
	if status != http.StatusOK {
		t.Fatalf("faq status = %d, body = %s", status, body)
	}
	var faqPage struct {
		Data struct {
			Items []struct {
				Question string `json:"question"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &faqPage); err != nil {
		t.Fatalf("faq decode: %v", err)
	}
	if len(faqPage.Data.Items) == 0 || faqPage.Data.Items[0].Question != "如何充值" {
		t.Fatalf("faq 未命中注入项: %s", body)
	}
}

// 通道认证:匿名 401(契约 messages.md)。
func TestStreamRequiresAuth_Anonymous401(t *testing.T) {
	h := newHarness(t, true)
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "COMMON_UNAUTHENTICATED") {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}
