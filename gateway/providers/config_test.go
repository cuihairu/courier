package providers

import "testing"

func TestLoadConfig(t *testing.T) {
	t.Run("合法路由表", func(t *testing.T) {
		cfg, err := LoadConfig([]byte(`{
			"announcements": {"primary": "herald", "fallbacks": ["custom"]},
			"payments": {"disabled": true}
		}`))
		if err != nil {
			t.Fatalf("LoadConfig 失败: %v", err)
		}
		if got := cfg[CapAnnouncements].Primary; got != "herald" {
			t.Fatalf("announcements.primary = %q, 期望 herald", got)
		}
		if got := len(cfg[CapAnnouncements].Fallbacks); got != 1 {
			t.Fatalf("fallbacks 长度 = %d, 期望 1", got)
		}
		if !cfg[CapPayments].Disabled {
			t.Fatal("payments 应为 disabled")
		}
	})

	t.Run("未知能力域报错", func(t *testing.T) {
		if _, err := LoadConfig([]byte(`{"accounts": {"primary": "x"}}`)); err == nil {
			t.Fatal("未知能力域应报错(防配置键拼错)")
		}
	})

	t.Run("非法 JSON 报错", func(t *testing.T) {
		if _, err := LoadConfig([]byte(`{`)); err == nil {
			t.Fatal("非法 JSON 应报错")
		}
	})

	t.Run("fallbacks 缺 primary 报错", func(t *testing.T) {
		if _, err := LoadConfig([]byte(`{"support": {"fallbacks": ["x"]}}`)); err == nil {
			t.Fatal("有 fallbacks 但缺 primary 应报错")
		}
	})

	t.Run("空配置键等价缺席", func(t *testing.T) {
		cfg, err := LoadConfig([]byte(`{"support": {}}`))
		if err != nil {
			t.Fatalf("LoadConfig 失败: %v", err)
		}
		if _, ok := cfg[CapSupport]; ok {
			t.Fatal("空配置键应被忽略")
		}
	})
}
