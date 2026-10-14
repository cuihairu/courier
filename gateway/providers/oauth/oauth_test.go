// oauth 注册表单测:按名注册/同名覆盖/显式注销/缺省兜底的优先级。
// wire 行为一致性由 e2e/identity_conformance_test.go 对账号域两实现跑同一场景。
package oauth

import "testing"

type stubVerifier struct{ subject string }

func (v stubVerifier) Verify(_, token, _ string) (string, error) { return v.subject + ":" + token, nil }

func TestRegisterOverwriteAndLookupPrecedence(t *testing.T) {
	// 注册表是全局状态:测试完恢复原样(未按名注册 + 无缺省)。
	defer func() { Register("wx", nil); Register("nilp", nil); SetDefault(nil) }()

	if _, ok := Lookup("wx"); ok {
		t.Fatal("未注册应 miss")
	}

	Register("wx", stubVerifier{subject: "wx_user"})
	v, ok := Lookup("wx")
	if !ok {
		t.Fatal("按名注册后应命中")
	}
	if subject, err := v.Verify("wx", "tok", ""); err != nil || subject != "wx_user:tok" {
		t.Fatalf("verify = %s/%v", subject, err)
	}

	// 同名覆盖:后注册者生效(管理面替换语义)。
	Register("wx", stubVerifier{subject: "wx_user2"})
	v, _ = Lookup("wx")
	if subject, _ := v.Verify("wx", "", ""); subject != "wx_user2:" {
		t.Fatalf("同名覆盖后 = %s", subject)
	}

	// 缺省兜底:按名优先,未按名的走缺省。
	SetDefault(stubVerifier{subject: "fallback"})
	if v, ok := Lookup("wx"); !ok {
		t.Fatal("按名应优先命中")
	} else if subject, _ := v.Verify("", "", ""); subject != "wx_user2:" {
		t.Fatalf("按名优先被破坏 = %s", subject)
	}
	if v, ok := Lookup("other"); !ok {
		t.Fatal("缺省兜底应命中")
	} else if subject, _ := v.Verify("", "", ""); subject != "fallback:" {
		t.Fatalf("兜底 = %s", subject)
	}

	// 显式注销(注册 nil)= 未注册 → 回落缺省。
	Register("nilp", nil)
	if v, ok := Lookup("nilp"); !ok {
		t.Fatal("注销后应回落缺省(有缺省时命中)")
	} else if subject, _ := v.Verify("", "", ""); subject != "fallback:" {
		t.Fatalf("注销后回落 = %s", subject)
	}
}
