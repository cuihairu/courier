// 批次 10(M5)网关级验收:支付全链——服务端定价下单(只收 skuId)、渠道回调
// HMAC 签名面(签名即认证,伪造/错钥/篡改一律 403)、CREATED→PAID→DELIVERED
// 状态机、回调幂等不二次发货、发货失败停 PAID 经管理面对账恢复、风控前置拒绝。
// 装配与 cmd/gateway/main.go 同构(account + teller)。
package e2e

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/cuihairu/courier/gateway/providers/teller"
	"github.com/cuihairu/courier/gateway/routing"
)

const m5ChannelSecret = "e2e_pay_secret"

// m5Harness M5 装配:teller(payments)+ account。
type m5Harness struct {
	srv      *httptest.Server
	teller   *teller.PaymentsProvider
	delivers []teller.Delivery
}

func newM5Harness(t *testing.T, deliver func(teller.Delivery) error, risk func(string, int64) error) *m5Harness {
	t.Helper()
	h := &m5Harness{}
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth, Heartbeat: time.Second})
	tellerP := teller.New(teller.Options{
		RequireAuth:   reqAuth,
		ChannelSecret: m5ChannelSecret,
		Notify:        chirpP.Hub().Publish,
		Deliver: func(d teller.Delivery) error {
			h.delivers = append(h.delivers, d)
			if deliver != nil {
				return deliver(d)
			}
			return nil
		},
		RiskCheck: risk,
	})
	h.teller = tellerP
	for _, hd := range []providers.Handler{acc, chirpP, tellerP} {
		if err := reg.Register(hd); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapMessages: {Primary: chirp.DefaultName},
		providers.CapPayments: {Primary: tellerP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	h.srv = srv
	return h
}

// login 游客登录取 token(复用 M4 同构链路)。
func (h *m5Harness) login(t *testing.T, deviceId string) string {
	t.Helper()
	body := `{"deviceId":"` + deviceId + `","platform":"linux"}`
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(body))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("guest login: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: err=%v body=%s", err, b)
	}
	return out.Data.AccessToken
}

// m5Do 带 Bearer + scope 的 JSON 请求。
func (h *m5Harness) m5Do(t *testing.T, token, method, path, body string) (int, string) {
	t.Helper()
	return h.rawDo(t, token, method, path, body, nil)
}

// rawDo 不带 Bearer 的裸请求(S2S 回调走渠道签名,不走 Bearer 链)。
func (h *m5Harness) rawDo(t *testing.T, token, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// m5Sign 按契约算法签名:X-Payment-Signature = hex HMAC-SHA256(secret, rawBody)。
func m5Sign(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// m5CreateOrder 买家下单,返回订单 id。
func m5CreateOrder(t *testing.T, h *m5Harness, token, skuID string) string {
	t.Helper()
	code, body := h.m5Do(t, token, http.MethodPost, "/v1/payments/orders", `{"skuId":"`+skuID+`"}`)
	if code != http.StatusOK {
		t.Fatalf("create order: %d %s", code, body)
	}
	var out struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Data.ID == "" {
		t.Fatalf("order decode: err=%v body=%s", err, body)
	}
	if out.Data.Status != "CREATED" {
		t.Fatalf("new order status = %s", body)
	}
	return out.Data.ID
}

func TestM5PaymentFullChain(t *testing.T) {
	h := newM5Harness(t, nil, nil)
	buyer := h.login(t, "m5-buyer")
	other := h.login(t, "m5-other")

	// 管理面登记价格表(接入方定义 SKU;服务端定价事实来源)。
	h.teller.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	h.teller.AddSku("sku_pass", "com.demo.pass", "GAME_WALLET", 3000, "CNY")

	// 可购 SKU 列表:价格表投影,登记序。
	code, body := h.m5Do(t, buyer, http.MethodGet, "/v1/payments/skus", "")
	if code != http.StatusOK {
		t.Fatalf("skus: %d %s", code, body)
	}
	var skus struct {
		Data struct {
			Items []struct {
				ID          string `json:"id"`
				AmountCents int64  `json:"amountCents"`
				Form        string `json:"form"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &skus); err != nil {
		t.Fatalf("skus decode: %v", err)
	}
	if len(skus.Data.Items) != 2 || skus.Data.Items[0].ID != "sku_gem_60" ||
		skus.Data.Items[0].AmountCents != 600 || skus.Data.Items[1].Form != "GAME_WALLET" {
		t.Fatalf("sku 投影 = %s", body)
	}

	// 下单:服务端定价(请求体只有 skuId)→ CREATED + payToken(仅下单响应)。
	code, body = h.m5Do(t, buyer, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_gem_60"}`)
	if code != http.StatusOK {
		t.Fatalf("order: %d %s", code, body)
	}
	var created struct {
		Data struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			AmountCents int64  `json:"amountCents"`
			PayToken    string `json:"payToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("order decode: %v", err)
	}
	orderID := created.Data.ID
	if created.Data.AmountCents != 600 || created.Data.PayToken == "" {
		t.Fatalf("下单快照(应含服务端定价与 payToken) = %s", body)
	}
	// 客户端伪造价格字段不生效(服务端只认 skuId)。
	code, body = h.m5Do(t, buyer, http.MethodPost, "/v1/payments/orders",
		`{"skuId":"sku_gem_60","amountCents":1}`)
	if code != http.StatusOK || !strings.Contains(body, `"amountCents":600`) {
		t.Fatalf("伪造价格应被忽略: %d %s", code, body)
	}

	// 未知 SKU → 404 PAYMENT_SKU_NOT_FOUND。
	code, body = h.m5Do(t, buyer, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_nope"}`)
	if code != http.StatusNotFound || !strings.Contains(body, "PAYMENT_SKU_NOT_FOUND") {
		t.Fatalf("未知 sku: %d %s", code, body)
	}

	// 归属红线:他人查单 → 404 PAYMENT_ORDER_NOT_FOUND,不泄露存在性。
	code, body = h.m5Do(t, other, http.MethodGet, "/v1/payments/orders/"+orderID, "")
	if code != http.StatusNotFound || !strings.Contains(body, "PAYMENT_ORDER_NOT_FOUND") {
		t.Fatalf("他人查单: %d %s", code, body)
	}

	// 渠道回调(签名即认证)→ PAID → DELIVERED;发货指令含服务端定价事实。
	cbBody := `{"orderId":"` + orderID + `","paidAt":"2026-10-09T12:00:01Z"}`
	code, body = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	if code != http.StatusOK {
		t.Fatalf("callback: %d %s", code, body)
	}
	if !strings.Contains(body, `"status":"DELIVERED"`) {
		t.Fatalf("回调后应已发货: %s", body)
	}
	if len(h.delivers) != 1 || h.delivers[0].OrderID != orderID ||
		h.delivers[0].AmountCents != 600 || h.delivers[0].ProductID != "com.demo.gem60" {
		t.Fatalf("发货指令 = %+v", h.delivers)
	}

	// 发货感知 = 轮询:详情 DELIVERED,详情/列表均不再下发 payToken。
	code, body = h.m5Do(t, buyer, http.MethodGet, "/v1/payments/orders/"+orderID, "")
	if code != http.StatusOK || !strings.Contains(body, `"status":"DELIVERED"`) ||
		strings.Contains(body, "payToken") {
		t.Fatalf("详情 = %d %s", code, body)
	}

	// 重复回调:幂等受理 200,不二次发货(契约红线 3)。
	code, body = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	if code != http.StatusOK || len(h.delivers) != 1 {
		t.Fatalf("重复回调: %d 发货数 %d body=%s", code, len(h.delivers), body)
	}

	// 我的订单列表:本人可见,updatedAt 倒序,无 payToken。
	code, body = h.m5Do(t, buyer, http.MethodGet, "/v1/payments/orders", "")
	if code != http.StatusOK || strings.Contains(body, "payToken") {
		t.Fatalf("列表 = %d %s", code, body)
	}
	var list struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("list decode: %v", err)
	}
	if len(list.Data.Items) != 2 { // 正常单 + 伪造价格单;未知 sku 被拒不入列
		t.Fatalf("列表项 = %d body=%s", len(list.Data.Items), body)
	}
}

func TestM5PaymentSignatureRejected(t *testing.T) {
	h := newM5Harness(t, nil, nil)
	buyer := h.login(t, "m5-sig")
	h.teller.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	orderID := m5CreateOrder(t, h, buyer, "sku_gem_60")

	cbBody := `{"orderId":"` + orderID + `","paidAt":"2026-10-09T12:00:01Z"}`
	cases := []struct {
		name string
		hdr  map[string]string
		body string
	}{
		{"缺签名头", nil, cbBody},
		{"错密钥(伪造)", map[string]string{"X-Payment-Signature": m5Sign("attacker_secret", cbBody)}, cbBody},
		{"篡改 body", map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)},
			`{"orderId":"` + orderID + `","paidAt":"2026-10-09T12:00:01Z","amountCents":0}`},
	}
	for _, tc := range cases {
		code, body := h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", tc.body, tc.hdr)
		if code != http.StatusForbidden || !strings.Contains(body, "PAYMENT_INVALID_SIGNATURE") {
			t.Fatalf("%s: %d %s", tc.name, code, body)
		}
		// 订单停 CREATED:伪造回调不能推进状态。
		code, body = h.m5Do(t, buyer, http.MethodGet, "/v1/payments/orders/"+orderID, "")
		if !strings.Contains(body, `"status":"CREATED"`) {
			t.Fatalf("%s 后状态应停 CREATED: %s", tc.name, body)
		}
	}
	if len(h.delivers) != 0 {
		t.Fatalf("伪造回调不应触发发货: %+v", h.delivers)
	}
}

func TestM5DeliverFailReconcile(t *testing.T) {
	fail := true
	h := newM5Harness(t, func(teller.Delivery) error {
		if fail {
			return errM5("发货下游不可用")
		}
		return nil
	}, nil)
	buyer := h.login(t, "m5-rec")
	h.teller.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	orderID := m5CreateOrder(t, h, buyer, "sku_gem_60")

	// 回调受理 200,但发货失败:订单停 PAID 不丢单(契约红线 6)。
	cbBody := `{"orderId":"` + orderID + `"}`
	code, body := h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	if code != http.StatusOK || !strings.Contains(body, `"status":"PAID"`) {
		t.Fatalf("断单回调 = %d %s", code, body)
	}

	// 管理面对账:停 PAID 订单重发 → DELIVERED(断单可对账恢复)。
	fail = false
	attempted, delivered := h.teller.Reconcile()
	if attempted != 1 || delivered != 1 {
		t.Fatalf("Reconcile = (%d,%d)", attempted, delivered)
	}
	code, body = h.m5Do(t, buyer, http.MethodGet, "/v1/payments/orders/"+orderID, "")
	if !strings.Contains(body, `"status":"DELIVERED"`) {
		t.Fatalf("对账后 = %s", body)
	}
}

type errM5 string

func (e errM5) Error() string { return string(e) }

func TestM5RiskRejectAndUnknownCallbackOrder(t *testing.T) {
	h := newM5Harness(t, nil, func(accountID string, amountCents int64) error {
		if amountCents > 1000 {
			return errM5("大额拦截")
		}
		return nil
	})
	buyer := h.login(t, "m5-risk")
	h.teller.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	h.teller.AddSku("sku_pass", "com.demo.pass", "GAME_WALLET", 3000, "CNY")

	// 风控前置拒绝:大额 SKU → 403 PAYMENT_RISK_REJECTED,订单不落库。
	code, body := h.m5Do(t, buyer, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_pass"}`)
	if code != http.StatusForbidden || !strings.Contains(body, "PAYMENT_RISK_REJECTED") {
		t.Fatalf("风控拒绝: %d %s", code, body)
	}
	code, body = h.m5Do(t, buyer, http.MethodGet, "/v1/payments/orders", "")
	if strings.Contains(body, "sku_pass") {
		t.Fatalf("被拒订单不应入列: %s", body)
	}

	// 小额 SKU 正常下单(风控不拦)。
	m5CreateOrder(t, h, buyer, "sku_gem_60")

	// 回调指向不存在订单 → 404 PAYMENT_ORDER_NOT_FOUND(签名正确仍 404)。
	cbBody := `{"orderId":"order_missing","paidAt":"2026-10-09T12:00:01Z"}`
	code, body = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	if code != http.StatusNotFound || !strings.Contains(body, "PAYMENT_ORDER_NOT_FOUND") {
		t.Fatalf("未知订单回调: %d %s", code, body)
	}
}

// m5SseEvent SSE 单事件(帧解析,同 m2 形状)。
type m5SseEvent struct {
	Type string
	Data string
}

// m5OpenStream 打开 SSE 流;返回事件通道。
func (h *m5Harness) m5OpenStream(t *testing.T, token string) (<-chan m5SseEvent, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("stream status = %d, body = %s", resp.StatusCode, body)
	}
	events := make(chan m5SseEvent, 16)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var cur m5SseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if cur.Type != "" {
					events <- cur
				}
				cur = m5SseEvent{}
			case strings.HasPrefix(line, "event: "):
				cur.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events, cancel
}

// m5AwaitEvent 等指定类型事件(5s 上限)。
func m5AwaitEvent(t *testing.T, events <-chan m5SseEvent, typ string) m5SseEvent {
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

// m5AwaitQuiet 静默窗断言:窗口内不得出现指定类型事件(负向:伪造不推送)。
func m5AwaitQuiet(t *testing.T, events <-chan m5SseEvent, typ string) {
	t.Helper()
	deadline := time.After(600 * time.Millisecond)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return
			}
			if ev.Type == typ {
				t.Fatalf("静默窗内收到 %s: %s", typ, ev.Data)
			}
		case <-deadline:
			return
		}
	}
}

// TestM5PushEvents v1.1 验收:paid/delivered 经 SSE 送达(载荷只含 orderId);
// 伪造回调不推送;断单只发 paid,对账恢复补发 delivered。
func TestM5PushEvents(t *testing.T) {
	fail := true
	h := newM5Harness(t, func(teller.Delivery) error {
		if fail {
			return errM5("发货下游不可用")
		}
		return nil
	}, nil)
	buyer := h.login(t, "m5-push")
	events, cancel := h.m5OpenStream(t, buyer)
	defer cancel()

	h.teller.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")

	// 全链:下单 → 签名回调 → paid + delivered 先后送达。
	orderID := m5CreateOrder(t, h, buyer, "sku_gem_60")
	cbBody := `{"orderId":"` + orderID + `","paidAt":"2026-10-09T12:00:01Z"}`

	// 断单场景先行:发货失败,回调只推 paid。
	// (改用第二笔订单走全链,本笔先验证断单推送语义。)
	failOrder := m5CreateOrder(t, h, buyer, "sku_gem_60")
	failBody := `{"orderId":"` + failOrder + `"}`
	code, body := h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", failBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, failBody)})
	if code != http.StatusOK || !strings.Contains(body, `"status":"PAID"`) {
		t.Fatalf("断单回调 = %d %s", code, body)
	}
	paidEv := m5AwaitEvent(t, events, "payment.paid")
	if !strings.Contains(paidEv.Data, `"orderId":"`+failOrder+`"`) {
		t.Fatalf("paid 载荷 = %s", paidEv.Data)
	}
	if strings.Contains(paidEv.Data, "amountCents") || strings.Contains(paidEv.Data, "sku") {
		t.Fatalf("paid 载荷不应含业务字段(广播): %s", paidEv.Data)
	}
	m5AwaitQuiet(t, events, "payment.delivered") // 断单不发 delivered

	// 对账恢复 → delivered 补发。
	fail = false
	if attempted, delivered := h.teller.Reconcile(); attempted != 1 || delivered != 1 {
		t.Fatalf("Reconcile = (%d,%d)", attempted, delivered)
	}
	delivEv := m5AwaitEvent(t, events, "payment.delivered")
	if !strings.Contains(delivEv.Data, `"orderId":"`+failOrder+`"`) {
		t.Fatalf("delivered 载荷 = %s", delivEv.Data)
	}

	// 正常全链(paid+delivered)+ 幂等重复回调不重发。
	cbBody = `{"orderId":"` + orderID + `","paidAt":"2026-10-09T12:00:02Z"}`
	code, body = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	if code != http.StatusOK || !strings.Contains(body, `"status":"DELIVERED"`) {
		t.Fatalf("回调 = %d %s", code, body)
	}
	m5AwaitEvent(t, events, "payment.paid")
	m5AwaitEvent(t, events, "payment.delivered")
	_, _ = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", cbBody,
		map[string]string{"X-Payment-Signature": m5Sign(m5ChannelSecret, cbBody)})
	m5AwaitQuiet(t, events, "payment.paid") // 幂等受理不重发

	// 伪造签名:不推送(订单状态不变,无事件帧)。
	forgeBody := `{"orderId":"` + orderID + `"}`
	_, _ = h.rawDo(t, "", http.MethodPost, "/v1/payments/callback", forgeBody,
		map[string]string{"X-Payment-Signature": m5Sign("attacker", forgeBody)})
	m5AwaitQuiet(t, events, "payment.paid")
}
