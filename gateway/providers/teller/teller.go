// Package teller 支付默认 Provider(M5,契约 docs/contract/payment.md)。
//
// 能力域 payments(/v1/payments/*),玩家侧(全 Bearer):
//
//	GET  /v1/payments/skus                 可购 SKU 列表(价格表投影)
//	POST /v1/payments/orders               下单(服务端定价;风控前置;返回 CREATED + payToken)
//	GET  /v1/payments/orders               我的订单(updatedAt 倒序)
//	GET  /v1/payments/orders/{orderId}     订单详情(发货感知 = 轮询)
//
// S2S 渠道回调(无 Bearer,渠道签名):
//
//	POST /v1/payments/callback             X-Payment-Signature = hex HMAC-SHA256(secret, rawBody)
//
// 安全红线落地:服务端定价(请求体只有 skuId);签名验证先于 body 解析,失败一律
// 403 PAYMENT_INVALID_SIGNATURE(密钥未配置 = fail-closed 同样 403);回调幂等
// (同状态重复受理 200 不二次发货);发货失败订单停 PAID 不丢单,管理面 Reconcile
// 对账重发(断单可对账恢复)。四形态只是订单标注,不产生不同状态机(契约 payment.md)。
package teller

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "teller"

const prefixPayments = "/v1/payments/"

// 订单状态(契约状态机:CREATED → PAID → DELIVERED;CREATED/PAID → CLOSED)。
const (
	StatusCreated   = "CREATED"
	StatusPaid      = "PAID"
	StatusDelivered = "DELIVERED"
	StatusClosed    = "CLOSED"
)

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(订单时间戳);默认 time.Now。
	Clock func() time.Time
	// RequireAuth 认证中间件(main 注入 auth.RequireAuth(verifier));
	// nil = fail-closed(玩家端点一律 401):忘注入不能变成裸奔。
	RequireAuth func(http.Handler) http.Handler
	// ChannelSecret 渠道签名密钥(S2S 回调 HMAC-SHA256;部署经环境注入)。
	// 空 = fail-closed:回调一律 403 PAYMENT_INVALID_SIGNATURE。
	ChannelSecret string
	// Deliver 发货钩子(接入方按订单发货:道具/入账/外部确认;契约要求按订单幂等)。
	// nil = 记账即发货成功(沙箱裸跑)。
	Deliver func(Delivery) error
	// RiskCheck 下单风控前置(大额/异常频次;非 nil 且返回 err → 403 PAYMENT_RISK_REJECTED)。
	// nil = 不拦。
	RiskCheck func(accountID string, amountCents int64) error
	// Notify 推送钩子(v1.1 推送事件:payment.paid / payment.delivered,接 chirp.Hub)。
	// 载荷只含 orderId(广播提示,业务字段归拉取端点);nil = 不推送。
	Notify func(eventType string, data any)
}

// 推送事件 type(契约 payment.md v1.1「推送事件」;events.md 登记)。
const (
	EventPaid      = "payment.paid"
	EventDelivered = "payment.delivered"
)

// Delivery 发货指令(管理面钩子入参;AmountCents 为服务端定价事实)。
type Delivery struct {
	OrderID     string
	AccountID   string
	SkuID       string
	ProductID   string
	Form        string
	AmountCents int64
	Currency    string
}

type skuRecord struct {
	ID          string
	ProductID   string
	Form        string
	AmountCents int64
	Currency    string
}

type orderRecord struct {
	ID          string
	Owner       string
	Status      string
	Sku         skuRecord
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PaidAt      *time.Time
	DeliveredAt *time.Time
	PayToken    string
}

// PaymentsProvider 订单-发货核心流(沙箱渠道;真实渠道按同一回调签名面接入)。
type PaymentsProvider struct {
	name          string
	clock         func() time.Time
	auth          func(http.Handler) http.Handler
	channelSecret string
	deliver       func(Delivery) error
	riskCheck     func(string, int64) error
	notify        func(eventType string, data any)

	mu      sync.Mutex
	skus    map[string]skuRecord // skuID → 价格表(管理面登记)
	skuSeq  []string             // 登记序(列表投影序)
	orders  map[string]*orderRecord
	byOwner map[string][]string // accountID → 登记序 orderIDs
}

// New 构造支付 Provider。
func New(opts Options) *PaymentsProvider {
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	authMW := func(next http.Handler) http.Handler {
		// fail-closed:未注入认证中间件时拒绝一切(契约:玩家端点全部 Bearer)。
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnauthenticated, "not authenticated")
		})
	}
	if opts.RequireAuth != nil {
		authMW = opts.RequireAuth
	}
	if opts.Notify == nil {
		opts.Notify = func(string, any) {} // 默认 no-op:未接通道 = 不推送(拉取兜底)
	}
	return &PaymentsProvider{
		name:          opts.Name,
		clock:         opts.Clock,
		auth:          authMW,
		channelSecret: opts.ChannelSecret,
		deliver:       opts.Deliver,
		riskCheck:     opts.RiskCheck,
		notify:        opts.Notify,
		skus:          make(map[string]skuRecord),
		orders:        make(map[string]*orderRecord),
		byOwner:       make(map[string][]string),
	}
}

// Name 供应商标识。
func (p *PaymentsProvider) Name() string { return p.name }

// Capability 能力域:payments。
func (p *PaymentsProvider) Capability() providers.Capability { return providers.CapPayments }

// HealthCheck 内存存储常驻可用。
func (p *PaymentsProvider) HealthCheck(_ context.Context) error { return nil }

// ---- 管理面(价格表登记 / 对账;不经玩家路由) ----

// AddSku 登记价格表条目(接入方定义 SKU;重复登记覆盖 = 改价,已建订单不受影响)。
func (p *PaymentsProvider) AddSku(id, productID, form string, amountCents int64, currency string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.skus[id]; !ok {
		p.skuSeq = append(p.skuSeq, id)
	}
	p.skus[id] = skuRecord{ID: id, ProductID: productID, Form: form, AmountCents: amountCents, Currency: currency}
}

// Reconcile 对账重发:枚举停 PAID(已付未发)的订单逐个重试发货(发货幂等由
// 接入方钩子兜底);返回(尝试数, 成功数)。断单可对账恢复的落点。
func (p *PaymentsProvider) Reconcile() (attempted, delivered int) {
	p.mu.Lock()
	var stuck []*orderRecord
	for _, o := range p.orders {
		if o.Status == StatusPaid {
			stuck = append(stuck, o)
		}
	}
	sort.Slice(stuck, func(i, j int) bool { return stuck[i].CreatedAt.Before(stuck[j].CreatedAt) })
	now := p.clock()
	var recovered []string
	for _, o := range stuck {
		attempted++
		if p.deliverOrderLocked(o, now) {
			delivered++
			recovered = append(recovered, o.ID)
		}
	}
	p.mu.Unlock()
	// 对账恢复的发货同样推送 delivered(v1.1:含对账恢复的重发)。
	for _, id := range recovered {
		p.notify(EventDelivered, orderEventDTO{OrderID: id})
	}
	return attempted, delivered
}

// CodeError 管理面错误:携带契约错误码(调用方据此写响应信封)。
type CodeError struct{ Code string }

func (e *CodeError) Error() string { return e.Code }

// CloseOrder 管理面关单:CREATED/PAID → CLOSED;已发货订单不可关(409 PAYMENT_ORDER_STATE)。
func (p *PaymentsProvider) CloseOrder(orderID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	o, ok := p.orders[orderID]
	if !ok {
		return &CodeError{Code: aggregation.CodePaymentOrderNotFound}
	}
	if o.Status == StatusDelivered {
		return &CodeError{Code: aggregation.CodePaymentOrderState}
	}
	o.Status = StatusClosed
	o.UpdatedAt = p.clock()
	return nil
}

// ---- HTTP:玩家端点(全 Bearer)+ S2S 回调(渠道签名) ----

// ServeHTTP 分发 /v1/payments/*;callback 无 Bearer(渠道签名),其余 auth 包裹。
func (p *PaymentsProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.TrimPrefix(r.URL.Path, prefixPayments) == "callback" {
		p.handleCallback(w, r) // S2S:签名即认证,不走 Bearer 链
		return
	}
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *PaymentsProvider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixPayments)
	switch {
	case path == "skus" && r.Method == http.MethodGet:
		p.handleSkus(w, r)
	case path == "orders" && r.Method == http.MethodPost:
		p.handleCreateOrder(w, r)
	case path == "orders" && r.Method == http.MethodGet:
		p.handleListOrders(w, r)
	case strings.HasPrefix(path, "orders/") && r.Method == http.MethodGet:
		p.handleGetOrder(w, r, strings.TrimPrefix(path, "orders/"))
	default:
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

func (p *PaymentsProvider) handleSkus(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	items := make([]skuDTO, 0, len(p.skuSeq))
	for _, id := range p.skuSeq {
		s := p.skus[id]
		items = append(items, skuDTO{
			ID: s.ID, ProductID: s.ProductID, Form: s.Form,
			AmountCents: s.AmountCents, Currency: s.Currency,
		})
	}
	p.mu.Unlock()
	aggregation.WriteData(w, skuPageDTO{Items: items, NextCursor: ""})
}

func (p *PaymentsProvider) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SkuID string `json:"skuId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.SkuID) == "" {
		p.writeInvalid(w, r, "body 须为 {\"skuId\":\"...\"}")
		return
	}
	p.mu.Lock()
	sku, ok := p.skus[req.SkuID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodePaymentSkuNotFound, "sku 不可购: "+req.SkuID)
		return
	}
	// 风控前置在锁内做定价读取、锁外执行?契约语义:拒绝即不下单。取定价事实后执行。
	amount := sku.AmountCents
	p.mu.Unlock()

	id, _ := auth.FromContext(r.Context())
	if p.riskCheck != nil {
		if err := p.riskCheck(id.AccountID, amount); err != nil {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodePaymentRiskRejected, "风控前置拒绝: "+err.Error())
			return
		}
	}

	now := p.clock()
	o := &orderRecord{
		ID:        "order_" + newOrderID(now, id.AccountID, req.SkuID),
		Owner:     id.AccountID,
		Status:    StatusCreated,
		Sku:       sku,
		CreatedAt: now,
		UpdatedAt: now,
		PayToken:  "sbox_" + hexfnv(now, id.AccountID+"|"+req.SkuID),
	}
	p.mu.Lock()
	p.orders[o.ID] = o
	p.byOwner[o.Owner] = append(p.byOwner[o.Owner], o.ID)
	p.mu.Unlock()
	aggregation.WriteData(w, orderDTOOf(o, true))
}

func (p *PaymentsProvider) handleListOrders(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	items := make([]orderDTO, 0)
	for _, oid := range p.byOwner[id.AccountID] {
		items = append(items, orderDTOOf(p.orders[oid], false))
	}
	p.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	aggregation.WriteData(w, orderPageDTO{Items: items, NextCursor: ""})
}

func (p *PaymentsProvider) handleGetOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	o, ok := p.orders[orderID]
	if !ok || o.Owner != id.AccountID {
		// 归属校验:非本人一律 404,不泄露存在性(契约红线 4)。
		p.mu.Unlock()
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	dto := orderDTOOf(o, false)
	p.mu.Unlock()
	aggregation.WriteData(w, dto)
}

// handleCallback S2S 渠道回调:签名验证先于 body 解析(raw body 参与 HMAC);
// 密钥未配置/签名缺失或不匹配 = 一律 403 PAYMENT_INVALID_SIGNATURE(伪造全部拒绝)。
func (p *PaymentsProvider) handleCallback(w http.ResponseWriter, r *http.Request) {
	traceID := middleware.TraceID(r.Context())
	if r.Method != http.MethodPost {
		aggregation.WriteError(w, traceID, aggregation.CodeNotFound, "callback 须 POST")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		aggregation.WriteError(w, traceID, aggregation.CodePaymentInvalidSign, "body 不可读")
		return
	}
	if p.channelSecret == "" || !hmacEqual(p.channelSecret, raw, r.Header.Get("X-Payment-Signature")) {
		aggregation.WriteError(w, traceID, aggregation.CodePaymentInvalidSign, "签名校验失败")
		return
	}

	var req struct {
		OrderID string `json:"orderId"`
		PaidAt  string `json:"paidAt"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.OrderID == "" {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "body 须为 {orderId,...}")
		return
	}

	p.mu.Lock()
	o, ok := p.orders[req.OrderID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	switch o.Status {
	case StatusPaid, StatusDelivered, StatusClosed:
		// 幂等受理:重复回调不回退、不二次发货(契约红线 3)。
		p.mu.Unlock()
		aggregation.WriteData(w, orderDTOOf(o, false))
		return
	}
	paidAt := p.clock()
	if t, err := time.Parse(time.RFC3339, req.PaidAt); err == nil {
		paidAt = t
	}
	o.Status = StatusPaid
	o.PaidAt = &paidAt
	o.UpdatedAt = paidAt
	deliveredNow := p.deliverOrderLocked(o, paidAt) // 发货失败不回滚 PAID:停 PAID 等对账(契约红线 6)
	p.mu.Unlock()

	// 推送只报状态转移,不报受理(幂等重复回调走上面提前返回,不重发事件)。
	p.notify(EventPaid, orderEventDTO{OrderID: o.ID})
	if deliveredNow {
		p.notify(EventDelivered, orderEventDTO{OrderID: o.ID})
	}

	// 回调本身已受理(200);发货进度看订单状态(轮询)。
	aggregation.WriteData(w, orderDTOOf(o, false))
}

// deliverOrderLocked 发货并推进状态(调用方须持锁;钩子为接入方代码,须快返);
// 返回是否转 DELIVERED。
func (p *PaymentsProvider) deliverOrderLocked(o *orderRecord, now time.Time) bool {
	if p.deliver != nil {
		if err := p.deliver(Delivery{
			OrderID:     o.ID,
			AccountID:   o.Owner,
			SkuID:       o.Sku.ID,
			ProductID:   o.Sku.ProductID,
			Form:        o.Sku.Form,
			AmountCents: o.Sku.AmountCents,
			Currency:    o.Sku.Currency,
		}); err != nil {
			return false // 停 PAID,不丢单(对账恢复)
		}
	}
	o.Status = StatusDelivered
	o.DeliveredAt = &now
	o.UpdatedAt = now
	return true
}

func (p *PaymentsProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// hmacEqual 恒时比较 hex(HMAC-SHA256(secret, rawBody)) 与请求头签名。
func hmacEqual(secret string, raw []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.ToLower(strings.TrimSpace(sig))))
}

func hexfnv(t time.Time, seed string) string {
	h := uint64(1469598103934665603)
	s := t.UTC().Format(time.RFC3339Nano) + "|" + seed
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return hex.EncodeToString([]byte{
		byte(h >> 56), byte(h >> 48), byte(h >> 40), byte(h >> 32),
		byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h),
	})
}

func newOrderID(t time.Time, owner, skuID string) string {
	return hexfnv(t, owner+"|"+skuID)[:16]
}

// ---- wire DTO(契约数据模型,camelCase;缺省边界输出 null/不下发) ----

type skuDTO struct {
	ID          string `json:"id"`
	ProductID   string `json:"productId"`
	Form        string `json:"form"`
	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`
}

type skuPageDTO struct {
	Items      []skuDTO `json:"items"`
	NextCursor string   `json:"nextCursor"`
}

type orderDTO struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	SkuID       string `json:"skuId"`
	ProductID   string `json:"productId"`
	Form        string `json:"form"`
	AmountCents int64  `json:"amountCents"`
	Currency    string `json:"currency"`
	PayToken    string `json:"payToken,omitempty"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	PaidAt      string `json:"paidAt,omitempty"`
	DeliveredAt string `json:"deliveredAt,omitempty"`
}

type orderPageDTO struct {
	Items      []orderDTO `json:"items"`
	NextCursor string     `json:"nextCursor"`
}

// orderEventDTO 推送事件载荷(v1.1 最小提示:只含 orderId;广播信道无归属,业务字段不下发)。
type orderEventDTO struct {
	OrderID string `json:"orderId"`
}

// orderDTOOf 订单快照;withToken 仅下单响应回传 payToken(契约:只出现一次)。
func orderDTOOf(o *orderRecord, withToken bool) orderDTO {
	dto := orderDTO{
		ID:          o.ID,
		Status:      o.Status,
		SkuID:       o.Sku.ID,
		ProductID:   o.Sku.ProductID,
		Form:        o.Sku.Form,
		AmountCents: o.Sku.AmountCents,
		Currency:    o.Sku.Currency,
		CreatedAt:   formatMilli(o.CreatedAt),
		UpdatedAt:   formatMilli(o.UpdatedAt),
	}
	if withToken {
		dto.PayToken = o.PayToken
	}
	if o.PaidAt != nil {
		dto.PaidAt = formatMilli(*o.PaidAt)
	}
	if o.DeliveredAt != nil {
		dto.DeliveredAt = formatMilli(*o.DeliveredAt)
	}
	return dto
}

func formatMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
