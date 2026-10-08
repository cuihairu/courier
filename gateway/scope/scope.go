// Package scope 提取与校验 game_id + env(契约:docs/contract/scope.md)。
//
// 隔离单元是二元组 game_id + env;scope 只走 header,URL path 与 payload 一律不携带。
package scope

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// 契约 header 名(scope.md「传输」)。
const (
	HeaderGameID = "X-Courier-Game-Id"
	HeaderEnv    = "X-Courier-Env"
)

// Scope 隔离单元二元组。
type Scope struct {
	GameID string
	Env    string
}

// ErrInvalid scope 缺失或不合法;网关映射为 COMMON_INVALID_ARGUMENT(scope.md「网关校验」1)。
var ErrInvalid = errors.New("scope: 缺失或不合法的 scope header")

var (
	envMu   sync.RWMutex
	envEnvs = map[string]bool{"dev": true, "staging": true, "prod": true}
)

// RegisterEnv 扩展 env 注册表(契约:注册表制,可扩展)。
func RegisterEnv(env string) {
	if env == "" {
		return
	}
	envMu.Lock()
	defer envMu.Unlock()
	envEnvs[env] = true
}

// ValidEnv env 是否在注册表内。
func ValidEnv(env string) bool {
	envMu.RLock()
	defer envMu.RUnlock()
	return envEnvs[env]
}

// FromRequest 提取并校验:两个 header 必填,env 必须在注册表内;
// 违反 → ErrInvalid(scope.md「网关校验」1)。
func FromRequest(r *http.Request) (Scope, error) {
	gameID := strings.TrimSpace(r.Header.Get(HeaderGameID))
	env := strings.TrimSpace(r.Header.Get(HeaderEnv))
	if gameID == "" || env == "" || !ValidEnv(env) {
		return Scope{}, ErrInvalid
	}
	return Scope{GameID: gameID, Env: env}, nil
}

type ctxKey struct{}

// WithContext 把校验后的 scope 注入请求上下文(网关以 header 为唯一事实源注入下游)。
func WithContext(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// FromContext 读取上下文中的 scope;缺失返回 false。
func FromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(ctxKey{}).(Scope)
	return s, ok
}
