// Package account Courier 自建账号 Provider(M1 默认实现,可换可关)。
//
// 实现能力域 identity(/v1/identity/*),契约:docs/contract/auth.md(Frozen v1)。
// 经 providers.Registry.Register 挂入;可整体替换为接入方自有实现。
package account

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
	"github.com/cuihairu/courier/gateway/session"
)

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "courier-account"

const prefixIdentity = "/v1/identity/"

// AccountProvider 自建账号:邮箱+密码、游客(设备)、access/refresh 轮换、
// 吊销、设备绑定、敏感端点基础限流。
type AccountProvider struct {
	name     string
	store    *Store
	verifier auth.Verifier
	reqAuth  func(http.Handler) http.Handler
	limiter  *middleware.RateLimiter
}

// New 构造自建账号 Provider。
func New(opts Options) *AccountProvider {
	opts.fillDefaults()
	store := NewStore(opts)
	v := session.NewVerifier(store, opts.Clock)
	return &AccountProvider{
		name:     DefaultName,
		store:    store,
		verifier: v,
		reqAuth:  auth.RequireAuth(v),
		limiter:  middleware.NewRateLimiter(opts.RatePerMinute, opts.RateBurst),
	}
}

// Store 暴露存储(运维观测/测试用)。
func (p *AccountProvider) Store() *Store { return p.store }

// Name 供应商标识。
func (p *AccountProvider) Name() string { return p.name }

// Capability 能力域:identity。
func (p *AccountProvider) Capability() providers.Capability { return providers.CapIdentity }

// HealthCheck 自建内存存储常驻可用。
func (p *AccountProvider) HealthCheck(_ context.Context) error { return nil }

// ServeHTTP 分发 /v1/identity/*(契约 auth.md「端点总览」);
// 方法不符 → COMMON_INVALID_ARGUMENT(400,契约未冻结 405 信封码)。
func (p *AccountProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (p *AccountProvider) sensitive(next http.Handler) http.Handler {
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

// --- 请求/响应 DTO(字段 camelCase,契约 primitives.md) ---

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

func toAccountDTO(a accountRow) accountDTO {
	dto := accountDTO{ID: a.id, Type: a.typ, Status: a.status, CreatedAt: fmtTime(a.createdAt)}
	if a.email != "" {
		dto.Email = &a.email
	}
	return dto
}

// writeSession 签发并写成功信封。
func (p *AccountProvider) writeSession(w http.ResponseWriter, r *http.Request, acc accountRow, deviceID string) {
	sc, ok := scope.FromContext(r.Context())
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument,
			scope.ErrInvalid.Error())
		return
	}
	sess, access, refresh, err := p.store.CreateSession(acc.id, sc.GameID, sc.Env, deviceID)
	if err != nil {
		p.writeStoreError(w, r, err)
		return
	}
	aggregation.WriteData(w, sessionDTO{
		Account:          toAccountDTO(acc),
		AccessToken:      access,
		AccessExpiresAt:  fmtTime(sess.accessExpiresAt),
		RefreshToken:     refresh,
		RefreshExpiresAt: fmtTime(sess.refreshExpiresAt),
		DeviceID:         deviceID,
	})
}

func (p *AccountProvider) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
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

// --- 端点实现 ---

// POST /register
func (p *AccountProvider) handleRegister(w http.ResponseWriter, r *http.Request) {
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
		if _, err := p.store.EnsureBindDevice(acc.id, req.DeviceID, req.Platform); err != nil {
			p.writeStoreError(w, r, err)
			return
		}
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /login
func (p *AccountProvider) handleLogin(w http.ResponseWriter, r *http.Request) {
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
	if acc.status != StatusActive {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeAccountDisabled,
			"account disabled")
		return
	}
	if req.DeviceID != "" {
		if !validDeviceID(req.DeviceID) {
			badRequest(w, r, "invalid deviceId")
			return
		}
		if _, err := p.store.EnsureBindDevice(acc.id, req.DeviceID, req.Platform); err != nil {
			p.writeStoreError(w, r, err)
			return
		}
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /guest — 按设备幂等。
func (p *AccountProvider) handleGuest(w http.ResponseWriter, r *http.Request) {
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
		acc, err := p.store.CreateGuestAccount(req.DeviceID, req.Platform)
		if err != nil {
			p.writeStoreError(w, r, err)
			return
		}
		p.writeSession(w, r, acc, req.DeviceID)
		return
	}
	if acc.status != StatusActive {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeAccountDisabled,
			"account disabled")
		return
	}
	p.writeSession(w, r, acc, req.DeviceID)
}

// POST /bind(Bearer,游客转正)
func (p *AccountProvider) handleBind(w http.ResponseWriter, r *http.Request) {
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

// POST /refresh — 轮换与重放检测(契约 auth.md POST /refresh 表)。
func (p *AccountProvider) handleRefresh(w http.ResponseWriter, r *http.Request) {
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
	sid, gen := p.store.LookupRefresh(req.RefreshToken)
	switch gen {
	case RefreshCurrent:
		sess, access, refresh, err := p.store.RotateSession(sid)
		if err != nil {
			p.writeStoreError(w, r, err)
			return
		}
		acc, ok := p.store.GetAccount(sess.accountID)
		if !ok {
			aggregation.WriteError(w, tid, aggregation.CodeInternal, "internal error")
			return
		}
		aggregation.WriteData(w, sessionDTO{
			Account:          toAccountDTO(acc),
			AccessToken:      access,
			AccessExpiresAt:  fmtTime(sess.accessExpiresAt),
			RefreshToken:     refresh,
			RefreshExpiresAt: fmtTime(sess.refreshExpiresAt),
			DeviceID:         sess.deviceID,
		})
	case RefreshPrevious:
		// 已轮换的 refresh 再次出现 = 令牌泄露 → 吊销整个会话。
		p.store.ReuseDetected(sid)
		aggregation.WriteError(w, tid, aggregation.CodeRefreshReused,
			"refresh token replay detected; session revoked")
	default:
		aggregation.WriteError(w, tid, aggregation.CodeInvalidCredentials, "invalid refresh token")
	}
}

// POST /logout(Bearer)— 吊销当前会话,幂等。
func (p *AccountProvider) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	id, _ := auth.FromContext(r.Context())
	p.store.RevokeSession(id.SessionID)
	aggregation.WriteData(w, struct{}{})
}

// GET /session(Bearer)
func (p *AccountProvider) handleSession(w http.ResponseWriter, r *http.Request) {
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
func (p *AccountProvider) handleListDevices(w http.ResponseWriter, r *http.Request) {
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
func (p *AccountProvider) handleUnbindDevice(w http.ResponseWriter, r *http.Request, deviceID string) {
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
	aggregation.WriteData(w, struct{}{})
}
