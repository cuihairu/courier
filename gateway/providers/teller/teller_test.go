// teller 单元测试:服务端定价、风控前置、归属 404、回调签名 fail-closed、
// 状态机 CREATED→PAID→DELIVERED、回调幂等不二次发货、断单对账恢复、关单保护。
package teller

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
)

// sign 测试侧按契约算法(X-Payment-Signature = hex HMAC-SHA256(secret, rawBody))签名。
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func harness(t *testing.T, secret string, deliver func(Delivery) error, risk func(string, int64) error, notify ...func(string, any)) (*PaymentsProvider, *string) {
	t.Helper()
	cur := new(string)
	*cur = "acc_1"
	var hook func(string, any)
	if len(notify) > 0 {
		hook = notify[0]
	}
	p := New(Options{
		Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
		ChannelSecret: secret,
		Deliver:       deliver,
		RiskCheck:     risk,
		Notify:        hook,
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: *cur, SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	return p, cur
}

func call(t *testing.T, p *PaymentsProvider, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

// createOrder 便利封装:acc 下单 sku,返回响应信封。
func createOrder(t *testing.T, p *PaymentsProvider, acc *string, skuID string) (*envelope, *httptest.ResponseRecorder) {
	t.Helper()
	*acc = "acc_1"
	rec := call(t, p, http.MethodPost, "/v1/payments/orders", `{"skuId":"`+skuID+`"}`, nil)
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("解析响应: %v body=%s", err, rec.Body.String())
	}
	return &env, rec
}

func seedSkus(p *PaymentsProvider) {
	p.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	p.AddSku("sku_pass", "com.demo.pass", "GAME_WALLET", 3000, "CNY")
}

func TestMeta(t *testing.T) {
	p, _ := harness(t, "s3cret", nil, nil)
	if p.Name() != DefaultName {
		t.Fatalf("name = %s", p.Name())
	}
	if string(p.Capability()) != "payments" {
		t.Fatalf("capability = %v", p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestCreateOrder_ServerSidePricing(t *testing.T) {
	p, cur := harness(t, "s3cret", nil, nil)
	seedSkus(p)

	// 未知 SKU → 404 PAYMENT_SKU_NOT_FOUND
	env, rec := createOrder(t, p, cur, "sku_nope")
	if rec.Code != 404 || env.Error == nil || env.Error.Code != "PAYMENT_SKU_NOT_FOUND" {
		t.Fatalf("未知 sku = %d %s", rec.Code, rec.Body.String())
	}

	// 已知 SKU → CREATED + payToken + 服务端定价(请求体只有 skuId,价格来自价格表)
	env, rec = createOrder(t, p, cur, "sku_gem_60")
	if rec.Code != 200 || env.Error != nil {
		t.Fatalf("下单 = %d %s", rec.Code, rec.Body.String())
	}
	var o orderDTO
	if err := json.Unmarshal(env.Data, &o); err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusCreated || o.PayToken == "" {
		t.Fatalf("下单快照 = %+v", o)
	}
	if o.AmountCents != 600 || o.Currency != "CNY" || o.ProductID != "com.demo.gem60" {
		t.Fatalf("定价不符 = %+v", o)
	}
}

func TestCreateOrder_RiskRejected(t *testing.T) {
	p, cur := harness(t, "s3cret", nil, func(accountID string, amountCents int64) error {
		if amountCents > 1000 {
			return errRisk("大额")
		}
		return nil
	})
	seedSkus(p)

	env, rec := createOrder(t, p, cur, "sku_pass") // 3000 > 1000 → 拒
	if rec.Code != 403 || env.Error == nil || env.Error.Code != "PAYMENT_RISK_REJECTED" {
		t.Fatalf("风控拒绝 = %d %s", rec.Code, rec.Body.String())
	}
	// 被拒订单不落库:我的订单为空
	*cur = "acc_1"
	rec = call(t, p, http.MethodGet, "/v1/payments/orders", "", nil)
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("被拒订单不应存在: %s", rec.Body.String())
	}
}

type errRisk string

func (e errRisk) Error() string { return string(e) }

func TestOrderOwnership_404NoLeak(t *testing.T) {
	p, cur := harness(t, "s3cret", nil, nil)
	seedSkus(p)
	_, rec := createOrder(t, p, cur, "sku_gem_60")
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	// 他人查询 → 404 PAYMENT_ORDER_NOT_FOUND(不泄露存在性)
	*cur = "acc_2"
	rec = call(t, p, http.MethodGet, "/v1/payments/orders/"+created.Data.ID, "", nil)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "PAYMENT_ORDER_NOT_FOUND") {
		t.Fatalf("他人查询 = %d %s", rec.Code, rec.Body.String())
	}
	// 他人列表不含他人订单
	rec = call(t, p, http.MethodGet, "/v1/payments/orders", "", nil)
	if strings.Contains(rec.Body.String(), created.Data.ID) {
		t.Fatalf("他人列表泄露订单: %s", rec.Body.String())
	}
	// 本人可见
	*cur = "acc_1"
	rec = call(t, p, http.MethodGet, "/v1/payments/orders/"+created.Data.ID, "", nil)
	if rec.Code != 200 {
		t.Fatalf("本人查询 = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCallback_SignatureFailClosed(t *testing.T) {
	// 密钥未配置:fail-closed,伪造签名一律 403
	p0, cur := harness(t, "", nil, nil)
	seedSkus(p0)
	_, rec := createOrder(t, p0, cur, "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}
	body := `{"orderId":"` + oid.Data.ID + `","paidAt":"2026-10-09T12:00:01Z"}`
	rec = call(t, p0, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("any", []byte(body))})
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "PAYMENT_INVALID_SIGNATURE") {
		t.Fatalf("无密钥回调 = %d %s", rec.Code, rec.Body.String())
	}

	// 密钥已配置:缺头/错密钥/篡改 body 全部 403,订单停留 CREATED
	p, cur := harness(t, "s3cret", nil, nil)
	seedSkus(p)
	_, rec = createOrder(t, p, cur, "sku_gem_60")
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		hdr  map[string]string
		body string
	}{
		{"缺签名头", nil, body},
		{"错密钥", map[string]string{"X-Payment-Signature": sign("wrong", []byte(body))}, body},
		{"篡改 body", map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))}, `{"orderId":"` + oid.Data.ID + `","paidAt":"2026-10-09T99:99:99Z"}`},
	}
	for _, tc := range cases {
		rec := call(t, p, http.MethodPost, "/v1/payments/callback", tc.body, tc.hdr)
		if rec.Code != 403 || !strings.Contains(rec.Body.String(), "PAYMENT_INVALID_SIGNATURE") {
			t.Fatalf("%s = %d %s", tc.name, rec.Code, rec.Body.String())
		}
		*cur = "acc_1"
		rec = call(t, p, http.MethodGet, "/v1/payments/orders/"+oid.Data.ID, "", nil)
		if !strings.Contains(rec.Body.String(), `"status":"CREATED"`) {
			t.Fatalf("%s 后订单状态应停 CREATED: %s", tc.name, rec.Body.String())
		}
	}
}

func TestCallback_FullFlow_Idempotent(t *testing.T) {
	var deliveries []Delivery
	p, cur := harness(t, "s3cret", func(d Delivery) error {
		deliveries = append(deliveries, d)
		return nil
	}, nil)
	seedSkus(p)
	_, rec := createOrder(t, p, cur, "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}

	body := `{"orderId":"` + oid.Data.ID + `","paidAt":"2026-10-09T12:00:01Z"}`
	rec = call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != 200 {
		t.Fatalf("回调 = %d %s", rec.Code, rec.Body.String())
	}
	var wrap struct {
		Data *orderDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrap); err != nil {
		t.Fatal(err)
	}
	o := wrap.Data
	// 回调响应即到 DELIVERED(沙箱同步发货);paidAt 取回调体;不含 payToken
	if o.Status != StatusDelivered || o.PaidAt == "" || o.DeliveredAt == "" || o.PayToken != "" {
		t.Fatalf("回调后快照 = %+v", o)
	}
	if len(deliveries) != 1 || deliveries[0].OrderID != oid.Data.ID ||
		deliveries[0].AccountID != "acc_1" || deliveries[0].AmountCents != 600 {
		t.Fatalf("发货指令 = %+v", deliveries)
	}

	// 重复回调:幂等受理 200,不二次发货
	rec = call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != 200 || len(deliveries) != 1 {
		t.Fatalf("重复回调 = %d 发货数 %d", rec.Code, len(deliveries))
	}
	// 详情轮询(发货感知):状态 DELIVERED,详情不下发 payToken
	*cur = "acc_1"
	rec = call(t, p, http.MethodGet, "/v1/payments/orders/"+oid.Data.ID, "", nil)
	if !strings.Contains(rec.Body.String(), `"status":"DELIVERED"`) || strings.Contains(rec.Body.String(), "payToken") {
		t.Fatalf("详情 = %s", rec.Body.String())
	}
}

func TestDeliverFail_StaysPaid_ReconcileRecovers(t *testing.T) {
	fail := true
	var attempts int
	p, cur := harness(t, "s3cret", func(Delivery) error {
		attempts++
		if fail {
			return errRisk("下游挂了")
		}
		return nil
	}, nil)
	seedSkus(p)
	_, rec := createOrder(t, p, cur, "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}

	body := `{"orderId":"` + oid.Data.ID + `"}`
	rec = call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	// 回调受理 200,但发货失败:订单停 PAID 不丢单
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"PAID"`) {
		t.Fatalf("发货失败回调 = %d %s", rec.Code, rec.Body.String())
	}
	if attempts != 1 {
		t.Fatalf("发货尝试 = %d", attempts)
	}

	// 下游恢复,对账重发
	fail = false
	attempted, delivered := p.Reconcile()
	if attempted != 1 || delivered != 1 {
		t.Fatalf("Reconcile = (%d,%d)", attempted, delivered)
	}
	*cur = "acc_1"
	rec = call(t, p, http.MethodGet, "/v1/payments/orders/"+oid.Data.ID, "", nil)
	if !strings.Contains(rec.Body.String(), `"status":"DELIVERED"`) {
		t.Fatalf("对账后 = %s", rec.Body.String())
	}
	// 已恢复订单不再入对账集合
	if attempted, delivered := p.Reconcile(); attempted != 0 || delivered != 0 {
		t.Fatalf("二次对账 = (%d,%d)", attempted, delivered)
	}
}

func TestCloseOrder_StateMachine(t *testing.T) {
	p, cur := harness(t, "s3cret", nil, nil)
	seedSkus(p)
	_, rec := createOrder(t, p, cur, "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}

	// 不存在的订单
	if err := p.CloseOrder("order_nope"); err == nil || err.Error() != "PAYMENT_ORDER_NOT_FOUND" {
		t.Fatalf("关不存在单 = %v", err)
	}

	// CREATED → CLOSED
	if err := p.CloseOrder(oid.Data.ID); err != nil {
		t.Fatalf("关单 = %v", err)
	}
	// 关单后回调:幂等受理 200,状态停 CLOSED,不发货
	body := `{"orderId":"` + oid.Data.ID + `"}`
	rec = call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"CLOSED"`) {
		t.Fatalf("关单后回调 = %d %s", rec.Code, rec.Body.String())
	}

	// DELIVERED 不可关(409 PAYMENT_ORDER_STATE)
	p2, cur2 := harness(t, "s3cret", nil, nil)
	seedSkus(p2)
	_, rec = createOrder(t, p2, cur2, "sku_gem_60")
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}
	body = `{"orderId":"` + oid.Data.ID + `"}`
	call(t, p2, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if err := p2.CloseOrder(oid.Data.ID); err == nil || err.Error() != "PAYMENT_ORDER_STATE" {
		t.Fatalf("关已发货单 = %v", err)
	}
}

// TestCallback_PublishesTransitions v1.1 推送:只报状态转移(重复受理不重发),
// 载荷只含 orderId;发货失败停 PAID 只发 paid,对账恢复补发 delivered。
func TestCallback_PublishesTransitions(t *testing.T) {
	type evt struct {
		typ     string
		orderID string
	}
	var evts []evt
	rec := func(typ string, data any) {
		if e, ok := data.(orderEventDTO); ok {
			evts = append(evts, evt{typ, e.OrderID})
		}
	}
	fail := true
	p, cur := harness(t, "s3cret", func(Delivery) error {
		if fail {
			return errRisk("下游挂了")
		}
		return nil
	}, nil, rec)
	seedSkus(p)
	_, rec1 := createOrder(t, p, cur, "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}

	// 受理:paid + delivered?——发货失败停 PAID,只发 paid。
	body := `{"orderId":"` + oid.Data.ID + `","paidAt":"2026-10-09T12:00:01Z"}`
	rc := call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rc.Code != 200 {
		t.Fatalf("回调 = %d %s", rc.Code, rc.Body.String())
	}
	if len(evts) != 1 || evts[0].typ != EventPaid || evts[0].orderID != oid.Data.ID {
		t.Fatalf("断单事件 = %+v, want [paid %s]", evts, oid.Data.ID)
	}

	// 幂等重复回调:受理不重发事件。
	call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if len(evts) != 1 {
		t.Fatalf("重复回调不应重发事件 = %+v", evts)
	}

	// 对账恢复 → delivered 补发。
	fail = false
	if attempted, delivered := p.Reconcile(); attempted != 1 || delivered != 1 {
		t.Fatalf("Reconcile = (%d,%d)", attempted, delivered)
	}
	if len(evts) != 2 || evts[1].typ != EventDelivered || evts[1].orderID != oid.Data.ID {
		t.Fatalf("对账事件 = %+v, want [.., delivered %s]", evts, oid.Data.ID)
	}

	// 同步发货成功路径:paid + delivered 顺序发。
	_, rec2 := createOrder(t, p, cur, "sku_gem_60")
	var oid2 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &oid2); err != nil {
		t.Fatal(err)
	}
	body2 := `{"orderId":"` + oid2.Data.ID + `"}`
	call(t, p, http.MethodPost, "/v1/payments/callback", body2,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body2))})
	if len(evts) != 4 || evts[2].typ != EventPaid || evts[3].typ != EventDelivered ||
		evts[2].orderID != oid2.Data.ID || evts[3].orderID != oid2.Data.ID {
		t.Fatalf("成功路径事件 = %+v, want [paid,delivered]×%s", evts, oid2.Data.ID)
	}
}

func TestListOrders_UpdatedAtDesc(t *testing.T) {
	seq := 0
	p := New(Options{
		Clock: func() time.Time {
			seq++
			return time.Date(2026, 10, 9, 12, 0, seq, 0, time.UTC)
		},
		ChannelSecret: "s3cret",
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	seedSkus(p)
	for range 3 {
		call(t, p, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_gem_60"}`, nil)
	}
	rec := call(t, p, http.MethodGet, "/v1/payments/orders", "", nil)
	var page struct {
		Data struct {
			Items []orderDTO `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 3 {
		t.Fatalf("列表 = %d 项", len(page.Data.Items))
	}
	for i := 1; i < len(page.Data.Items); i++ {
		if page.Data.Items[i-1].UpdatedAt < page.Data.Items[i].UpdatedAt {
			t.Fatalf("列表未按 updatedAt 倒序: %s < %s",
				page.Data.Items[i-1].UpdatedAt, page.Data.Items[i].UpdatedAt)
		}
		// 列表投影不下发 payToken
		if page.Data.Items[i].PayToken != "" {
			t.Fatalf("列表泄露 payToken: %+v", page.Data.Items[i])
		}
	}
}
