// teller-alt 单元测试:与 teller 同一契约面(服务端定价、风控前置、归属 404、
// 回调签名 fail-closed、状态机、回调幂等、断单对账、关单保护、推送事件、列表序)
// + 异构面(事件账本追加只读、投影重放、两模型操作序列等价)。
package telleralt

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
	"github.com/cuihairu/courier/gateway/providers/teller"
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

func TestFailClosedWithoutAuth(t *testing.T) {
	// 未注入认证中间件:fail-closed 一律 401(契约:玩家端点全部 Bearer)。
	p := New(Options{ChannelSecret: "s3cret"})
	rec := call(t, p, http.MethodGet, "/v1/payments/orders", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no auth = %d, want 401", rec.Code)
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

// ---- 异构面:事件账本追加只读 + 投影重放 ----

func TestJournalAppendOnly_ProjectionReplay(t *testing.T) {
	seq := 0
	p := New(Options{
		Clock: func() time.Time {
			seq++
			return time.Date(2026, 10, 9, 12, 0, seq, 0, time.UTC)
		},
		ChannelSecret: "s3cret",
		Deliver:       func(Delivery) error { return nil },
		RequireAuth: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		},
	})
	seedSkus(p)
	_, rec := createOrder(t, p, new(string), "sku_gem_60")
	var oid struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &oid); err != nil {
		t.Fatal(err)
	}
	e := p.orders[oid.Data.ID]
	if e == nil {
		t.Fatal("订单账本缺失")
	}

	// 下单后账本只有 created 一个事件;无状态字段(异构面核心:状态=投影)。
	if len(e.Events) != 1 || e.Events[0].Type != evCreated {
		t.Fatalf("下单后事件 = %+v, want [created]", e.Events)
	}
	v := projectOrder(e)
	if v.Status != StatusCreated || v.PaidAt != nil {
		t.Fatalf("下单投影 = %+v", v)
	}
	createdAt := v.CreatedAt

	// 回调(发货成功)→ paid + delivered 两事件追加;首事件(created)不可变。
	body := `{"orderId":"` + oid.Data.ID + `","paidAt":"2026-10-09T12:00:30Z"}`
	rec = call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if rec.Code != 200 {
		t.Fatalf("回调 = %d", rec.Code)
	}
	if len(e.Events) != 3 || e.Events[1].Type != evPaid || e.Events[2].Type != evDelivered {
		t.Fatalf("回调后事件 = %+v, want [created,paid,delivered]", e.Events)
	}
	if !e.Events[0].At.Equal(createdAt) {
		t.Fatalf("首事件被改写: %v != %v", e.Events[0].At, createdAt)
	}
	v = projectOrder(e)
	if v.Status != StatusDelivered || v.PaidAt == nil || v.DeliveredAt == nil {
		t.Fatalf("回调投影 = %+v", v)
	}
	if !v.PaidAt.Equal(time.Date(2026, 10, 9, 12, 0, 30, 0, time.UTC)) {
		t.Fatalf("paidAt 应取回调体: %v", v.PaidAt)
	}
	// updatedAt = 末事件(delivered),createdAt = 首事件。
	if !v.UpdatedAt.Equal(v.DeliveredAt.UTC()) || !v.CreatedAt.Equal(createdAt) {
		t.Fatalf("时间戳投影 = %+v", v)
	}

	// 幂等重复回调:不追加事件(账本只记录真实转移)。
	n := len(e.Events)
	call(t, p, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))})
	if len(e.Events) != n {
		t.Fatalf("幂等回调追加事件: %d -> %d", n, len(e.Events))
	}

	// 管理面关单 → closed 事件追加;投影停 CLOSED。
	if err := p.CloseOrder("order_nope"); err == nil {
		t.Fatal("关不存在单应报错")
	}
}

func TestCloseOrder_AppendsClosedEvent(t *testing.T) {
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
	e := p.orders[oid.Data.ID]
	if err := p.CloseOrder(oid.Data.ID); err != nil {
		t.Fatalf("关单: %v", err)
	}
	if len(e.Events) != 2 || e.Events[1].Type != evClosed {
		t.Fatalf("关单后事件 = %+v, want [created,closed]", e.Events)
	}
	if v := projectOrder(e); v.Status != StatusClosed {
		t.Fatalf("关单投影 = %s", v.Status)
	}
}

// TestProjectionEquivalenceWithTeller 等价证明:同一操作脚本(下单→回调→重复回调
// →断单→对账→关单)对 teller(可变状态表)与本实现(事件账本投影)各跑一遍,
// 逐字段比对 wire 快照——「投影规则=可变字段语义」的可执行证明。
func TestProjectionEquivalenceWithTeller(t *testing.T) {
	newTeller := func(deliver func(teller.Delivery) error) *teller.PaymentsProvider {
		return teller.New(teller.Options{
			Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
			ChannelSecret: "s3cret",
			Deliver:       deliver,
			RequireAuth: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
					next.ServeHTTP(w, r.WithContext(ctx))
				})
			},
		})
	}
	newAlt := func(deliver func(Delivery) error) *PaymentsProvider {
		return New(Options{
			Clock:         func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
			ChannelSecret: "s3cret",
			Deliver:       deliver,
			RequireAuth: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					ctx := auth.WithIdentity(r.Context(), auth.Identity{AccountID: "acc_1", SessionID: "ses_1"})
					next.ServeHTTP(w, r.WithContext(ctx))
				})
			},
		})
	}
	okDeliverT := func(teller.Delivery) error { return nil }
	okDeliverA := func(Delivery) error { return nil }

	tp := newTeller(okDeliverT)
	ap := newAlt(okDeliverA)
	tp.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	ap.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")

	// 步骤 1:下单——同一 clock 下 token/时间/状态等状态字段必须逐字段一致;
	// 订单 ID 是 UUIDv7 随机位(审核 2026-10-18 表一#5),两实例各自生成,
	// 等价证明比对去 id 快照(比的是投影语义,不是随机标识)。
	idT, idA := createVia(t, tp), createVia(t, ap)
	assertSnapEq(t, "after-create", snapWOD(t, tp, idT), snapWOD(t, ap, idA))

	// 步骤 2:签名回调(发货成功)→ DELIVERED。回调体带各自订单 ID。
	for _, pair := range []struct {
		pr interface {
			ServeHTTP(http.ResponseWriter, *http.Request)
		}
		id string
	}{{tp, idT}, {ap, idA}} {
		body := `{"orderId":"` + pair.id + `","paidAt":"2026-10-09T12:00:30Z"}`
		rec := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/v1/payments/callback", strings.NewReader(body))
		req.Header.Set("X-Payment-Signature", sign("s3cret", []byte(body)))
		pair.pr.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("回调 = %d %s", rec.Code, rec.Body.String())
		}
	}
	assertSnapEq(t, "after-callback", snapWOD(t, tp, idT), snapWOD(t, ap, idA))

	// 步骤 3:重复回调(幂等)——快照不变、不二次发货由各自单测承载,此处比快照。
	for _, pair := range []struct {
		pr interface {
			ServeHTTP(http.ResponseWriter, *http.Request)
		}
		id string
	}{{tp, idT}, {ap, idA}} {
		body := `{"orderId":"` + pair.id + `","paidAt":"2026-10-09T12:00:30Z"}`
		rec := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/v1/payments/callback", strings.NewReader(body))
		req.Header.Set("X-Payment-Signature", sign("s3cret", []byte(body)))
		pair.pr.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("重复回调 = %d", rec.Code)
		}
	}
	assertSnapEq(t, "after-replay", snapWOD(t, tp, idT), snapWOD(t, ap, idA))

	// 步骤 4:断单链路(独立实现对)——deliver 挂→停 PAID→对账恢复。
	failT := true
	failA := true
	tp2 := newTeller(func(teller.Delivery) error {
		if failT {
			return errRisk("down")
		}
		return nil
	})
	ap2 := newAlt(func(Delivery) error {
		if failA {
			return errRisk("down")
		}
		return nil
	})
	tp2.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	ap2.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	idT2, idA2 := createVia(t, tp2), createVia(t, ap2)
	for _, pair := range []struct {
		pr interface {
			ServeHTTP(http.ResponseWriter, *http.Request)
		}
		id string
	}{{tp2, idT2}, {ap2, idA2}} {
		body2 := `{"orderId":"` + pair.id + `"}`
		rec := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/v1/payments/callback", strings.NewReader(body2))
		req.Header.Set("X-Payment-Signature", sign("s3cret", []byte(body2)))
		pair.pr.ServeHTTP(rec, req)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"PAID"`) {
			t.Fatalf("断单回调 = %d %s", rec.Code, rec.Body.String())
		}
	}
	assertSnapEq(t, "stuck-paid", snapWOD(t, tp2, idT2), snapWOD(t, ap2, idA2))
	failT, failA = false, false
	atT, deT := tp2.Reconcile()
	atA, deA := ap2.Reconcile()
	if atT != atA || deT != deA {
		t.Fatalf("对账计数分叉: teller=(%d,%d) alt=(%d,%d)", atT, deT, atA, deA)
	}
	assertSnapEq(t, "recovered", snapWOD(t, tp2, idT2), snapWOD(t, ap2, idA2))

	// 步骤 5:关单(独立 CREATED 单)→ CLOSED,快照同。
	tp3, ap3 := newTeller(okDeliverT), newAlt(okDeliverA)
	tp3.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	ap3.AddSku("sku_gem_60", "com.demo.gem60", "DIRECT_PURCHASE", 600, "CNY")
	idT3, idA3 := createVia(t, tp3), createVia(t, ap3)
	if err := tp3.CloseOrder(idT3); err != nil {
		t.Fatalf("teller 关单: %v", err)
	}
	if err := ap3.CloseOrder(idA3); err != nil {
		t.Fatalf("teller-alt 关单: %v", err)
	}
	assertSnapEq(t, "closed", snapWOD(t, tp3, idT3), snapWOD(t, ap3, idA3))
}

// snapWOD 取详情快照并去掉 id 字段:订单 ID 是 UUIDv7 随机位(审核
// 2026-10-18 表一#5),两实现实例各自生成;等价证明比对状态语义。
func snapWOD(t *testing.T, p interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, id string) map[string]any {
	t.Helper()
	m := getOrder(t, p, id)
	delete(m, "id")
	return m
}

// createVia 经 wire 下单返回订单 ID(两实现调用形状一致)。
func createVia(t *testing.T, p interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/v1/payments/orders", strings.NewReader(`{"skuId":"sku_gem_60"}`))
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("下单 = %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data.ID
}

// getOrder 经 wire 拉详情,返回规范化 DTO(两实现 wire 快照逐字段可比)。
func getOrder(t *testing.T, p interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, id string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v1/payments/orders/"+id, nil)
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("详情 = %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Data
}

// assertSnapEq 逐字段比对两实现 wire 快照。
func assertSnapEq(t *testing.T, at string, a, b map[string]any) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s: 字段数分叉 %d vs %d (%v vs %v)", at, len(a), len(b), a, b)
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || va != vb {
			t.Fatalf("%s: %s 分叉 %v vs %v", at, k, va, vb)
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

// TestOrdersPersistAcrossRestart 审核整改 2026-10-18 表三 F77(已代决一期):
// 事件账本快照 + 启动重放——重启不丢未完结订单,投影重放恢复状态机。
func TestOrdersPersistAcrossRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "orders.json")

	p1 := fixedStateHarness(t, stateFile)
	env1, rec1 := createOrder(t, p1, new(string), "sku_gem_60")
	if rec1.Code != http.StatusOK {
		t.Fatalf("下单: %d %s", rec1.Code, rec1.Body.String())
	}
	id1 := orderIDOf(t, env1)
	env2, rec2 := createOrder(t, p1, new(string), "sku_gem_60")
	if rec2.Code != http.StatusOK {
		t.Fatalf("下单2: %d %s", rec2.Code, rec2.Body.String())
	}
	id2 := orderIDOf(t, env2)
	if err := p1.CloseOrder(id2); err != nil {
		t.Fatalf("关单: %v", err)
	}

	// 第二代 = 重启:同一快照文件重新构造,事件账本回放。
	p2 := fixedStateHarness(t, stateFile)
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

	// 第三代:DELIVERED 状态保留。
	p3 := fixedStateHarness(t, stateFile)
	if s, code := statusOf(t, p3, id1); code != http.StatusOK || s != StatusDelivered {
		t.Fatalf("三代重启: status=%q code=%d, 期望 DELIVERED", s, code)
	}
}

// TestOrdersNoStateFileMemoryOnly 未配置 StateFile = 纯内存历史行为。
func TestOrdersNoStateFileMemoryOnly(t *testing.T) {
	p := fixedStateHarness(t, "")
	env, rec := createOrder(t, p, new(string), "sku_gem_60")
	if rec.Code != http.StatusOK {
		t.Fatalf("下单: %d %s", rec.Code, rec.Body.String())
	}
	id := orderIDOf(t, env)

	p2 := fixedStateHarness(t, "")
	if _, code := statusOf(t, p2, id); code != http.StatusNotFound {
		t.Fatalf("未配置 StateFile 时重启应丢单(code=%d), 与历史内存态一致", code)
	}
}

// TestOrdersCorruptSnapshotFailClosed 快照损坏不静默从空态启动(会丢单);
// HealthCheck fail-closed,运维删文件即可恢复。
func TestOrdersCorruptSnapshotFailClosed(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "orders.json")
	if err := os.WriteFile(stateFile, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := fixedStateHarness(t, stateFile)
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("损坏快照应使 HealthCheck fail-closed")
	}
	if err := os.Remove(stateFile); err != nil {
		t.Fatal(err)
	}
	p2 := fixedStateHarness(t, stateFile)
	if err := p2.HealthCheck(context.Background()); err != nil {
		t.Fatalf("无快照时应就绪: %v", err)
	}
}

// mustCreate 下单成功路径捷径(失败即 Fatal;sku 固定 gem)。
func mustCreate(t *testing.T, p *PaymentsProvider) *envelope {
	t.Helper()
	env, rec := createOrder(t, p, new(string), "sku_gem_60")
	if rec.Code != http.StatusOK {
		t.Fatalf("下单: %d %s", rec.Code, rec.Body.String())
	}
	return env
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

// TestOrdersStorePersistAcrossRestart 二期 Store 接缝:事件账本经字节 blob
// 存储等价 StateFile 的三代重启语义(回放投影恢复状态机)。
func TestOrdersStorePersistAcrossRestart(t *testing.T) {
	st := &memStore{}

	p1 := fixedStoreHarness(t, st)
	env1, rec1 := createOrder(t, p1, new(string), "sku_gem_60")
	if rec1.Code != http.StatusOK {
		t.Fatalf("下单: %d %s", rec1.Code, rec1.Body.String())
	}
	id1 := orderIDOf(t, env1)
	id2 := orderIDOf(t, mustCreate(t, p1))
	if err := p1.CloseOrder(id2); err != nil {
		t.Fatalf("关单: %v", err)
	}
	if st.saves == 0 {
		t.Fatal("账本追加应经 Store.Save 写全量")
	}

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

	p3 := fixedStoreHarness(t, st)
	if s, code := statusOf(t, p3, id1); code != http.StatusOK || s != StatusDelivered {
		t.Fatalf("三代重启: status=%q code=%d, 期望 DELIVERED", s, code)
	}
}

// TestOrdersStorePrecedenceOverStateFile Store 与 StateFile 同设时 Store 优先:
// 文件里的旧快照不回放,也不被写入。
func TestOrdersStorePrecedenceOverStateFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "orders.json")
	stale := []byte(`{"orders":{"stale_id":{"owner":"acc_x","status":"CREATED","amountCents":1}}}`)
	if err := os.WriteFile(file, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	st := &memStore{}
	p := fixedDualHarness(t, file, st)
	if _, code := statusOf(t, p, "stale_id"); code != http.StatusNotFound {
		t.Fatalf("Store 优先生效时不应回放文件旧快照(code=%d)", code)
	}
	id := orderIDOf(t, mustCreate(t, p))
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
	p2 := fixedDualHarness(t, file, st)
	if s, code := statusOf(t, p2, id); code != http.StatusOK || s != StatusCreated {
		t.Fatalf("重启后应从 Store 回放: status=%q code=%d", s, code)
	}
}

// TestOrdersStoreMissingIsFirstBoot ErrSnapshotMissing = 首次启动:健康就绪空态。
func TestOrdersStoreMissingIsFirstBoot(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{})
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("首次启动应就绪: %v", err)
	}
}

// TestOrdersStoreCorruptSnapshotFailClosed Store 返回损坏字节 → fail-closed。
func TestOrdersStoreCorruptSnapshotFailClosed(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{b: []byte("{not json")})
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("损坏 Store 字节应使 HealthCheck fail-closed")
	}
}

// TestOrdersStoreLoadFailClosed Load 非缺失错误 → fail-closed,不以空态静默启动。
func TestOrdersStoreLoadFailClosed(t *testing.T) {
	p := fixedStoreHarness(t, &memStore{loadErr: errors.New("store down")})
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("Store 读故障应使 HealthCheck fail-closed")
	}
}

// TestOrdersStoreSaveFailClosed Save 失败 → fail-closed,置位后不再重试写。
func TestOrdersStoreSaveFailClosed(t *testing.T) {
	st := &memStore{}
	p := fixedStoreHarness(t, st)
	mustCreate(t, p)
	st.saveErr = errors.New("boom")
	mustCreate(t, p)
	if err := p.HealthCheck(context.Background()); err == nil {
		t.Fatal("Store 写失败应使 HealthCheck fail-closed")
	}
	saves := st.saves
	mustCreate(t, p)
	if st.saves != saves {
		t.Fatal("stateErr 置位后不应再写 Store")
	}
}
