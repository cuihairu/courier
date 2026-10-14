// 批次 72 支付域第二实现一致性 e2e(批次 18/64/67/68/69/70/71 先例同构):
// 同一 wire 场景对 teller(可变状态表)与 teller-alt(追加式事件账本 + 投影
// 重放)各跑一遍、同一断言——「支付 Provider 可换」有行为证明而非仅接口冻结。
// 两实现异构面不在本文件断言,由各自单测承载(teller 由 e2e/m5 承载 /
// providers/telleralt 含 TestProjectionEquivalenceWithTeller 显式等价证明)。
package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/teller"
	"github.com/cuihairu/courier/gateway/providers/telleralt"
	"github.com/cuihairu/courier/gateway/routing"
)

const payConcSecret = "e2e_pay_conc_secret"

// paymentsHarness 装配 account + 一个支付实现(经路由,与 main 同构)。
type paymentsHarness struct {
	srv        *httptest.Server
	tp         *teller.PaymentsProvider
	ap         *telleralt.PaymentsProvider
	deliveries int
	events     []string
	fail       *bool
}

func newPaymentsHarness(t *testing.T, useAlt bool) *paymentsHarness {
	t.Helper()
	h := &paymentsHarness{fail: new(bool)}
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var payP providers.Handler
	if useAlt {
		h.ap = telleralt.New(telleralt.Options{
			RequireAuth:   reqAuth,
			ChannelSecret: payConcSecret,
			Deliver: func(d telleralt.Delivery) error {
				h.deliveries++
				if *h.fail {
					return errConc("downstream down")
				}
				return nil
			},
			RiskCheck: func(accountID string, amountCents int64) error {
				if amountCents > 1000 {
					return errConc("large amount")
				}
				return nil
			},
			Notify: h.capture,
		})
		payP = h.ap
	} else {
		h.tp = teller.New(teller.Options{
			RequireAuth:   reqAuth,
			ChannelSecret: payConcSecret,
			Deliver: func(d teller.Delivery) error {
				h.deliveries++
				if *h.fail {
					return errConc("downstream down")
				}
				return nil
			},
			RiskCheck: func(accountID string, amountCents int64) error {
				if amountCents > 1000 {
					return errConc("large amount")
				}
				return nil
			},
			Notify: h.capture,
		})
		payP = h.tp
	}
	if err := reg.Register(payP); err != nil {
		t.Fatalf("register payments provider: %v", err)
	}
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapPayments: {Primary: payP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	h.srv = srv
	return h
}

type errConc string

func (e errConc) Error() string { return string(e) }

// capture 推送事件捕获:type + 载荷 JSON(v1.1 载荷只含 orderId)。
func (h *paymentsHarness) capture(typ string, data any) {
	b, _ := json.Marshal(data)
	h.events = append(h.events, typ+"|"+string(b))
}

func (h *paymentsHarness) login(t *testing.T, deviceId string) string {
	t.Helper()
	body := `{"deviceId":"` + deviceId + `","platform":"linux"}`
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(body))
	h.scopeHeaders(req)
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

func (h *paymentsHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

// do 带 Bearer + scope 的 JSON 请求;token 空 = 匿名。
func (h *paymentsHarness) do(t *testing.T, token, method, path, body string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	h.scopeHeaders(req)
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

// sign 按契约算法签名:X-Payment-Signature = hex HMAC-SHA256(secret, rawBody)。
func payConcSign(secret string, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// addSku 管理面登记价格表(两实现同语义)。
func (h *paymentsHarness) addSku(useAlt bool, id, productID, form string, amountCents int64, currency string) {
	if useAlt {
		h.ap.AddSku(id, productID, form, amountCents, currency)
		return
	}
	h.tp.AddSku(id, productID, form, amountCents, currency)
}

// create 买家下单,返回订单 ID。
func (h *paymentsHarness) create(t *testing.T, token, skuID string) string {
	t.Helper()
	code, body := h.do(t, token, http.MethodPost, "/v1/payments/orders", `{"skuId":"`+skuID+`"}`, nil)
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

// callback 渠道回调(签名按契约算法)。
func (h *paymentsHarness) callback(t *testing.T, orderID, paidAt string, secret string) (int, string) {
	t.Helper()
	body := `{"orderId":"` + orderID + `"`
	if paidAt != "" {
		body += `,"paidAt":"` + paidAt + `"`
	}
	body += `}`
	hdr := map[string]string{"X-Payment-Signature": payConcSign(secret, body)}
	return h.do(t, "", http.MethodPost, "/v1/payments/callback", body, hdr)
}

// getOrder 拉订单详情,返回 data 段。
func (h *paymentsHarness) getOrder(t *testing.T, token, orderID string) map[string]any {
	t.Helper()
	code, body := h.do(t, token, http.MethodGet, "/v1/payments/orders/"+orderID, "", nil)
	if code != http.StatusOK {
		t.Fatalf("get order %s: %d %s", orderID, code, body)
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v body=%s", err, body)
	}
	return out.Data
}

// reconcile 管理面对账重发(两实现同语义)。
func (h *paymentsHarness) reconcile(useAlt bool) (int, int) {
	if useAlt {
		return h.ap.Reconcile()
	}
	return h.tp.Reconcile()
}

// closeOrder 管理面关单(两实现同语义)。
func (h *paymentsHarness) closeOrder(useAlt bool, orderID string) error {
	if useAlt {
		return h.ap.CloseOrder(orderID)
	}
	return h.tp.CloseOrder(orderID)
}

// runPaymentsContract 支付契约链:认证 → 路由形状 → 价格表 → 下单(服务端定价/
// 风控)→ 归属 404 → 回调签名 fail-closed → 状态机 → 幂等 → 断单对账 → 关单
// → 推送事件 → 列表序。
func runPaymentsContract(t *testing.T, h *paymentsHarness, useAlt bool) {
	t.Helper()
	token := h.login(t, "buyer_conc")
	other := h.login(t, "other_conc")

	// 匿名 → 401 COMMON_UNAUTHENTICATED(契约 payment.md)。
	if code, body := h.do(t, "", http.MethodGet, "/v1/payments/orders", "", nil); code != http.StatusUnauthorized ||
		!strings.Contains(body, "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", code, body)
	}
	// 路由形状:未知路径 → 404;callback 非 POST → 404。
	if code, _ := h.do(t, token, http.MethodGet, "/v1/payments/telemetry", "", nil); code != http.StatusNotFound {
		t.Fatalf("unknown path = %d, want 404", code)
	}
	if code, _ := h.do(t, token, http.MethodGet, "/v1/payments/callback", "", nil); code != http.StatusNotFound {
		t.Fatalf("callback GET = %d, want 404", code)
	}

	// 价格表:登记后按登记序投影。
	h.addSku(useAlt, "sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	h.addSku(useAlt, "sku_pass", "com.demo.pass", "GAME_WALLET", 3000, "CNY")
	code, body := h.do(t, token, http.MethodGet, "/v1/payments/skus", "", nil)
	if code != http.StatusOK || !strings.Contains(body, `"id":"sku_gem_60"`) ||
		!strings.Contains(body, `"amountCents":600`) {
		t.Fatalf("skus = %d %s", code, body)
	}

	// 未知 SKU → 404 PAYMENT_SKU_NOT_FOUND。
	code, body = h.do(t, token, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_nope"}`, nil)
	if code != http.StatusNotFound || !strings.Contains(body, "PAYMENT_SKU_NOT_FOUND") {
		t.Fatalf("unknown sku = %d %s", code, body)
	}

	// 风控前置:3000 > 1000 → 403 PAYMENT_RISK_REJECTED,订单不落库。
	code, body = h.do(t, token, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_pass"}`, nil)
	if code != http.StatusForbidden || !strings.Contains(body, "PAYMENT_RISK_REJECTED") {
		t.Fatalf("risk reject = %d %s", code, body)
	}
	if code, body = h.do(t, token, http.MethodGet, "/v1/payments/orders", "", nil); code != http.StatusOK ||
		!strings.Contains(body, `"items":[]`) {
		t.Fatalf("risk-rejected order must not exist: %d %s", code, body)
	}

	// 下单:服务端定价(请求体只有 skuId)+ payToken 只出现一次。
	oid := h.create(t, token, "sku_gem_60")
	d := h.getOrder(t, token, oid)
	if d["status"] != "CREATED" || d["payToken"] == "" || d["amountCents"].(float64) != 600 ||
		d["currency"] != "CNY" || d["productId"] != "com.demo.gem60" {
		t.Fatalf("create snapshot = %v", d)
	}

	// 归属:他人 404 不泄露;本人可见。
	if code, body = h.do(t, other, http.MethodGet, "/v1/payments/orders/"+oid, "", nil); code != http.StatusNotFound ||
		!strings.Contains(body, "PAYMENT_ORDER_NOT_FOUND") {
		t.Fatalf("other get = %d %s", code, body)
	}
	if code, body = h.do(t, other, http.MethodGet, "/v1/payments/orders", "", nil); code != http.StatusOK ||
		strings.Contains(body, oid) {
		t.Fatalf("other list leaks order: %d %s", code, body)
	}

	// 回调签名 fail-closed:缺头/错密钥/篡改 body 一律 403,订单停 CREATED。
	badBody := `{"orderId":"` + oid + `","paidAt":"2026-10-09T12:00:01Z"}`
	for _, tc := range []struct {
		name string
		hdr  map[string]string
		body string
	}{
		{"缺签名头", nil, badBody},
		{"错密钥", map[string]string{"X-Payment-Signature": payConcSign("wrong", badBody)}, badBody},
		{"篡改 body", map[string]string{"X-Payment-Signature": payConcSign(payConcSecret, badBody)}, `{"orderId":"` + oid + `","paidAt":"2026-10-09T99:99:99Z"}`},
	} {
		code, body = h.do(t, "", http.MethodPost, "/v1/payments/callback", tc.body, tc.hdr)
		if code != http.StatusForbidden || !strings.Contains(body, "PAYMENT_INVALID_SIGNATURE") {
			t.Fatalf("%s = %d %s", tc.name, code, body)
		}
		if d = h.getOrder(t, token, oid); d["status"] != "CREATED" {
			t.Fatalf("%s 后订单应停 CREATED: %v", tc.name, d)
		}
	}
	// 未知订单回调 → 404 PAYMENT_ORDER_NOT_FOUND。
	code, body = h.do(t, "", http.MethodPost, "/v1/payments/callback",
		`{"orderId":"order_nope"}`, map[string]string{"X-Payment-Signature": payConcSign(payConcSecret, `{"orderId":"order_nope"}`)})
	if code != http.StatusNotFound || !strings.Contains(body, "PAYMENT_ORDER_NOT_FOUND") {
		t.Fatalf("unknown order callback = %d %s", code, body)
	}

	// 签名回调(发货成功)→ DELIVERED;paidAt 取回调体;响应不含 payToken。
	code, body = h.callback(t, oid, "2026-10-09T12:00:30Z", payConcSecret)
	if code != http.StatusOK {
		t.Fatalf("callback = %d %s", code, body)
	}
	var cbOut struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &cbOut); err != nil {
		t.Fatal(err)
	}
	if _, hasToken := cbOut.Data["payToken"]; cbOut.Data["status"] != "DELIVERED" ||
		cbOut.Data["paidAt"] == "" || cbOut.Data["deliveredAt"] == "" || hasToken {
		t.Fatalf("callback snapshot = %v", cbOut.Data)
	}
	if h.deliveries != 1 {
		t.Fatalf("deliveries = %d, want 1", h.deliveries)
	}
	// 推送事件:paid + delivered 顺序,载荷只含 orderId。
	if len(h.events) != 2 || h.events[0] != `payment.paid|{"orderId":"`+oid+`"}` ||
		h.events[1] != `payment.delivered|{"orderId":"`+oid+`"}` {
		t.Fatalf("events = %v", h.events)
	}

	// 重复回调:幂等受理 200,不二次发货、不重发事件。
	code, body = h.callback(t, oid, "2026-10-09T12:00:30Z", payConcSecret)
	if code != http.StatusOK || h.deliveries != 1 || len(h.events) != 2 {
		t.Fatalf("replay = %d deliveries=%d events=%d", code, h.deliveries, len(h.events))
	}

	// 断单:发货失败停 PAID 不丢单;对账重发恢复;已恢复不再入对账。
	*h.fail = true
	oid2 := h.create(t, token, "sku_gem_60")
	code, body = h.callback(t, oid2, "", payConcSecret)
	if code != http.StatusOK {
		t.Fatalf("stuck callback = %d %s", code, body)
	}
	if d = h.getOrder(t, token, oid2); d["status"] != "PAID" {
		t.Fatalf("stuck order = %v", d)
	}
	if len(h.events) != 3 || h.events[2] != `payment.paid|{"orderId":"`+oid2+`"}` {
		t.Fatalf("stuck events = %v", h.events)
	}
	*h.fail = false
	if attempted, delivered := h.reconcile(useAlt); attempted != 1 || delivered != 1 {
		t.Fatalf("reconcile = (%d,%d)", attempted, delivered)
	}
	if d = h.getOrder(t, token, oid2); d["status"] != "DELIVERED" {
		t.Fatalf("recovered = %v", d)
	}
	if len(h.events) != 4 || h.events[3] != `payment.delivered|{"orderId":"`+oid2+`"}` {
		t.Fatalf("recovered events = %v", h.events)
	}
	if attempted, delivered := h.reconcile(useAlt); attempted != 0 || delivered != 0 {
		t.Fatalf("second reconcile = (%d,%d)", attempted, delivered)
	}

	// 关单:CREATED → CLOSED;关单后回调幂等受理停 CLOSED;DELIVERED 不可关。
	oid3 := h.create(t, token, "sku_gem_60")
	if err := h.closeOrder(useAlt, oid3); err != nil {
		t.Fatalf("close: %v", err)
	}
	if d = h.getOrder(t, token, oid3); d["status"] != "CLOSED" {
		t.Fatalf("closed = %v", d)
	}
	code, body = h.callback(t, oid3, "", payConcSecret)
	if code != http.StatusOK {
		t.Fatalf("callback after close = %d %s", code, body)
	}
	if d = h.getOrder(t, token, oid3); d["status"] != "CLOSED" {
		t.Fatalf("after close callback = %v", d)
	}
	if err := h.closeOrder(useAlt, oid); err == nil || err.Error() != "PAYMENT_ORDER_STATE" {
		t.Fatalf("close delivered = %v", err)
	}

	// 列表:updatedAt 倒序;不下发 payToken。
	code, body = h.do(t, token, http.MethodGet, "/v1/payments/orders", "", nil)
	if code != http.StatusOK {
		t.Fatalf("list = %d %s", code, body)
	}
	var page struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 3 {
		t.Fatalf("list = %d items, want 3", len(page.Data.Items))
	}
	for i, it := range page.Data.Items {
		if _, ok := it["payToken"]; ok {
			t.Fatalf("list leaks payToken: %v", it)
		}
		if i > 0 && page.Data.Items[i-1]["updatedAt"].(string) < it["updatedAt"].(string) {
			t.Fatalf("list not desc: %v", page.Data.Items)
		}
	}
}

// TestPaymentsContractConformance 两实现 × 同一契约,同一断言。
func TestPaymentsContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"teller", false},
		{"teller-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runPaymentsContract(t, newPaymentsHarness(t, impl.useAlt), impl.useAlt)
		})
	}
}
