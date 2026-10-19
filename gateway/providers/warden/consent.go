// consent.go consent 组实现(realname.md v1.1,F31;设计过程见
// research/consent-v1.1-draft.md,契约冻结口径为准):
//   - 六态状态机 + 防回拨(重复申报仅趋严生效,纠错走运维面);
//   - 判定阈值全归 Provider 配置(契约「法域中立」:换法域 = 换配置);
//   - VPC 请求 TTL 幂等(未过期同一 requestId,过期换新);
//   - S2S 判定面 = Provider 运维通道(契约明示「家长验证本身不在本契约」,
//     玩家凭证不可见,不在契约端点表内,接入方文档说明);
//   - 审计五事件(契约逐字命名,账号维度)+ SSE realname.consent.updated
//     (仅状态迁移时推送,数据 {"state": 新态},客户端拉全量 status)。
//
// 数据红线:出生日期只经 declare-age 入参传输一次——不落盘(判定后只留状态
// 与时间戳)、不进审计、不进 trace、不进诊断采集;UI 展示仅年份(接入方义务)。
package warden

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

// 同意状态(契约 v1.1 六态)。
const (
	consentUndeclared    = "UNDECLARED"
	consentAdult         = "ADULT"
	consentPendingParent = "PENDING_PARENT"
	consentGranted       = "GRANTED"
	consentDenied        = "DENIED"
	consentRevoked       = "REVOKED"
)

// S2S 判定动作(Provider 运维通道,契约外;对应审计 granted/denied/revoked)。
const (
	consentVerdictGrant  = "grant"
	consentVerdictDeny   = "deny"
	consentVerdictRevoke = "revoke"
)

// ConsentConfig 同意阈值与 VPC 配置。零值字段走缺省;换法域 = 换配置,
// 契约与六端 SDK 不动(契约 v1.1「法域中立」)。
type ConsentConfig struct {
	// MinimumAge 最低游玩年龄:低于恒判 DENIED(表达由 Provider 决定)。
	// 0 = 不设下限。
	MinimumAge int
	// DigitalConsentAge 数字同意年龄(缺省 13;GDPR-K 各成员国 13–16,COPPA 13):
	// 未成年但达此龄 → 本人同意成立(GRANTED)。
	DigitalConsentAge int
	// AdultAge 成年年龄(缺省 18):达到即 ADULT 终态,不再要求同意。
	AdultAge int
	// RequestTTL VPC 请求有效期(缺省 72h):TTL 内幂等,过期换新 requestId。
	RequestTTL time.Duration
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

// consentAuditAction S2S 判定动作 → 审计事件(契约 v1.1 审计节逐字命名)。
var consentAuditAction = map[string]string{
	consentVerdictGrant:  audit.AuditActionConsentGranted,
	consentVerdictDeny:   audit.AuditActionConsentDenied,
	consentVerdictRevoke: audit.AuditActionConsentRevoked,
}

// consentRank 严格度序(防回拨,契约 v1.1):ADULT < GRANTED < PENDING_PARENT
// < DENIED = REVOKED;越靠后越严,UNDECLARED 最松(任何判定可生效)。
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

// consentTightens 防回拨:目标判定不比当前更松才生效(放宽申报不生效,
// 契约 v1.1「成年→未成年可,反向不行;纠错走 Provider 运维面」)。
func consentTightens(from, to string) bool {
	return consentRank(to) >= consentRank(from)
}

// verdictTarget 家长判定目标态(S2S 运维面策略,契约外):
//   - grant:PENDING_PARENT / DENIED / REVOKED → GRANTED(含运维翻案与再同意);
//     GRANTED 幂等;
//   - deny:PENDING_PARENT → DENIED;DENIED 幂等(GRANTED 应走 revoke);
//   - revoke:GRANTED / PENDING_PARENT → REVOKED(在途请求一并作废);REVOKED 幂等;
//   - ADULT / UNDECLARED 不适用任何判定(成年无同意可撤,未申报无基)。
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

// --- wire DTO(与 wardenalt 同形;一致性 e2e 的同断言基础)---

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

// formatUTC consent 域时间戳(与 v1 DTO 同格式)。
func formatUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// consentGate 能力关闭语义(契约 v1.1「默认关闭」):未配置 Consent = 端点
// 501 COMMON_CAPABILITY_DISABLED,路由级降级先于认证与业务,不进报错路径(同 v1)。
func (w *Warden) consentGate(resp http.ResponseWriter, req *http.Request) bool {
	if w.consent != nil {
		return true
	}
	aggregation.WriteError(resp, middleware.TraceID(req.Context()),
		aggregation.CodeCapabilityDisabled, "consent not enabled")
	return false
}

// --- consent 玩家侧端点(v1.1,挂载 /v1/realname/consent/*)---

func (w *Warden) handleConsentStatus(resp http.ResponseWriter, req *http.Request) {
	if !w.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	w.writeConsentStatus(resp, w.rt.ConsentStatus(id.AccountID))
}

func (w *Warden) handleDeclareAge(resp http.ResponseWriter, req *http.Request) {
	if !w.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	var body struct {
		BirthDate string `json:"birthDate"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil ||
		!datePattern(body.BirthDate) {
		w.writeInvalid(resp, req, "birthDate required (YYYY-MM-DD)")
		return
	}
	// 入参形状与口径校验挡在边缘(400):合法形状才进治理链——core 报错语义
	// 是「调用失败」(计熔断,全链失败落 PENDING_PARENT 安全态),非法输入
	// 不能走那条路(同 v1 verify 口径)。未来日期与远古代日期皆非合法申报。
	if birth, err := time.Parse("2006-01-02", body.BirthDate); err != nil ||
		birth.Year() < 1900 || birth.After(w.clock()) {
		w.writeInvalid(resp, req, "birthDate out of range")
		return
	}
	out := w.rt.DeclareAge(id.AccountID, body.BirthDate)
	w.emitAudit(audit.AuditActionConsentDeclared, id.AccountID, out.State,
		middleware.TraceID(req.Context()))
	w.writeConsentStatus(resp, out)
}

func (w *Warden) handleParentConsentRequest(resp http.ResponseWriter, req *http.Request) {
	if !w.consentGate(resp, req) {
		return
	}
	id, ok := auth.FromContext(req.Context())
	if !ok {
		w.writeUnauthenticated(resp, req)
		return
	}
	out := w.rt.RequestParentConsent(id.AccountID)
	switch {
	case out.HasRequest:
		w.emitAudit(audit.AuditActionConsentParentRequested, id.AccountID, out.State,
			middleware.TraceID(req.Context()))
		aggregation.WriteData(resp, consentRequestDTO{
			RequestID:        out.RequestID,
			RequestExpiresAt: formatUTC(out.ExpiresAt),
		})
	case out.State == "":
		// 全链失败:无安全请求语义 → 503(不造请求,转人工;零新增错误码)。
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeUnavailable, "consent chain unavailable")
	default:
		// 仅 PENDING_PARENT 可发起(契约 v1.1);其余态 400 现有码。
		w.writeInvalid(resp, req, "parent consent request only from PENDING_PARENT")
	}
}

// writeConsentStatus status DTO 投影:declaredAt 申报即留;grantedAt/revokedAt
// 仅各自终态下发;requestExpiresAt 仅 PENDING_PARENT 下发(契约 v1.1 数据模型)。
func (w *Warden) writeConsentStatus(resp http.ResponseWriter, out router.ConsentStatus) {
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

// --- S2S 判定端点(v1.1 Provider 运维通道,SDK 不得调用;接入方文档说明)---

func (w *Warden) handleConsentVerdict(resp http.ResponseWriter, req *http.Request) {
	if !w.consentGate(resp, req) {
		return
	}
	action := strings.TrimPrefix(strings.TrimPrefix(req.URL.Path, prefixRealname), "consent/s2s/")
	auditAction, known := consentAuditAction[action]
	if !known {
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeNotFound, "no route for "+req.URL.Path)
		return
	}
	// 玩家 Bearer 不是合法凭证(同 playtime-report):只认配置的接入方服务端 token。
	if w.s2s == "" || req.Header.Get("Authorization") != "Bearer "+w.s2s {
		aggregation.WriteError(resp, middleware.TraceID(req.Context()),
			aggregation.CodeUnauthenticated, "s2s token required")
		return
	}
	var body struct {
		AccountID string `json:"accountId"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body.AccountID == "" {
		w.writeInvalid(resp, req, "accountId required")
		return
	}
	out, ok := w.core.consentApplyVerdict(body.AccountID, action)
	if !ok {
		w.writeInvalid(resp, req, "verdict not applicable to current consent state")
		return
	}
	w.emitAudit(auditAction, body.AccountID, out.State, middleware.TraceID(req.Context()))
	w.writeConsentStatus(resp, out)
}

// --- core:consent 状态机(router.RealNameProvider consent 方法)---

// consentRecord 同意记录:契约 v1.1「同意记录与身份数据分存储、独立保留期」
// ——独立 map(不混 rnRecord 身份记录),只存状态与判定时间戳;
// 出生日期永不落库。
type consentRecord struct {
	state            string
	declaredAt       time.Time
	hasDeclared      bool
	grantedAt        time.Time
	hasGranted       bool
	revokedAt        time.Time
	hasRevoked       bool
	requestID        string // 在途 VPC 请求(仅 PENDING_PARENT 存在,迁移即作废)
	requestExpiresAt time.Time
}

func (c *core) ConsentStatus(accountID string) (router.ConsentStatus, error) {
	if c.consent == nil {
		return router.ConsentStatus{}, errConsentDisabled
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consentStatusLocked(accountID), nil
}

func (c *core) DeclareAge(accountID, birthDate string) (router.ConsentStatus, error) {
	if c.consent == nil {
		return router.ConsentStatus{}, errConsentDisabled
	}
	birth, err := time.Parse("2006-01-02", birthDate)
	if err != nil || birth.Year() < 1900 || birth.After(c.clock()) {
		return router.ConsentStatus{}, errInvalid // HTTP 层已挡格式;此为二次防线
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
			rec.declaredAt, rec.hasDeclared = c.clock(), true // 首次申报时刻,放宽申报不留痕
		}
		if verdict != rec.state {
			rec.state = verdict
			rec.requestID, rec.requestExpiresAt = "", time.Time{} // 迁移即在途请求作废
			transition = verdict
		}
	}
	out := c.consentStatusLocked(accountID)
	c.mu.Unlock()
	if transition != "" {
		c.emitConsent(transition) // 通知不占状态锁(广播可能阻塞)
	}
	return out, nil
}

func (c *core) RequestParentConsent(accountID string) (router.ConsentRequestOutcome, error) {
	if c.consent == nil {
		return router.ConsentRequestOutcome{}, errConsentDisabled
	}
	c.mu.Lock()
	rec := c.consents[accountID]
	if rec == nil {
		c.mu.Unlock()
		// 未申报:State=UNDECLARED(handler 400);全链失败的 safe 才是零值 State。
		return router.ConsentRequestOutcome{State: consentUndeclared}, nil
	}
	if rec.state != consentPendingParent {
		c.mu.Unlock()
		return router.ConsentRequestOutcome{State: rec.state}, nil // 当前态不可发起(handler 400)
	}
	now := c.clock()
	if rec.requestID != "" && now.Before(rec.requestExpiresAt) {
		out := router.ConsentRequestOutcome{State: rec.state, RequestID: rec.requestID,
			ExpiresAt: rec.requestExpiresAt, HasRequest: true}
		c.mu.Unlock()
		return out, nil // TTL 内幂等:同一 requestId(契约 v1.1)
	}
	rec.requestID = newUUIDv7(now)
	rec.requestExpiresAt = now.Add(c.consent.RequestTTL)
	out := router.ConsentRequestOutcome{State: rec.state, RequestID: rec.requestID,
		ExpiresAt: rec.requestExpiresAt, HasRequest: true}
	c.mu.Unlock()
	return out, nil // 请求发起非状态迁移:不推送
}

// consentApplyVerdict 家长判定(S2S 运维通道):返回迁移后状态与是否适用
// (不适用由 handler 转 400);同态幂等不迁移不通知。
func (c *core) consentApplyVerdict(accountID, action string) (router.ConsentStatus, bool) {
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
	if rec == nil { // UNDECLARED 判不到适用,此处恒非 nil;防御兜底
		rec = &consentRecord{state: consentUndeclared}
		c.consents[accountID] = rec
	}
	rec.state = to
	rec.requestID, rec.requestExpiresAt = "", time.Time{} // 在途请求随判定作废
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

// consentVerdict 阈值分档(契约 v1.1「判定输入 = birthDate + Provider 阈值配置」)。
func (c *core) consentVerdict(birth time.Time) string {
	age := ageAt(birth, c.clock())
	cc := c.consent
	switch {
	case age >= cc.AdultAge:
		return consentAdult
	case cc.MinimumAge > 0 && age < cc.MinimumAge:
		return consentDenied
	case age >= cc.DigitalConsentAge:
		return consentGranted // 未成年但达数字同意年龄:本人同意成立
	default:
		return consentPendingParent
	}
}

// consentStatusLocked 无锁版(调用方持 c.mu);granted/revoked/requestExpiresAt
// 仅各自状态下发(契约 v1.1 数据模型)。
func (c *core) consentStatusLocked(accountID string) router.ConsentStatus {
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

// emitConsent 状态变化推送(契约 v1.1:SSE realname.consent.updated,数据
// {"state": 新态};nil 钩子零开销;仅状态迁移时调用)。
func (c *core) emitConsent(state string) {
	if c.notify == nil {
		return
	}
	c.notify(EventConsentUpdated, map[string]any{"state": state})
}

var errConsentDisabled = errors.New("warden: consent not enabled")
