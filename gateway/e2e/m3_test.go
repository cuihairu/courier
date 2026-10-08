// 批次 8(M3)网关级验收:改远程配置 → 命中条件的客户端热生效(config.updated
// 5s 内广播 → 重拉生效)、未命中客户端内容不受影响;维护开启 → 新会话一律
// 503 APP_MAINTENANCE,/v1/app/* 与 /healthz 照常,已建立的推送流不断。
// 装配与 cmd/gateway/main.go 同构(account + chirp + scribe,维护门包入口)。
package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/scribe"
	"github.com/cuihairu/courier/gateway/routing"
)

// m3Harness M3 装配:scribe(config/app)+ chirp 推送 + 维护门。
type m3Harness struct {
	srv    *httptest.Server
	scribe *scribe.AppProvider
}

func newM3Harness(t *testing.T) *m3Harness {
	t.Helper()
	reg := providers.NewRegistry()
	acc := account.New(account.Options{Iterations: 1000})
	reqAuth := auth.RequireAuth(acc.Verifier())
	chirpP := chirp.New(chirp.Options{RequireAuth: reqAuth})
	scribeP := scribe.New(scribe.Options{RequireAuth: reqAuth, Notify: chirpP.Hub().Publish})
	for _, h := range []providers.Handler{acc, chirpP, scribeP} {
		if err := reg.Register(h); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	cfg := providers.Config{
		providers.CapIdentity: {Primary: account.DefaultName},
		providers.CapMessages: {Primary: chirp.DefaultName},
		providers.CapApp:      {Primary: scribe.DefaultName},
	}
	chain := middleware.Chain(middleware.NewRateLimiter(120, 60))
	handler := middleware.Maintenance(scribeP.InMaintenance)(routing.New(reg, cfg, chain))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &m3Harness{srv: srv, scribe: scribeP}
}

// login 游客登录取 token(deviceId 区分客户端)。
func (h *m3Harness) login(t *testing.T, deviceId string) string {
	t.Helper()
	body := `{"deviceId":"` + deviceId + `","platform":"linux"}`
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(body))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("guest login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("guest login: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Data.AccessToken == "" {
		t.Fatalf("login decode: err=%v", err)
	}
	return out.Data.AccessToken
}

func (h *m3Harness) scopeHeaders(r *http.Request) {
	r.Header.Set("X-Courier-Game-Id", "game_demo")
	r.Header.Set("X-Courier-Env", "prod")
}

// openM3Stream 开 SSE 流(Bearer + scope),返回事件通道(解析同 m2)。
func (h *m3Harness) openM3Stream(t *testing.T, token string) (<-chan sseEvent, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		t.Fatalf("open stream: status=%d body=%s", resp.StatusCode, body)
	}
	events := make(chan sseEvent, 16)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var cur sseEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case line == "":
				if cur.Type != "" {
					events <- cur
				}
				cur = sseEvent{}
			case strings.HasPrefix(line, "event: "):
				cur.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.Data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return events, cancel
}

// getConfig 拉配置投影。
func (h *m3Harness) getConfig(t *testing.T, token, query string) (int64, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/app/config"+query, nil)
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("get config: status=%d body=%s", resp.StatusCode, b)
	}
	var out struct {
		Data struct {
			ConfigVersion int64          `json:"configVersion"`
			Items         map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return out.Data.ConfigVersion, out.Data.Items
}

// getJSON 匿名 GET(维护/版本/环境端点),返回状态码与解析后的 data。
func (h *m3Harness) getJSON(t *testing.T, path string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+path, nil)
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || out == nil {
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return resp.StatusCode
}

// errorCode 读失败信封的 error.code。
func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return env.Error.Code
}

// TestM3ConfigHotEffect 验收:改远程配置 → 命中客户端热生效、未命中不受影响。
func TestM3ConfigHotEffect(t *testing.T) {
	h := newM3Harness(t)
	token := h.login(t, "e2e-ios")

	// v1:无门槛键 + iOS 条件键。
	if _, err := h.scribe.Publish([]scribe.Entry{
		{Key: "shop_switch", Value: false},
		{Key: "ios_banner", Value: "summer_a", Conditions: []scribe.Condition{
			{Dimension: scribe.DimPlatform, Values: []string{"ios"}},
		}},
	}); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	v, iosItems := h.getConfig(t, token, "?platform=ios")
	if v != 1 || iosItems["shop_switch"] != false || iosItems["ios_banner"] != "summer_a" {
		t.Fatalf("ios v1 = %d %v", v, iosItems)
	}
	androidToken := h.login(t, "e2e-android")
	_, androidItems := h.getConfig(t, androidToken, "?platform=android")
	if _, has := androidItems["ios_banner"]; has {
		t.Fatalf("android 不应命中 ios_banner: %v", androidItems)
	}

	// v1 之后才开流:首个 config.updated 必属 v2(热生效验收不混入历史事件)。
	events, cancel := h.openM3Stream(t, token)
	defer cancel()

	// v2:只改 iOS 条件键的值。
	if _, err := h.scribe.Publish([]scribe.Entry{
		{Key: "shop_switch", Value: false},
		{Key: "ios_banner", Value: "summer_b", Conditions: []scribe.Condition{
			{Dimension: scribe.DimPlatform, Values: []string{"ios"}},
		}},
	}); err != nil {
		t.Fatalf("publish v2: %v", err)
	}

	// 热生效:config.updated 5s 内广播,只带 version。
	ev := awaitEvent(t, events, "config.updated")
	var data struct {
		ConfigVersion int64 `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &data); err != nil || data.ConfigVersion != 2 {
		t.Fatalf("config.updated data = %q", ev.Data)
	}

	// 命中客户端重拉热生效。
	v, iosItems = h.getConfig(t, token, "?platform=ios")
	if v != 2 || iosItems["ios_banner"] != "summer_b" {
		t.Fatalf("ios v2 = %d %v(应热生效)", v, iosItems)
	}
	// 未命中客户端:内容与 v1 完全一致(version 全局前进,items 不变)。
	v2, androidItems2 := h.getConfig(t, androidToken, "?platform=android")
	if v2 != 2 {
		t.Fatalf("android v2 = %d, want 2(全局 version 前进)", v2)
	}
	if len(androidItems2) != len(androidItems) || androidItems2["shop_switch"] != androidItems["shop_switch"] {
		t.Fatalf("android items 不应变: v1=%v v2=%v", androidItems, androidItems2)
	}
}

// TestM3MaintenanceBlocksNewSessions 验收:维护开启 → 新会话 503 APP_MAINTENANCE;
// 本域端点与探活照常;恢复后放行。
func TestM3MaintenanceBlocksNewSessions(t *testing.T) {
	h := newM3Harness(t)
	token := h.login(t, "e2e-m3") // 维护前登录(已建立会话)

	// 维护开启(带预计恢复时间)。
	recAt := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
	h.scribe.SetMaintenance(scribe.MaintenanceState{InMaintenance: true, EstimatedRecoveryAt: &recAt})

	// 新会话(登录)→ 503 APP_MAINTENANCE。
	req, _ := http.NewRequest(http.MethodPost, h.srv.URL+"/v1/identity/guest", strings.NewReader(`{"deviceId":"e2e-new"}`))
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("new session status = %d, want 503", resp.StatusCode)
	}
	if code := errorCode(t, resp); code != "APP_MAINTENANCE" {
		t.Fatalf("code = %s, want APP_MAINTENANCE", code)
	}
	resp.Body.Close()

	// 维护中本域端点与探活照常(否则客户端无从得知恢复)。
	var maint struct {
		Data struct {
			InMaintenance       bool    `json:"inMaintenance"`
			EstimatedRecoveryAt *string `json:"estimatedRecoveryAt"`
		} `json:"data"`
	}
	if code := h.getJSON(t, "/v1/app/maintenance", &maint); code != http.StatusOK || !maint.Data.InMaintenance {
		t.Fatalf("maintenance endpoint: code=%d data=%+v", code, maint.Data)
	}
	if maint.Data.EstimatedRecoveryAt == nil {
		t.Fatal("estimatedRecoveryAt 应下发")
	}
	if code := h.getJSON(t, "/healthz", nil); code != http.StatusOK {
		t.Fatalf("healthz: %d", code)
	}

	// 维护中:配置端点在本域白名单内,已认证客户端仍可拉。
	if v, _ := h.getConfig(t, token, ""); v != 0 {
		t.Fatalf("config in maintenance: v=%d(应可用)", v)
	}

	// 维护关闭 → 新会话恢复。
	h.scribe.SetMaintenance(scribe.MaintenanceState{})
	if tok := h.login(t, "e2e-new2"); tok == "" {
		t.Fatal("恢复后登录失败")
	}
}

// TestM3MaintenanceKeepsEstablishedStream 已建立的推送流不受维护影响(不强制踢出)。
func TestM3MaintenanceKeepsEstablishedStream(t *testing.T) {
	h := newM3Harness(t)
	token := h.login(t, "e2e-stream")
	events, cancel := h.openM3Stream(t, token)
	defer cancel()

	h.scribe.SetMaintenance(scribe.MaintenanceState{InMaintenance: true})

	// 已建立流上仍收到 config.updated(踢出会收不到)。
	if _, err := h.scribe.Publish([]scribe.Entry{{Key: "k", Value: 1}}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	ev := awaitEvent(t, events, "config.updated")
	if !strings.Contains(ev.Data, `"configVersion":1`) {
		t.Fatalf("event data = %q", ev.Data)
	}

	// 但新开流被维护门拦(503,非 401/200)。
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/messages/stream", nil)
	h.scopeHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("new stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || errorCode(t, resp) != "APP_MAINTENANCE" {
		t.Fatalf("new stream in maintenance: status=%d", resp.StatusCode)
	}
}

// TestM3AppEndpoints version/maintenance/environment 经网关全链。
func TestM3AppEndpoints(t *testing.T) {
	h := newM3Harness(t)
	h.scribe.SetVersion(scribe.VersionInfo{
		LatestVersion: "2.1.0", MinVersion: "2.0.0", UpdateURL: "https://example.com/dl",
	})
	var ver struct {
		Data struct {
			ForceUpdate   bool   `json:"forceUpdate"`
			LatestVersion string `json:"latestVersion"`
			UpdateURL     string `json:"updateUrl"`
		} `json:"data"`
	}
	if code := h.getJSON(t, "/v1/app/version?appVersion=1.5.0", &ver); code != 200 || !ver.Data.ForceUpdate {
		t.Fatalf("version: code=%d data=%+v", code, ver.Data)
	}
	if code := h.getJSON(t, "/v1/app/version", &ver); code != 200 || ver.Data.ForceUpdate {
		t.Fatalf("version 缺省不判定: code=%d data=%+v", code, ver.Data)
	}

	var env struct {
		Data struct {
			GameID string `json:"gameId"`
			Env    string `json:"env"`
		} `json:"data"`
	}
	if code := h.getJSON(t, "/v1/app/environment", &env); code != 200 || env.Data.GameID != "game_demo" || env.Data.Env != "prod" {
		t.Fatalf("environment: code=%d data=%+v", code, env.Data)
	}
}

// TestM3BrandingHotSwitch 验收:品牌配置变更 → branding.updated 广播 → 重拉热切换;
// 品牌端点匿名可达(登录页就要显示品牌)。
func TestM3BrandingHotSwitch(t *testing.T) {
	h := newM3Harness(t)
	token := h.login(t, "e2e-brand")
	events, cancel := h.openM3Stream(t, token)
	defer cancel()

	// 匿名(无 Bearer)可达,初始为空物料 + version 0。
	req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/app/branding", nil)
	h.scopeHeaders(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("anonymous branding: %v", err)
	}
	var initial struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initial); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if initial.Data["version"] != float64(0) {
		t.Fatalf("initial branding = %v", initial.Data)
	}

	// 管理面变更 → 事件 + 重拉热切换。
	if _, err := h.scribe.SetBranding([]byte(
		`{"companyName":"示例互娱","theme":{"primaryColor":"#4C8DFF"}}`)); err != nil {
		t.Fatalf("set branding: %v", err)
	}
	ev := awaitEvent(t, events, "branding.updated")
	if !strings.Contains(ev.Data, `"version":1`) {
		t.Fatalf("event data = %q", ev.Data)
	}

	req2, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v1/app/branding", nil)
	h.scopeHeaders(req2)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("refetch branding: %v", err)
	}
	defer resp2.Body.Close()
	var out struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Data["version"] != float64(1) || out.Data["companyName"] != "示例互娱" {
		t.Fatalf("updated branding = %v", out.Data)
	}
	theme, ok := out.Data["theme"].(map[string]any)
	if !ok || theme["primaryColor"] != "#4C8DFF" {
		t.Fatalf("theme = %v", out.Data["theme"])
	}
}
