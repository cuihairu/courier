// Package voucher is the default F62 redeem-code (cdk) Provider.
//
// A code is redeemed once per account (idempotency key = account + code). A
// code may be single-use (maxRedemptions=1) or limited-redemption (counting).
// The code's granting semantics belong to the integrator's fulfillment hook.
package voucher

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
	DefaultName   = "voucher"
	prefixCdk     = "/v1/cdk/"
	EventRedeemed = "cdk.redeemed"
)

// Code 兑换码(管理面实体)。
type Code struct {
	Value          string
	MaxRedemptions int
	RedeemCount    int
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

type codeDTO struct {
	Value  string `json:"value"`
	Grant  string `json:"grant"`
	Count  int    `json:"count"`
	Remain int    `json:"remain"`
}

type redeemDTO struct {
	Code     string `json:"code"`
	Grant    string `json:"grant"`
	Repeated bool   `json:"repeated"`
}

type Provider struct {
	name      string
	clock     func() time.Time
	auth      func(http.Handler) http.Handler
	notify    func(string, any)
	auditSink audit.AuditSink
	grantHook func(accountID, code, grant string) error

	mu       sync.Mutex
	codes    map[string]*Code
	redeemed map[string]map[string]bool // code → accountID → redeemed
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
	return &Provider{
		name: opts.Name, clock: opts.Clock, auth: authMW, notify: opts.Notify,
		auditSink: opts.AuditSink, grantHook: opts.Grant,
		codes: map[string]*Code{}, redeemed: map[string]map[string]bool{},
	}
}

func (p *Provider) Name() string                        { return p.name }
func (p *Provider) Capability() providers.Capability    { return providers.CapCdk }
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// Register 管理面登记兑换码(重复登记覆盖;限次语义在 Provider 内)。
func (p *Provider) Register(value, grant string, maxRedemptions int) error {
	if value == "" || grant == "" || maxRedemptions < 1 {
		return errInvalid("code/grant 必填,maxRedemptions>=1")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.codes[value]; ok {
		existing.Grant = grant
		existing.MaxRedemptions = maxRedemptions
		return nil
	}
	p.codes[value] = &Code{Value: value, MaxRedemptions: maxRedemptions, Grant: grant}
	return nil
}

// Redeem 兑换(幂等:同账号同码重复 = 同结果,不二次发放)。
func (p *Provider) Redeem(accountID, value string) (redeemDTO, error) {
	p.mu.Lock()
	c, ok := p.codes[value]
	if !ok {
		p.mu.Unlock()
		return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeCdkNotFound}
	}
	if p.redeemed[value] == nil {
		p.redeemed[value] = map[string]bool{}
	}
	if p.redeemed[value][accountID] {
		p.mu.Unlock()
		return redeemDTO{Code: value, Grant: c.Grant, Repeated: true}, nil
	}
	if c.RedeemCount >= c.MaxRedemptions {
		p.mu.Unlock()
		return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeCdkExhausted}
	}
	if p.grantHook != nil {
		if err := p.grantHook(accountID, value, c.Grant); err != nil {
			p.mu.Unlock()
			return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeUnavailable}
		}
	}
	p.redeemed[value][accountID] = true
	c.RedeemCount++
	p.mu.Unlock()
	return redeemDTO{Code: value, Grant: c.Grant}, nil
}

// ServeHTTP 分发 /v1/cdk/*(全部要求 Bearer)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixCdk)
	if path == "redeem" && r.Method == http.MethodPost {
		p.handleRedeem(w, r)
		return
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeNotFound, "no route for "+r.URL.Path)
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

type CodeError struct{ Code string }

func (e *CodeError) Error() string { return e.Code }

type invalidError string

func (e invalidError) Error() string { return string(e) }
func errInvalid(msg string) error    { return invalidError(msg) }
