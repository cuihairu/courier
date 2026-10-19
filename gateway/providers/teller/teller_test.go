// teller 单元测试:服务端定价、风控前置、归属 404、回调签名 fail-closed、
// 状态机 CREATED→PAID→DELIVERED、回调幂等不二次发货、断单对账恢复、关单保护。
package teller

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestCreateOrderInvalidBody(t *testing.T) {
	p, _ := harness(t, "s3cret", nil, nil)
	for _, body := range []string{`{not json`, `{"skuId":" "}`, ``} {
		rec := call(t, p, http.MethodPost, "/v1/payments/orders", body, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: %d, want 400", body, rec.Code)
		}
	}
}

// TestCreateOrder_UniqueOrderIDs 审核整改(2026-10-18 表一#5):订单 ID 为
// UUIDv7(契约 payment.md),同毫秒连续下单不碰撞、不覆盖已有订单。
func TestCreateOrder_UniqueOrderIDs(t *testing.T) {
	p, acc := harness(t, "s3cret", nil, nil)
	seedSkus(p)
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		env, rec := createOrder(t, p, acc, "sku_gem_60")
		if rec.Code != http.StatusOK || env.Error != nil {
			t.Fatalf("第 %d 单: status=%d body=%s", i, rec.Code, rec.Body.String())
		}
		var got struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Data, &got); err != nil || got.ID == "" {
			t.Fatalf("解析订单 id 失败: %v data=%s", err, env.Data)
		}
		u := strings.TrimPrefix(got.ID, "order_")
		if len(u) != 36 || strings.Count(u, "-") != 4 || u[14] != '7' {
			t.Fatalf("订单 id 非 UUIDv7 形状: %q", got.ID)
		}
		if seen[got.ID] {
			t.Fatalf("订单 id 重复(碰撞): %q", got.ID)
		}
		seen[got.ID] = true
	}
	p.mu.Lock()
	n := len(p.orders)
	p.mu.Unlock()
	if n != 50 {
		t.Fatalf("订单落库 %d 条, 期望 50(碰撞不得覆盖)", n)
	}
}

// orderIDOf 取下单响应里的订单 ID。
func orderIDOf(t *testing.T, env *envelope) string {
	t.Helper()
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &got); err != nil || got.ID == "" {
		t.Fatalf("解析订单 id 失败: %v data=%s", err, env.Data)
	}
	return got.ID
}

// fixedStateHarness 构造带固定 clock + 快照路径的 Provider(重启语义测试)。
func fixedStateHarness(t *testing.T, stateFile string) *PaymentsProvider {
	t.Helper()
	p := New(Options{
		Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
		ChannelSecret: "s3cret",
		StateFile:     stateFile,
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	seedSkus(p)
	return p
}

// statusOf 经 wire 取订单状态;不存在返回空串与 404。
func statusOf(t *testing.T, p *PaymentsProvider, id string) (string, int) {
	t.Helper()
	rec := call(t, p, http.MethodGet, "/v1/payments/orders/"+id, "", nil)
	if rec.Code != http.StatusOK {
		return "", rec.Code
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("解析状态失败: %v body=%s", err, rec.Body.String())
	}
	s, _ := env.Data["status"].(string)
	return s, rec.Code
}

// TestOrdersPersistAcrossRestart 审核整改 2026-10-18 表三 F77(已代决一期):
// 配置 StateFile 后,重启回放快照——未完结订单不丢、关单状态保留、回调可
// 在重启后的实例上继续完成。
func TestOrdersPersistAcrossRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "orders.json")

	p1 := fixedStateHarness(t, stateFile)
	id1 := orderIDOf(t, mustCreateOrder(t, p1))
	id2 := orderIDOf(t, mustCreateOrder(t, p1))
	if err := p1.CloseOrder(id2); err != nil {
		t.Fatalf("关单: %v", err)
	}

	// 第二代 = 重启:同一快照文件重新构造。
	p2 := fixedStateHarness(t, stateFile)
	if s, code := statusOf(t, p2, id1); code != http.StatusOK || s != StatusCreated {
		t.Fatalf("重启后未完结订单: status=%q code=%d, 期望 CREATED", s, code)
	}
	if s, code := statusOf(t, p2, id2); code != http.StatusOK || s != StatusClosed {
		t.Fatalf("重启后关单订单: status=%q code=%d, 期望 CLOSED", s, code)
	}
	// 回调在重启后的实例上继续完成(幂等发货)。
	body := `{"orderId":"` + id1 + `","paidAt":"2026-10-09T12:00:30Z"}`
	rec := call(t, p2, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"DELIVERED"`) {
		t.Fatalf("重启后回调 = %d %s", rec.Code, rec.Body.String())
	}

	// 第三代:DELIVERED 状态同样保留。
	p3 := fixedStateHarness(t, stateFile)
	if s, code := statusOf(t, p3, id1); code != http.StatusOK || s != StatusDelivered {
		t.Fatalf("三代重启: status=%q code=%d, 期望 DELIVERED", s, code)
	}
}

// TestOrdersNoStateFileMemoryOnly 未配置 StateFile = 纯内存历史行为(不写任何
// 文件,重启即空),快照口径默认关闭不改变现有部署。
func TestOrdersNoStateFileMemoryOnly(t *testing.T) {
	p := fixedStateHarness(t, "")
	id := orderIDOf(t, mustCreateOrder(t, p))

	p2 := fixedStateHarness(t, "")
	if _, code := statusOf(t, p2, id); code != http.StatusNotFound {
		t.Fatalf("未配置 StateFile 时重启应丢单(code=%d), 与历史内存态一致", code)
	}
}

// TestOrdersCorruptSnapshotFailClosed 快照损坏不静默从空态启动(会丢单);
// HealthCheck fail-closed,由路由面降级为 503,运维删文件即可恢复。
func TestOrdersCorruptSnapshotFailClosed(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "orders.json")
	if err := os.WriteFile(stateFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := fixedStateHarness(t, stateFile)
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("损坏快照应使 HealthCheck fail-closed")
	}
	// 删除损坏文件后新实例可正常启动(不残留不可恢复状态)。
	if err := os.Remove(stateFile); err != nil {
		t.Fatal(err)
	}
	p2 := fixedStateHarness(t, stateFile)
	if err := p2.HealthCheck(context.Background()); err != nil {
		t.Fatalf("无快照时应就绪: %v", err)
	}
}

// mustCreateOrder 下单并断言成功,返回响应信封。
func mustCreateOrder(t *testing.T, p *PaymentsProvider) *envelope {
	t.Helper()
	env, rec := createOrder(t, p, new(string), "sku_gem_60")
	if rec.Code != http.StatusOK || env.Error != nil {
		t.Fatalf("下单失败: %d %s", rec.Code, rec.Body.String())
	}
	return env
}

// ---- F77 二期(2026-10-19 代决)Options Store 接缝 ----

// memStore 测试用内存 OrderStore(字节 blob,模拟接入方 DB / Redis / 对象存储侧)。
type memStore struct {
	b       []byte
	saves   int
	loadErr error
	saveErr error
}

func (m *memStore) Load() ([]byte, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	if m.b == nil {
		return nil, ErrSnapshotMissing
	}
	return append([]byte(nil), m.b...), nil
}

func (m *memStore) Save(b []byte) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	m.b = append([]byte(nil), b...)
	return nil
}

// fixedDualHarness Store 与 StateFile 同设(优先级语义)。
func fixedDualHarness(t *testing.T, stateFile string, st OrderStore) *PaymentsProvider {
	t.Helper()
	p := New(Options{
		Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
		ChannelSecret: "s3cret",
		StateFile:     stateFile,
		Store:         st,
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	seedSkus(p)
	return p
}

// fixedStoreHarness Store 接缝单设。
func fixedStoreHarness(t *testing.T, st OrderStore) *PaymentsProvider {
	t.Helper()
	p := New(Options{
		Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
		ChannelSecret: "s3cret",
		Store:         st,
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	seedSkus(p)
	return p
}

// TestOrdersStorePersistAcrossRestart 二期 Store 接缝:字节 blob 存储等价
// StateFile 的三代重启语义(未完结订单不丢、关单/发货状态保留、回调可在
// 重启后实例续完成、状态变更即写全量)。
func TestOrdersStorePersistAcrossRestart(t *testing.T) {
	st := &memStore{}

	p1 := fixedStoreHarness(t, st)
	id1 := orderIDOf(t, mustCreateOrder(t, p1))
	id2 := orderIDOf(t, mustCreateOrder(t, p1))
	if err := p1.CloseOrder(id2); err != nil {
		t.Fatalf("关单: %v", err)
	}
	if st.saves == 0 {
		t.Fatal("状态变更应经 Store.Save 写全量")
	}

	// 第二代 = 重启:同一个 Store 重新构造。
	p2 := fixedStoreHarness(t, st)
	if s, code := statusOf(t, p2, id1); code != http.StatusOK || s != StatusCreated {
		t.Fatalf("重启后未完结订单: status=%q code=%d, 期望 CREATED", s, code)
	}
	if s, code := statusOf(t, p2, id2); code != http.StatusOK || s != StatusClosed {
		t.Fatalf("重启后关单订单: status=%q code=%d, 期望 CLOSED", s, code)
	}
	body := `{"orderId":"` + id1 + `","paidAt":"2026-10-09T12:00:30Z"}`
	rec := call(t, p2, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"DELIVERED"`) {
		t.Fatalf("重启后回调 = %d %s", rec.Code, rec.Body.String())
	}

	// 第三代:DELIVERED 状态同样保留。
	p3 := fixedStoreHarness(t, st)
	if s, code := statusOf(t, p3, id1); code != http.StatusOK || s != StatusDelivered {
		t.Fatalf("三代重启: status=%q code=%d, 期望 DELIVERED", s, code)
	}
}

// TestOrdersStorePrecedenceOverStateFile Store 与 StateFile 同设时 Store 优先:
// 文件里的旧快照不回放(证明走 Store),也不被写入(文件字节保持原样)。
func TestOrdersStorePrecedenceOverStateFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "orders.json")
	stale := []byte(`{"orders":{"stale_id":{"owner":"acc_x","status":"CREATED","amountCents":1}}}`)
	if err := os.WriteFile(file, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	st := &memStore{}
	p := fixedDualHarness(t, file, st) // 与 Store 同设:Store 应优先生效
	if _, code := statusOf(t, p, "stale_id"); code != http.StatusNotFound {
		t.Fatalf("Store 优先生效时不应回放文件旧快照(code=%d)", code)
	}
	id := orderIDOf(t, mustCreateOrder(t, p))
	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(stale) {
		t.Fatal("Store 配置时不应写 StateFile")
	}
	if st.saves == 0 {
		t.Fatal("写入应走 Store.Save")
	}
	// 重启(同 Store 同文件):文件旧快照仍不参与,Store 里的新订单在。
	p2 := fixedDualHarness(t, file, st)
	if s, code := statusOf(t, p2, id); code != http.StatusOK || s != StatusCreated {
		t.Fatalf("重启后应从 Store 回放: status=%q code=%d", s, code)
	}
}

// TestOrdersStoreMissingIsFirstBoot ErrSnapshotMissing = 首次启动:健康就绪
// 空态,不 fail-closed(等价文件不存在)。
func TestOrdersStoreMissingIsFirstBoot(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{})
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("首次启动应就绪: %v", err)
	}
}

// TestOrdersStoreCorruptSnapshotFailClosed Store 返回损坏字节 → HealthCheck
// fail-closed(等价损坏文件),不静默从空态启动丢单。
func TestOrdersStoreCorruptSnapshotFailClosed(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{b: []byte("{not json")})
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("损坏 Store 字节应使 HealthCheck fail-closed")
	}
}

// TestOrdersStoreLoadFailClosed Load 非缺失错误(存储故障)→ fail-closed,
// 不以空态静默启动。
func TestOrdersStoreLoadFailClosed(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{loadErr: errors.New("store down")})
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("Store 读故障应使 HealthCheck fail-closed")
	}
}

// TestOrdersStoreSaveFailClosed Save 失败 → HealthCheck fail-closed,且
// stateErr 置位后不再重试写(避免反复写坏)。
func TestOrdersStoreSaveFailClosed(t *testing.T) {
	st := &memStore{}
	p := fixedStoreHarness(t, st)
	mustCreateOrder(t, p) // Save 成功
	st.saveErr = errors.New("boom")
	mustCreateOrder(t, p) // Save 失败 → stateErr
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("Store 写失败应使 HealthCheck fail-closed")
	}
	saves := st.saves
	mustCreateOrder(t, p)
	if st.saves != saves {
		t.Fatal("stateErr 置位后不应再写 Store")
	}
}
