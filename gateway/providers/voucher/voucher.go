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

	// inflight 按码登记交付中的信号量:grantHook 在锁外执行期间,同码并发
	// 兑换在此等待、定局后重查——与原锁内执行的外部可观察行为一致;钩子
	// 本身零锁,可安全回调本 Provider(审核 2026-11-11 P1)。
	inflight map[string]chan struct{}
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
		inflight: map[string]chan struct{}{},
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
// grantHook 在锁外执行(审核 2026-11-11 P1:原实现持 p.mu 调接入方代码,
// 钩子回调本 Provider 即自锁死锁)。同码并发经 inflight 信号串行化:后到者
// 等待交付定局后整轮重查——先到成功则幂等返回 repeated、先到失败则名额
// 回滚由后到者接手,限次与重复判定与原锁内执行的外部可观察行为一致;
// 预占一格(计数先行)保证钩子执行期间并发请求不越过限次。
func (p *Provider) Redeem(accountID, value string) (redeemDTO, error) {
	for {
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
		if f, busy := p.inflight[value]; busy {
			// 同码交付进行中:等定局后重查(钩子在锁外执行期间)。
			p.mu.Unlock()
			<-f
			continue
		}
		if c.RedeemCount >= c.MaxRedemptions {
			p.mu.Unlock()
			return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeCdkExhausted}
		}
		f := make(chan struct{})
		p.inflight[value] = f
		c.RedeemCount++ // 预占一格,失败回滚
		p.mu.Unlock()

		err := error(nil)
		if p.grantHook != nil {
			err = p.grantHook(accountID, value, c.Grant)
		}

		p.mu.Lock()
		delete(p.inflight, value)
		if err != nil {
			c.RedeemCount-- // 回滚预占
		} else {
			p.redeemed[value][accountID] = true
		}
		p.mu.Unlock()
		close(f)

		if err != nil {
			return redeemDTO{}, &aggregation.CodeError{Code: aggregation.CodeUnavailable}
		}
		return redeemDTO{Code: value, Grant: c.Grant}, nil
	}
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

type invalidError string

func (e invalidError) Error() string { return string(e) }
func errInvalid(msg string) error    { return invalidError(msg) }
