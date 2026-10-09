// 批次 18:SwitchableVerifier 重定向语义(nil 忽略/切换生效)。
package auth

import (
	"context"
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
