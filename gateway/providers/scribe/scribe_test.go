// scribe 单元测试:条件投影、灰度稳定性、发布校验、版本门槛、维护/环境端点。
package scribe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

// withAccount 测试用认证中间件:注入固定身份(替代 auth.RequireAuth)。
func withAccount(accountID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
				auth.Identity{AccountID: accountID, SessionID: "ses_1"})))
		})
	}
}

// call 直打 Provider(不经网关链;身份经注入的认证中间件带上)。
func call(t *testing.T, p *AppProvider, method, path, query string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(method, path+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		t.Fatalf("decode data: %v body=%s", err, rec.Body.String())
	}
}

func TestCapabilityAndName(t *testing.T) {
	p := New(Options{})
	if p.Name() != DefaultName || p.Capability() != providers.CapApp {
		t.Fatalf("name/capability = %s/%v", p.Name(), p.Capability())
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestConfigEmptyBeforePublish(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	rec := call(t, p, http.MethodGet, "/v1/app/config", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		ConfigVersion int64          `json:"configVersion"`
		Items         map[string]any `json:"items"`
	}
	decodeData(t, rec, &got)
	if got.ConfigVersion != 0 {
		t.Fatalf("version = %d, want 0", got.ConfigVersion)
	}
	if got.Items == nil {
		t.Fatal("items = null, want {}(空集不是 null)")
	}
}

func TestConfigProjectionConditions(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	if _, err := p.Publish([]Entry{
		{Key: "shop_switch", Value: true},
		{Key: "ios_only", Value: "v1", Conditions: []Condition{{Dimension: DimPlatform, Values: []string{"ios"}}}},
		{Key: "cn_new", Value: 3, Conditions: []Condition{
			{Dimension: DimRegion, Values: []string{"cn"}},
			{Dimension: DimAppVersion, Values: []string{"2.0.0"}},
		}},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	get := func(query string) map[string]any {
		rec := call(t, p, http.MethodGet, "/v1/app/config", query)
		var got struct {
			Items map[string]any `json:"items"`
		}
		decodeData(t, rec, &got)
		return got.Items
	}

	// 缺省维度不参与过滤:不带任何参数 → 全量(所有条件被跳过)。
	if n := len(get("")); n != 3 {
		t.Fatalf("no-dim items = %d, want 3(缺省维度不参与过滤)", n)
	}
	// 只带 platform=ios:ios_only 命中;cn_new 的 region/appVersion 缺省被跳过 → 仍 3 键。
	items := get("?platform=ios")
	if len(items) != 3 || items["ios_only"] != "v1" {
		t.Fatalf("ios items = %v", items)
	}
	// platform=android:ios_only 被滤掉,cn_new 条件跳过 → 2 键。
	items = get("?platform=android")
	if len(items) != 2 || items["shop_switch"] != true {
		t.Fatalf("android items = %v", items)
	}
	// 全维度提供:cn+2.0.0 命中 cn_new(us 不命中);ios_only 条件跳过仍命中。
	items = get("?region=cn&appVersion=2.0.0")
	if len(items) != 3 || items["cn_new"] != float64(3) {
		t.Fatalf("cn items = %v", items)
	}
	items = get("?platform=ios&region=us&appVersion=2.0.0")
	if len(items) != 2 {
		t.Fatalf("ios+us items = %v(ios_only 在、cn_new 出)", items)
	}
	if _, has := items["cn_new"]; has {
		t.Fatalf("us 不应命中 cn_new: %v", items)
	}
}

func TestVersionMonotonicAndRollback(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	v1, err := p.Publish([]Entry{{Key: "k", Value: "a"}})
	if err != nil || v1 != 1 {
		t.Fatalf("publish v1 = %d err=%v", v1, err)
	}
	v2, err := p.Publish([]Entry{{Key: "k", Value: "b"}})
	if err != nil || v2 != 2 {
		t.Fatalf("publish v2 = %d err=%v", v2, err)
	}
	// 回滚 = 历史内容再发布 → 新版本号,不回退。
	v3, err := p.Publish([]Entry{{Key: "k", Value: "a"}})
	if err != nil || v3 != 3 {
		t.Fatalf("rollback publish = %d err=%v", v3, err)
	}
	rec := call(t, p, http.MethodGet, "/v1/app/config", "")
	var got struct {
		ConfigVersion int64          `json:"configVersion"`
		Items         map[string]any `json:"items"`
	}
	decodeData(t, rec, &got)
	if got.ConfigVersion != 3 || got.Items["k"] != "a" {
		t.Fatalf("after rollback = %v", got)
	}
}

func TestGrayBucketStableAndMonotonic(t *testing.T) {
	// 身份经闭包注入:同一 Provider 换账号重拉,不复制带锁结构。
	cur := "acc_default"
	p := New(Options{RequireAuth: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
				auth.Identity{AccountID: cur, SessionID: "ses_1"})))
		})
	}})
	fetch := func(accountID string) map[string]any {
		cur = accountID
		rec := call(t, p, http.MethodGet, "/v1/app/config", "")
		var got struct {
			Items map[string]any `json:"items"`
		}
		decodeData(t, rec, &got)
		return got.Items
	}
	accounts := make([]string, 100)
	for i := range accounts {
		accounts[i] = "acc_gray_" + strconv.Itoa(i)
	}

	if _, err := p.Publish([]Entry{{Key: "gray", Value: true, Percent: 50}}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	first := make(map[string]bool, len(accounts))
	var in, out int
	for _, a := range accounts {
		_, hit := fetch(a)["gray"]
		first[a] = hit
		if hit {
			in++
		} else {
			out++
		}
	}
	if in == 0 || out == 0 {
		t.Fatalf("50%% 分桶应两侧皆非空:in=%d out=%d", in, out)
	}

	// 稳定性:同账号同键再发布(内容不变),命中集不抖动。
	if _, err := p.Publish([]Entry{{Key: "gray", Value: true, Percent: 50}}); err != nil {
		t.Fatalf("republish: %v", err)
	}
	for _, a := range accounts {
		if _, hit := fetch(a)["gray"]; hit != first[a] {
			t.Fatalf("账号 %s 灰度命中态不稳定:before=%v after=%v", a, first[a], hit)
		}
	}

	// 放量单调:100% 全量;缺省 0 = 全量(零值安全);「不下发」= 不发布该键。
	if _, err := p.Publish([]Entry{{Key: "gray", Value: true, Percent: 100}}); err != nil {
		t.Fatalf("publish 100: %v", err)
	}
	for _, a := range accounts {
		if _, ok := fetch(a)["gray"]; !ok {
			t.Fatalf("100%% 应全量,账号 %s 缺失", a)
		}
	}
	if _, err := p.Publish([]Entry{{Key: "gray", Value: true}}); err != nil { // 缺省 Percent
		t.Fatalf("publish default: %v", err)
	}
	for _, a := range accounts {
		if _, ok := fetch(a)["gray"]; !ok {
			t.Fatalf("缺省 Percent 应全量,账号 %s 缺失", a)
		}
	}
	if _, err := p.Publish(nil); err != nil { // 移除键 = 不下发
		t.Fatalf("publish empty: %v", err)
	}
	for _, a := range accounts {
		if _, ok := fetch(a)["gray"]; ok {
			t.Fatalf("移除键后应全空,账号 %s 命中", a)
		}
	}
}

func TestPublishValidation(t *testing.T) {
	p := New(Options{})
	bad := func(name string, entries []Entry) {
		t.Helper()
		if _, err := p.Publish(entries); err == nil {
			t.Fatalf("%s: 期望报错", name)
		}
	}
	bad("非法键字符", []Entry{{Key: "bad key!", Value: 1}})
	bad("空键", []Entry{{Key: "", Value: 1}})
	bad("超长键", []Entry{{Key: strings.Repeat("k", 129), Value: 1}})
	bad("重复键", []Entry{{Key: "a", Value: 1}, {Key: "a", Value: 2}})
	bad("percent 越界", []Entry{{Key: "a", Value: 1, Percent: 101}})
	bad("未知维度", []Entry{{Key: "a", Value: 1, Conditions: []Condition{{Dimension: "deviceModel", Values: []string{"x"}}}}})
	bad("条件缺 Values", []Entry{{Key: "a", Value: 1, Conditions: []Condition{{Dimension: DimPlatform}}}})
	many := make([]Entry, maxKeys+1)
	for i := range many {
		many[i] = Entry{Key: "k" + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676%26)), Value: i}
	}
	bad("超 200 键", many)
	bad("超 32KiB 值", []Entry{{Key: "big", Value: strings.Repeat("x", maxValueBytes+1)}})
}

func TestConfigFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{}) // 未注入 RequireAuth:fail-closed
	rec := call(t, p, http.MethodGet, "/v1/app/config", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401(fail-closed)", rec.Code)
	}
}

func TestVersionEndpoint(t *testing.T) {
	p := New(Options{})
	p.SetVersion(VersionInfo{LatestVersion: "2.1.0", MinVersion: "2.0.0", UpdateURL: "https://example.com/dl"})

	get := func(query string) (int, map[string]any) {
		rec := call(t, p, http.MethodGet, "/v1/app/version", query)
		var m map[string]any
		if rec.Code == http.StatusOK {
			decodeData(t, rec, &m)
		}
		return rec.Code, m
	}
	code, m := get("?appVersion=1.9.9")
	if code != 200 || m["forceUpdate"] != true || m["latestVersion"] != "2.1.0" || m["updateUrl"] != "https://example.com/dl" {
		t.Fatalf("below min: code=%d m=%v", code, m)
	}
	if _, m := get("?appVersion=2.0.0"); m["forceUpdate"] != false {
		t.Fatalf("equal min: %v", m) // 等于 minVersion 可用
	}
	if _, m := get("?appVersion=2.0.1"); m["forceUpdate"] != false {
		t.Fatalf("above min: %v", m)
	}
	if _, m := get(""); m["forceUpdate"] != false {
		t.Fatalf("缺省不判定: %v", m)
	}
	if _, m := get("?appVersion=1"); m["forceUpdate"] != true { // 1 < 2.0.0(段数补齐比较)
		t.Fatalf("short version: %v", m)
	}
	if code, _ := get("?appVersion=2.0.x"); code != http.StatusBadRequest {
		t.Fatalf("malformed: code=%d, want 400", code)
	}
}

func TestMaintenanceEndpointAndGate(t *testing.T) {
	p := New(Options{})
	rec := call(t, p, http.MethodGet, "/v1/app/maintenance", "")
	var m map[string]any
	decodeData(t, rec, &m)
	if m["inMaintenance"] != false {
		t.Fatalf("initial: %v", m)
	}
	if p.InMaintenance() {
		t.Fatal("gate 初始应关")
	}

	recAt := time.Date(2026, 10, 10, 2, 0, 0, 0, time.UTC)
	p.SetMaintenance(MaintenanceState{InMaintenance: true, EstimatedRecoveryAt: &recAt, Message: "升级维护"})
	if !p.InMaintenance() {
		t.Fatal("gate 应开")
	}
	rec = call(t, p, http.MethodGet, "/v1/app/maintenance", "")
	decodeData(t, rec, &m)
	if m["inMaintenance"] != true || m["message"] != "升级维护" {
		t.Fatalf("maintenance dto: %v", m)
	}
	if s, _ := m["estimatedRecoveryAt"].(string); !strings.HasPrefix(s, "2026-10-10T02:00:00.000") {
		t.Fatalf("recoveryAt = %v", m["estimatedRecoveryAt"])
	}

	p.SetMaintenance(MaintenanceState{})
	rec = call(t, p, http.MethodGet, "/v1/app/maintenance", "")
	var m2 map[string]any // 新 map:防上次 decode 的键残影(json.Unmarshal 合并语义)
	decodeData(t, rec, &m2)
	if m2["inMaintenance"] != false {
		t.Fatalf("recovered: %v", m2)
	}
	if _, has := m2["message"]; has {
		t.Fatalf("message 应省略: %v", m2)
	}
	if _, has := m2["estimatedRecoveryAt"]; has {
		t.Fatalf("estimatedRecoveryAt 应省略: %v", m2)
	}
}

func TestEnvironmentEcho(t *testing.T) {
	p := New(Options{})
	req, _ := http.NewRequest(http.MethodGet, "/v1/app/environment", nil)
	req = req.WithContext(scope.WithContext(req.Context(), scope.Scope{GameID: "game_demo", Env: "prod"}))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	var m map[string]any
	decodeData(t, rec, &m)
	if m["gameId"] != "game_demo" || m["env"] != "prod" {
		t.Fatalf("environment = %v", m)
	}

	// 无 scope(未经网关链直打)→ 400 而非裸 panic。
	req2, _ := http.NewRequest(http.MethodGet, "/v1/app/environment", nil)
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("no scope: code=%d, want 400", rec2.Code)
	}
}

func TestNotifyBroadcast(t *testing.T) {
	var gotEvent string
	var gotData map[string]any
	p := New(Options{RequireAuth: withAccount("acc_1"),
		Notify: func(eventType string, data any) {
			gotEvent = eventType
			b, _ := json.Marshal(data)
			_ = json.Unmarshal(b, &gotData)
		}})
	v, err := p.Publish([]Entry{{Key: "k", Value: 1}})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if gotEvent != EventConfigUpdated {
		t.Fatalf("event = %s", gotEvent)
	}
	if gotData["configVersion"] != float64(v) || len(gotData) != 1 {
		t.Fatalf("data = %v(只带 version)", gotData)
	}
}

func TestRoutingShape(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	if rec := call(t, p, http.MethodPost, "/v1/app/config", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("POST config: %d", rec.Code)
	}
	if rec := call(t, p, http.MethodGet, "/v1/app/telemetry", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path: %d", rec.Code)
	}
}

func TestBrandingEndpoint(t *testing.T) {
	var events []string
	var datas []map[string]any
	p := New(Options{Notify: func(eventType string, data any) {
		events = append(events, eventType)
		b, _ := json.Marshal(data)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		datas = append(datas, m)
	}})

	// 未设置:{"version":0},空字段非错误。
	rec := call(t, p, http.MethodGet, "/v1/app/branding", "")
	var m map[string]any
	decodeData(t, rec, &m)
	if m["version"] != float64(0) || len(m) != 1 {
		t.Fatalf("initial branding = %v", m)
	}

	// 设置 → version 1 + 业务字段透传 + 事件广播。
	v, err := p.SetBranding([]byte(
		`{"companyName":"示例互娱","theme":{"primaryColor":"#4C8DFF"}}`))
	if err != nil || v != 1 {
		t.Fatalf("set branding: v=%d err=%v", v, err)
	}
	rec = call(t, p, http.MethodGet, "/v1/app/branding", "")
	var m2 map[string]any
	decodeData(t, rec, &m2)
	if m2["version"] != float64(1) || m2["companyName"] != "示例互娱" {
		t.Fatalf("branding = %v", m2)
	}
	theme, ok := m2["theme"].(map[string]any)
	if !ok || theme["primaryColor"] != "#4C8DFF" {
		t.Fatalf("theme = %v(嵌套对象透传)", m2["theme"])
	}

	// 再设置 → version 单调递增。
	if v, err := p.SetBranding([]byte(`{"companyName":"新名字"}`)); err != nil || v != 2 {
		t.Fatalf("reset branding: v=%d err=%v", v, err)
	}

	if len(events) != 2 || events[0] != EventBrandingUpdated || events[1] != EventBrandingUpdated {
		t.Fatalf("events = %v", events)
	}
	if len(datas) != 2 || datas[1]["version"] != float64(2) || len(datas[1]) != 1 {
		t.Fatalf("event data = %v(只带 version)", datas)
	}
}

func TestSetBrandingValidation(t *testing.T) {
	p := New(Options{})
	bad := func(name string, data []byte) {
		t.Helper()
		if _, err := p.SetBranding(data); err == nil {
			t.Fatalf("%s: 期望报错", name)
		}
	}
	bad("非 JSON", []byte(`{oops`))
	bad("数组", []byte(`[1,2]`))
	bad("null", []byte(`null`))
	bad("占用 version 键", []byte(`{"version":9,"companyName":"x"}`))
}
