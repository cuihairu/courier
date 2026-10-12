// handlers.go oauth 域共享端点处理(契约 oauth.md「端点契约」)。
//
// account 与 accountalt 双实现共用本层,保证同一 wire 口径(一致性 e2e 覆盖);
// 认证(Bearer)与敏感限流由宿主 Provider 包裹,本层只管域语义:
//
//	POST   /v1/identity/oauth/exchange           匿名,第三方凭证换会话
//	POST   /v1/identity/oauth/bind               Bearer,绑定第三方身份
//	DELETE /v1/identity/oauth/bindings/{provider} Bearer,解绑(幂等)
//	GET    /v1/identity/oauth/bindings            Bearer,已绑定列表
package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers/hooks"
	"github.com/cuihairu/courier/gateway/scope"
)

// Account 账号投影(oauth 域只读字段;exchange 可命中已绑定的 GUEST/EMAIL 账号)。
type Account struct {
	ID        string
	Type      string
	Status    string
	Email     string
	CreatedAt time.Time
}

// Session 会话签发结果(exchange 响应与 auth 域 POST /login 同构)。
type Session struct {
	Account          Account
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
	DeviceID         string
}

// Store oauth 域后端接口:账号存储实现(account/accountalt)各自适配,
// 哨兵错误用本包 Err*(errors.Is 分派)。
type Store interface {
	// FindOAuth 按 (provider, subject) 查已绑定账号(不含已匿名化——映射行
	// 随匿名化清理,查不到即未绑定)。
	FindOAuth(provider, subject string) (Account, bool)
	// CreateOAuth 注册 type=OAUTH 账号并写入映射;可选绑定设备
	//(超限 → ErrDeviceLimit,注册回滚)。
	CreateOAuth(provider, subject, deviceID, platform string) (Account, error)
	// EnsureBindDevice 登录路径设备绑定(幂等;超限 → ErrDeviceLimit)。
	EnsureBindDevice(accountID, deviceID, platform string) error
	// FinalizeIfExpired 宽限期满惰性终态化(auth.md v1.1 同一口径)。
	FinalizeIfExpired(accountID string) bool
	// BindOAuth 绑定第三方身份到账号;重复绑定同 provider → ErrProviderBound,
	// subject 已属其他账号 → ErrIdentityTaken,宽限期账号 → ErrPendingDelete。
	BindOAuth(accountID, provider, subject string) (IdentityRow, error)
	// UnbindOAuth 解绑并删除映射行(幂等);返回是否删除了行。
	UnbindOAuth(accountID, provider string) bool
	// ListBindings 已绑定列表,createdAt 升序(不返回 subject,防客户端侧泄露)。
	ListBindings(accountID string) []IdentityRow
	// CreateOAuthSession 签发会话(与 auth 域 login 同构;账号非 ACTIVE → ErrDisabled)。
	CreateOAuthSession(accountID, gameID, env, deviceID string) (Session, error)
}

// Handlers oauth 域端点处理。
type Handlers struct {
	Store Store
	// Disabled 能力关闭(管理面可关,契约 oauth.md「能力与范围」可换可关);
	// true → 全部端点 501 COMMON_CAPABILITY_DISABLED。nil = 开启。
	Disabled func() bool
}

func (h *Handlers) guardDisabled(w http.ResponseWriter, r *http.Request) bool {
	if h.Disabled == nil || !h.Disabled() {
		return false
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeCapabilityDisabled, "oauth disabled")
	return true
}

// --- 请求/响应 DTO(字段 camelCase,契约 primitives.md) ---

type exchangeReq struct {
	Provider  string `json:"provider"`
	Token     string `json:"token"`
	TokenType string `json:"tokenType,omitempty"`
	DeviceID  string `json:"deviceId,omitempty"`
	Platform  string `json:"platform,omitempty"`
}

type bindReq struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
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

// bindingDTO 列表项:不返回 subject(契约 GET bindings,避免客户端侧泄露)。
type bindingDTO struct {
	Provider  string `json:"provider"`
	CreatedAt string `json:"createdAt"`
}

func fmtTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func toAccountDTO(a Account) accountDTO {
	dto := accountDTO{ID: a.ID, Type: a.Type, Status: a.Status, CreatedAt: fmtTime(a.CreatedAt)}
	if a.Email != "" {
		dto.Email = &a.Email
	}
	return dto
}

func toSessionDTO(s Session) sessionDTO {
	return sessionDTO{
		Account:          toAccountDTO(s.Account),
		AccessToken:      s.AccessToken,
		AccessExpiresAt:  fmtTime(s.AccessExpiresAt),
		RefreshToken:     s.RefreshToken,
		RefreshExpiresAt: fmtTime(s.RefreshExpiresAt),
		DeviceID:         s.DeviceID,
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

func badRequest(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument, msg)
}

func validDeviceID(s string) bool {
	return utf8.RuneCountInString(s) >= 1 && utf8.RuneCountInString(s) <= 128
}

// writeStoreError 存储哨兵 → 冻结错误码(契约 oauth.md「错误码」)。
func (h *Handlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	tid := middleware.TraceID(r.Context())
	switch {
	case errors.Is(err, ErrIdentityTaken):
		aggregation.WriteError(w, tid, aggregation.CodeOauthIdentityTaken, err.Error())
	case errors.Is(err, ErrProviderBound):
		aggregation.WriteError(w, tid, aggregation.CodeInvalidArgument, err.Error())
	case errors.Is(err, ErrPendingDelete):
		aggregation.WriteError(w, tid, aggregation.CodeAccountPending, err.Error())
	case errors.Is(err, ErrDisabled):
		aggregation.WriteError(w, tid, aggregation.CodeAccountDisabled, err.Error())
	case errors.Is(err, ErrDeviceLimit):
		aggregation.WriteError(w, tid, aggregation.CodeDeviceLimit, err.Error())
	default:
		aggregation.WriteError(w, tid, aggregation.CodeInternal, "internal error")
	}
}

// writeSession 签发会话并写成功信封(exchange 响应 = auth 域 login 同构)。
func (h *Handlers) writeSession(w http.ResponseWriter, r *http.Request, acc Account, deviceID string) {
	sc, ok := scope.FromContext(r.Context())
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()), aggregation.CodeInvalidArgument,
			scope.ErrInvalid.Error())
		return
	}
	sess, err := h.Store.CreateOAuthSession(acc.ID, sc.GameID, sc.Env, deviceID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	aggregation.WriteData(w, toSessionDTO(sess))
}

// verify 验证第三方凭证 → 归一 subject;失败一律 OAUTH_IDENTITY_INVALID(防枚举)。
func (h *Handlers) verify(w http.ResponseWriter, r *http.Request, provider, token, tokenType string) (string, bool) {
	v, ok := Lookup(provider)
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeOauthProviderUnknown, "provider unknown")
		return "", false
	}
	subject, err := v.Verify(provider, token, tokenType)
	if err != nil {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeOauthIdentityInvalid, "identity invalid")
		return "", false
	}
	return subject, true
}

// Exchange POST /v1/identity/oauth/exchange(匿名):第三方凭证换 Courier 会话
// (无匹配即创建 type=OAUTH 账号;命中即登录,每次签发新会话)。
func (h *Handlers) Exchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	if h.guardDisabled(w, r) {
		return
	}
	req, ok := decode[exchangeReq](w, r)
	if !ok {
		return
	}
	if !ValidProvider(req.Provider) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeOauthProviderUnknown, "provider unknown")
		return
	}
	if req.Token == "" || !ValidTokenType(req.TokenType) {
		badRequest(w, r, "token required; tokenType in ID_TOKEN/ACCESS_TOKEN/CODE")
		return
	}
	if req.DeviceID != "" && !validDeviceID(req.DeviceID) {
		badRequest(w, r, "invalid deviceId")
		return
	}
	subject, ok := h.verify(w, r, req.Provider, req.Token, req.TokenType)
	if !ok {
		return
	}
	acc, found := h.Store.FindOAuth(req.Provider, subject)
	if found {
		switch acc.Status {
		case "PENDING_DELETE":
			// 宽限期账号:惰性终态化后映射随匿名化清除 → 按未命中注册;
			// 未满 → 登录被拒(与 auth 域 PENDING 拒登同口径,auth.md v1.1)。
			if h.Store.FinalizeIfExpired(acc.ID) {
				hooks.Emit(hooks.AccountDeleted, acc.ID)
				found = false
			} else {
				aggregation.WriteError(w, middleware.TraceID(r.Context()),
					aggregation.CodeAccountPending, "account pending delete")
				return
			}
		case "ACTIVE":
			if req.DeviceID != "" {
				if err := h.Store.EnsureBindDevice(acc.ID, req.DeviceID, req.Platform); err != nil {
					h.writeStoreError(w, r, err)
					return
				}
			}
			h.writeSession(w, r, acc, req.DeviceID)
			return
		default:
			h.writeStoreError(w, r, ErrDisabled)
			return
		}
	}
	acc, err := h.Store.CreateOAuth(req.Provider, subject, req.DeviceID, req.Platform)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.writeSession(w, r, acc, req.DeviceID)
}

// Bind POST /v1/identity/oauth/bind(Bearer):当前账号绑定第三方身份,
// 供下次一键登录;重复绑定同 provider → COMMON_INVALID_ARGUMENT,
// subject 已属其他账号 → OAUTH_IDENTITY_TAKEN。
func (h *Handlers) Bind(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		badRequest(w, r, "use POST")
		return
	}
	if h.guardDisabled(w, r) {
		return
	}
	req, ok := decode[bindReq](w, r)
	if !ok {
		return
	}
	if !ValidProvider(req.Provider) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeOauthProviderUnknown, "provider unknown")
		return
	}
	if req.Token == "" {
		badRequest(w, r, "token required")
		return
	}
	subject, ok := h.verify(w, r, req.Provider, req.Token, "")
	if !ok {
		return
	}
	id, _ := auth.FromContext(r.Context())
	row, err := h.Store.BindOAuth(id.AccountID, req.Provider, subject)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	aggregation.WriteData(w, bindingDTO{Provider: row.Provider, CreatedAt: fmtTime(row.CreatedAt)})
}

// Unbind DELETE /v1/identity/oauth/bindings/{provider}(Bearer):解绑并删除
// 映射行;幂等(不存在再删仍 200);解绑不影响账号本身。
func (h *Handlers) Unbind(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodDelete {
		badRequest(w, r, "use DELETE")
		return
	}
	if h.guardDisabled(w, r) {
		return
	}
	id, _ := auth.FromContext(r.Context())
	h.Store.UnbindOAuth(id.AccountID, provider)
	aggregation.WriteData(w, struct{}{})
}

// ListBindings GET /v1/identity/oauth/bindings(Bearer):已绑定列表,
// 按 createdAt 升序;不返回 subject(避免客户端侧泄露第三方标识)。
func (h *Handlers) ListBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		badRequest(w, r, "use GET")
		return
	}
	if h.guardDisabled(w, r) {
		return
	}
	id, _ := auth.FromContext(r.Context())
	rows := h.Store.ListBindings(id.AccountID)
	items := make([]bindingDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, bindingDTO{Provider: row.Provider, CreatedAt: fmtTime(row.CreatedAt)})
	}
	// 列表信封:data.items,nextCursor 缺失即末页(契约 primitives.md)。
	aggregation.WriteData(w, map[string]any{"items": items})
}
