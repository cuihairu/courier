package routing

import (
	"net/http"
	"sync/atomic"

	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// Hot 热重载路由包装(F78 一期,批次 99):SIGHUP 配置重载把新路由表原子换入。
// Reload 整体重建 root handler 并以 atomic.Pointer 替换——在途请求按旧 handler
// 完成,新请求走新表(同实名域 router.Reload「在途按旧表」契约语义);
// 已注册 Provider 实例不重建,只重排降级链,会话/SSE/内存状态全部保留。
type Hot struct {
	reg   *providers.Registry
	chain middleware.Middleware
	root  atomic.Pointer[http.Handler]
}

// NewHot 装配并以 cfg 初始化(reg/chain 语义同 New)。
func NewHot(reg *providers.Registry, chain middleware.Middleware, cfg providers.Config) *Hot {
	h := &Hot{reg: reg, chain: chain}
	h.Reload(cfg)
	return h
}

// Reload 重建路由并原子换入;cfg 已由调用方校验(LoadConfig),此处不失败。
func (h *Hot) Reload(cfg providers.Config) {
	var handler http.Handler = New(h.reg, cfg, h.chain)
	h.root.Store(&handler)
}

func (h *Hot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*h.root.Load()).ServeHTTP(w, r)
}
