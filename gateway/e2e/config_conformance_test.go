// 批次 71 配置域第二实现一致性 e2e(批次 18/64/67/68/69/70 先例同构):
// 同一 wire 场景对 scribe(读时逐条目条件 AND 短路)与 scribe-alt(写时维度
// 索引 + 读时按请求维度分阶段过滤)各跑一遍、同一断言——「配置 Provider
// 可换」有行为证明而非仅接口冻结。两实现异构面不在本文件断言,由各自单测
// 承载(scribe 由 e2e/m3 承载 / providers/scribealt)。
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
	"github.com/cuihairu/courier/gateway/providers/scribe"
	"github.com/cuihairu/courier/gateway/providers/scribealt"
	"github.com/cuihairu/courier/gateway/routing"
)

// configHarness 装配 account + 一个配置实现(经路由,与 main 同构)。
type configHarness struct {
	srv    *httptest.Server
	scribe *scribe.AppProvider
	scrAlt *scribealt.AppProvider
}

func newConfigHarness(t *testing.T, useAlt bool) *configHarness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())

	var aP providers.Handler
	var scribeP *scribe.AppProvider
	var scrAltP *scribealt.AppProvider
	if useAlt {
		scrAltP = scribealt.New(scribealt.Options{RequireAuth: reqAuth})
		aP = scrAltP
	} else {
		scribeP = scribe.New(scribe.Options{RequireAuth: reqAuth})
		aP = scribeP
	}
	if err := reg.Register(aP); err != nil {
		t.Fatalf("register app provider: %v", err)
	}
	if err := reg.Register(acc); err != nil {
		t.Fatalf("register acc: %v", err)
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapApp:      {Primary: aP.Name()},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	srv := httptest.NewServer(routing.New(reg, cfg, chain))
	t.Cleanup(srv.Close)
	return &configHarness{srv: srv, scribe: scribeP, scrAlt: scrAltP}
}

func (h *configHarness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

func (h *configHarness) login(t *testing.T) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest",
		strings.NewReader(`{"deviceId":"config-e2e","platform":"linux"}`))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: status=%d err=%v", resp.StatusCode, err)
	}
	return out.Data.AccessToken
}

// publish 管理面发布(两实现同签名,逐参数转换)。
func (h *configHarness) publish(t *testing.T, useAlt bool, entries []struct {
	Key     string
	Value   any
	Plat    []string
	Region  []string
	AppVer  []string
	Percent int
}) int64 {
	t.Helper()
	if useAlt {
		converted := make([]scribealt.Entry, len(entries))
		for i, e := range entries {
			var conds []scribealt.Condition
			if e.Plat != nil {
				conds = append(conds, scribealt.Condition{Dimension: scribealt.DimPlatform, Values: e.Plat})
			}
			if e.Region != nil {
				conds = append(conds, scribealt.Condition{Dimension: scribealt.DimRegion, Values: e.Region})
			}
			if e.AppVer != nil {
				conds = append(conds, scribealt.Condition{Dimension: scribealt.DimAppVersion, Values: e.AppVer})
			}
			converted[i] = scribealt.Entry{Key: e.Key, Value: e.Value, Conditions: conds, Percent: e.Percent}
		}
		v, err := h.scrAlt.Publish(converted)
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
		return v
	}
	converted := make([]scribe.Entry, len(entries))
	for i, e := range entries {
		var conds []scribe.Condition
		if e.Plat != nil {
			conds = append(conds, scribe.Condition{Dimension: scribe.DimPlatform, Values: e.Plat})
		}
		if e.Region != nil {
			conds = append(conds, scribe.Condition{Dimension: scribe.DimRegion, Values: e.Region})
		}
		if e.AppVer != nil {
			conds = append(conds, scribe.Condition{Dimension: scribe.DimAppVersion, Values: e.AppVer})
		}
		converted[i] = scribe.Entry{Key: e.Key, Value: e.Value, Conditions: conds, Percent: e.Percent}
	}
	v, err := h.scribe.Publish(converted)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return v
}

func (h *configHarness) get(t *testing.T, path, token string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+path, nil)
	h.scopeHeaders(req)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// runConfigContract 配置契约链:认证 → 路由形状 → 空集 → 条件投影 → 版本
// 单调/回滚 → 灰度稳定 → 版本门槛 → 维护 → 环境 → 品牌。
func runConfigContract(t *testing.T, h *configHarness, useAlt bool) {
	t.Helper()
	token := h.login(t)

	// 匿名 → 401 COMMON_UNAUTHENTICATED(契约 config.md)。
	if code, body := h.get(t, "/v1/app/config", ""); code != http.StatusUnauthorized ||
		!strings.Contains(body, "COMMON_UNAUTHENTICATED") {
		t.Fatalf("anon = %d %s, want 401 COMMON_UNAUTHENTICATED", code, body)
	}
	// 路由形状:POST config → 404;未知路径 → 404。
	if code, _ := h.get(t, "/v1/app/version", token); code != http.StatusOK {
		t.Fatalf("version = %d, want 200", code)
	}
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/app/config", strings.NewReader(""))
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST config: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST config = %d, want 404", resp.StatusCode)
	}
	if code, _ := h.get(t, "/v1/app/telemetry", token); code != http.StatusNotFound {
		t.Fatalf("unknown path = %d, want 404", code)
	}

	// 空配置集:200 + configVersion=0 + items {}(不是 404/不是 null)。
	code, body := h.get(t, "/v1/app/config", token)
	if code != http.StatusOK || !strings.Contains(body, `"configVersion":0`) || strings.Contains(body, `"items":null`) {
		t.Fatalf("empty config = %d %s", code, body)
	}

	// 条件投影(契约:缺省维度不参与过滤)。
	h.publish(t, useAlt, []struct {
		Key     string
		Value   any
		Plat    []string
		Region  []string
		AppVer  []string
		Percent int
	}{
		{Key: "shop_switch", Value: true},
		{Key: "ios_only", Value: "v1", Plat: []string{"ios"}},
		{Key: "cn_new", Value: 3, Region: []string{"cn"}, AppVer: []string{"2.0.0"}},
	})
	assertItems := func(query string, want ...string) {
		t.Helper()
		status, body := h.get(t, "/v1/app/config"+query, token)
		if status != http.StatusOK {
			t.Fatalf("config %s = %d %s", query, status, body)
		}
		var got struct {
			Data struct {
				ConfigVersion int64          `json:"configVersion"`
				Items         map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		if got.Data.ConfigVersion != 1 {
			t.Fatalf("version = %d, want 1", got.Data.ConfigVersion)
		}
		if len(got.Data.Items) != len(want) {
			t.Fatalf("config %s = %v, want %v", query, got.Data.Items, want)
		}
		for _, k := range want {
			if _, ok := got.Data.Items[k]; !ok {
				t.Fatalf("config %s 缺 %s: %v", query, k, got.Data.Items)
			}
		}
	}
	assertItems("", "shop_switch", "ios_only", "cn_new")              // 全缺省 → 全量
	assertItems("?platform=ios", "shop_switch", "ios_only", "cn_new") // ios 命中
	assertItems("?platform=android", "shop_switch", "cn_new")         // android 滤掉 ios_only
	assertItems("?region=cn&appVersion=2.0.0", "shop_switch", "ios_only", "cn_new")
	assertItems("?region=us&appVersion=2.0.0", "shop_switch", "ios_only") // us 不命中 cn_new

	// 版本单调 + 回滚(以历史内容再发布 → 新版本号,不回退)。
	v2 := h.publish(t, useAlt, []struct {
		Key     string
		Value   any
		Plat    []string
		Region  []string
		AppVer  []string
		Percent int
	}{{Key: "shop_switch", Value: false}})
	if v2 != 2 {
		t.Fatalf("v2 = %d, want 2", v2)
	}
	v3 := h.publish(t, useAlt, []struct {
		Key     string
		Value   any
		Plat    []string
		Region  []string
		AppVer  []string
		Percent int
	}{{Key: "shop_switch", Value: true}})
	if v3 != 3 {
		t.Fatalf("rollback v3 = %d, want 3", v3)
	}
	_, body = h.get(t, "/v1/app/config", token)
	if !strings.Contains(body, `"configVersion":3`) || !strings.Contains(body, `"shop_switch":true`) {
		t.Fatalf("after rollback = %s", body)
	}

	// 版本门槛:appVersion < min → forceUpdate;缺省不判定;非法 → 400。
	setVersion(t, h, useAlt, "2.1.0", "2.0.0", "https://example.com/dl")
	code, body = h.get(t, "/v1/app/version?appVersion=1.9.9", token)
	if code != 200 || !strings.Contains(body, `"forceUpdate":true`) {
		t.Fatalf("below min = %d %s", code, body)
	}
	_, body = h.get(t, "/v1/app/version?appVersion=2.0.0", token)
	if strings.Contains(body, `"forceUpdate":true`) {
		t.Fatalf("equal min: %s", body)
	}
	_, body = h.get(t, "/v1/app/version", token)
	if strings.Contains(body, `"forceUpdate":true`) {
		t.Fatalf("缺省不判定: %s", body)
	}
	if code, _ = h.get(t, "/v1/app/version?appVersion=2.0.x", token); code != http.StatusBadRequest {
		t.Fatalf("malformed = %d, want 400", code)
	}

	// 维护:初始关;开启后 InMaintenance + maintenance 端点回显;关闭恢复。
	setMaintenance(t, h, useAlt, true, "升级维护")
	code, body = h.get(t, "/v1/app/maintenance", "")
	if code != 200 || !strings.Contains(body, `"inMaintenance":true`) ||
		!strings.Contains(body, `"message":"升级维护"`) {
		t.Fatalf("maintenance = %d %s", code, body)
	}
	setMaintenance(t, h, useAlt, false, "")
	_, body = h.get(t, "/v1/app/maintenance", "")
	if strings.Contains(body, `"inMaintenance":true`) || strings.Contains(body, `"message"`) {
		t.Fatalf("recovered = %s", body)
	}

	// 环境回显:scope 中间件注入 → gameId/env 原样。
	code, body = h.get(t, "/v1/app/environment", token)
	if code != 200 || !strings.Contains(body, `"gameId":"game_demo"`) || !strings.Contains(body, `"env":"prod"`) {
		t.Fatalf("environment = %d %s", code, body)
	}

	// 品牌:初始 {"version":0};设置后业务字段透传 + version 单调;version 键占用拒绝。
	_, body = h.get(t, "/v1/app/branding", "")
	if !strings.Contains(body, `"version":0`) {
		t.Fatalf("initial branding = %s", body)
	}
	setBranding(t, h, useAlt, `{"companyName":"示例互娱"}`, 1)
	_, body = h.get(t, "/v1/app/branding", "")
	if !strings.Contains(body, `"version":1`) || !strings.Contains(body, `"companyName":"示例互娱"`) {
		t.Fatalf("branding = %s", body)
	}
	if code := setBrandingErr(t, h, useAlt, `{"version":9,"companyName":"x"}`); code == 0 {
		t.Fatal("version 键占用应被拒")
	}
}

// setVersion 管理面设置版本门槛(两实现同语义)。
func setVersion(t *testing.T, h *configHarness, useAlt bool, latest, min, updateURL string) {
	t.Helper()
	if useAlt {
		h.scrAlt.SetVersion(scribealt.VersionInfo{LatestVersion: latest, MinVersion: min, UpdateURL: updateURL})
		return
	}
	h.scribe.SetVersion(scribe.VersionInfo{LatestVersion: latest, MinVersion: min, UpdateURL: updateURL})
}

// setMaintenance 管理面设置维护开关(两实现同语义)。
func setMaintenance(t *testing.T, h *configHarness, useAlt bool, in bool, msg string) {
	t.Helper()
	if useAlt {
		h.scrAlt.SetMaintenance(scribealt.MaintenanceState{InMaintenance: in, Message: msg})
		return
	}
	h.scribe.SetMaintenance(scribe.MaintenanceState{InMaintenance: in, Message: msg})
}

// setBranding 管理面设置品牌(两实现同语义);返回分配的 version。
func setBranding(t *testing.T, h *configHarness, useAlt bool, data string, wantV int64) int64 {
	t.Helper()
	if useAlt {
		v, err := h.scrAlt.SetBranding([]byte(data))
		if err != nil || v != wantV {
			t.Fatalf("set branding: v=%d err=%v", v, err)
		}
		return v
	}
	v, err := h.scribe.SetBranding([]byte(data))
	if err != nil || v != wantV {
		t.Fatalf("set branding: v=%d err=%v", v, err)
	}
	return v
}

// setBrandingErr 管理面品牌校验失败应返回错误;成功返回 0。
func setBrandingErr(t *testing.T, h *configHarness, useAlt bool, data string) int {
	t.Helper()
	if useAlt {
		if _, err := h.scrAlt.SetBranding([]byte(data)); err != nil {
			return 1
		}
		return 0
	}
	if _, err := h.scribe.SetBranding([]byte(data)); err != nil {
		return 1
	}
	return 0
}

// TestConfigContractConformance 两实现 × 同一契约,同一断言。
func TestConfigContractConformance(t *testing.T) {
	for _, impl := range []struct {
		name   string
		useAlt bool
	}{
		{"scribe", false},
		{"scribe-alt", true},
	} {
		t.Run(impl.name, func(t *testing.T) {
			runConfigContract(t, newConfigHarness(t, impl.useAlt), impl.useAlt)
		})
	}
}
