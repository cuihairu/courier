package routing

import (
	"net/http"
	"sync"
	"testing"

	"github.com/cuihairu/courier/gateway/providers"
)

// TestHotReload F78 一期验收:SIGHUP 配置热重载接缝——Reload 原子换表,
// 在途语义同实名域 router.Reload(在途按旧表完成,新请求走新表)。
func TestHotReload(t *testing.T) {
	reg := providers.NewRegistry()
	a := &stubHandler{name: "a", cap: providers.CapAnnouncements, healthy: true}
	if err := reg.Register(a); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	hot := NewHot(reg, nil, providers.Config{})

	t.Run("初始未配置 → 501 降级", func(t *testing.T) {
		rec, env := do(t, hot, "/v1/announcements/")
		if rec.Code != http.StatusNotImplemented || env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Fatalf("status=%d error=%+v, 期望 501 COMMON_CAPABILITY_DISABLED", rec.Code, env.Error)
		}
	})

	t.Run("Reload 配置 primary → 路由生效", func(t *testing.T) {
		hot.Reload(providers.Config{
			providers.CapAnnouncements: {Primary: "a"},
		})
		rec, _ := do(t, hot, "/v1/announcements/")
		if rec.Code != http.StatusNoContent || rec.Header().Get("X-Served-By") != "a" {
			t.Fatalf("status=%d servedBy=%q, 期望 204 由 a 服务", rec.Code, rec.Header().Get("X-Served-By"))
		}
	})

	t.Run("Reload 显式关闭 → 回落 501", func(t *testing.T) {
		hot.Reload(providers.Config{
			providers.CapAnnouncements: {Disabled: true},
		})
		rec, env := do(t, hot, "/v1/announcements/")
		if rec.Code != http.StatusNotImplemented || env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Fatalf("status=%d error=%+v, 期望 501", rec.Code, env.Error)
		}
	})

	t.Run("Reload 换 fallback 链顺序", func(t *testing.T) {
		b := &stubHandler{name: "b", cap: providers.CapAnnouncements, healthy: false}
		if err := reg.Register(b); err != nil {
			t.Fatalf("注册失败: %v", err)
		}
		hot.Reload(providers.Config{
			providers.CapAnnouncements: {Primary: "a", Fallbacks: []string{"b"}},
		})
		rec, _ := do(t, hot, "/v1/announcements/")
		if rec.Header().Get("X-Served-By") != "a" {
			t.Fatalf("servedBy=%q, 期望健康检查通过的 a", rec.Header().Get("X-Served-By"))
		}
	})

	t.Run("并发 Reload 与请求无竞争(-race 下验证)", func(t *testing.T) {
		cfgs := []providers.Config{
			{providers.CapAnnouncements: {Primary: "a"}},
			{providers.CapAnnouncements: {Primary: "a", Fallbacks: []string{"b"}}},
			{providers.CapAnnouncements: {Disabled: true}},
		}
		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
					hot.Reload(cfgs[i%len(cfgs)])
				}
			}
		}()
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
						rec, _ := do(t, hot, "/v1/announcements/")
						// 只可能三态:204(链上健康) / 501(关闭) / 503(链全不健康)
						switch rec.Code {
						case http.StatusNoContent, http.StatusNotImplemented, http.StatusServiceUnavailable:
						default:
							t.Errorf("并发换表期间出现异常状态 %d", rec.Code)
						}
					}
				}
			}()
		}
		for i := 0; i < 200; i++ {
			_, _ = do(t, hot, "/v1/announcements/")
		}
		close(stop)
		wg.Wait()
	})
}
