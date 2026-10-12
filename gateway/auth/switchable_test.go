// 批次 18:SwitchableVerifier 重定向语义(nil 忽略/切换生效)。
package auth

import (
	"context"
	"sync"
	"testing"
)

func TestSwitchableRedirect(t *testing.T) {
	v := NewSwitchable(stubVerifier{id: Identity{AccountID: "acc_1"}})
	if id, err := v.Verify(context.Background(), "tok"); err != nil || id.AccountID != "acc_1" {
		t.Fatalf("初始目标: %v %v", id, err)
	}

	v.SetTarget(stubVerifier{id: Identity{AccountID: "acc_2"}})
	if id, err := v.Verify(context.Background(), "tok"); err != nil || id.AccountID != "acc_2" {
		t.Fatalf("切换后: %v %v", id, err)
	}

	v.SetTarget(nil) // nil 忽略,不残废
	if id, err := v.Verify(context.Background(), "tok"); err != nil || id.AccountID != "acc_2" {
		t.Fatalf("nil 后: %v %v", id, err)
	}
}

// 路线图阶段 C「禁用/热重载不破坏在途请求」identity 侧实证:切换与在途验证并发时,
// 每个在途调用要么拿到旧目标、要么拿到新目标的身份(捕获引用即定,不存在中间态),
// 无 panic / 无竞态(-race 下跑)。与 router.Reload 的「在途按旧表完成」同口径。
func TestSwitchableSetTarget_ConcurrentInFlightCompletes(t *testing.T) {
	a := stubVerifier{id: Identity{AccountID: "acc_a"}}
	b := stubVerifier{id: Identity{AccountID: "acc_b"}}
	v := NewSwitchable(a)

	const workers = 8
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() { // 切换方:在验证进行中反复重指
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				v.SetTarget(b)
			} else {
				v.SetTarget(a)
			}
		}
		close(stop)
	}()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				id, err := v.Verify(context.Background(), "tok")
				if err != nil || (id.AccountID != "acc_a" && id.AccountID != "acc_b") {
					t.Errorf("在途验证出现中间态: %v %v", id, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
