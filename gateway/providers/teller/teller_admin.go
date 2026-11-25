// teller 管理面 HTTP(v1.2 契约新增,只加不改):/v1/payments/admin/*。
//
// 玩家面/回调面零改动;管理面独立凭证(Authorization: Bearer <AdminToken>,
// fail-closed 401),不经玩家 Bearer-scope 路由。四端点:
//
//	POST /v1/payments/admin/skus                  登记/改价 SKU(服务端定价)
//	GET  /v1/payments/admin/orders                全局检索(gameId/status/accountId + 有界分页)
//	GET  /v1/payments/admin/orders/{orderId}      管理详情(含断单态 PAID 标识)
//	POST /v1/payments/admin/orders/{orderId}/redeliver  断单(停 PAID)对账重发
//	POST /v1/payments/admin/orders/{orderId}/refund     退款/冲正登记(REFUND/CORRECTION)
//
// 金额口径:退款金额不超实付(端点校验);EXECUTED/REJECTED 状态映射与分成
// 基数归接入方(spawn 管理适配),本域只登记冲减事实——与订单状态机正交。
package teller

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
)

// fourForms 契约四形态(订单发货目标)。
var fourForms = map[string]bool{
	"DIRECT_PURCHASE":  true,
	"ACCOUNT_WALLET":   true,
	"GAME_WALLET":      true,
	"EXTERNAL_PAYMENT": true,
}

// serveAdmin 管理面分发:先校验管理凭证(fail-closed),再按路径/方法分派。
func (p *PaymentsProvider) serveAdmin(w http.ResponseWriter, r *http.Request) {
	traceID := middleware.TraceID(r.Context())
	if !p.adminAuthorized(r) {
		aggregation.WriteError(w, traceID, aggregation.CodeUnauthenticated, "管理凭证缺失或无效")
		return
	}
	path := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefixPayments), "admin")
	switch {
	case path == "/skus" && r.Method == http.MethodPost:
		p.handleAdminSku(w, r)
	case path == "/orders" && r.Method == http.MethodGet:
		p.handleAdminListOrders(w, r)
	case strings.HasPrefix(path, "/orders/") && r.Method == http.MethodGet:
		p.handleAdminGetOrder(w, r, strings.TrimPrefix(path, "/orders/"))
	case strings.HasPrefix(path, "/orders/") && strings.HasSuffix(path, "/redeliver") && r.Method == http.MethodPost:
		p.handleAdminRedeliver(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/orders/"), "/redeliver"))
	case strings.HasPrefix(path, "/orders/") && strings.HasSuffix(path, "/refund") && r.Method == http.MethodPost:
		p.handleAdminRefund(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/orders/"), "/refund"))
	default:
		aggregation.WriteError(w, traceID, aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

// adminAuthorized 管理凭证校验:Authorization: Bearer <AdminToken>。
// 空配置/缺头/错值一律 false(fail-closed);恒时比较防时序侧信道。
func (p *PaymentsProvider) adminAuthorized(r *http.Request) bool {
	if p.adminToken == "" {
		return false
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimSpace(h[len(prefix):])
	if got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(p.adminToken)) == 1
}

// handleAdminSku 登记/改价 SKU(重复登记覆盖 = 改价,已建订单不受影响——与
// AddSku 语义一致,只是补了 HTTP 面)。服务端定价事实,接入方按 game 归属登记。
func (p *PaymentsProvider) handleAdminSku(w http.ResponseWriter, r *http.Request) {
	traceID := middleware.TraceID(r.Context())
	var req struct {
		SkuID       string `json:"skuId"`
		ProductID   string `json:"productId"`
		Form        string `json:"form"`
		AmountCents int64  `json:"amountCents"`
		Currency    string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "body 不可解析")
		return
	}
	if strings.TrimSpace(req.SkuID) == "" || strings.TrimSpace(req.ProductID) == "" ||
		strings.TrimSpace(req.Currency) == "" || !fourForms[req.Form] || req.AmountCents <= 0 {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument,
			"sku 须为 {skuId,productId,form∈四形态,amountCents>0,currency}")
		return
	}
	p.AddSku(req.SkuID, req.ProductID, req.Form, req.AmountCents, req.Currency)
	aggregation.WriteData(w, map[string]any{
		"sku": skuDTO{
			ID: req.SkuID, ProductID: req.ProductID, Form: req.Form,
			AmountCents: req.AmountCents, Currency: req.Currency,
		},
	})
}

// handleAdminListOrders 全局订单检索:gameId/status/accountId 筛选 + 有界分页
// (limit 缺省 20/最大 100,nextCursor keyset = updatedAt+orderID base64)。
// 排序 updatedAt 倒序(与玩家面列表一致);不下发 payToken。
func (p *PaymentsProvider) handleAdminListOrders(w http.ResponseWriter, r *http.Request) {
	traceID := middleware.TraceID(r.Context())
	q := r.URL.Query()
	gameID := q.Get("gameId")
	status := q.Get("status")
	accountID := q.Get("accountId")
	limit := 20
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "limit 须为正整数")
			return
		}
		if n > 100 {
			n = 100
		}
		limit = n
	}
	curTime, curID, hasCursor := decodeAdminCursor(q.Get("cursor"))

	p.mu.Lock()
	items := make([]orderRecord, 0, len(p.orders))
	for _, o := range p.orders {
		if gameID != "" && o.GameID != gameID {
			continue
		}
		if status != "" && o.Status != status {
			continue
		}
		if accountID != "" && o.Owner != accountID {
			continue
		}
		items = append(items, *o)
	}
	p.mu.Unlock()

	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].ID > items[j].ID
	})
	if hasCursor {
		filtered := items[:0]
		for _, it := range items {
			if afterAdminCursor(it, curTime, curID) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	next := ""
	if len(items) > limit {
		next = encodeAdminCursor(items[limit-1].UpdatedAt, items[limit-1].ID)
		items = items[:limit]
	}
	dtos := make([]adminOrderDTO, 0, len(items))
	for i := range items {
		dtos = append(dtos, adminOrderDTOOf(&items[i]))
	}
	aggregation.WriteData(w, adminOrderPageDTO{Items: dtos, NextCursor: next})
}

// handleAdminGetOrder 管理订单详情(含断单态 PAID 标识与退款登记事实)。
func (p *PaymentsProvider) handleAdminGetOrder(w http.ResponseWriter, r *http.Request, orderID string) {
	traceID := middleware.TraceID(r.Context())
	p.mu.Lock()
	o, ok := p.orders[orderID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	dto := adminOrderDTOOf(o)
	p.mu.Unlock()
	aggregation.WriteData(w, dto)
}

// handleAdminRedeliver 断单对账重发:仅停 PAID(已付未发)可重发;DELIVERED/CLOSED
// 409 不二次发货(幂等红线);CREATED 未付款 409。同步执行发货钩子,成败回
// DELIVERED / 留 PAID(redelivered 标志如实报告)。
func (p *PaymentsProvider) handleAdminRedeliver(w http.ResponseWriter, r *http.Request, orderID string) {
	traceID := middleware.TraceID(r.Context())
	if orderID == "" {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "orderId 缺失")
		return
	}
	p.mu.Lock()
	o, ok := p.orders[orderID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	status := o.Status
	p.mu.Unlock()
	if status != StatusPaid {
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderState,
			"仅停 PAID(已付未发)可重发,当前 "+status)
		return
	}
	now := p.clock()
	advanced, snap := p.deliverAndAdvance(o, now)
	if advanced {
		p.notify(EventDelivered, orderEventDTO{OrderID: o.ID}) // 对账恢复的重发同样推送(v1.1)
	}
	aggregation.WriteData(w, map[string]any{"order": snap, "redelivered": advanced})
}

// handleAdminRefund 退款/冲正登记:REFUND / CORRECTION;订单须已付款(paidAt 非
// 空,即 PAID/DELIVERED);金额 ≤ 实付,≤0 记全额;重复登记同动作 = 幂等返回既有。
// 只登记冲减事实,不改订单状态(状态机正交);EXECUTED/REJECTED 映射归接入方。
func (p *PaymentsProvider) handleAdminRefund(w http.ResponseWriter, r *http.Request, orderID string) {
	traceID := middleware.TraceID(r.Context())
	if orderID == "" {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "orderId 缺失")
		return
	}
	var req struct {
		Action      string `json:"action"`
		AmountCents int64  `json:"amountCents"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "body 不可解析")
		return
	}
	action := strings.ToUpper(strings.TrimSpace(req.Action))
	if action == "" {
		action = "REFUND"
	}
	if action != "REFUND" && action != "CORRECTION" {
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "action 仅支持 REFUND/CORRECTION")
		return
	}

	p.mu.Lock()
	o, ok := p.orders[orderID]
	if !ok {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderNotFound, "订单不存在")
		return
	}
	if o.PaidAt == nil {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodePaymentOrderState, "订单未付款,不可退款")
		return
	}
	for _, rf := range o.Refunds {
		if rf.Action == action { // 幂等:重复登记同动作返回既有,不叠加
			agg := aggregateRefund(rf)
			p.mu.Unlock()
			aggregation.WriteData(w, adminRefundDTO{OrderID: orderID, Refund: agg})
			return
		}
	}
	amount := req.AmountCents
	if amount <= 0 {
		amount = o.Sku.AmountCents // ≤0 = 记全额(实付)
	}
	if amount > o.Sku.AmountCents {
		p.mu.Unlock()
		aggregation.WriteError(w, traceID, aggregation.CodeInvalidArgument, "退款金额不能超过实付")
		return
	}
	rf := refundRecord{Action: action, AmountCents: amount, Reason: req.Reason, At: p.clock()}
	o.Refunds = append(o.Refunds, rf)
	o.UpdatedAt = rf.At
	p.persistLocked()
	p.mu.Unlock()

	aggregation.WriteData(w, adminRefundDTO{OrderID: orderID, Refund: aggregateRefund(rf)})
}

// ---- admin wire DTO(管理面数据模型;不下发 payToken) ----

type adminOrderDTO struct {
	ID          string           `json:"id"`
	Status      string           `json:"status"`
	AccountID   string           `json:"accountId"`
	GameID      string           `json:"gameId"`
	SkuID       string           `json:"skuId"`
	ProductID   string           `json:"productId"`
	Form        string           `json:"form"`
	AmountCents int64            `json:"amountCents"`
	Currency    string           `json:"currency"`
	CreatedAt   string           `json:"createdAt"`
	UpdatedAt   string           `json:"updatedAt"`
	PaidAt      string           `json:"paidAt,omitempty"`
	DeliveredAt string           `json:"deliveredAt,omitempty"`
	Refunds     []adminRefundDTO `json:"refunds,omitempty"`
}

type adminOrderPageDTO struct {
	Items      []adminOrderDTO `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

type adminRefundDTO struct {
	OrderID string    `json:"orderId"`
	Refund  refundDTO `json:"refund"`
}

type refundDTO struct {
	Action      string `json:"action"`
	AmountCents int64  `json:"amountCents"`
	Reason      string `json:"reason"`
	At          string `json:"at"`
}

func adminOrderDTOOf(o *orderRecord) adminOrderDTO {
	dto := adminOrderDTO{
		ID:          o.ID,
		Status:      o.Status,
		AccountID:   o.Owner,
		GameID:      o.GameID,
		SkuID:       o.Sku.ID,
		ProductID:   o.Sku.ProductID,
		Form:        o.Sku.Form,
		AmountCents: o.Sku.AmountCents,
		Currency:    o.Sku.Currency,
		CreatedAt:   formatMilli(o.CreatedAt),
		UpdatedAt:   formatMilli(o.UpdatedAt),
	}
	if o.PaidAt != nil {
		dto.PaidAt = formatMilli(*o.PaidAt)
	}
	if o.DeliveredAt != nil {
		dto.DeliveredAt = formatMilli(*o.DeliveredAt)
	}
	if len(o.Refunds) > 0 {
		dto.Refunds = make([]adminRefundDTO, 0, len(o.Refunds))
		for _, rf := range o.Refunds {
			dto.Refunds = append(dto.Refunds, adminRefundDTO{OrderID: o.ID, Refund: aggregateRefund(rf)})
		}
	}
	return dto
}

func aggregateRefund(rf refundRecord) refundDTO {
	return refundDTO{Action: rf.Action, AmountCents: rf.AmountCents, Reason: rf.Reason, At: formatMilli(rf.At)}
}

// ---- 分页游标(keyset = updatedAt + orderID,base64 不透明) ----

func encodeAdminCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeAdminCursor(s string) (time.Time, string, bool) {
	if s == "" {
		return time.Time{}, "", false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", false
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", false
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, "", false
	}
	return t, parts[1], true
}

// afterAdminCursor 判断订单是否严格排在游标之后(updatedAt 倒序,id 倒序平局)。
func afterAdminCursor(o orderRecord, curTime time.Time, curID string) bool {
	if !o.UpdatedAt.Equal(curTime) {
		return o.UpdatedAt.Before(curTime)
	}
	return o.ID < curID
}
