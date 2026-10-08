package providers

import (
	"encoding/json"
	"fmt"
)

// CapabilityConfig 配置驱动路由表的一行:primary + 降级链 fallbacks[]
// (架构:docs/architecture.md「Provider 治理」)。
//
//   - Primary   首选 Provider 名;为空 = 未配置。
//   - Fallbacks 降级链,按序尝试(运行时热重载与熔断自批次 7 实现)。
//   - Disabled  显式关闭,与未配置同样返回 COMMON_CAPABILITY_DISABLED。
type CapabilityConfig struct {
	Primary   string   `json:"primary,omitempty"`
	Fallbacks []string `json:"fallbacks,omitempty"`
	Disabled  bool     `json:"disabled,omitempty"`
}

// Config 路由表:能力域 → Provider 链配置。nil = 全部未配置(全部能力降级)。
type Config map[Capability]CapabilityConfig

// LoadConfig 解析 JSON 路由表,例:
//
//	{"announcements":{"primary":"herald","fallbacks":["custom"]},"payments":{"disabled":true}}
//
// 未知能力域或空 Provider 名视为错误(防配置键拼错静默失效)。
func LoadConfig(data []byte) (Config, error) {
	var raw map[string]CapabilityConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("providers: 解析路由表: %w", err)
	}
	known := make(map[Capability]bool, len(AllCapabilities()))
	for _, c := range AllCapabilities() {
		known[c] = true
	}
	cfg := make(Config, len(raw))
	for name, cc := range raw {
		cap := Capability(name)
		if !known[cap] {
			return nil, fmt.Errorf("providers: 未知能力域 %q", name)
		}
		if cc.Disabled {
			// 显式关闭无需 primary(关闭态不需要供应商)。
			cfg[cap] = cc
			continue
		}
		if cc.Primary == "" && len(cc.Fallbacks) == 0 {
			// 空配置无意义,视为未配置(等价于缺席该键)。
			continue
		}
		if cc.Primary == "" {
			return nil, fmt.Errorf("providers: 能力域 %q 有 fallbacks 但缺 primary", name)
		}
		for i, fb := range cc.Fallbacks {
			if fb == "" {
				return nil, fmt.Errorf("providers: 能力域 %q 的 fallbacks[%d] 为空", name, i)
			}
		}
		cfg[cap] = cc
	}
	return cfg, nil
}
