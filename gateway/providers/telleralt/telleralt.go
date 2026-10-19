// Package telleralt 支付第二实现(批次 72,契约 docs/contract/payment.md)。
//
// 与默认 teller 同一冻结契约,但订单状态表示模型刻意不同——证明契约约束的是
// 行为而非实现:
//   - teller(可变状态表):订单存可变记录,状态转移直接改 status/时间戳字段
//   - 本实现(追加式事件账本):每次状态转移追加不可变事件(created/paid/delivered/
//     closed),status/createdAt/updatedAt/paidAt/deliveredAt 全部为事件序列的
//     投影重放;无状态字段可改,历史不可变
//
// 两模型等价:投影规则与可变字段语义逐条对应(状态=末事件类型、createdAt=首事件、
// updatedAt=末事件、paidAt/deliveredAt=首个对应事件),同一操作序列产生同一 wire
// 快照。发布校验/签名 fail-closed/幂等受理/断单停 PAID/对账重发/关单保护/
// 推送事件逐字同源(契约行为);一致性证明:e2e/payments_conformance_test.go
// ——两实现同一 wire 场景同断言(批次 18/64/67/68/69/70/71 先例)。接入方切换 =
// {"payments":{"primary":"teller-alt"}} 一行配置。
package telleralt

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名;路由表 {"payments":{"primary":"teller-alt"}} 即换。
const DefaultName = "teller-alt"

const prefixPayments = "/v1/payments/"

// 订单状态(契约状态机:CREATED → PAID → DELIVERED;CREATED/PAID → CLOSED)。
const (
	StatusCreated   = "CREATED"
	StatusPaid      = "PAID"
	StatusDelivered = "DELIVERED"
	StatusClosed    = "CLOSED"
)

// 事件类型(账本追加单元;投影规则见 projectOrder)。
const (
	evCreated   = "created"
	evPaid      = "paid"
	evDelivered = "delivered"
	evClosed    = "closed"
)

// Options 构造参数(与 teller.Options 同形状)。
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
	// StateFile 订单事件账本快照路径(可选,空 = 纯内存态,与历史行为一致)。
	// 设置后:每次账本追加原子写全量快照(临时文件 + rename),构造时文件存在
	// 即回放投影——重启不再丢未完结订单(审核 2026-10-18 表三 F77 一期,零外部
	// 依赖;二期改 Options Store 接缝)。快照损坏 → HealthCheck fail-closed,
	// 不静默从空态启动丢单。SKU 价格表不快照:管理面启动时登记。
	StateFile string
	// Store 事件账本存储接缝(F77 二期 2026-10-19 代决:字节级全量快照;可选,
	// nil = 退回 StateFile / 纯内存)。设置后优先生效(与 StateFile 同设时文件
	// 不读写):每次账本追加经 Save 写全量,构造时 Load 回放投影——接入方按部署
	// 面接 DB / Redis / 对象存储。返回 ErrSnapshotMissing = 首次启动(等价文件
	// 不存在);其余读错误与损坏字节同 fail-closed(HealthCheck → 503,不静默
	// 从空态启动丢单)。接缝只换字节,账本结构留在 Provider 内。
	Store OrderStore
}

// OrderStore 事件账本持久化接缝(F77 二期已代决:字节 blob 形状,与 teller
// 同构)。只交换全量快照字节,账本结构 / 键序 / 归属索引留在 Provider 内
// (被否选项见 research/decisions.md D-002)。
type OrderStore interface {
	// Load 读回最近一次 Save 的字节;无快照返回 ErrSnapshotMissing。
	Load() ([]byte, error)
	// Save 全量覆盖写;实现方须保证写失败不产生半态(原子 / 事务)。
	Save(b []byte) error
}

// ErrSnapshotMissing Store 无快照(首次启动)哨兵;Load 实现按 errors.Is 判定。
var ErrSnapshotMissing = errors.New("telleralt: order store snapshot missing")

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

// orderEvent 账本事件(不可变追加单元;异构面)。
type orderEvent struct {
	Type string
	At   time.Time
}

// journalEntry 订单账本(追加式;无状态字段,状态 = 投影重放)。
type journalEntry struct {
	ID     string
	Owner  string
	Sku    skuRecord
	Token  string
	Events []orderEvent
}

// orderView 投影结果(读路径全部经此;与 teller.orderRecord 字段语义一一对应)。
type orderView struct {
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	PaidAt      *time.Time
	DeliveredAt *time.Time
}

// projectOrder 投影重放:状态=末事件类型;createdAt=首事件;updatedAt=末事件;
// paidAt/deliveredAt=首个对应事件(幂等:重复转移不追加,故「首个」即「唯一」)。
func projectOrder(e *journalEntry) orderView {
	v := orderView{Status: StatusCreated}
	for i, ev := range e.Events {
		if i == 0 {
			v.CreatedAt = ev.At
		}
		v.UpdatedAt = ev.At
		switch ev.Type {
		case evCreated:
			v.Status = StatusCreated
		case evPaid:
			v.Status = StatusPaid
			t := ev.At
			v.PaidAt = &t
		case evDelivered:
			v.Status = StatusDelivered
			t := ev.At
			v.DeliveredAt = &t
		case evClosed:
			v.Status = StatusClosed
		}
	}
	return v
}

// PaymentsProvider 订单-发货核心流第二实现(事件账本模型;沙箱渠道同契约)。
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
	orders  map[string]*journalEntry
	byOwner map[string][]string // accountID → 登记序 orderIDs

	stateFile string     // StateFile 快照路径(空 = 纯内存)
	store     OrderStore // 二期存储接缝(nil = 未配置;非 nil 时优先于 stateFile)
	stateErr  error      // 快照回放/写入失败(HealthCheck fail-closed,不静默丢单)
}

// New 构造支付 Provider 第二实现。
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
	p := &PaymentsProvider{
		name:          opts.Name,
		clock:         opts.Clock,
		auth:          authMW,
		channelSecret: opts.ChannelSecret,
		deliver:       opts.Deliver,
		riskCheck:     opts.RiskCheck,
		notify:        opts.Notify,
		stateFile:     opts.StateFile,
		store:         opts.Store,
		skus:          make(map[string]skuRecord),
		orders:        make(map[string]*journalEntry),
		byOwner:       make(map[string][]string),
	}
	p.loadState()
	return p
}

// snapshot 快照形状:事件账本全量(重启回放投影即恢复状态机)。
// SKU 价格表不快照(管理面启动登记)。
type snapshot struct {
	Orders map[string]*journalEntry `json:"orders"`
}

// loadState 回放快照(StateFile 配置时)。不存在 = 首次启动;损坏/不可读 =
// 记 stateErr → HealthCheck fail-closed,不静默从空态启动丢单;运维删文件重启。
func (p *PaymentsProvider) loadState() {
	if p.store != nil {
		b, err := p.store.Load()
		if err != nil {
			if !errors.Is(err, ErrSnapshotMissing) {
				p.stateErr = err // 存储故障不静默以空态启动
			}
			return
		}
		var s snapshot
		if err := json.Unmarshal(b, &s); err != nil {
			p.stateErr = err
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.restoreLocked(s.Orders)
		return
	}
	if p.stateFile == "" {
		return
	}
	b, err := os.ReadFile(p.stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			p.stateErr = err
		}
		return
	}
	var s snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		p.stateErr = err
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restoreLocked(s.Orders)
}

// restoreLocked 用快照重建账本与归属索引(须持 p.mu)。
func (p *PaymentsProvider) restoreLocked(orders map[string]*journalEntry) {
	p.orders = make(map[string]*journalEntry, len(orders))
	p.byOwner = make(map[string][]string)
	for id, e := range orders {
		if e == nil {
			continue
		}
		e.ID = id // 以快照键为准,防键与账目 ID 不一致
		p.orders[id] = e
		p.byOwner[e.Owner] = append(p.byOwner[e.Owner], id)
	}
}

// persistLocked 原子写全量快照(临时文件 + rename;须持 p.mu)。失败记
// stateErr → HealthCheck fail-closed。stateErr 非 nil 后不再重试(避免反复写坏),
// 由 HealthCheck 暴露给运维。
func (p *PaymentsProvider) persistLocked() {
	if p.stateErr != nil || (p.store == nil && p.stateFile == "") {
		return
	}
	b, err := json.Marshal(snapshot{Orders: p.orders})
	if err != nil {
		p.stateErr = err
		return
	}
	if p.store != nil {
		if serr := p.store.Save(b); serr != nil {
			p.stateErr = serr
		}
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.stateFile), ".courier-orders-*.tmp")
	if err != nil {
		p.stateErr = err
		return
	}
	name := tmp.Name()
	if _, werr := tmp.Write(b); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		p.stateErr = werr
		return
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(name)
		p.stateErr = cerr
		return
	}
	if ren := os.Rename(name, p.stateFile); ren != nil {
		_ = os.Remove(name)
		p.stateErr = ren
	}
}

// Name 供应商标识。
func (p *PaymentsProvider) Name() string { return p.name }

// Capability 能力域:payments。
func (p *PaymentsProvider) Capability() providers.Capability { return providers.CapPayments }

// HealthCheck 内存存储常驻可用;快照不可用 = fail-closed(不静默丢单)。
func (p *PaymentsProvider) HealthCheck(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stateErr != nil {
		return fmt.Errorf("teller-alt: order state snapshot unavailable: %w", p.stateErr)
	}
	return nil
}

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
// 异构面:teller 全表扫描可变状态找停 PAID;本实现投影重放每个账本取末事件。
func (p *PaymentsProvider) Reconcile() (attempted, delivered int) {
	p.mu.Lock()
	var stuck []*journalEntry
	for _, e := range p.orders {
		if projectOrder(e).Status == StatusPaid {
			stuck = append(stuck, e)
		}
	}
	sort.Slice(stuck, func(i, j int) bool { return projectOrder(stuck[i]).CreatedAt.Before(projectOrder(stuck[j]).CreatedAt) })
	now := p.clock()
	var recovered []string
	for _, e := range stuck {
		attempted++
		if p.deliverJournalLocked(e, now) {
			delivered++
			recovered = append(recovered, e.ID)
		}
	}
	p.persistLocked()
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
	e, ok := p.orders[orderID]
	if !ok {
		return &CodeError{Code: aggregation.CodePaymentOrderNotFound}
	}
	if projectOrder(e).Status == StatusDelivered {
		return &CodeError{Code: aggregation.CodePaymentOrderState}
	}
	e.Events = append(e.Events, orderEvent{Type: evClosed, At: p.clock()})
	p.persistLocked()
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
	e := &journalEntry{
		Owner:  id.AccountID,
		Sku:    sku,
		Token:  "sbox_" + hexfnv(now, id.AccountID+"|"+req.SkuID),
		Events: []orderEvent{{Type: evCreated, At: now}},
	}
	p.mu.Lock()
	// ID 在锁内生成并查重,与落库原子(审核 2026-10-18 表一#5:旧 hexfnv 16 hex
	// 碰撞会覆盖已有订单;UUIDv7 + 查重后不可能覆盖)。
	e.ID = p.nextOrderID(now)
	p.orders[e.ID] = e
	p.byOwner[e.Owner] = append(p.byOwner[e.Owner], e.ID)
	p.persistLocked()
	p.mu.Unlock()
	aggregation.WriteData(w, orderDTOOf(e, projectOrder(e), true))
}

func (p *PaymentsProvider) handleListOrders(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	items := make([]orderDTO, 0)
	for _, oid := range p.byOwner[id.AccountID] {
		e := p.orders[oid]
		items = append(items, orderDTOOf(e, projectOrder(e), false))
	}
	p.mu.Unlock()
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	aggregation.WriteData(w, orderPageDTO{Items: items, NextCursor: ""})
}

func (p *PaymentsProvider) handleGetOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	e, ok := p.orders[orderID]
	if !ok || e.Owner != id.AccountID {
		// 归属校验:非本人一律 404,不泄露存在性(契约红线 4)。
		p.mu.Unlock()
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	dto := orderDTOOf(e, projectOrder(e), false)
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
	e, ok := p.orders[req.OrderID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	view := projectOrder(e)
	switch view.Status {
	case StatusPaid, StatusDelivered, StatusClosed:
		// 幂等受理:重复回调不回退、不二次发货(契约红线 3)。
		p.mu.Unlock()
		aggregation.WriteData(w, orderDTOOf(e, view, false))
		return
	}
	paidAt := p.clock()
	if t, err := time.Parse(time.RFC3339, req.PaidAt); err == nil {
		paidAt = t
	}
	e.Events = append(e.Events, orderEvent{Type: evPaid, At: paidAt})
	deliveredNow := p.deliverJournalLocked(e, paidAt) // 发货失败不回滚 PAID:停 PAID 等对账(契约红线 6)
	p.persistLocked()
	p.mu.Unlock()

	// 推送只报状态转移,不报受理(幂等重复回调走上面提前返回,不重发事件)。
	p.notify(EventPaid, orderEventDTO{OrderID: e.ID})
	if deliveredNow {
		p.notify(EventDelivered, orderEventDTO{OrderID: e.ID})
	}

	// 回调本身已受理(200);发货进度看订单状态(轮询)。
	aggregation.WriteData(w, orderDTOOf(e, projectOrder(e), false))
}

// deliverJournalLocked 发货并追加 delivered 事件(调用方须持锁;钩子为接入方代码,
// 须快返);返回是否转 DELIVERED。
func (p *PaymentsProvider) deliverJournalLocked(e *journalEntry, now time.Time) bool {
	if p.deliver != nil {
		if err := p.deliver(Delivery{
			OrderID:     e.ID,
			AccountID:   e.Owner,
			SkuID:       e.Sku.ID,
			ProductID:   e.Sku.ProductID,
			Form:        e.Sku.Form,
			AmountCents: e.Sku.AmountCents,
			Currency:    e.Sku.Currency,
		}); err != nil {
			return false // 停 PAID,不丢单(对账恢复)
		}
	}
	e.Events = append(e.Events, orderEvent{Type: evDelivered, At: now})
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

// nextOrderID 生成未被占用的订单号(UUIDv7,契约 payment.md「订单 ID」;
// 审核整改 2026-10-18 表一#5)。须持 p.mu 调用;重试只作兜底,UUIDv7 的
// 74 位随机下碰撞概率可忽略。
func (p *PaymentsProvider) nextOrderID(t time.Time) string {
	for i := 0; i < 5; i++ {
		id := "order_" + newUUIDv7(t)
		if _, exists := p.orders[id]; !exists {
			return id
		}
	}
	return "order_" + newUUIDv7(t.Add(time.Millisecond)) // 理论不可达;绝不覆盖
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

// orderDTOOf 订单快照(账本静态字段 + 投影状态/时间戳);withToken 仅下单响应
// 回传 payToken(契约:只出现一次)。
func orderDTOOf(e *journalEntry, v orderView, withToken bool) orderDTO {
	dto := orderDTO{
		ID:          e.ID,
		Status:      v.Status,
		SkuID:       e.Sku.ID,
		ProductID:   e.Sku.ProductID,
		Form:        e.Sku.Form,
		AmountCents: e.Sku.AmountCents,
		Currency:    e.Sku.Currency,
		CreatedAt:   formatMilli(v.CreatedAt),
		UpdatedAt:   formatMilli(v.UpdatedAt),
	}
	if withToken {
		dto.PayToken = e.Token
	}
	if v.PaidAt != nil {
		dto.PaidAt = formatMilli(*v.PaidAt)
	}
	if v.DeliveredAt != nil {
		dto.DeliveredAt = formatMilli(*v.DeliveredAt)
	}
	return dto
}

func formatMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
