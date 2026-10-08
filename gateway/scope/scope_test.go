package scope

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

func TestFromRequest(t *testing.T) {
	t.Run("合法 scope", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/x/", nil)
		r.Header.Set(HeaderGameID, "game_demo")
		r.Header.Set(HeaderEnv, "staging")
		s, err := FromRequest(r)
		if err != nil {
			t.Fatalf("FromRequest 失败: %v", err)
		}
		if s.GameID != "game_demo" || s.Env != "staging" {
			t.Fatalf("scope = %+v", s)
		}
	})

	t.Run("缺失/非法 → ErrInvalid", func(t *testing.T) {
		cases := map[string][2]string{
			"全缺":      {"", ""},
			"缺 game":  {"", "prod"},
			"缺 env":   {"game_demo", ""},
			"env 非注册": {"game_demo", "qa"},
		}
		for name, kv := range cases {
			t.Run(name, func(t *testing.T) {
				r := httptest.NewRequest("GET", "/v1/x/", nil)
				if kv[0] != "" {
					r.Header.Set(HeaderGameID, kv[0])
				}
				if kv[1] != "" {
					r.Header.Set(HeaderEnv, kv[1])
				}
				if _, err := FromRequest(r); !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, 期望 ErrInvalid", err)
				}
			})
		}
	})

	t.Run("空白裁剪", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/v1/x/", nil)
		r.Header.Set(HeaderGameID, "  game_demo ")
		r.Header.Set(HeaderEnv, " prod")
		s, err := FromRequest(r)
		if err != nil || s.GameID != "game_demo" || s.Env != "prod" {
			t.Fatalf("scope=%+v err=%v, 期望裁剪后合法", s, err)
		}
	})
}

func TestEnvRegistry(t *testing.T) {
	if !ValidEnv("dev") || !ValidEnv("staging") || !ValidEnv("prod") {
		t.Fatal("内置 env 注册表应含 dev/staging/prod")
	}
	if ValidEnv("qa") {
		t.Fatal("qa 默认不在注册表")
	}
	RegisterEnv("qa")
	if !ValidEnv("qa") {
		t.Fatal("RegisterEnv 扩展后 qa 应合法")
	}
}

func TestContext(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("空上下文应无 scope")
	}
	want := Scope{GameID: "g", Env: "prod"}
	ctx := WithContext(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok || got != want {
		t.Fatalf("ctx scope = %+v, %v", got, ok)
	}
}
