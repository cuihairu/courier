// Package session 会话验证:access token → 身份(契约 docs/contract/auth.md「Token 模型」)。
//
// 职责:过期/吊销/scope 绑定/账号状态的统一验证;数据事实留在存储实现侧
// (providers/account),网关不落业务数据。
package session

import (
	"context"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/scope"
)

// SessionView 会话只读投影(存储实现方提供)。
type SessionView struct {
	ID            string
	AccountID     string
	AccountStatus string // ACTIVE | DISABLED
	GameID        string
	Env           string
	DeviceID      string
	AccessExpires time.Time
	Revoked       bool
}

// SessionStore 会话仓储抽象(消费方定义接口,存储方实现)。
type SessionStore interface {
	// SessionByAccessToken 按 access token 原文查找会话(实现方负责哈希比对)。
	SessionByAccessToken(ctx context.Context, accessToken string) (SessionView, bool)
}

// NewVerifier 构造 auth.Verifier;clock 与存储共享同一时钟(测试注入伪时钟)。
func NewVerifier(store SessionStore, clock func() time.Time) auth.Verifier {
	if clock == nil {
		clock = time.Now
	}
	return verifier{store: store, clock: clock}
}

type verifier struct {
	store SessionStore
	clock func() time.Time
}

// Verify 验证顺序:查无 → 吊销 → 过期 → 账号状态 → scope 绑定(auth.md)。
func (v verifier) Verify(ctx context.Context, accessToken string) (auth.Identity, error) {
	if accessToken == "" {
		return auth.Identity{}, auth.ErrUnauthenticated
	}
	s, ok := v.store.SessionByAccessToken(ctx, accessToken)
	if !ok {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if s.Revoked {
		return auth.Identity{}, auth.ErrTokenRevoked
	}
	if v.clock().After(s.AccessExpires) {
		return auth.Identity{}, auth.ErrTokenExpired
	}
	if s.AccountStatus == "DISABLED" {
		return auth.Identity{}, auth.ErrAccountDisabled
	}
	if sc, ok := scope.FromContext(ctx); ok && (sc.GameID != s.GameID || sc.Env != s.Env) {
		return auth.Identity{}, auth.ErrScopeMismatch
	}
	return auth.Identity{
		AccountID: s.AccountID,
		SessionID: s.ID,
		DeviceID:  s.DeviceID,
		GameID:    s.GameID,
		Env:       s.Env,
	}, nil
}
