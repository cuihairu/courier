// Package carrier is the default F62 mail Provider.
//
// Mail is an operations-to-player projection: individual mail is owned by one
// account, server-wide mail is owned by nobody and projected for everyone.
// Attachments are opaque item descriptors; granting stays in the integrator's
// fulfillment hook and mail is only the carrier (no second unit of value).
package carrier

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/audit"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

const (
	// DefaultName 注册名(main.go 默认路由表指向它)。
	DefaultName = "carrier"
	// EventUpdated 推送事件 type(契约 messages.md 事件登记)。
	EventUpdated = "mail.updated"

	prefixMail = "/v1/mail/"
)

// Attachment 附件(发奖载体,非第二价值单位;发放语义归接入方钩子)。
type Attachment struct {
	ID       string
	Type     string
	Amount   int64
	Currency string
}

// Mail 邮件(管理面实体;Owner 空 = 全量邮件)。
type Mail struct {
	ID          string
	Owner       string
	Title       string
	Body        string
	Attachments []Attachment
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(创建时间与过期判定);默认 time.Now。
	Clock func() time.Time
	// RequireAuth 认证中间件;nil = fail-closed(一律 401)。
	RequireAuth func(http.Handler) http.Handler
	// Notify 推送钩子(mail.updated);nil = 不推送。
	Notify func(eventType string, data any)
	// AuditSink 审计留痕接缝(F33);nil = 不产出审计事件。
	AuditSink audit.AuditSink
	// Claim 发放钩子(按账号+邮件幂等;nil = 沙箱裸跑记账成功)。
	Claim func(accountID, mailID string) error
}

type attachmentDTO struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency,omitempty"`
}

type mailDTO struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	Attachments []attachmentDTO `json:"attachments"`
	CreatedAt   string          `json:"createdAt"`
	ExpiresAt   *string         `json:"expiresAt,omitempty"`
}

type pageDTO struct {
	Items      []mailDTO `json:"items"`
	NextCursor string    `json:"nextCursor"`
}

type mailEventDTO struct {
	MailID string `json:"mailId"`
}

// Provider 邮件只读投影(默认实现:按所有者索引,读时投影)。
type Provider struct {
	name      string
	clock     func() time.Time
	auth      func(http.Handler) http.Handler
	notify    func(string, any)
	auditSink audit.AuditSink
	claimHook func(accountID, mailID string) error

	mu      sync.RWMutex
	byID    map[string]Mail
	byOwner map[string][]string
	claimed map[string]map[string]bool

	// inflight 按邮件登记交付中的信号量:claimHook 在锁外执行期间,同邮件
	// 并发领取在此等待、定局后重查——与原锁内执行的外部可观察行为一致;
	// 钩子本身零锁,可安全回调本 Provider(审核 2026-11-11 P1)。
	inflight map[string]chan struct{}
}

// New 构造邮件 Provider。
func New(opts Options) *Provider {
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
	return &Provider{
		name: opts.Name, clock: opts.Clock, auth: authMW,
		notify: opts.Notify, auditSink: opts.AuditSink, claimHook: opts.Claim,
		byID: map[string]Mail{}, byOwner: map[string][]string{},
		claimed:  map[string]map[string]bool{},
		inflight: map[string]chan struct{}{},
	}
}

// Name 供应商标识。
func (p *Provider) Name() string { return p.name }

// Capability 能力域:mail。
func (p *Provider) Capability() providers.Capability { return providers.CapMail }

// HealthCheck 内存存储常驻可用。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// Publish 管理面投递(运营侧):owner 空 = 全量,否则定向个人。
func (p *Provider) Publish(owner, title, body string, attachments []Attachment, expiresAt *time.Time) (Mail, error) {
	if title == "" || len([]rune(title)) > 120 {
		return Mail{}, errInvalid("title 1-120 字符")
	}
	if body == "" || len([]rune(body)) > 4000 {
		return Mail{}, errInvalid("body 1-4000 字符")
	}
	for _, a := range attachments {
		if a.ID == "" || a.Type == "" || a.Amount < 0 {
			return Mail{}, errInvalid("attachment 须 id/type/amount>=0")
		}
	}
	p.mu.Lock()
	m := Mail{
		ID: "mail_" + newUUIDv7(p.clock()), Owner: owner, Title: title, Body: body,
		Attachments: attachments, CreatedAt: p.clock(), ExpiresAt: expiresAt,
	}
	p.byID[m.ID] = m
	p.byOwner[owner] = append(p.byOwner[owner], m.ID)
	p.mu.Unlock()
	return m, nil
}

// ServeHTTP 分发 /v1/mail/*(全部要求 Bearer)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixMail)
	if path == "" || path == "/" {
		if r.Method == http.MethodGet {
			p.handleList(w, r)
			return
		}
		p.writeInvalid(w, r, "list requires GET")
		return
	}
	if strings.HasSuffix(path, "/claim") && r.Method == http.MethodPost {
		p.handleClaim(w, r, strings.TrimSuffix(path, "/claim"))
		return
	}
	if !strings.Contains(path, "/") && r.Method == http.MethodGet {
		p.handleDetail(w, r, path)
		return
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeNotFound, "no route for "+r.URL.Path)
}

func (p *Provider) handleList(w http.ResponseWriter, r *http.Request) {
	account, _ := auth.FromContext(r.Context())
	limit, anchor, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	now := p.clock()
	p.mu.RLock()
	ids := make([]string, 0, len(p.byOwner[account.AccountID])+len(p.byOwner[""]))
	ids = append(ids, p.byOwner[account.AccountID]...)
	ids = append(ids, p.byOwner[""]...)
	mails := make([]Mail, 0, len(ids))
	for _, mid := range ids {
		m := p.byID[mid]
		if m.visible(now) {
			mails = append(mails, m)
		}
	}
	p.mu.RUnlock()
	// 契约 mail.md: createdAt 为列表排序键(最新在前);同毫秒内按 ID 升序作全序,
	// 使键集游标(primitives.md 不透明)下翻页稳定。
	sort.Slice(mails, func(i, j int) bool {
		if !mails[i].CreatedAt.Equal(mails[j].CreatedAt) {
			return mails[i].CreatedAt.After(mails[j].CreatedAt)
		}
		return mails[i].ID < mails[j].ID
	})
	start := 0
	if anchor.id != "" {
		start = len(mails) // 锚点失效 → 空页(与既有 croupieralt 越界同形)。
		for i, m := range mails {
			if m.ID == anchor.id && m.CreatedAt.UnixMilli() == anchor.ms {
				start = i + 1
				break
			}
		}
	}
	end := start + limit
	if end > len(mails) {
		end = len(mails)
	}
	items := make([]mailDTO, 0, end-start)
	for _, m := range mails[start:end] {
		items = append(items, dto(m))
	}
	var next string
	if end < len(mails) && end > start {
		next = encodeCursor(mails[end-1])
	}
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: next})
}

func (p *Provider) handleDetail(w http.ResponseWriter, r *http.Request, id string) {
	account, _ := auth.FromContext(r.Context())
	p.mu.RLock()
	m, ok := p.byID[id]
	p.mu.RUnlock()
	if !ok || !m.ownedBy(account.AccountID) || !m.visible(p.clock()) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeMailNotFound, "mail not found")
		return
	}
	aggregation.WriteData(w, dto(m))
}

// handleClaim 领取(一次性发放,同账号同邮件重复 = 同结果)。
// claimHook 在锁外执行(审核 2026-11-11 P1:原实现持 p.mu 调接入方代码,
// 钩子回调本 Provider 即自锁死锁)。同邮件并发领取经 inflight 信号串行化:
// 后到者等待交付定局后整轮重查——先到成功则幂等返回、先到失败由后到者
// 接手,与原锁内执行的外部可观察行为一致。
func (p *Provider) handleClaim(w http.ResponseWriter, r *http.Request, id string) {
	account, _ := auth.FromContext(r.Context())
	for {
		// 写锁:inflight 登记是 map 写,读锁下并发登记即 map 并发写。
		p.mu.Lock()
		m, ok := p.byID[id]
		if ok && (!m.ownedBy(account.AccountID) || !m.visible(p.clock())) {
			ok = false
		}
		if !ok {
			p.mu.Unlock()
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeMailNotFound, "mail not found")
			return
		}
		if p.claimed[account.AccountID][id] {
			p.mu.Unlock()
			p.emit(account.AccountID, id, true, r)
			aggregation.WriteData(w, dto(m))
			return
		}
		if f, busy := p.inflight[id]; busy {
			// 同邮件交付进行中:等定局后重查(钩子在锁外执行期间)。
			p.mu.Unlock()
			<-f
			continue
		}
		f := make(chan struct{})
		p.inflight[id] = f
		p.mu.Unlock()

		hookErr := error(nil)
		if p.claimHook != nil {
			hookErr = p.claimHook(account.AccountID, id)
		}

		p.mu.Lock()
		delete(p.inflight, id)
		if hookErr == nil {
			if p.claimed[account.AccountID] == nil {
				p.claimed[account.AccountID] = map[string]bool{}
			}
			p.claimed[account.AccountID][id] = true
		}
		p.mu.Unlock()
		close(f)

		if hookErr != nil {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnavailable, hookErr.Error())
			return
		}
		p.emit(account.AccountID, id, false, r)
		aggregation.WriteData(w, dto(m))
		return
	}
}

func (p *Provider) emit(accountID, mailID string, repeated bool, r *http.Request) {
	if p.auditSink != nil {
		outcome := audit.AuditOutcomeOK
		if repeated {
			outcome = audit.AuditOutcomeRepeated
		}
		p.auditSink.Record(audit.AuditEvent{TraceID: middleware.TraceID(r.Context()),
			AccountID: accountID, Action: audit.AuditActionMailClaimed, Outcome: outcome, At: p.clock()})
	}
	if !repeated && p.notify != nil {
		p.notify(EventUpdated, mailEventDTO{MailID: mailID})
	}
}

func (m Mail) ownedBy(accountID string) bool { return m.Owner == "" || m.Owner == accountID }
func (m Mail) visible(now time.Time) bool    { return m.ExpiresAt == nil || now.Before(*m.ExpiresAt) }

func dto(m Mail) mailDTO {
	atts := make([]attachmentDTO, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		atts = append(atts, attachmentDTO{ID: a.ID, Type: a.Type, Amount: a.Amount, Currency: a.Currency})
	}
	out := mailDTO{ID: m.ID, Title: m.Title, Body: m.Body,
		Attachments: atts, CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano)}
	if m.ExpiresAt != nil {
		s := m.ExpiresAt.UTC().Format(time.RFC3339Nano)
		out.ExpiresAt = &s
	}
	return out
}

type cursorAnchor struct {
	ms int64
	id string
}

// encodeCursor 锚点编码:`<毫秒>|<ID>`(wire 不透明,契约 primitives.md)。
func encodeCursor(m Mail) string {
	return strconv.FormatInt(m.CreatedAt.UnixMilli(), 10) + "|" + m.ID
}

// decodeCursor 锚点解码:格式非法 → COMMON_INVALID_ARGUMENT。
func decodeCursor(v string) (cursorAnchor, error) {
	i := strings.Index(v, "|")
	if i <= 0 || i == len(v)-1 {
		return cursorAnchor{}, errInvalid("cursor 非法")
	}
	ms, err := strconv.ParseInt(v[:i], 10, 64)
	if err != nil {
		return cursorAnchor{}, errInvalid("cursor 非法")
	}
	return cursorAnchor{ms: ms, id: v[i+1:]}, nil
}

func pageParams(r *http.Request) (int, cursorAnchor, error) {
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return 0, cursorAnchor{}, errInvalid("limit 1-100")
		}
		limit = n
	}
	var anchor cursorAnchor
	if s := r.URL.Query().Get("cursor"); s != "" {
		var err error
		anchor, err = decodeCursor(s)
		if err != nil {
			return 0, cursorAnchor{}, err
		}
	}
	return limit, anchor, nil
}

func (p *Provider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// errInvalid 参数类错误(信封 COMMON_INVALID_ARGUMENT)。
type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(msg string) error { return invalidError(msg) }

// newUUIDv7 生成 RFC 9562 UUIDv7(时间有序;契约 mail.md「ID」)。
func newUUIDv7(t time.Time) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand 不可用属致命环境问题;退化为零基仍保形状。
		for i := range b {
			b[i] = 0
		}
	}
	ms := t.UnixMilli()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
