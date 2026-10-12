// 举报与处罚申诉(support.md v1.1「举报与处罚申诉」节,F45):举报受理 + 处罚只读投影 +
// 申诉入口。处罚的判定与执行全在管理面(croupier / GM / 接入方风控),网关不执行风控;
// 投影源挡在 PenaltyStore 后(换实现 = 换配置,契约不动)。默认未配置 = fail-closed
// 501 COMMON_CAPABILITY_DISABLED(同 Signer 口径,生产 main.go 不内置假风控数据)。
// 举报本身是玩家侧写入,落 Provider 内存登记(与工单同),不依赖投影源。
package croupier

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
)

// ErrAppealExists 投影源返回:该处罚已有进行中申诉(409 SUPPORT_APPEAL_EXISTS)。
var ErrAppealExists = errors.New("appeal exists")

const maxReportDetailChars = 1000 // 契约:detail ≤1000 字符

// 举报对象类型与理由(契约登记集合;管理面可扩,客户端容忍未知)。
var reportTargetTypes = map[string]bool{"PLAYER": true, "MESSAGE": true, "TICKET": true}
var reportReasons = map[string]bool{
	"CHEATING": true, "ABUSE": true, "SPAM": true, "INAPPROPRIATE": true, "OTHER": true,
}

// reportRequest 举报请求体(契约 support.md F45)。
type reportRequest struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Reason     string `json:"reason"`
	Detail     string `json:"detail"`
}

// reportDTO 举报受理响应 data:仅 id(进展不向举报人反馈,防骚扰设计)。
type reportDTO struct {
	ID string `json:"id"`
}

// appealDTO 申诉状态(契约 support.md F45)。
type appealDTO struct {
	State       string `json:"state"`
	SubmittedAt string `json:"submittedAt"`
	DecidedAt   string `json:"decidedAt,omitempty"`
}

// penaltyDTO 处罚投影(契约 support.md F45);reason 面向玩家,不含内部与坐席信息。
type penaltyDTO struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Reason    string     `json:"reason"`
	State     string     `json:"state"`
	IssuedAt  string     `json:"issuedAt"`
	ExpiresAt string     `json:"expiresAt,omitempty"`
	Appeal    *appealDTO `json:"appeal,omitempty"`
}

type penaltyPageDTO struct {
	Items      []penaltyDTO `json:"items"`
	NextCursor string       `json:"nextCursor"`
}

// PenaltyItem 管理面投影行(时间由网关统一格式化;type/reason 语义网关不解释)。
type PenaltyItem struct {
	ID        string
	Type      string
	Reason    string
	State     string
	IssuedAt  time.Time
	ExpiresAt *time.Time
	Appeal    *AppealItem
}

type AppealItem struct {
	State       string
	SubmittedAt time.Time
	DecidedAt   *time.Time
}

// PenaltyStore 处罚投影源:管理面签发处罚与判定申诉,网关只读投影 + 申诉入口。
// 实现方接 croupier 内部管理面或接入方风控系统。
type PenaltyStore interface {
	// List 返回该玩家全部处罚(issuedAt 倒序;分页由网关本地完成)。
	List(owner string) []PenaltyItem
	// Get 单条处罚;ok=false 一律归一 404 SUPPORT_PENALTY_NOT_FOUND(不泄露存在性)。
	Get(owner, penaltyID string) (PenaltyItem, bool)
	// Appeal 登记申诉;ErrAppealExists = 已有进行中申诉。
	Appeal(owner, penaltyID string) (PenaltyItem, error)
}

// reportRecord 举报登记行(管理面侧消费;玩家侧无读端点)。
type reportRecord struct {
	id         string
	owner      string
	targetType string
	targetID   string
	reason     string
	detail     string
	createdAt  time.Time
}

// handleReport POST /v1/support/reports(Bearer):受理举报,返回 id。
func (p *SupportProvider) handleReport(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	var req reportRequest
	if !p.decode(w, r, &req) {
		return
	}
	if err := validReportRequest(req); err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	now := p.clock()
	recID := "rpt_" + newUUIDv7(now)
	p.mu.Lock()
	p.reports = append(p.reports, reportRecord{
		id: recID, owner: id.AccountID,
		targetType: req.TargetType, targetID: req.TargetID,
		reason: req.Reason, detail: req.Detail, createdAt: now,
	})
	p.mu.Unlock()
	aggregation.WriteData(w, reportDTO{ID: recID})
}

// handleListPenalties GET /v1/support/penalties(Bearer):我的处罚(issuedAt 倒序,分页)。
func (p *SupportProvider) handleListPenalties(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	if p.penalties == nil { // 未配置投影源:fail-closed
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeCapabilityDisabled, "penalty projection not configured")
		return
	}
	limit, cursor, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	all := sortPenalties(p.penalties.List(id.AccountID))
	items, next := pageSlice(all, limit, cursor)
	page := make([]penaltyDTO, 0, len(items))
	for _, it := range items {
		page = append(page, penaltyDTOOf(it))
	}
	aggregation.WriteData(w, penaltyPageDTO{Items: page, NextCursor: next})
}

// handleGetPenalty GET /v1/support/penalties/{penaltyId}(Bearer):详情含申诉状态;
// 非本人 / 不存在一律 404 SUPPORT_PENALTY_NOT_FOUND(不泄露存在性)。
func (p *SupportProvider) handleGetPenalty(w http.ResponseWriter, r *http.Request, penaltyID string) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	if p.penalties == nil {
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeCapabilityDisabled, "penalty projection not configured")
		return
	}
	item, found := p.penalties.Get(id.AccountID, penaltyID)
	if !found {
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportPenaltyNotFound, "penalty not found")
		return
	}
	aggregation.WriteData(w, penaltyDTOOf(item))
}

// handleAppeal POST /v1/support/penalties/{penaltyId}/appeal(Bearer):
// 每处罚至多一个进行中申诉;已有 → 409 SUPPORT_APPEAL_EXISTS。
func (p *SupportProvider) handleAppeal(w http.ResponseWriter, r *http.Request, penaltyID string) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	if p.penalties == nil {
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeCapabilityDisabled, "penalty projection not configured")
		return
	}
	item, err := p.penalties.Appeal(id.AccountID, penaltyID)
	if err != nil {
		if errors.Is(err, ErrAppealExists) {
			p.writeError(w, middleware.TraceID(r.Context()),
				aggregation.CodeSupportAppealExists, "appeal already exists")
			return
		}
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportPenaltyNotFound, "penalty not found")
		return
	}
	aggregation.WriteData(w, penaltyDTOOf(item))
}

// penaltyDTOOf 投影行 → wire DTO(时间统一毫秒精度;空指针 = 字段省略)。
func penaltyDTOOf(it PenaltyItem) penaltyDTO {
	dto := penaltyDTO{
		ID: it.ID, Type: it.Type, Reason: it.Reason, State: it.State,
		IssuedAt: formatMilli(it.IssuedAt),
	}
	if it.ExpiresAt != nil {
		dto.ExpiresAt = formatMilli(*it.ExpiresAt)
	}
	if it.Appeal != nil {
		dto.Appeal = &appealDTO{
			State: it.Appeal.State, SubmittedAt: formatMilli(it.Appeal.SubmittedAt),
		}
		if it.Appeal.DecidedAt != nil {
			dto.Appeal.DecidedAt = formatMilli(*it.Appeal.DecidedAt)
		}
	}
	return dto
}

// pageSlice 按游标切页:items 已排序(调用方保证),返回窗口与下一个游标。
func pageSlice(all []PenaltyItem, limit, cursor int) ([]PenaltyItem, string) {
	items := make([]PenaltyItem, 0, limit)
	next := ""
	if cursor < len(all) {
		end := cursor + limit
		if end > len(all) {
			end = len(all)
		}
		items = all[cursor:end]
		if end < len(all) {
			next = strconv.Itoa(end)
		}
	}
	return items, next
}

// validReportRequest 参数校验(契约:targetType/reason 登记集合;targetId 非空;
// detail ≤1000 字符)。
func validReportRequest(req reportRequest) error {
	if !reportTargetTypes[req.TargetType] {
		return errInvalid("targetType 须为 PLAYER/MESSAGE/TICKET")
	}
	if req.TargetID == "" {
		return errInvalid("targetId 必填")
	}
	if !reportReasons[req.Reason] {
		return errInvalid("reason 须为 CHEATING/ABUSE/SPAM/INAPPROPRIATE/OTHER")
	}
	if utf8.RuneCountInString(req.Detail) > maxReportDetailChars {
		return errInvalid("detail 超 1000 字符")
	}
	return nil
}

// sortPenalties 投影源未排序时的兜底(issuedAt 倒序)。
func sortPenalties(all []PenaltyItem) []PenaltyItem {
	sort.Slice(all, func(i, j int) bool { return all[i].IssuedAt.After(all[j].IssuedAt) })
	return all
}
