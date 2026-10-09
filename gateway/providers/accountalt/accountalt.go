// Package accountalt identity 第二实现(批次 18:可换性 e2e 证明)。
//
// 与自建 account 实现同一冻结契约(docs/contract/auth.md Frozen v1),但内部
// 架构刻意不同——证明契约约束的是行为而非实现:
//   - 自建:不透明随机 token(64-hex)即会话表键,验证走全表查找
//   - 本实现:HMAC 签名自验证令牌,claim 内嵌会话事实;存储仅保留吊销态与
//     refresh 代际计数(类 JWT 模型 vs 会话表模型)
//
// 第二实现的验收方式:e2e 一致性套件对两个实现跑同一 wire 场景(同一函数、
// 同一断言),全绿 = 可换性成立;main 经 auth.SwitchableVerifier 按配置切换。
package accountalt

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

// DefaultName 注册名;路由表 {"identity":{"primary":"courier-account-alt"}} 即换。
const DefaultName = "courier-account-alt"

const prefixIdentity = "/v1/identity/"

// Provider identity 第二实现。
type Provider struct {
	name     string
	store    *Store
	reqAuth  func(http.Handler) http.Handler
	limiter  *middleware.RateLimiter
	verifier *verifier
}

// New 构造第二实现(签名密钥非法 panic 快速失败)。
func New(opts Options) *Provider {
	opts.fillDefaults()
	p := &Provider{name: DefaultName, store: NewStore(&opts)}
	p.verifier = &verifier{p: p}
	p.reqAuth = auth.RequireAuth(p.verifier)
	p.limiter = middleware.NewRateLimiter(opts.RatePerMinute, opts.RateBurst)
	return p
}

// Store 暴露存储(运维观测/测试用)。
func (p *Provider) Store() *Store { return p.store }

// Verifier 暴露会话验证器(SwitchableVerifier 重指目标)。
func (p *Provider) Verifier() auth.Verifier { return p.verifier }

// Name 供应商标识。
func (p *Provider) Name() string { return p.name }

// Capability 能力域:identity。
func (p *Provider) Capability() providers.Capability { return providers.CapIdentity }

// HealthCheck 自建内存存储常驻可用。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

type verifier struct {
	p *Provider
}

// Verify 验证顺序与 session 包一致:查无(含验签)→ 吊销 → 过期 → 账号状态 →
// scope 绑定(auth.md「Token 模型」);签名令牌免查表拿 claim,状态查小表。
func (v *verifier) Verify(ctx context.Context, accessToken string) (auth.Identity, error) {
	if accessToken == "" {
		return auth.Identity{}, auth.ErrUnauthenticated
	}
	claims, err := v.p.store.codec.parseAccess(accessToken)
	if err != nil {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	sess, ok := v.p.store.Session(claims.sessionID)
	if !ok {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if sess.accessNonce != claims.nonce {
		// 已被更新的 access 轮换取代(语义同自建:旧令牌查无)。
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if sess.revoked {
		return auth.Identity{}, auth.ErrTokenRevoked
	}
	if v.p.store.now().After(claims.expiresAt) {
		return auth.Identity{}, auth.ErrTokenExpired
	}
	acc, ok := v.p.store.GetAccount(claims.accountID)
	if !ok || acc.status == "DISABLED" {
		return auth.Identity{}, auth.ErrAccountDisabled
	}
	if sc, ok := scope.FromContext(ctx); ok && (sc.GameID != claims.gameID || sc.Env != claims.env) {
		return auth.Identity{}, auth.ErrScopeMismatch
	}
	return auth.Identity{
		AccountID: claims.accountID,
		SessionID: claims.sessionID,
		DeviceID:  claims.deviceID,
		GameID:    claims.gameID,
		Env:       claims.env,
	}, nil
}

// ServeHTTP 分发 /v1/identity/*(契约 auth.md「端点总览」;与自建同一路由表)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixIdentity)
	switch path {
	case "register":
		p.sensitive(http.HandlerFunc(p.handleRegister)).ServeHTTP(w, r)
	case "login":
		p.sensitive(http.HandlerFunc(p.handleLogin)).ServeHTTP(w, r)
	case "guest":
		p.sensitive(http.HandlerFunc(p.handleGuest)).ServeHTTP(w, r)
	case "bind":
		p.sensitive(p.reqAuth(http.HandlerFunc(p.handleBind))).ServeHTTP(w, r)
	case "refresh":
		p.sensitive(http.HandlerFunc(p.handleRefresh)).ServeHTTP(w, r)
	case "logout":
		p.reqAuth(http.HandlerFunc(p.handleLogout)).ServeHTTP(w, r)
	case "session":
		p.reqAuth(http.HandlerFunc(p.handleSession)).ServeHTTP(w, r)
	case "devices":
		p.reqAuth(http.HandlerFunc(p.handleListDevices)).ServeHTTP(w, r)
	default:
		if devID, ok := strings.CutPrefix(path, "devices/"); ok && devID != "" {
			p.reqAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p.handleUnbindDevice(w, r, devID)
			})).ServeHTTP(w, r)
			return
		}
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeNotFound,
			"no route for "+r.URL.Path)
	}
}

// sensitive 敏感端点基础限流(契约 auth.md「基础限流」:register/login/guest/bind/refresh)。
func (p *Provider) sensitive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.limiter != nil {
			if ok, retry := p.limiter.Allow(middleware.ClientIP(r)); !ok {
				seconds := int(retry.Seconds())
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", itoa(seconds))
				aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeRateLimited, "rate limited")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// --- 请求/响应 DTO(字段 camelCase,契约 primitives.md;形状与自建一致) ---

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	DeviceID string `json:"deviceId,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	DeviceID string `json:"deviceId,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type guestReq struct {
	DeviceID string `json:"deviceId"`
	Platform string `json:"platform,omitempty"`
}

type bindReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshReq struct {
	RefreshToken string `json:"refreshToken"`
}

type accountDTO struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Email     *string `json:"email,omitempty"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"createdAt"`
}

type sessionDTO struct {
	Account          accountDTO `json:"account"`
	AccessToken      string     `json:"accessToken"`
	AccessExpiresAt  string     `json:"accessExpiresAt"`
	RefreshToken     string     `json:"refreshToken"`
	RefreshExpiresAt string     `json:"refreshExpiresAt"`
	DeviceID         string     `json:"deviceId,omitempty"`
}

type deviceDTO struct {
	ID        string `json:"id"`
	DeviceID  string `json:"deviceId"`
	Platform  string `json:"platform,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// fmtTime RFC 3339 毫秒 UTC(契约 primitives.md「Timestamp」)。
func fmtTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func toAccountDTO(a altAccount) accountDTO {
	dto := accountDTO{ID: a.id, Type: a.typ, Status: a.status, CreatedAt: fmtTime(a.createdAt)}
	if a.email != "" {
		dto.Email = &a.email
	}
	return dto
}

// writeSession 签发并写成功信封。
func (p *Provider) writeSession(w http.ResponseWriter, r *http.Request, acc altAccount, deviceID string) {
	sc, ok := scope.FromContext(r.Context())
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument,
			scope.ErrInvalid.Error())
		return
	}
	sessID, access, refresh, err := p.store.CreateSession(acc.id, sc.GameID, sc.Env, deviceID)
	if err != nil {
		p.writeStoreError(w, r, err)
		return
	}
	sess, _ := p.store.Session(sessID)
	aggregation.WriteData(w, sessionDTO{
		Account:          toAccountDTO(acc),
		AccessToken:      access,
		AccessExpiresAt:  fmtTime(p.store.now().Add(p.store.opts.AccessTTL)),
		RefreshToken:     refresh,
		RefreshExpiresAt: fmtTime(sess.refreshExpires),
		DeviceID:         deviceID,
	})
}

func (p *Provider) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	tid := middleware.TraceID(r.Context())
	switch {
	case errors.Is(err, ErrEmailTaken):
		aggregation.WriteError(w, tid, aggregation.CodeEmailTaken, err.Error())
	case errors.Is(err, ErrDeviceLimit):
		aggregation.WriteError(w, tid, aggregation.CodeDeviceLimit, err.Error())
	case errors.Is(err, ErrAlreadyBound):
		aggregation.WriteError(w, tid, aggregation.CodeInvalidArgument, err.Error())
	case errors.Is(err, ErrDisabled):
		aggregation.WriteError(w, tid, aggregation.CodeAccountDisabled, err.Error())
	case errors.Is(err, auth.ErrTokenExpired):
		aggregation.WriteError(w, tid, aggregation.CodeTokenExpired, err.Error())
	case errors.Is(err, auth.ErrTokenRevoked):
		aggregation.WriteError(w, tid, aggregation.CodeTokenRevoked, err.Error())
	default:
		aggregation.WriteError(w, tid, aggregation.CodeInternal, "internal error")
	}
}

// decode 严格只校验类型,容忍未知字段(契约 versioning:未知字段必须容忍)。
func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var req T
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument,
			"invalid JSON body")
		return req, false
	}
	return req, true
}

// 校验口径(契约 auth.md):email 格式、password 8–128 字符、deviceId 1–128 字符。
var emailOK = func(s string) bool {
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-1 &&
		strings.IndexByte(s[at+1:], '.') > 0 &&
		!strings.ContainsAny(s, " \t\r\n")
}

func validPassword(s string) bool {
	return utf8.RuneCountInString(s) >= 8 && utf8.RuneCountInString(s) <= 128
}
func validDeviceID(s string) bool {
	return utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 128
}

func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument, msg)
}

// --- 端点实现(wire 行为与自建一致) ---

// POST /register
func (p *Provider) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	req, ok := decode[registerReq](w, r)
	if !ok {
		return
	}
	if !emailOK(req.Email) || !validPassword(req.Password) {
		badRequest(w, r, "invalid email or password (8-128 chars)")
		return
	}
	if req.DeviceID != "" && !validDeviceID(req.DeviceID) {
		badRequest(w, r, "invalid deviceId")
		return
	}
	acc, err := p.store.CreateEmailAccount(req.Email, req.Password)
	if err != nil {
		p.writeStoreError(w, r, err)
		return
	}
	if req.DeviceID != "" {
		if err := p.store.EnsureBindDevice(acc.id, req.DeviceID, req.Platform); err != nil {
			p.writeStoreError(w, r, err)
			return
		}
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /login
func (p *Provider) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	req, ok := decode[loginReq](w, r)
	if !ok {
		return
	}
	acc, ok := p.store.VerifyEmailPassword(req.Email, req.Password)
	if !ok {
		// 防账号枚举:账号不存在与密码错误返回一致(auth.md POST /login)。
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidCredentials,
			"invalid email or password")
		return
	}
	if acc.status != "ACTIVE" {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeAccountDisabled,
			"account disabled")
		return
	}
	if req.DeviceID != "" {
		if !validDeviceID(req.DeviceID) {
			badRequest(w, r, "invalid deviceId")
			return
		}
		if err := p.store.EnsureBindDevice(acc.id, req.DeviceID, req.Platform); err != nil {
			p.writeStoreError(w, r, err)
			return
		}
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /guest — 按设备幂等。
func (p *Provider) handleGuest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	req, ok := decode[guestReq](w, r)
	if !ok {
		return
	}
	if !validDeviceID(req.DeviceID) {
		badRequest(w, r, "deviceId required")
		return
	}
	acc, found := p.store.FindGuestByDevice(req.DeviceID)
	if !found {
		acc = p.store.CreateGuestAccount(req.DeviceID, req.Platform)
		p.writeSession(w, r, acc, req.DeviceID)
		return
	}
	if acc.status != "ACTIVE" {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeAccountDisabled,
			"account disabled")
		return
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /bind(Bearer,游客转正)
func (p *Provider) handleBind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	req, ok := decode[bindReq](w, r)
	if !ok {
		return
	}
	if !emailOK(req.Email) || !validPassword(req.Password) {
		badRequest(w, r, "invalid email or password (8-128 chars)")
		return
	}
	id, _ := auth.FromContext(r.Context())
	acc, err := p.store.BindEmail(id.AccountID, req.Email, req.Password)
	if err != nil {
		p.writeStoreError(w, r, err)
		return
	}
	aggregation.WriteData(w, toAccountDTO(acc))
}

// POST /refresh — 代际轮换与重放检测(契约 auth.md POST /refresh 表)。
func (p *Provider) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	req, ok := decode[refreshReq](w, r)
	if !ok {
		return
	}
	tid := middleware.TraceID(r.Context())
	if req.RefreshToken == "" {
		badRequest(w, r, "refreshToken required")
		return
	}
	claims, err := p.store.codec.parseRefresh(req.RefreshToken)
	if err != nil {
		aggregation.WriteError(w, tid, aggregation.CodeInvalidCredentials, "invalid refresh token")
		return
	}
	sess, ok := p.store.Session(claims.sessionID)
	if !ok {
		aggregation.WriteError(w, tid, aggregation.CodeInvalidCredentials, "invalid refresh token")
		return
	}
	if sess.revoked {
		aggregation.WriteError(w, tid, aggregation.CodeTokenRevoked, "session revoked")
		return
	}
	if p.store.now().After(claims.expiresAt) {
		aggregation.WriteError(w, tid, aggregation.CodeTokenExpired, "refresh token expired")
		return
	}
	switch {
	case claims.gen < sess.gen:
		// 已轮换的 refresh 再次出现 = 令牌泄露 → 吊销整个会话。
		p.store.RevokeSession(sess.id)
		aggregation.WriteError(w, tid, aggregation.CodeRefreshReused,
			"refresh token replay detected; session revoked")
	case claims.gen > sess.gen:
		aggregation.WriteError(w, tid, aggregation.CodeInvalidCredentials, "invalid refresh token")
	default:
		access, refresh, err := p.store.Rotate(sess.id)
		if err != nil {
			p.writeStoreError(w, r, err)
			return
		}
		acc, ok := p.store.GetAccount(sess.accountID)
		if !ok {
			aggregation.WriteError(w, tid, aggregation.CodeInternal, "internal error")
			return
		}
		rotated, _ := p.store.Session(sess.id)
		aggregation.WriteData(w, sessionDTO{
			Account:          toAccountDTO(acc),
			AccessToken:      access,
			AccessExpiresAt:  fmtTime(p.store.now().Add(p.store.opts.AccessTTL)),
			RefreshToken:     refresh,
			RefreshExpiresAt: fmtTime(rotated.refreshExpires),
			DeviceID:         sess.deviceID,
		})
	}
}

// POST /logout(Bearer)— 吊销当前会话,幂等。
func (p *Provider) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	id, _ := auth.FromContext(r.Context())
	p.store.RevokeSession(id.SessionID)
	aggregation.WriteData(w, struct{}{})
}

// GET /session(Bearer)
func (p *Provider) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		badRequest(w, r, "use GET")
		return
	}
	id, _ := auth.FromContext(r.Context())
	acc, ok := p.store.GetAccount(id.AccountID)
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInternal, "internal error")
		return
	}
	aggregation.WriteData(w, map[string]any{
		"account":   toAccountDTO(acc),
		"sessionId": id.SessionID,
		"deviceId":  id.DeviceID,
	})
}

// GET /devices(Bearer)
func (p *Provider) handleListDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		badRequest(w, r, "use GET")
		return
	}
	id, _ := auth.FromContext(r.Context())
	rows := p.store.ListDevices(id.AccountID)
	items := make([]deviceDTO, 0, len(rows))
	for _, d := range rows {
		items = append(items, deviceDTO{
			ID: d.id, DeviceID: d.deviceID, Platform: d.platform, CreatedAt: fmtTime(d.createdAt),
		})
	}
	// 列表信封:data.items,nextCursor 缺失即末页(契约 primitives.md)。
	aggregation.WriteData(w, map[string]any{"items": items})
}

// DELETE /devices/{deviceId}(Bearer)— 解绑并吊销该设备全部会话。
func (p *Provider) handleUnbindDevice(w http.ResponseWriter, r *http.Request, deviceID string) {
	if r.Method != http.MethodDelete {
		badRequest(w, r, "use DELETE")
		return
	}
	id, _ := auth.FromContext(r.Context())
	if !p.store.UnbindDevice(id.AccountID, deviceID) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeNotFound,
			"device not found")
		return
	}
	p.store.RevokeDeviceSessions(id.AccountID, deviceID)
	aggregation.WriteData(w, struct{}{})
}
