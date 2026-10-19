// consent.go consent 组实现(realname.md v1.1,F31)——与 warden 同契约异构:
// 六态状态机、防回拨、VPC TTL 幂等、S2S 判定面、审计与 SSE 全部与 warden 同
// 形状(一致性由 e2e/realname_conformance_test.go 的 consent 场景守护)。
// 本实现的异构点:核验判定来自注入的托管服务(不解析证件号),但 consent 的
// 判定输入是 declare-age 的直接出生日期入参,因此分档逻辑与 warden 重合。
package wardenalt

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/audit"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers/router"
)

// EventConsentUpdated 同意状态变化推送事件名(契约 v1.1;同 config.updated 帧形状)。
const EventConsentUpdated = "realname.consent.updated"

// 同意状态(契约 v1.1 六态;与 warden 同表)。
const (
	consentUndeclared    = "UNDECLARED"
	consentAdult         = "ADULT"
	consentPendingParent = "PENDING_PARENT"
	consentGranted       = "GRANTED"
	consentDenied        = "DENIED"
	consentRevoked       = "REVOKED"
)

// S2S 判定动作(Provider 运维通道,契约外)。
const (
	consentVerdictGrant  = "grant"
	consentVerdictDeny   = "deny"
	consentVerdictRevoke = "revoke"
)

// ConsentConfig 同意阈值与 VPC 配置(零值字段走缺省;换法域 = 换配置)。
type ConsentConfig struct {
	MinimumAge        int
	DigitalConsentAge int
	AdultAge          int
	RequestTTL        time.Duration
}

// withDefaults 缺省填充(返回副本,不改调用方配置)。
func (c ConsentConfig) withDefaults() *ConsentConfig {
	out := c
	if out.DigitalConsentAge <= 0 {
		out.DigitalConsentAge = 13
	}
	if out.AdultAge <= 0 {
		out.AdultAge = 18
	}
	if out.RequestTTL <= 0 {
		out.RequestTTL = 72 * time.Hour
	}
	return &out
}

// consentAuditAction S2S 判定动作 → 审计事件(契约 v1.1 逐字命名)。
var consentAuditAction = map[string]string{
	consentVerdictGrant:  audit.AuditActionConsentGranted,
	consentVerdictDeny:   audit.AuditActionConsentDenied,
	consentVerdictRevoke: audit.AuditActionConsentRevoked,
}

// consentRank 严格度序(防回拨):ADULT < GRANTED < PENDING_PARENT < DENIED
// = REVOKED;UNDECLARED 最松。
func consentRank(state string) int {
	switch state {
	case consentUndeclared:
		return -1
	case consentAdult:
		return 0
	case consentGranted:
		return 1
	case consentPendingParent:
		return 2
	default:
		return 3 // DENIED / REVOKED
	}
}

func consentTightens(from, to string) bool { return consentRank(to) >= consentRank(from) }

// verdictTarget 家长判定目标态(与 warden 同策略):grant 自
// PENDING_PARENT/DENIED/REVOKED;deny 自 PENDING_PARENT;revoke 自
// GRANTED/PENDING_PARENT;同态幂等;ADULT/UNDECLARED 不适用。
func verdictTarget(from, action string) (to string, ok bool) {
	switch action {
	case consentVerdictGrant:
		switch from {
		case consentGranted, consentPendingParent, consentDenied, consentRevoked:
			return consentGranted, true
		}
	case consentVerdictDeny:
		switch from {
		case consentDenied, consentPendingParent:
			return consentDenied, true
		}
	case consentVerdictRevoke:
		switch from {
		case consentRevoked, consentGranted, consentPendingParent:
			return consentRevoked, true
		}
	}
	return from, false
}

// --- wire DTO(与 warden 同形)---

type consentStatusDTO struct {
	State            string  `json:"state"`
	DeclaredAt       *string `json:"declaredAt,omitempty"`
	GrantedAt        *string `json:"grantedAt,omitempty"`
	RevokedAt        *string `json:"revokedAt,omitempty"`
	RequestExpiresAt *string `json:"requestExpiresAt,omitempty"`
}

type consentRequestDTO struct {
	RequestID        string `json:"requestId"`
	RequestExpiresAt string `json:"requestExpiresAt"`
}

func formatUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (p *Provider) consentGate(resp http.ResponseWriter, req *http.Request) bool {
	if p.consent != nil {
		return true
	}
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeCapabilityDisabled, "consent not enabled")
	return false
}

// --- consent 玩家侧端点(v1.1,挂载 /v1/realname/consent/*)---

func (p *Provider) handleConsentStatus(resp http.ResponseWriter, req *http.Request) {
	if !p.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	p.writeConsentStatus(resp, p.rt.ConsentStatus(id.AccountID))
}

func (p *Provider) handleDeclareAge(resp http.ResponseWriter, req *http.Request) {
	if !p.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		BirthDate string `json:"birthDate"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil ||
		!datePattern(body.BirthDate) {
		p.writeInvalid(resp, req, "birthDate required (YYYY-MM-DD)")
		return
	}
	// 入参形状与口径校验挡在边缘(400,同 warden):core 报错语义是「调用失败」
	// (计熔断),非法输入不能走那条路。
	if birth, err := time.Parse("2006-01-02", body.BirthDate); err != nil ||
		birth.Year() < 1900 || birth.After(p.clock()) {
		p.writeInvalid(resp, req, "birthDate out of range")
		return
	}
	out := p.rt.DeclareAge(id.AccountID, body.BirthDate)
	p.emitAudit(audit.AuditActionConsentDeclared, id.AccountID, out.State,
		middleware.TraceID(req.Context()))
	p.writeConsentStatus(resp, out)
}

func (p *Provider) handleParentConsentRequest(resp http.ResponseWriter, req *http.Request) {
	if !p.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		p.writeUnauthenticated(resp, req)
		return
	}
	out := p.rt.RequestParentConsent(id.AccountID)
	switch {
	case out.HasRequest:
		p.emitAudit(audit.AuditActionConsentParentRequested, id.AccountID, out.State,
			middleware.TraceID(req.Context()))
		aggregation.WriteData(resp, consentRequestDTO{
			RequestID:        out.RequestID,
			RequestExpiresAt: formatUTC(out.ExpiresAt),
		})
	case out.State == "":
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeUnavailable, "consent chain unavailable")
	default:
		p.writeInvalid(resp, req, "parent consent request only from PENDING_PARENT")
	}
}

func (p *Provider) writeConsentStatus(resp http.ResponseWriter, out router.ConsentStatus) {
	d := consentStatusDTO{State: out.State}
	if out.HasDeclared {
		at := formatUTC(out.DeclaredAt)
		d.DeclaredAt = &at
	}
	if out.HasGranted && out.State == consentGranted {
		at := formatUTC(out.GrantedAt)
		d.GrantedAt = &at
	}
	if out.HasRevoked && out.State == consentRevoked {
		at := formatUTC(out.RevokedAt)
		d.RevokedAt = &at
	}
	if out.HasRequest && out.State == consentPendingParent {
		at := formatUTC(out.RequestExpiresAt)
		d.RequestExpiresAt = &at
	}
	aggregation.WriteData(resp, d)
}

// --- S2S 判定端点(v1.1 Provider 运维通道,SDK 不得调用)---

func (p *Provider) handleConsentVerdict(resp http.ResponseWriter, req *http.Request) {
	if !p.consentGate(resp, req) {
		return
	}
	action := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, prefixRealname), "consent/s2s/")
	auditAction, known := consentAuditAction[action]
	if !known {
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeNotFound, "no route for "+req.URL.Path)
		return
	}
	if p.s2s == "" || req.Header.Get("Authorization") != "Bearer "+p.s2s {
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeUnauthenticated, "s2s token required")
		return
	}
	var body struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.AccountID == "" {
		p.writeInvalid(resp, req, "accountId required")
		return
	}
	out, ok := p.core.consentApplyVerdict(body.AccountID, action)
	if !ok {
		p.writeInvalid(resp, req, "verdict not applicable to current consent state")
		return
	}
	p.emitAudit(auditAction, body.AccountID, out.State, middleware.TraceID(req.Context()))
	p.writeConsentStatus(resp, out)
}

// --- cloudCore:consent 状态机(与 warden core 同形状)---

type consentRecord struct {
	state            string
	declaredAt       time.Time
	hasDeclared      bool
	grantedAt        time.Time
	hasGranted       bool
	revokedAt        time.Time
	hasRevoked       bool
	requestID        string
	requestExpiresAt time.Time
}

func (c *cloudCore) ConsentStatus(accountID string) (router.ConsentStatus, error) {
	if c.consent == nil {
		return router.ConsentStatus{}, errConsentDisabled
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consentStatusLocked(accountID), nil
}

func (c *cloudCore) DeclareAge(accountID, birthDate string) (router.ConsentStatus, error) {
	if c.consent == nil {
		return router.ConsentStatus{}, errConsentDisabled
	}
	birth, err := time.Parse("2006-01-02", birthDate)
	if err != nil || birth.Year() < 1900 || birth.After(c.clock()) {
		return router.ConsentStatus{}, errInvalid
	}
	verdict := c.consentVerdict(birth)
	c.mu.Lock()
	rec := c.consents[accountID]
	if rec == nil {
		rec = &consentRecord{state: consentUndeclared}
		c.consents[accountID] = rec
	}
	transition := ""
	if consentTightens(rec.state, verdict) {
		if !rec.hasDeclared {
			rec.declaredAt, rec.hasDeclared = c.clock(), true
		}
		if verdict != rec.state {
			rec.state = verdict
			rec.requestID, rec.requestExpiresAt = "", time.Time{}
			transition = verdict
		}
	}
	out := c.consentStatusLocked(accountID)
	c.mu.Unlock()
	if transition != "" {
		c.emitConsent(transition)
	}
	return out, nil
}

func (c *cloudCore) RequestParentConsent(accountID string) (router.ConsentRequestOutcome, error) {
	if c.consent == nil {
		return router.ConsentRequestOutcome{}, errConsentDisabled
	}
	c.mu.Lock()
	rec := c.consents[accountID]
	if rec == nil {
		c.mu.Unlock()
		return router.ConsentRequestOutcome{State: consentUndeclared}, nil
	}
	if rec.state != consentPendingParent {
		c.mu.Unlock()
		return router.ConsentRequestOutcome{State: rec.state}, nil
	}
	now := c.clock()
	if rec.requestID != "" && now.Before(rec.requestExpiresAt) {
		out := router.ConsentRequestOutcome{State: rec.state, RequestID: rec.requestID,
			ExpiresAt: rec.requestExpiresAt, HasRequest: true}
		c.mu.Unlock()
		return out, nil
	}
	rec.requestID = newUUIDv7(now)
	rec.requestExpiresAt = now.Add(c.consent.RequestTTL)
	out := router.ConsentRequestOutcome{State: rec.state, RequestID: rec.requestID,
		ExpiresAt: rec.requestExpiresAt, HasRequest: true}
	c.mu.Unlock()
	return out, nil
}

func (c *cloudCore) consentApplyVerdict(accountID, action string) (router.ConsentStatus, bool) {
	c.mu.Lock()
	rec := c.consents[accountID]
	from := consentUndeclared
	if rec != nil {
		from = rec.state
	}
	c.mu.Unlock()
	to, ok := verdictTarget(from, action)
	if !ok {
		return router.ConsentStatus{}, false
	}
	c.mu.Lock()
	if rec == nil {
		rec = &consentRecord{state: consentUndeclared}
		c.consents[accountID] = rec
	}
	rec.state = to
	rec.requestID, rec.requestExpiresAt = "", time.Time{}
	now := c.clock()
	switch to {
	case consentGranted:
		rec.grantedAt, rec.hasGranted = now, true
	case consentRevoked:
		rec.revokedAt, rec.hasRevoked = now, true
	}
	out := c.consentStatusLocked(accountID)
	c.mu.Unlock()
	if to != from {
		c.emitConsent(to)
	}
	return out, true
}

// consentVerdict 阈值分档(判定输入 = birthDate + Provider 阈值配置)。
func (c *cloudCore) consentVerdict(birth time.Time) string {
	age := ageAt(birth, c.clock())
	cc := c.consent
	switch {
	case age >= cc.AdultAge:
		return consentAdult
	case cc.MinimumAge > 0 && age < cc.MinimumAge:
		return consentDenied
	case age >= cc.DigitalConsentAge:
		return consentGranted
	default:
		return consentPendingParent
	}
}

func (c *cloudCore) consentStatusLocked(accountID string) router.ConsentStatus {
	rec := c.consents[accountID]
	if rec == nil {
		return router.ConsentStatus{State: consentUndeclared}
	}
	out := router.ConsentStatus{State: rec.state}
	if rec.hasDeclared {
		out.DeclaredAt, out.HasDeclared = rec.declaredAt, true
	}
	if rec.hasGranted && rec.state == consentGranted {
		out.GrantedAt, out.HasGranted = rec.grantedAt, true
	}
	if rec.hasRevoked && rec.state == consentRevoked {
		out.RevokedAt, out.HasRevoked = rec.revokedAt, true
	}
	if rec.requestID != "" && rec.state == consentPendingParent {
		out.RequestExpiresAt, out.HasRequest = rec.requestExpiresAt, true
	}
	return out
}

func (c *cloudCore) emitConsent(state string) {
	if c.notify == nil {
		return
	}
	c.notify(EventConsentUpdated, map[string]any{"state": state})
}

// ageAt 周岁(consent 阈值分档用;不解析证件号)。
func ageAt(birth, now time.Time) int {
	age := now.Year() - birth.Year()
	if now.Month() < birth.Month() ||
		(now.Month() == birth.Month() && now.Day() < birth.Day()) {
		age--
	}
	return age
}

var errConsentDisabled = errors.New("wardenalt: consent not enabled")
