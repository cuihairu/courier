// Package carrieralt is the F62 mail second implementation (event-journal model).
//
// Same frozen contract as carrier but a different storage shape: mail state is
// derived by replaying an append-only event journal rather than a mutable map.
package carrieralt

import (
	"context"
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
	DefaultName  = "carrier-alt"
	EventUpdated = "mail.updated"
	prefixMail   = "/v1/mail/"
)

type Attachment struct {
	ID       string
	Type     string
	Amount   int64
	Currency string
}

type Mail struct {
	ID          string
	Owner       string
	Title       string
	Body        string
	Attachments []Attachment
	CreatedAt   time.Time
	ExpiresAt   *time.Time
}

type Options struct {
	Name        string
	Clock       func() time.Time
	RequireAuth func(http.Handler) http.Handler
	Notify      func(eventType string, data any)
	AuditSink   audit.AuditSink
	Claim       func(accountID, mailID string) error
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

type journalEvent struct {
	Kind    string
	Mail    Mail
	Account string
	At      time.Time
}

type Provider struct {
	name      string
	clock     func() time.Time
	auth      func(http.Handler) http.Handler
	notify    func(string, any)
	auditSink audit.AuditSink
	claimHook func(accountID, mailID string) error

	mu      sync.RWMutex
	journal []journalEvent
	next    int
}

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
	return &Provider{name: opts.Name, clock: opts.Clock, auth: authMW,
		notify: opts.Notify, auditSink: opts.AuditSink, claimHook: opts.Claim,
		journal: []journalEvent{}, next: 1}
}

func (p *Provider) Name() string                        { return p.name }
func (p *Provider) Capability() providers.Capability    { return providers.CapMail }
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

func (p *Provider) Publish(owner, title, body string, attachments []Attachment, expiresAt *time.Time) (Mail, error) {
	if title == "" || len([]rune(title)) > 120 {
		return Mail{}, errInvalid("title 1-120")
	}
	if body == "" || len([]rune(body)) > 4000 {
		return Mail{}, errInvalid("body 1-4000")
	}
	for _, a := range attachments {
		if a.ID == "" || a.Type == "" || a.Amount < 0 {
			return Mail{}, errInvalid("attachment")
		}
	}
	m := Mail{ID: "mail_" + newID(p.clock()), Owner: owner, Title: title, Body: body,
		Attachments: attachments, CreatedAt: p.clock(), ExpiresAt: expiresAt}
	p.mu.Lock()
	p.journal = append(p.journal, journalEvent{Kind: "publish", Mail: m, At: m.CreatedAt})
	p.next++
	p.mu.Unlock()
	return m, nil
}

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

// snapshot 重放账本 → 投影(所有者索引 + 已领集合)。
func (p *Provider) snapshot(accountID string, now time.Time) ([]mailDTO, map[string]bool) {
	seen := map[string]bool{}
	var mails []Mail
	for _, e := range p.journal {
		if e.Kind != "publish" {
			continue
		}
		m := e.Mail
		if !(m.Owner == "" || m.Owner == accountID) {
			continue
		}
		if m.ExpiresAt != nil && !now.Before(*m.ExpiresAt) {
			continue
		}
		mails = append(mails, m)
		if !seen[m.ID] {
			seen[m.ID] = true
		}
	}
	// 契约 mail.md: createdAt 为列表排序键(最新在前)。
	sort.Slice(mails, func(i, j int) bool { return mails[i].CreatedAt.After(mails[j].CreatedAt) })
	items := make([]mailDTO, 0, len(mails))
	for _, m := range mails {
		items = append(items, dto(m))
	}
	return items, seen
}

func (p *Provider) handleList(w http.ResponseWriter, r *http.Request) {
	account, _ := auth.FromContext(r.Context())
	limit, _, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	p.mu.RLock()
	items, _ := p.snapshot(account.AccountID, p.clock())
	p.mu.RUnlock()
	if len(items) > limit {
		items = items[:limit]
	}
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: ""})
}

func (p *Provider) handleDetail(w http.ResponseWriter, r *http.Request, id string) {
	account, _ := auth.FromContext(r.Context())
	p.mu.RLock()
	items, _ := p.snapshot(account.AccountID, p.clock())
	p.mu.RUnlock()
	for _, it := range items {
		if it.ID == id {
			aggregation.WriteData(w, it)
			return
		}
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeMailNotFound, "mail not found")
}

func (p *Provider) handleClaim(w http.ResponseWriter, r *http.Request, id string) {
	account, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	items, _ := p.snapshot(account.AccountID, p.clock())
	var found *mailDTO
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		p.mu.Unlock()
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeMailNotFound, "mail not found")
		return
	}
	repeated := false
	for _, e := range p.journal {
		if e.Kind == "claim" && e.Mail.ID == id && e.Account == account.AccountID {
			repeated = true
		}
	}
	if !repeated {
		if p.claimHook != nil {
			if err := p.claimHook(account.AccountID, id); err != nil {
				p.mu.Unlock()
				aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeUnavailable, err.Error())
				return
			}
		}
		p.journal = append(p.journal, journalEvent{Kind: "claim", Account: account.AccountID, Mail: Mail{ID: id}, At: p.clock()})
	}
	p.mu.Unlock()
	p.emit(account.AccountID, id, repeated, r)
	aggregation.WriteData(w, *found)
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

func dto(m Mail) mailDTO {
	atts := make([]attachmentDTO, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		atts = append(atts, attachmentDTO{ID: a.ID, Type: a.Type, Amount: a.Amount, Currency: a.Currency})
	}
	out := mailDTO{ID: m.ID, Title: m.Title, Body: m.Body, Attachments: atts,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339Nano)}
	if m.ExpiresAt != nil {
		s := m.ExpiresAt.UTC().Format(time.RFC3339Nano)
		out.ExpiresAt = &s
	}
	return out
}

func pageParams(r *http.Request) (int, int, error) {
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 100 {
			return 0, 0, errInvalid("limit 1-100")
		}
		limit = n
	}
	return limit, 0, nil
}

func (p *Provider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument, msg)
}

type invalidError string

func (e invalidError) Error() string { return string(e) }
func errInvalid(msg string) error    { return invalidError(msg) }
func newID(t time.Time) string       { return strconv.FormatInt(t.UnixNano(), 36) }
