package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/scope"
)

type stubStore struct {
	view SessionView
	ok   bool
}

func (s *stubStore) SessionByAccessToken(context.Context, string) (SessionView, bool) {
	return s.view, s.ok
}

func scopeCtx(gameID, env string) context.Context {
	ctx := context.Background()
	if gameID == "" {
		return ctx
	}
	r := scope.WithContext(ctx, scope.Scope{GameID: gameID, Env: env})
	return r
}

func TestVerify(t *testing.T) {
	now := time.Now()
	base := SessionView{
		ID: "ses_1", AccountID: "acc_1", AccountStatus: "ACTIVE",
		GameID: "game_a", Env: "prod",
		AccessExpires: now.Add(15 * time.Minute),
	}

	t.Run("查无 → ErrInvalidCredentials", func(t *testing.T) {
		v := NewVerifier(&stubStore{ok: false}, nil)
		if _, err := v.Verify(scopeCtx("game_a", "prod"), "tok"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("吊销 → ErrTokenRevoked", func(t *testing.T) {
		v := NewVerifier(&stubStore{view: func() SessionView { v := base; v.Revoked = true; return v }(), ok: true}, nil)
		if _, err := v.Verify(scopeCtx("game_a", "prod"), "tok"); !errors.Is(err, auth.ErrTokenRevoked) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("过期 → ErrTokenExpired", func(t *testing.T) {
		v := NewVerifier(&stubStore{view: base, ok: true}, func() time.Time { return now.Add(16 * time.Minute) })
		if _, err := v.Verify(scopeCtx("game_a", "prod"), "tok"); !errors.Is(err, auth.ErrTokenExpired) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("账号禁用 → ErrAccountDisabled", func(t *testing.T) {
		view := base
		view.AccountStatus = "DISABLED"
		v := NewVerifier(&stubStore{view: view, ok: true}, nil)
		if _, err := v.Verify(scopeCtx("game_a", "prod"), "tok"); !errors.Is(err, auth.ErrAccountDisabled) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("scope 绑定不符 → ErrScopeMismatch", func(t *testing.T) {
		v := NewVerifier(&stubStore{view: base, ok: true}, nil)
		if _, err := v.Verify(scopeCtx("game_b", "prod"), "tok"); !errors.Is(err, auth.ErrScopeMismatch) {
			t.Fatalf("err = %v", err)
		}
		if _, err := v.Verify(scopeCtx("game_a", "dev"), "tok"); !errors.Is(err, auth.ErrScopeMismatch) {
			t.Fatalf("env 不符也应拒绝: %v", err)
		}
	})

	t.Run("通过并注入身份", func(t *testing.T) {
		v := NewVerifier(&stubStore{view: base, ok: true}, func() time.Time { return now })
		id, err := v.Verify(scopeCtx("game_a", "prod"), "tok")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if id.AccountID != "acc_1" || id.SessionID != "ses_1" || id.GameID != "game_a" || id.Env != "prod" {
			t.Fatalf("identity = %+v", id)
		}
	})

	t.Run("空 token → ErrUnauthenticated", func(t *testing.T) {
		v := NewVerifier(&stubStore{view: base, ok: true}, nil)
		if _, err := v.Verify(scopeCtx("game_a", "prod"), ""); !errors.Is(err, auth.ErrUnauthenticated) {
			t.Fatalf("err = %v", err)
		}
	})
}
