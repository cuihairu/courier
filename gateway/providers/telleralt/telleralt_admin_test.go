// telleralt 管理面单测(v1.2):凭证 fail-closed、SKU 登记校验、检索/详情分支、
// 断单重发守卫、退款金额口径与幂等、游标编解码。wire 行为一致性由 e2e
// conformance 对两实现同断言承载,此处补包内可直达的分支臂。
package telleralt

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAdminTok = "unit_admin_tok"

func adminHarness(t *testing.T) *PaymentsProvider {
	t.Helper()
	p, _ := harness(t, "s3cret", nil, nil)
	p.adminToken = testAdminTok
	return p
}

func adminCall(t *testing.T, p *PaymentsProvider, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	hdr := map[string]string{}
	if token != "" {
		hdr["Authorization"] = "Bearer " + token
	}
	return call(t, p, method, path, body, hdr)
}

func TestAdminAuthFailClosed(t *testing.T) {
	p := adminHarness(t)
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders", "", ""); rec.Code != 401 {
		t.Fatalf("缺凭证 = %d", rec.Code)
	}
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders", "", "wrong"); rec.Code != 401 {
		t.Fatalf("错凭证 = %d", rec.Code)
	}
	p.adminToken = ""
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders", "", testAdminTok); rec.Code != 401 {
		t.Fatalf("未配置 = %d", rec.Code)
	}
}

func TestAdminSkuValidation(t *testing.T) {
	p := adminHarness(t)
	if rec := adminCall(t, p, http.MethodPost, "/v1/payments/admin/skus",
		`{"skuId":"s1","productId":"p1","form":"DIRECT_PURCHASE","amountCents":100,"currency":"CNY"}`, testAdminTok); rec.Code != 200 {
		t.Fatalf("合法 sku = %d %s", rec.Code, rec.Body.String())
	}
	for _, body := range []string{
		`{"skuId":"","productId":"p1","form":"DIRECT_PURCHASE","amountCents":100,"currency":"CNY"}`,
		`{"skuId":"s1","productId":"","form":"DIRECT_PURCHASE","amountCents":100,"currency":"CNY"}`,
		`{"skuId":"s1","productId":"p1","form":"NOT_A_FORM","amountCents":100,"currency":"CNY"}`,
		`{"skuId":"s1","productId":"p1","form":"DIRECT_PURCHASE","amountCents":0,"currency":"CNY"}`,
		`{"skuId":"s1","productId":"p1","form":"DIRECT_PURCHASE","amountCents":100,"currency":""}`,
	} {
		if rec := adminCall(t, p, http.MethodPost, "/v1/payments/admin/skus", body, testAdminTok); rec.Code != 400 {
			t.Fatalf("非法 sku %s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminOrderBranches(t *testing.T) {
	p := adminHarness(t)
	seedSkus(p)
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders/", "", testAdminTok); rec.Code != 404 {
		t.Fatalf("管理详情空 id = %d", rec.Code)
	}
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders/order_nope", "", testAdminTok); rec.Code != 404 {
		t.Fatalf("管理详情未知 = %d", rec.Code)
	}
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders?limit=0", "", testAdminTok); rec.Code != 400 {
		t.Fatalf("limit 0 = %d", rec.Code)
	}
	if rec := adminCall(t, p, http.MethodGet, "/v1/payments/admin/orders?limit=abc", "", testAdminTok); rec.Code != 400 {
		t.Fatalf("limit abc = %d", rec.Code)
	}
	if rec := adminCall(t, p, http.MethodPost, "/v1/payments/admin/orders/order_nope/redeliver", "", testAdminTok); rec.Code != 404 {
		t.Fatalf("重发未知 = %d", rec.Code)
	}
	cur := new(string)
	*cur = "acc_1"
	env, _ := createOrder(t, p, cur, "sku_gem_60")
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(env.Data, &created); err != nil || created.ID == "" {
		t.Fatalf("下单解析: %v data=%s", err, env.Data)
	}
	if rec := adminCall(t, p, http.MethodPost, "/v1/payments/admin/orders/"+created.ID+"/redeliver", "", testAdminTok); rec.Code != 409 {
		t.Fatalf("重发 CREATED = %d, want 409", rec.Code)
	}
}

func TestAdminCursorRoundtrip(t *testing.T) {
	ts := time.Date(2026, 10, 9, 12, 0, 30, 0, time.UTC)
	cur := encodeAdminCursor(ts, "order_abc")
	got, id, ok := decodeAdminCursor(cur)
	if !ok || got != ts || id != "order_abc" {
		t.Fatalf("roundtrip = %v %q ok=%v", got, id, ok)
	}
	if _, _, ok := decodeAdminCursor(""); ok {
		t.Fatal("空游标应无效")
	}
	if _, _, ok := decodeAdminCursor("not-base64!!!"); ok {
		t.Fatal("坏游标应无效")
	}
	if _, _, ok := decodeAdminCursor(base64.RawURLEncoding.EncodeToString([]byte("no-pipe"))); ok {
		t.Fatal("无分隔游标应无效")
	}
}

// TestAdminStateSurvivesRestart v1.2 新增字段随 F77 全量快照跨重启保留:
// GameID scope 快照 + 退款登记事实;旧快照(无新字段)回放缺省空,不误造数据。
func TestAdminStateSurvivesRestart(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "orders.json")
	p1 := fixedStateHarness(t, stateFile)
	p1.adminToken = testAdminTok
	rec := call(t, p1, http.MethodPost, "/v1/payments/orders", `{"skuId":"sku_gem_60"}`,
		map[string]string{"X-Courier-Game-Id": "game_42"})
	if rec.Code != http.StatusOK {
		t.Fatalf("下单 = %d %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || env.Data.ID == "" {
		t.Fatalf("下单解析: %v %s", err, rec.Body.String())
	}
	oid := env.Data.ID
	body := `{"orderId":"` + oid + `","paidAt":"2026-10-09T12:00:30Z"}`
	if rec := call(t, p1, http.MethodPost, "/v1/payments/callback", body,
		map[string]string{"X-Payment-Signature": sign("s3cret", []byte(body))}); rec.Code != http.StatusOK {
		t.Fatalf("回调 = %d %s", rec.Code, rec.Body.String())
	}
	if rec := adminCall(t, p1, http.MethodPost, "/v1/payments/admin/orders/"+oid+"/refund",
		`{"action":"REFUND","amountCents":200}`, testAdminTok); rec.Code != http.StatusOK {
		t.Fatalf("退款登记 = %d %s", rec.Code, rec.Body.String())
	}

	p2 := fixedStateHarness(t, stateFile)
	p2.adminToken = testAdminTok
	rec = adminCall(t, p2, http.MethodGet, "/v1/payments/admin/orders/"+oid, "", testAdminTok)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"gameId":"game_42"`) ||
		!strings.Contains(rec.Body.String(), `"action":"REFUND"`) {
		t.Fatalf("重启后管理详情 = %d %s", rec.Code, rec.Body.String())
	}
	rec = adminCall(t, p2, http.MethodGet, "/v1/payments/admin/orders?gameId=game_42", "", testAdminTok)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), oid) {
		t.Fatalf("重启后 gameId 过滤 = %d %s", rec.Code, rec.Body.String())
	}

	oldFile := filepath.Join(t.TempDir(), "old.json")
	legacy := `{"orders":{"order_legacy":{"ID":"order_legacy","Owner":"acc_1","Token":"x",` +
		`"Sku":{"ID":"sku_gem_60","ProductID":"p","Form":"DIRECT_PURCHASE","AmountCents":600,"Currency":"CNY"},` +
		`"Events":[{"Type":"CREATED","At":"2026-10-09T12:00:00Z"}]}}}`
	if err := os.WriteFile(oldFile, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	p3 := fixedStateHarness(t, oldFile)
	p3.adminToken = testAdminTok
	rec = adminCall(t, p3, http.MethodGet, "/v1/payments/admin/orders/order_legacy", "", testAdminTok)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"gameId":"game_42"`) ||
		strings.Contains(rec.Body.String(), `"refunds"`) {
		t.Fatalf("旧快照回放 = %d %s", rec.Code, rec.Body.String())
	}
}
