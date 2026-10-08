package aggregation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCodeTableMatchesContractFixture 契约测试骨架(服务端参考实现侧):
// 网关 codeTable 与冻结错误码表 fixture 双向对齐,新增/漂移即失败。
// fixture 源:docs/contract/errors.md 错误码表 v1(经 tools/contractgen 与各端枚举同步)。
func TestCodeTableMatchesContractFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "fixtures", "errors.json"))
	if err != nil {
		t.Fatalf("读 fixture: %v", err)
	}
	var f struct {
		Version int `json:"version"`
		Codes   []struct {
			Code      string `json:"code"`
			HTTP      int    `json:"http"`
			Retryable bool   `json:"retryable"`
		} `json:"codes"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解析 errors.json: %v", err)
	}
	if f.Version != 1 {
		t.Fatalf("fixture version = %d,期望 1(升版需走契约变更流程)", f.Version)
	}

	for _, row := range f.Codes {
		spec, ok := codeTable[row.Code]
		if !ok {
			t.Errorf("网关缺冻结码 %s", row.Code)
			continue
		}
		if spec.status != row.HTTP || spec.retryable != row.Retryable {
			t.Errorf("%s 网关映射 (%d,%v) ≠ fixture (%d,%v)",
				row.Code, spec.status, spec.retryable, row.HTTP, row.Retryable)
		}
	}
	if len(codeTable) != len(f.Codes) {
		t.Errorf("codeTable 有 %d 码,fixture 有 %d;网关多出的码须先入契约表", len(codeTable), len(f.Codes))
	}
}
