package voucheralt

import (
	"errors"
	"testing"

	"github.com/cuihairu/courier/gateway/aggregation"
)

func TestRedeemIdempotent(t *testing.T) {
	p := New(Options{})
	p.Register("G1", "coin", 1)
	out, err := p.Redeem("a", "G1")
	if err != nil || out.Repeated {
		t.Fatal(out, err)
	}
	out2, err := p.Redeem("a", "G1")
	if err != nil || !out2.Repeated {
		t.Fatal("want repeated", out2, err)
	}
	// 第二账号:计数已达 1 → 穷尽
	_, err = p.Redeem("b", "G1")
	var ce *aggregation.CodeError
	if !errors.As(err, &ce) || ce.Code != aggregation.CodeCdkExhausted {
		t.Fatalf("want exhausted, got %v", err)
	}
}
