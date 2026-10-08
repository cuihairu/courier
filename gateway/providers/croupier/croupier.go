// Package croupier 客服默认 Provider(M2,契约 docs/contract/support.md)。
//
// 能力域 support(/v1/support/*),玩家侧:提单、追加消息、列表/详情、FAQ 检索;
// 坐席侧(回复/关单)走管理面——本包 AgentReply/AgentClose 即管理面入口,
// 操作后经 Notify 钩子广播 support.ticket_replied(接 chirp.Hub;nil = 不推送)。
// 工单归属校验:非本人工单一律 SUPPORT_TICKET_NOT_FOUND(不泄露存在性)。
package croupier

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "croupier"

// EventTicketReplied 推送事件 type(契约 messages.md 事件登记;回复与关单都发)。
const EventTicketReplied = "support.ticket_replied"

const prefixSupport = "/v1/support/"

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(默认 time.Now)。
	Clock func() time.Time
	// RequireAuth 认证中间件(main 注入;nil = fail-closed 一律 401)。
	RequireAuth func(http.Handler) http.Handler
	// Notify 推送钩子(support.ticket_replied,参数为 replyEventDTO);nil = 不推送。
	Notify func(eventType string, data any)
	// TicketRatePerMinute / TicketRateBurst 提单限流(契约:默认 5/分钟)。
	TicketRatePerMinute int
	TicketRateBurst     int
}

// replyEventDTO 推送事件 data(契约 messages.md:{ticketId})。
type replyEventDTO struct {
	TicketID string `json:"ticketId"`
}

// ticketDTO wire 形状(契约数据模型)。
type ticketDTO struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Category  string `json:"category"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type messageDTO struct {
	SenderType string `json:"senderType"`
	Body       string `json:"body"`
	CreatedAt  string `json:"createdAt"`
}

type ticketPageDTO struct {
	Items      []ticketDTO `json:"items"`
	NextCursor string      `json:"nextCursor"`
}

type detailDTO struct {
	Ticket   ticketDTO    `json:"ticket"`
	Messages []messageDTO `json:"messages"`
}

type faqDTO struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Keywords []string `json:"keywords"`
}

type faqPageDTO struct {
	Items      []faqDTO `json:"items"`
	NextCursor string   `json:"nextCursor"`
}

type messageRecord struct {
	senderType string
	body       string
	createdAt  time.Time
}

type ticketRecord struct {
	id        string
	owner     string // accountId
	title     string
	status    string
	category  string
	messages  []messageRecord
	createdAt time.Time
	updatedAt time.Time
}

// SupportProvider 客服工单与 FAQ。
type SupportProvider struct {
	name    string
	clock   func() time.Time
	auth    func(http.Handler) http.Handler
	notify  func(eventType string, data any)
	limiter *middleware.RateLimiter

	mu      sync.RWMutex
	tickets map[string]*ticketRecord
	faqs    []faqRecord
}

type faqRecord struct {
	id       string
	question string
	answer   string
	keywords []string
}

// New 构造客服 Provider。
func New(opts Options) *SupportProvider {
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnauthenticated, "not authenticated")
		})
	}
	if opts.RequireAuth != nil {
		authMW = opts.RequireAuth
	}
	if opts.TicketRatePerMinute <= 0 {
		opts.TicketRatePerMinute = 5
	}
	if opts.TicketRateBurst <= 0 {
		opts.TicketRateBurst = 5
	}
	return &SupportProvider{
		name:    opts.Name,
		clock:   opts.Clock,
		auth:    authMW,
		notify:  opts.Notify,
		limiter: middleware.NewRateLimiter(opts.TicketRatePerMinute, opts.TicketRateBurst),
		tickets: make(map[string]*ticketRecord),
	}
}

// Name 供应商标识。
func (p *SupportProvider) Name() string { return p.name }

// Capability 能力域:support。
func (p *SupportProvider) Capability() providers.Capability { return providers.CapSupport }

// HealthCheck 内存存储常驻可用。
func (p *SupportProvider) HealthCheck(_ context.Context) error { return nil }

// CreateTicket 管理面之外的测试/运维便捷入口(等价 POST /tickets,不占限流)。
func (p *SupportProvider) CreateTicket(owner, title, body, category string) (Ticket, error) {
	return p.createTicket(owner, title, body, category)
}

// Ticket 管理面视角的工单快照。
type Ticket struct {
	ID        string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentReply 管理面:坐席回复 → 状态转 REPLIED + 广播 support.ticket_replied。
func (p *SupportProvider) AgentReply(ticketID, body string) error {
	return p.agentAppend(ticketID, "AGENT", body, "REPLIED")
}

// AgentClose 管理面:关单(CLOSED)+ 广播(客户端需感知不再可回)。
func (p *SupportProvider) AgentClose(ticketID string) error {
	p.mu.Lock()
	rec, ok := p.tickets[ticketID]
	if !ok {
		p.mu.Unlock()
		return errNotFound
	}
	rec.status = "CLOSED"
	rec.updatedAt = p.clock()
	p.mu.Unlock()
	if p.notify != nil {
		p.notify(EventTicketReplied, replyEventDTO{TicketID: ticketID})
	}
	return nil
}

// AddFAQ 管理面:新增知识条目。
func (p *SupportProvider) AddFAQ(question, answer string, keywords []string) faqDTO {
	p.mu.Lock()
	defer p.mu.Unlock()
	f := faqRecord{
		id:       "faq_" + newUUIDv7(p.clock()),
		question: question,
		answer:   answer,
		keywords: keywords,
	}
	p.faqs = append(p.faqs, f)
	return faqDTO{ID: f.id, Question: f.question, Answer: f.answer, Keywords: f.keywords}
}

// ServeHTTP 分发 /v1/support/*(全部要求 Bearer)。
func (p *SupportProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *SupportProvider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixSupport)
	switch {
	case path == "faq" && r.Method == http.MethodGet:
		p.handleFAQ(w, r)
	case path == "tickets":
		switch r.Method {
		case http.MethodPost:
			p.handleCreate(w, r)
		case http.MethodGet:
			p.handleList(w, r)
		default:
			p.writeInvalid(w, r, "tickets requires POST/GET")
		}
	case strings.HasPrefix(path, "tickets/"):
		rest := strings.TrimPrefix(path, "tickets/")
		if i := strings.Index(rest, "/"); i >= 0 {
			id, action := rest[:i], rest[i+1:]
			if action == "messages" && r.Method == http.MethodPost {
				p.handleAppend(w, r, id)
				return
			}
			p.writeNotFound(w, r)
			return
		}
		if r.Method == http.MethodGet {
			p.handleDetail(w, r, rest)
			return
		}
		p.writeInvalid(w, r, "ticket detail requires GET")
	default:
		p.writeNotFound(w, r)
	}
}

func (p *SupportProvider) handleCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	var req struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Category string `json:"category"`
	}
	if !p.decode(w, r, &req) {
		return
	}
	if p.limiter != nil {
		if ok, retryAfter := p.limiter.Allow(id.AccountID); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeRateLimited, "too many tickets")
			return
		}
	}
	t, err := p.createTicket(id.AccountID, req.Title, req.Body, req.Category)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	p.mu.RLock()
	rec := p.tickets[t.ID]
	dto := ticketDTOOf(rec)
	p.mu.RUnlock()
	aggregation.WriteData(w, dto)
}

func (p *SupportProvider) handleAppend(w http.ResponseWriter, r *http.Request, ticketID string) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	var req struct {
		Body string `json:"body"`
	}
	if !p.decode(w, r, &req) {
		return
	}
	p.mu.Lock()
	rec, ok := p.tickets[ticketID]
	if !ok || rec.owner != id.AccountID {
		p.mu.Unlock()
		p.writeNotFound(w, r)
		return
	}
	if rec.status == "CLOSED" {
		p.mu.Unlock()
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportTicketClosed, "ticket closed")
		return
	}
	if req.Body == "" || len([]rune(req.Body)) > 4000 {
		p.mu.Unlock()
		p.writeInvalid(w, r, "body 1-4000 字符")
		return
	}
	now := p.clock()
	rec.messages = append(rec.messages, messageRecord{senderType: "PLAYER", body: req.Body, createdAt: now})
	rec.updatedAt = now
	// 玩家说话不改变状态(契约:坐席回复才转 REPLIED)。
	dto := messageDTO{SenderType: "PLAYER", Body: req.Body, CreatedAt: formatMilli(now)}
	p.mu.Unlock()

	aggregation.WriteData(w, dto)
}

func (p *SupportProvider) handleList(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	limit, cursor, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	p.mu.RLock()
	mine := make([]*ticketRecord, 0)
	for _, rec := range p.tickets {
		if rec.owner == id.AccountID {
			mine = append(mine, rec)
		}
	}
	p.mu.RUnlock()
	sort.Slice(mine, func(i, j int) bool { return mine[i].updatedAt.After(mine[j].updatedAt) })

	items := make([]ticketDTO, 0, limit)
	next := ""
	if cursor < len(mine) {
		end := cursor + limit
		if end > len(mine) {
			end = len(mine)
		}
		for _, rec := range mine[cursor:end] {
			items = append(items, ticketDTOOf(rec))
		}
		if end < len(mine) {
			next = strconv.Itoa(end)
		}
	}
	aggregation.WriteData(w, ticketPageDTO{Items: items, NextCursor: next})
}

func (p *SupportProvider) handleDetail(w http.ResponseWriter, r *http.Request, ticketID string) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	p.mu.RLock()
	rec, owned := p.tickets[ticketID]
	if !owned || rec.owner != id.AccountID {
		p.mu.RUnlock()
		p.writeNotFound(w, r)
		return
	}
	dto := detailDTO{Ticket: ticketDTOOf(rec), Messages: make([]messageDTO, 0, len(rec.messages))}
	for _, m := range rec.messages { // createdAt 升序(追加序)
		dto.Messages = append(dto.Messages, messageDTO{
			SenderType: m.senderType, Body: m.body, CreatedAt: formatMilli(m.createdAt),
		})
	}
	p.mu.RUnlock()
	aggregation.WriteData(w, dto)
}

func (p *SupportProvider) handleFAQ(w http.ResponseWriter, r *http.Request) {
	keyword := strings.ToLower(r.URL.Query().Get("keyword"))
	if keyword == "" && r.URL.Query().Has("keyword") {
		p.writeInvalid(w, r, "keyword 不能为空串")
		return
	}
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 50 {
			p.writeInvalid(w, r, "limit 须为 1-50")
			return
		}
		limit = n
	}
	p.mu.RLock()
	items := make([]faqDTO, 0)
	for _, f := range p.faqs {
		if keyword == "" || faqMatch(f, keyword) {
			items = append(items, faqDTO{ID: f.id, Question: f.question, Answer: f.answer, Keywords: f.keywords})
		}
	}
	p.mu.RUnlock()
	// 最多 limit 条(FAQ 全量小,不做跨页;nextCursor 恒空 = 末页)。
	if len(items) > limit {
		items = items[:limit]
	}
	aggregation.WriteData(w, faqPageDTO{Items: items, NextCursor: ""})
}

func faqMatch(f faqRecord, keywordLower string) bool {
	if strings.Contains(strings.ToLower(f.question), keywordLower) ||
		strings.Contains(strings.ToLower(f.answer), keywordLower) {
		return true
	}
	for _, k := range f.keywords {
		if strings.Contains(strings.ToLower(k), keywordLower) {
			return true
		}
	}
	return false
}

func (p *SupportProvider) createTicket(owner, title, body, category string) (Ticket, error) {
	if title == "" || len([]rune(title)) > 120 {
		return Ticket{}, errInvalid("title 1-120 字符")
	}
	if body == "" || len([]rune(body)) > 4000 {
		return Ticket{}, errInvalid("body 1-4000 字符")
	}
	if category == "" {
		category = "OTHER"
	}
	now := p.clock()
	rec := &ticketRecord{
		id:        "tkt_" + newUUIDv7(now),
		owner:     owner,
		title:     title,
		status:    "OPEN",
		category:  category,
		messages:  []messageRecord{{senderType: "PLAYER", body: body, createdAt: now}},
		createdAt: now,
		updatedAt: now,
	}
	p.mu.Lock()
	p.tickets[rec.id] = rec
	p.mu.Unlock()
	return Ticket{ID: rec.id, Status: rec.status, CreatedAt: rec.createdAt, UpdatedAt: rec.updatedAt}, nil
}

func (p *SupportProvider) agentAppend(ticketID, senderType, body, status string) error {
	if body == "" || len([]rune(body)) > 4000 {
		return errInvalid("body 1-4000 字符")
	}
	p.mu.Lock()
	rec, ok := p.tickets[ticketID]
	if !ok {
		p.mu.Unlock()
		return errNotFound
	}
	now := p.clock()
	rec.messages = append(rec.messages, messageRecord{senderType: senderType, body: body, createdAt: now})
	rec.status = status
	rec.updatedAt = now
	p.mu.Unlock()
	if p.notify != nil {
		p.notify(EventTicketReplied, replyEventDTO{TicketID: ticketID})
	}
	return nil
}

func ticketDTOOf(rec *ticketRecord) ticketDTO {
	return ticketDTO{
		ID:        rec.id,
		Title:     rec.title,
		Status:    rec.status,
		Category:  rec.category,
		CreatedAt: formatMilli(rec.createdAt),
		UpdatedAt: formatMilli(rec.updatedAt),
	}
}

func (p *SupportProvider) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInvalidArgument, "invalid JSON body")
		return false
	}
	return true
}

func (p *SupportProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

func (p *SupportProvider) writeUnauthenticated(w http.ResponseWriter, r *http.Request) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeUnauthenticated, "not authenticated")
}

func (p *SupportProvider) writeNotFound(w http.ResponseWriter, r *http.Request) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeSupportTicketNotFound, "ticket not found")
}

// pageParams 解析 limit(默认 20,最大 100)与 cursor(offset 序号;空 = 0)。
func pageParams(r *http.Request) (limit, cursor int, err error) {
	limit = 20
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, errInvalid("limit 须为 1-100")
		}
	}
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor, err = strconv.Atoi(v)
		if err != nil || cursor < 0 {
			return 0, 0, errInvalid("cursor 非法")
		}
	}
	return limit, cursor, nil
}

// errInvalid / errNotFound:参数类与不存在(内部哨兵)。
type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(msg string) error { return invalidError(msg) }

var errNotFound = invalidError("not found")

func formatMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
