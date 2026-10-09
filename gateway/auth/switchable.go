// SwitchableVerifier:可重定向的会话验证器(身份 Provider 可换的接缝)。
//
// 现状问题:reqAuth 由 main 装配时直接绑死默认账号 Provider 的 Verifier;
// 接入方把 identity 路由表换到第二实现(如 accountalt)后,M2+ 域仍拿着旧
// Verifier,新 Provider 签发的 token 全部验不过——「可换」名存实亡。
// 此包装让配置层换主后重指:verifier.SetTarget(alternative.Verifier())。
//
// 边界:重定向只在装配期(读 COURIER_GATEWAY_CONFIG 后一次);跟随路由表
// identity 域的 primary,fallbacks 不参与(验证器与主存同源,不逐请求切换)。
package auth

import (
	"context"
	"sync"
)

// Switchable 可重定向的 Verifier(实现 Verifier 接口;装配期可 SetTarget)。
type Switchable struct {
	mu     sync.RWMutex
	target Verifier
}

// NewSwitchable 包装初始 Verifier。
func NewSwitchable(initial Verifier) *Switchable {
	return &Switchable{target: initial}
}

// SetTarget 重指目标(装配期调用;nil 忽略,保持原目标不残废)。
func (s *Switchable) SetTarget(v Verifier) {
	if v == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target = v
}

// Verify 委托当前目标。
func (s *Switchable) Verify(ctx context.Context, accessToken string) (Identity, error) {
	s.mu.RLock()
	v := s.target
	s.mu.RUnlock()
	return v.Verify(ctx, accessToken)
}
