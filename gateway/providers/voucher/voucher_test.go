package voucher

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
)

func authStub(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
			auth.Identity{AccountID: "acct1", SessionID: "ses_1"})))
	})
}

func newP() *Provider { return New(Options{RequireAuth: authStub}) }

func TestRedeem(t *testing.T) {
	p := newP()
	if err := p.Register("GIFT1", "coin_100", 1); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/cdk/redeem", strings.NewReader(`{"code":"GIFT1"}`))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("redeem=%d %s", rec.Code, rec.Body.String())
	}
	// 同账号重复 = 幂等同结果
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/v1/cdk/redeem", strings.NewReader(`{"code":"GIFT1"}`)))
	if rec2.Code != 200 || !strings.Contains(rec2.Body.String(), `"repeated":true`) {
		t.Fatalf("repeat=%d %s", rec2.Code, rec2.Body.String())
	}
}

func TestRedeemExhausted(t *testing.T) {
	p := newP()
	p.Register("LIMIT1", "coin", 1)
	// acct1 用掉唯一一次
	out, err := p.Redeem("acct1", "LIMIT1")
	if err != nil || out.Repeated {
		t.Fatal(out, err)
	}
	// acct2 应穷尽
	_, err = p.Redeem("acct2", "LIMIT1")
	var ce *aggregation.CodeError
	if !errors.As(err, &ce) || ce.Code != aggregation.CodeCdkExhausted {
		t.Fatalf("want exhausted, got %v", err)
	}
}

func TestRedeemNotFound(t *testing.T) {
	p := newP()
	_, err := p.Redeem("acct1", "NOPE")
	var ce *aggregation.CodeError
	if !errors.As(err, &ce) || ce.Code != aggregation.CodeCdkNotFound {
		t.Fatalf("want notfound, got %v", err)
	}
}
