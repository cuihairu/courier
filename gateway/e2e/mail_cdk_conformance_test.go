// F62 邮件/兑换码一致性 e2e(批次先例同构):同一 wire 场景对默认实现与
// 第二实现各跑一遍、同一断言——「F62 Provider 可换」有行为证明。
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/carrier"
	"github.com/cuihairu/courier/gateway/providers/carrieralt"
	"github.com/cuihairu/courier/gateway/providers/voucher"
	"github.com/cuihairu/courier/gateway/providers/voucheralt"
	"github.com/cuihairu/courier/gateway/routing"
)

type f62Harness struct {
	srv    *httptest.Server
	car    *carrier.Provider
	carAlt *carrieralt.Provider
	vou    *voucher.Provider
	vouAlt *voucheralt.Provider
}

func newF62Harness(t *testing.T, alt bool) *f62Harness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	h := &f62Harness{}
	if alt {
		h.carAlt = carrieralt.New(carrieralt.Options{RequireAuth: reqAuth})
		h.vouAlt = voucheralt.New(voucheralt.Options{RequireAuth: reqAuth})
		if err := reg.Register(h.carAlt); err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(h.vouAlt); err != nil {
			t.Fatal(err)
		}
	} else {
		h.car = carrier.New(carrier.Options{RequireAuth: reqAuth})
		h.vou = voucher.New(voucher.Options{RequireAuth: reqAuth})
		if err := reg.Register(h.car); err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(h.vou); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Register(acc); err != nil {
		t.Fatal(err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapMail:     {Primary: h.carName(alt)},
		providers.CapCdk:      {Primary: h.vouName(alt)},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	h.srv = httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *f62Harness) carName(alt bool) string {
	if alt {
		return carrieralt.DefaultName
	}
	return carrier.DefaultName
}
func (h *f62Harness) vouName(alt bool) string {
	if alt {
		return voucheralt.DefaultName
	}
	return voucher.DefaultName
}

func (h *f62Harness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"f62-e2e","platform":"linux"}`))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: %v", err)
	}
	return out.Data.AccessToken
}

func (h *f62Harness) do(t *testing.T, method, path, token string, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, strings.NewReader(body))
	req.Header.Set("X-Courier-Game-Id", "game_demo")
	req.Header.Set("X-Courier-Env", "prod")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestMailAndCdkConformance(t *testing.T) {
	for _, alt := range []bool{false, true} {
		alt := alt
		t.Run(map[bool]string{false: "default", true: "alt"}[alt], func(t *testing.T) {
			h := newF62Harness(t, alt)
			token := h.login(t)
			// 匿名 → 401
			if code, _ := h.do(t, "GET", "/v1/mail/", "", ""); code != 401 {
				t.Fatalf("anon mail=%d", code)
			}
			if code, _ := h.do(t, "POST", "/v1/cdk/redeem", "", `{"code":"X"}`); code != 401 {
				t.Fatalf("anon cdk=%d", code)
			}
			// 管理面投递(全量+个人)
			mailID := ""
			if alt {
				m, err := h.carAlt.Publish("", "服务器维护补偿", "感谢", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				mailID = m.ID
			} else {
				m, err := h.car.Publish("", "服务器维护补偿", "感谢", nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				mailID = m.ID
			}
			if code, body := h.do(t, "GET", "/v1/mail/", token, ""); code != 200 || !strings.Contains(body, mailID) {
				t.Fatalf("mail list=%d %s", code, body)
			}
			// 领取(幂等)
			if code, _ := h.do(t, "POST", "/v1/mail/"+mailID+"/claim", token, "{}"); code != 200 {
				t.Fatalf("claim=%d", code)
			}
			if code, _ := h.do(t, "POST", "/v1/mail/"+mailID+"/claim", token, "{}"); code != 200 {
				t.Fatalf("claim2=%d", code)
			}
			// cdk 注册 + 兑换(幂等)
			if alt {
				h.vouAlt.Register("E2E-GIFT", "coin_50", 5)
			} else {
				h.vou.Register("E2E-GIFT", "coin_50", 5)
			}
			if code, body := h.do(t, "POST", "/v1/cdk/redeem", token, `{"code":"E2E-GIFT"}`); code != 200 || !strings.Contains(body, `"grant":"coin_50"`) {
				t.Fatalf("redeem=%d %s", code, body)
			}
			if code, body := h.do(t, "POST", "/v1/cdk/redeem", token, `{"code":"E2E-GIFT"}`); code != 200 || !strings.Contains(body, `"repeated":true`) {
				t.Fatalf("redeem2=%d %s", code, body)
			}
			// 未注册码 → 404 CDK_NOT_FOUND
			if code, _ := h.do(t, "POST", "/v1/cdk/redeem", token, `{"code":"NOPE"}`); code != 404 {
				t.Fatalf("nope=%d", code)
			}
		})
	}
}
