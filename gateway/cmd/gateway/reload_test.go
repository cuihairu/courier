package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/accountalt"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/chirpalt"
	"github.com/cuihairu/courier/gateway/providers/scribe"
	"github.com/cuihairu/courier/gateway/providers/scribealt"
)

// --- F78 一期(批次 99):SIGHUP 配置热重载 ---

// fakeVerifier 回显自己名字的测试验证器(观察 Switchable 当前指向)。
type fakeVerifier struct{ name string }

func (f *fakeVerifier) Verify(context.Context, string) (auth.Identity, error) {
	return auth.Identity{}, fmt.Errorf("verifier:%s", f.name)
}

func TestMergeConfig(t *testing.T) {
	defaults := providers.Config{
		providers.CapIdentity:      {Primary: account.DefaultName},
		providers.CapAnnouncements: {Primary: "herald"},
	}

	t.Run("用户表覆盖同域、保留未涉域", func(t *testing.T) {
		user := providers.Config{
			providers.CapAnnouncements: {Primary: "herald-alt"},
			providers.CapRealname:      {Primary: "warden"},
		}
		merged := mergeConfig(defaults, user)
		if merged[providers.CapIdentity].Primary != account.DefaultName {
			t.Fatalf("identity=%+v, 期望默认保留", merged[providers.CapIdentity])
		}
		if merged[providers.CapAnnouncements].Primary != "herald-alt" {
			t.Fatalf("announcements=%+v, 期望用户覆盖", merged[providers.CapAnnouncements])
		}
		if merged[providers.CapRealname].Primary != "warden" {
			t.Fatalf("realname=%+v, 期望用户新增", merged[providers.CapRealname])
		}
	})

	t.Run("用户表为 nil = 纯默认", func(t *testing.T) {
		merged := mergeConfig(defaults, nil)
		if len(merged) != len(defaults) || merged[providers.CapIdentity].Primary != account.DefaultName {
			t.Fatalf("merged=%+v", merged)
		}
	})
}

func TestFollowTargetsApply(t *testing.T) {
	var pubDefault, pubAlt bool
	ft := &followTargets{
		identity:      auth.NewSwitchable(&fakeVerifier{"default"}),
		idVerifier:    &fakeVerifier{"default"},
		idAltVerifier: &fakeVerifier{"alt"},
		publish:       newAtomicFn(func(string, any) {}),
		pubDefault:    func(string, any) { pubDefault = true },
		pubAlt:        func(string, any) { pubAlt = true },
		maint:         newAtomicFn(func() bool { return false }),
		maintDefault:  func() bool { return false },
		maintAlt:      func() bool { return true },
	}

	currentVerifier := func() string {
		_, err := ft.identity.Verify(context.Background(), "")
		return strings.TrimPrefix(err.Error(), "verifier:")
	}
	firePublish := func() { ft.publish.Get()("e", nil) }

	t.Run("空用户表不重指", func(t *testing.T) {
		ft.apply(nil)
		if currentVerifier() != "default" {
			t.Fatalf("verifier=%q", currentVerifier())
		}
	})

	t.Run("primary 指向第二实现 → 三接缝全重指", func(t *testing.T) {
		ft.apply(providers.Config{
			providers.CapIdentity: {Primary: accountalt.DefaultName},
			providers.CapMessages: {Primary: chirpalt.DefaultName},
			providers.CapApp:      {Primary: scribealt.DefaultName},
		})
		if currentVerifier() != "alt" {
			t.Fatalf("verifier=%q, 期望 alt", currentVerifier())
		}
		firePublish()
		if !pubAlt || pubDefault {
			t.Fatalf("publish 落点错: default=%v alt=%v", pubDefault, pubAlt)
		}
		if !ft.maint.Get()() {
			t.Fatal("维护门应读 alt 源")
		}
	})

	t.Run("primary 指回默认实现 → 三接缝全回指(重载回退必需)", func(t *testing.T) {
		pubDefault, pubAlt = false, false
		ft.apply(providers.Config{
			providers.CapIdentity: {Primary: account.DefaultName},
			providers.CapMessages: {Primary: chirp.DefaultName},
			providers.CapApp:      {Primary: scribe.DefaultName},
		})
		if currentVerifier() != "default" {
			t.Fatalf("verifier=%q, 期望指回 default", currentVerifier())
		}
		firePublish()
		if pubAlt || !pubDefault {
			t.Fatalf("publish 落点错: default=%v alt=%v", pubDefault, pubAlt)
		}
		if ft.maint.Get()() {
			t.Fatal("维护门应读默认源")
		}
	})

	t.Run("不认识的 primary / Disabled / 缺席键不重指", func(t *testing.T) {
		ft.apply(providers.Config{
			providers.CapIdentity: {Primary: "mystery"},
		})
		if currentVerifier() != "default" {
			t.Fatalf("未知 primary 不应重指, got %q", currentVerifier())
		}
		ft.apply(providers.Config{
			providers.CapIdentity: {Disabled: true},
		})
		if currentVerifier() != "default" {
			t.Fatalf("Disabled 不应重指, got %q", currentVerifier())
		}
		ft.apply(providers.Config{
			providers.CapRealname: {Primary: "warden"},
		})
		if currentVerifier() != "default" {
			t.Fatalf("缺席键不应重指, got %q", currentVerifier())
		}
	})
}

// TestGatewaySIGHUPReload F78 一期端到端:改配置文件 → SIGHUP → 不重启即换表;
// 损坏文件 → 保留旧表(fail-safe,只记日志)。
func TestGatewaySIGHUPReload(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	t.Setenv("COURIER_GATEWAY_CONFIG", cfgPath)
	baseURL := startGateway(t)

	post := func(path string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("构造请求失败: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Courier-Game-Id", "game_demo")
		req.Header.Set("X-Courier-Env", "prod")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	waitFor := func(desc string, want int) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if got := post("/v1/realname/verify"); got == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: 3s 内未到 %d", desc, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	hup := func() {
		t.Helper()
		if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("发 SIGHUP 失败: %v", err)
		}
	}

	waitFor("初始空表", http.StatusNotImplemented)

	t.Run("启用 realname → SIGHUP → 不重启换表 501→401", func(t *testing.T) {
		if err := os.WriteFile(cfgPath, []byte(`{"realname":{"primary":"warden"}}`), 0o600); err != nil {
			t.Fatalf("写配置失败: %v", err)
		}
		hup()
		waitFor("SIGHUP 后启用", http.StatusUnauthorized)
	})

	t.Run("损坏文件 → SIGHUP → 保留旧表(fail-safe)", func(t *testing.T) {
		if err := os.WriteFile(cfgPath, []byte(`{invalid`), 0o600); err != nil {
			t.Fatalf("写配置失败: %v", err)
		}
		hup()
		time.Sleep(300 * time.Millisecond) // 给重载 goroutine 处理信号的时间
		if got := post("/v1/realname/verify"); got != http.StatusUnauthorized {
			t.Fatalf("损坏配置不应换表: got %d, 期望 401(旧表保留)", got)
		}
	})

	t.Run("清空配置 → SIGHUP → 回落默认表 401→501", func(t *testing.T) {
		if err := os.WriteFile(cfgPath, []byte(`{}`), 0o600); err != nil {
			t.Fatalf("写配置失败: %v", err)
		}
		hup()
		waitFor("SIGHUP 后回落", http.StatusNotImplemented)
	})
}
