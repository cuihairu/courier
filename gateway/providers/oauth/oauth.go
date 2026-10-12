// Package oauth 第三方身份提供方域(契约 docs/contract/oauth.md,Frozen v1)。
//
// 路由挂 /v1/identity/oauth/*(identity 能力域子路径,不做独立挂载点;能力域
// 关闭走 Handlers.Disabled,不经路由表)。本包提供:
//
//   - 第三方凭证校验器注册表(管理面按 provider 注册,契约不描述校验细节;
//     未注册 = OAUTH_PROVIDER_UNKNOWN,fail-closed);
//   - account_identities 映射表(只存 provider + subject,不存凭证原文);
//   - 跨实现共享的 HTTP 端点处理(account 与 accountalt 双实现同一 wire 口径,
//     一致性 e2e 覆盖)。
//
// 红线(契约「数据红线」):不存第三方凭证原文;subject 必须来自第三方验证结果,
// 客户端不得自报;验证失败一律 OAUTH_IDENTITY_INVALID,不区分无效与过期(防枚举)。
package oauth

import (
	"errors"
	"strings"
	"sync"
)

// ErrInvalidIdentity 凭证验证失败(统一口径:无效/过期/伪造同返回,防枚举)。
var ErrInvalidIdentity = errors.New("oauth: 第三方凭证验证失败")

// Verifier 第三方凭证校验器:按第三方提供方名注册;校验方式(JWKS 验签 /
// introspection / code 换 token)由实现决定,契约不描述。
type Verifier interface {
	// Verify 在线校验第三方凭证,返回经第三方验证的归一 subject;
	// 失败返回非 nil error(一律映射 OAUTH_IDENTITY_INVALID)。
	Verify(provider, token, tokenType string) (subject string, err error)
}

var (
	regMu    sync.RWMutex
	registry = map[string]Verifier{}
	fallback Verifier // 缺省校验器(未按名注册的 provider 走它;nil = 无)
)

// Register 按第三方提供方名注册校验器(管理面注册制;同名覆盖)。
func Register(provider string, v Verifier) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[provider] = v
}

// SetDefault 注册缺省校验器(联调/测试;生产按名注册真实实现替换)。
func SetDefault(v Verifier) {
	regMu.Lock()
	defer regMu.Unlock()
	fallback = v
}

// Lookup 解析 provider 的校验器:按名优先,缺省兜底;都没有(或注册值为 nil,
// 即管理面显式注销) = 未注册。
func Lookup(provider string) (Verifier, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	if v, ok := registry[provider]; ok && v != nil {
		return v, true
	}
	if fallback != nil {
		return fallback, true
	}
	return nil, false
}

// DeterministicVerifier 确定性校验器(e2e/联调):token 必须形如 "ok.<subject>",
// 取 <subject> 为归一标识;其余一律验证失败。生产接入方注册真实
// JWKS/introspection 实现替换(契约不描述校验细节)。
type DeterministicVerifier struct{}

func (DeterministicVerifier) Verify(_, token, _ string) (string, error) {
	if subject, ok := strings.CutPrefix(token, "ok."); ok && subject != "" && len(token) <= 4096 {
		return subject, nil
	}
	return "", ErrInvalidIdentity
}

// ValidProvider 契约:提供方标识为小写字母数字与连字符,1–32 字符
// (管理面注册集合,契约不枚举);不符 = 未注册同码 OAUTH_PROVIDER_UNKNOWN。
func ValidProvider(s string) bool {
	if len(s) < 1 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// ValidTokenType tokenType 枚举(契约 oauth.md):ID_TOKEN / ACCESS_TOKEN / CODE,
// 空串 = 缺省 ID_TOKEN。
func ValidTokenType(s string) bool {
	switch s {
	case "", "ID_TOKEN", "ACCESS_TOKEN", "CODE":
		return true
	}
	return false
}
