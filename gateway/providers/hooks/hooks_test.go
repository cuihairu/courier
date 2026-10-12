// 钩子注册表单测:注册/触发/取消/panic 隔离(契约 auth.md v1.1 注册制)。
package hooks

import "testing"

func TestRegisterEmitUnsubscribe(t *testing.T) {
	var got []string
	cancel := Register(AccountDeleted, func(id string) { got = append(got, id) })
	Emit(AccountDeleted, "acc_1")
	Emit("other.event", "acc_2") // 其他事件不串
	if len(got) != 1 || got[0] != "acc_1" {
		t.Fatalf("got = %v", got)
	}
	cancel()
	Emit(AccountDeleted, "acc_3")
	if len(got) != 1 {
		t.Fatalf("取消后不应再触发: %v", got)
	}
}

func TestEmitIsolatesPanic(t *testing.T) {
	var hit bool
	Register(AccountDeleted, func(string) { panic("subscriber boom") })
	Register(AccountDeleted, func(string) { hit = true })
	Emit(AccountDeleted, "acc_x") // 不应向上抛
	if !hit {
		t.Fatal("panic 订阅者不应阻断其余订阅")
	}
}
