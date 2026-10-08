package scope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestScopeRegistryMatchesContractFixture 契约测试骨架(服务端参考实现侧):
// scope 头名与 env 注册表和 fixture 对齐(env 注册表制,fixture ⊆ 注册表)。
func TestScopeRegistryMatchesContractFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "fixtures", "scope.json"))
	if err != nil {
		t.Fatalf("读 fixture: %v", err)
	}
	var f struct {
		Headers struct {
			GameID string `json:"gameId"`
			Env    string `json:"env"`
		} `json:"headers"`
		Envs []string `json:"envs"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解析 scope.json: %v", err)
	}

	if HeaderGameID != f.Headers.GameID {
		t.Errorf("HeaderGameID = %q,fixture = %q", HeaderGameID, f.Headers.GameID)
	}
	if HeaderEnv != f.Headers.Env {
		t.Errorf("HeaderEnv = %q,fixture = %q", HeaderEnv, f.Headers.Env)
	}

	envMu.RLock()
	defer envMu.RUnlock()
	for _, e := range f.Envs {
		if !envEnvs[e] {
			t.Errorf("env 注册表缺 %q", e)
		}
	}
}
