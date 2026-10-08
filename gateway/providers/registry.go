package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Handler 能力域处理器:经 Register 挂进注册表,以 /v1/{capability}/ 为前缀服务。
//
// 治理语义(docs/architecture.md「Provider 治理」):请求分发时沿降级链逐个
// HealthCheck,失败则尝试下一个;健康缓存与熔断自批次 7 实现,批次 2 为逐请求探活。
type Handler interface {
	// Name 供应商标识(如 "herald"),注册表内唯一。
	Name() string
	// Capability 能力域;同一能力可注册多个 Handler(primary + fallbacks)。
	Capability() Capability
	// HealthCheck 单次可用性探测;不可用返回非 nil error。
	HealthCheck(ctx context.Context) error
	// ServeHTTP 服务 /v1/{capability}/ 下的请求(Path 中保留能力域前缀)。
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// ErrDuplicateName 同名 Provider 重复注册(首注册者保留)。
var ErrDuplicateName = errors.New("providers: 重复注册同名 Provider")

// Registry Provider 注册表:实现方通过 Register 挂入(默认供应商、自建、第三方同一入口),
// 路由装配只认注册表 + 配置,不感知具体供应商。
type Registry struct {
	mu     sync.RWMutex
	byName map[string]Handler
	byCap  map[Capability][]Handler
}

// NewRegistry 空注册表。
func NewRegistry() *Registry {
	return &Registry{
		byName: make(map[string]Handler),
		byCap:  make(map[Capability][]Handler),
	}
}

// Register 挂入一个 Handler。同名重复注册报错;同能力多注册即降级链候选。
func (r *Registry) Register(h Handler) error {
	if h == nil {
		return errors.New("providers: 注册 nil Handler")
	}
	name := h.Name()
	if name == "" {
		return errors.New("providers: Handler.Name 为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.byName[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateName, name)
	}
	r.byName[name] = h
	r.byCap[h.Capability()] = append(r.byCap[h.Capability()], h)
	return nil
}

// Lookup 按供应商名查询。
func (r *Registry) Lookup(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.byName[name]
	return h, ok
}

// Chain 解析某能力域的降级链:primary + fallbacks[](保序、去重),
// 过滤注册表中不存在的名字(未接入的供应商静默跳过,不残废);
// Disabled 或无任何已注册候选 → nil(能力未配置,返回 COMMON_CAPABILITY_DISABLED)。
func (r *Registry) Chain(cap Capability, cfg CapabilityConfig) []Handler {
	if cfg.Disabled {
		return nil
	}
	names := make([]string, 0, 1+len(cfg.Fallbacks))
	if cfg.Primary != "" {
		names = append(names, cfg.Primary)
	}
	names = append(names, cfg.Fallbacks...)
	if len(names) == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]bool, len(names))
	chain := make([]Handler, 0, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		if h, ok := r.byName[n]; ok {
			chain = append(chain, h)
		}
	}
	if len(chain) == 0 {
		return nil
	}
	return chain
}

// CapabilityHandlers 查询某能力域的全部已注册 Handler(测试与运维观测用)。
func (r *Registry) CapabilityHandlers(cap Capability) []Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Handler, len(r.byCap[cap]))
	copy(out, r.byCap[cap])
	return out
}
