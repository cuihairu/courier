// Package voucheralt is the F62 redeem-code second implementation (append-only
// ledger projection). Same frozen contract as voucher but state is derived by
// replaying redemption events instead of a mutable counter.
package voucheralt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	DefaultName   = "voucher-alt"
	prefixCdk     = "/v1/cdk/"
	EventRedeemed = "cdk.redeemed"
)

type Code struct {
	Value          string
	MaxRedemptions int
	Grant          string
}

type Options struct {
	Name        string
	Clock       func() time.Time
	RequireAuth func(http.Handler) http.Handler
	Notify      func(eventType string, data any)
	AuditSink   audit.AuditSink
	Grant       func(accountID, code, grant string) error
}

type redeemDTO struct {
	Code     string `json:"code"`
	Grant    string `json:"grant"`
	Repeated bool   `json:"repeated"`
}

type ledgerEvent struct {
	Kind    string
	Code    string
	Account string
	Grant   string
	At      time.Time
}

type Provider struct {
	name      string
	clock     func() time.Time
	auth      func(http.Handler) http.Handler
	notify    func(string, any)
	auditSink audit.AuditSink
	grantHook func(accountID, code, grant string) error

	mu     sync.RWMutex
	codes  map[string]*Code
	ledger []ledgerEvent
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
		notify: opts.Notify, auditSink: opts.AuditSink, grantHook: opts.Grant,
		codes: map[string]*Code{}, ledger: []ledgerEvent{}}
}

func (p *Provider) Name() string                        { return p.name }
func (p *Provider) Capability() providers.Capability    { return providers.CapCdk }
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

func (p *Provider) Register(value, grant string, maxRedemptions int) error {
	if value == "" || grant == "" || maxRedemptions < 1 {
		return errInvalid("bad code")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.codes[value]; ok {
		c.Grant = grant
		c.MaxRedemptions = maxRedemptions
		return nil
	}
	p.codes[value] = &Code{Value: value, MaxRedemptions: maxRedemptions, Grant: grant}
	return nil
}

// redeemLocked 账本投影:同账号重复 → repeated;计数已达 → exhausted。
func (p *Provider) redeemLocked(accountID, value string) (redeemDTO, bool, bool) {
	c, ok := p.codes[value]
	if !ok {
		return redeemDTO{}, false, false
	}
	count := 0
	seen := false
	for _, e := range p.ledger {
		if e.Kind != "redeem" || e.Code != value {
			continue
		}
		count++
		if e.Account == accountID {
			seen = true
		}
	}
	if seen {
		return redeemDTO{Code: value, Grant: c.Grant, Repeated: true}, true, false
	}
	if count >= c.MaxRedemptions {
		return redeemDTO{}, true, true
	}
	return redeemDTO{Code: value, Grant: c.Grant}, true, false
}

func (p *Provider) Redeem(accountID, value string) (redeemDTO, error) {
	p.mu.Lock()
	out, known, exhausted := p.redeemLocked(accountID, value)
	if !known {
		p.mu.Unlock()
		return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeCdkNotFound}
	}
	if exhausted {
		p.mu.Unlock()
		return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeCdkExhausted}
	}
	if out.Repeated {
		p.mu.Unlock()
		return out, nil
	}
	if p.grantHook != nil {
		if err := p.grantHook(accountID, value, out.Grant); err != nil {
			p.mu.Unlock()
			return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeUnavailable}
		}
	}
	p.ledger = append(p.ledger, ledgerEvent{Kind: "redeem", Code: value, Account: accountID, Grant: out.Grant, At: p.clock()})
	p.mu.Unlock()
	return out, nil
}

func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixCdk)
	if path == "redeem" && r.Method == http.MethodPost {
		p.handleRedeem(w, r)
		return
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeNotFound, "no route for "+r.URL.Path)
}

func (p *Provider) handleRedeem(w http.ResponseWriter, r *http.Request) {
	account, _ := auth.FromContext(r.Context())
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil {
		p.writeInvalid(w, r, "body 不可读")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &req); err != nil || req.Code == "" {
		p.writeInvalid(w, r, "body 须为 {code}")
		return
	}
	out, err := p.Redeem(account.AccountID, req.Code)
	if err != nil {
		if ce, ok := err.(*aggregation.CodeError); ok {
			aggregation.WriteError(w, middleware.TraceID(r.Context()), ce.Code, err.Error())
			return
		}
		p.writeInvalid(w, r, err.Error())
		return
	}
	p.emit(account.AccountID, req.Code, out.Repeated, r)
	aggregation.WriteData(w, out)
}

func (p *Provider) emit(accountID, code string, repeated bool, r *http.Request) {
	if p.auditSink != nil {
		outcome := audit.AuditOutcomeOK
		if repeated {
			outcome = audit.AuditOutcomeRepeated
		}
		p.auditSink.Record(audit.AuditEvent{TraceID: middleware.TraceID(r.Context()),
			AccountID: accountID, Action: audit.AuditActionCdkRedeemed, Outcome: outcome, At: p.clock()})
	}
	if !repeated && p.notify != nil {
		p.notify(EventRedeemed, map[string]string{"code": code})
	}
}

func (p *Provider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument, msg)
}

type invalidError string

func (e invalidError) Error() string { return string(e) }
func errInvalid(msg string) error    { return invalidError(msg) }
