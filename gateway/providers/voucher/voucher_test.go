package voucher

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// 审核回归(2026-11-11 P1):grantHook 在锁外执行——钩子回调本 Provider
// (Register/Redeem 都取 p.mu)不得自锁死锁;原实现持 p.mu 调接入方代码,
// 回调即死锁。死锁以守护超时呈现为失败。
func TestGrantHookReentrantNoDeadlock(t *testing.T) {
	var p *Provider
	var once sync.Once
	done := make(chan struct{})
	p = New(Options{
		RequireAuth: authStub,
		Grant: func(accountID, code, grant string) error {
			// 钩子回调本 Provider:取 p.mu 的两条路径(登记/兑换)。
			if err := p.Register("probe", "g", 1); err != nil {
				t.Errorf("钩子内 Register: %v", err)
			}
			if code != "probe" { // probe 自身的兑换不再入钩子递归
				if _, err := p.Redeem(accountID, "probe"); err != nil {
					t.Errorf("钩子内 Redeem: %v", err)
				}
			}
			once.Do(func() { close(done) })
			return nil
		},
	})
	p.Register("GIFT1", "coin_100", 1)
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/cdk/redeem",
		strings.NewReader(`{"code":"GIFT1"}`)))
	if rec.Code != 200 {
		t.Fatalf("redeem=%d %s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("grantHook 回调本 Provider 死锁(缺陷:钩子曾在 p.mu 内执行)")
	}
}

// 并发同码兑换幂等:恰好一次真实发放,其余 repeated;限次不翻倍。
func TestRedeemConcurrentIdempotent(t *testing.T) {
	var grants atomic.Int32
	p := New(Options{
		RequireAuth: authStub,
		Grant: func(_, _, _ string) error { grants.Add(1); return nil },
	})
	p.Register("RACE1", "coin", 10)

	const n = 8
	start := make(chan struct{})
	out := make(chan string, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/cdk/redeem",
				strings.NewReader(`{"code":"RACE1"}`)))
			out <- rec.Body.String()
		}()
	}
	close(start)
	real := 0
	for i := 0; i < n; i++ {
		body := <-out
		if strings.Contains(body, `"repeated":true`) {
			continue
		}
		if strings.Contains(body, `"repeated":false`) {
			real++
			continue
		}
		t.Fatalf("意外响应: %s", body)
	}
	if real != 1 || grants.Load() != 1 {
		t.Fatalf("真实发放=%d grants=%d,期望恰好 1/1", real, grants.Load())
	}
}
