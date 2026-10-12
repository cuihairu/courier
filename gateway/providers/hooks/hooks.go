// Package hooks 账号级联钩子注册表(契约 auth.md v1.1「扩展与演进」)。
//
// 注销的数据级联范围由各下游 Provider 自行订阅(OnAccountDeleted 注册制):
// 网关不认识任何下游存储,新 Provider 接入 = 注册钩子,契约不动。
// 进程级注册表,与内存存储同生命周期;持久化实现替换时随存储面演进。
package hooks

import "sync"

// 事件名(域.事件,契约 events.md 命名规则;钩子走同一命名习惯)。
const AccountDeleted = "account.deleted"

type subscription struct {
	id uint64
	fn func(accountID string)
}

var (
	mu   sync.Mutex
	next uint64
	subs = map[string][]subscription{}
)

// Register 订阅事件;返回取消函数(测试隔离/Provider 卸载用)。
func Register(event string, fn func(accountID string)) func() {
	mu.Lock()
	defer mu.Unlock()
	next++
	id := next
	subs[event] = append(subs[event], subscription{id: id, fn: fn})
	return func() {
		mu.Lock()
		defer mu.Unlock()
		list := subs[event]
		for i, s := range list {
			if s.id == id {
				subs[event] = append(list[:i], list[i+1:]...)
				return
			}
		}
	}
}

// Emit 触发事件;单个订阅者 panic 不影响其余(级联失败不阻断注销受理)。
func Emit(event, accountID string) {
	mu.Lock()
	list := make([]subscription, len(subs[event]))
	copy(list, subs[event])
	mu.Unlock()
	for _, s := range list {
		run(s.fn, accountID)
	}
}

func run(fn func(string), accountID string) {
	defer func() { _ = recover() }()
	fn(accountID)
}
