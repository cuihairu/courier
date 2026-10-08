package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// fakeHandler 可配置健康状态的测试 Handler。
type fakeHandler struct {
	name    string
	cap     Capability
	healthy bool
}

func (f *fakeHandler) Name() string                                 { return f.name }
func (f *fakeHandler) Capability() Capability                       { return f.cap }
func (f *fakeHandler) HealthCheck(context.Context) error            { return f.healthErr() }
func (f *fakeHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}
func (f *fakeHandler) healthErr() error {
	if f.healthy {
		return nil
	}
	return errors.New("unhealthy")
}

func names(hs []Handler) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name()
	}
	return out
}

func TestRegister(t *testing.T) {
	reg := NewRegistry()

	t.Run("注册与查询", func(t *testing.T) {
		if err := reg.Register(&fakeHandler{name: "herald", cap: CapAnnouncements, healthy: true}); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
		h, ok := reg.Lookup("herald")
		if !ok || h.Capability() != CapAnnouncements {
			t.Fatalf("Lookup(herald) = %v, %v", h, ok)
		}
	})

	t.Run("同能力多注册允许(降级链候选)", func(t *testing.T) {
		if err := reg.Register(&fakeHandler{name: "custom", cap: CapAnnouncements, healthy: true}); err != nil {
			t.Fatalf("同能力第二注册失败: %v", err)
		}
		if got := len(reg.CapabilityHandlers(CapAnnouncements)); got != 2 {
			t.Fatalf("announcements 已注册数 = %d, 期望 2", got)
		}
	})

	t.Run("同名重复注册报错", func(t *testing.T) {
		err := reg.Register(&fakeHandler{name: "herald", cap: CapSupport, healthy: true})
		if !errors.Is(err, ErrDuplicateName) {
			t.Fatalf("err = %v, 期望 ErrDuplicateName", err)
		}
		if h, _ := reg.Lookup("herald"); h.Capability() != CapAnnouncements {
			t.Fatal("重复注册不应覆盖首注册者")
		}
	})

	t.Run("空名与 nil 报错", func(t *testing.T) {
		if err := reg.Register(&fakeHandler{name: "", cap: CapSupport}); err == nil {
			t.Fatal("空名注册应报错")
		}
		if err := reg.Register(nil); err == nil { //nolint:staticcheck // 显式测 nil 接口
			t.Fatal("nil 注册应报错")
		}
	})
}

func TestChain(t *testing.T) {
	reg := NewRegistry()
	mustReg := func(h Handler) {
		t.Helper()
		if err := reg.Register(h); err != nil {
			t.Fatalf("注册 %s 失败: %v", h.Name(), err)
		}
	}
	mustReg(&fakeHandler{name: "herald", cap: CapAnnouncements, healthy: true})
	mustReg(&fakeHandler{name: "backup", cap: CapAnnouncements, healthy: true})
	mustReg(&fakeHandler{name: "croupier", cap: CapSupport, healthy: true})

	t.Run("primary+fallbacks 保序", func(t *testing.T) {
		got := names(reg.Chain(CapAnnouncements, CapabilityConfig{
			Primary:   "herald",
			Fallbacks: []string{"backup", "missing"},
		}))
		want := []string{"herald", "backup"} // 未注册的 missing 静默跳过
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("chain = %v, 期望 %v", got, want)
		}
	})

	t.Run("primary 未注册则退到 fallback", func(t *testing.T) {
		got := names(reg.Chain(CapAnnouncements, CapabilityConfig{
			Primary:   "never-registered",
			Fallbacks: []string{"backup"},
		}))
		if len(got) != 1 || got[0] != "backup" {
			t.Fatalf("chain = %v, 期望 [backup]", got)
		}
	})

	t.Run("去重", func(t *testing.T) {
		got := names(reg.Chain(CapSupport, CapabilityConfig{
			Primary:   "croupier",
			Fallbacks: []string{"croupier"},
		}))
		if len(got) != 1 {
			t.Fatalf("chain = %v, 期望去重后 1 项", got)
		}
	})

	t.Run("Disabled → nil", func(t *testing.T) {
		if got := reg.Chain(CapSupport, CapabilityConfig{Primary: "croupier", Disabled: true}); got != nil {
			t.Fatalf("disabled chain = %v, 期望 nil", got)
		}
	})

	t.Run("未配置 → nil", func(t *testing.T) {
		if got := reg.Chain(CapSupport, CapabilityConfig{}); got != nil {
			t.Fatalf("未配置 chain = %v, 期望 nil", got)
		}
		if got := reg.Chain(CapPayments, CapabilityConfig{Primary: "nobody"}); got != nil {
			t.Fatalf("全部未注册 chain = %v, 期望 nil", got)
		}
	})
}
