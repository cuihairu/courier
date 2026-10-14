// scribealt 单元测试:wire 面与 scribe 同形状(条件投影、灰度稳定性、
// 发布校验、版本门槛、维护/环境/品牌端点)+ 条件求值异构面(写时维度索引:
// 读时按请求维度分阶段过滤,与 scribe 逐条目 AND 短路逐条等价)。
package scribealt

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

func withAccount(accountID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(),
				auth.Identity{AccountID: accountID, SessionID: "ses_1"})))
		})
	}
}

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

// TestConfigProjectionConditions 条件投影(与 scribe 同源断言):缺省维度
// 不参与过滤;只带部分维度;全维度提供。
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

	// 缺省维度不参与过滤:不带任何参数 → 全量。
	if n := len(get("")); n != 3 {
		t.Fatalf("no-dim items = %d, want 3(缺省维度不参与过滤)", n)
	}
	// 只带 platform=ios:ios_only 命中;cn_new 的 region/appVersion 缺省被跳过。
	items := get("?platform=ios")
	if len(items) != 3 || items["ios_only"] != "v1" {
		t.Fatalf("ios items = %v", items)
	}
	// platform=android:ios_only 被滤掉,cn_new 条件跳过。
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

// TestConfigProjectionCrossDimSubset 跨维度子集语义:条目条件维度与请求
// 提供维度的交集决定过滤——条目声明了请求未提供的维度时该条件跳过;
// 请求提供了条目未声明的维度时该维度不过滤。
func TestConfigProjectionCrossDimSubset(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	if _, err := p.Publish([]Entry{
		// 三维度条件:缺任一维度不参与过滤,三维度全提供才全检。
		{Key: "full", Value: "x", Conditions: []Condition{
			{Dimension: DimPlatform, Values: []string{"ios"}},
			{Dimension: DimRegion, Values: []string{"cn"}},
			{Dimension: DimAppVersion, Values: []string{"3.0.0"}},
		}},
		// 单维度条件:该维度缺省时跳过,请求提供不匹配维度时过滤。
		{Key: "plat", Value: "y", Conditions: []Condition{
			{Dimension: DimPlatform, Values: []string{"android"}},
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
	has := func(m map[string]any, k string) bool { _, ok := m[k]; return ok }
	// 无维度:两个条目的条件全跳过 → 都在。
	if !has(get(""), "full") || !has(get(""), "plat") {
		t.Fatalf("no-dim = %v", get(""))
	}
	// platform=ios:full 的 platform 条件满足(其余跳过);plat 的 platform 不匹配 → 滤。
	if !has(get("?platform=ios"), "full") || has(get("?platform=ios"), "plat") {
		t.Fatalf("ios = %v", get("?platform=ios"))
	}
	// platform=android:full 的 platform 不匹配 → 滤;plat 满足。
	if has(get("?platform=android"), "full") || !has(get("?platform=android"), "plat") {
		t.Fatalf("android = %v", get("?platform=android"))
	}
	// 三维度全提供匹配 → 都在;region=us → full 滤、plat 仍满足(platform 未提供跳过)。
	if !has(get("?platform=ios&region=cn&appVersion=3.0.0"), "full") {
		t.Fatalf("full-match = %v", get("?platform=ios&region=cn&appVersion=3.0.0"))
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

	// 放量单调:100% 全量;缺省 0 = 全量;「不下发」= 不发布该键。
	if _, err := p.Publish([]Entry{{Key: "gray", Value: true, Percent: 100}}); err != nil {
		t.Fatalf("publish 100: %v", err)
	}
	for _, a := range accounts {
		if _, ok := fetch(a)["gray"]; !ok {
			t.Fatalf("100%% 应全量,账号 %s 缺失", a)
		}
	}
	if _, err := p.Publish([]Entry{{Key: "gray", Value: true}}); err != nil {
		t.Fatalf("publish default: %v", err)
	}
	for _, a := range accounts {
		if _, ok := fetch(a)["gray"]; !ok {
			t.Fatalf("缺省 Percent 应全量,账号 %s 缺失", a)
		}
	}
	if _, err := p.Publish(nil); err != nil {
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

// TestWriteTimeDimIndex 异构面:Publish 时把条件切片转为维度→允许值集合
// 索引;读时迭代请求维度查表,而非迭代条目条件切片。
func TestWriteTimeDimIndex(t *testing.T) {
	p := New(Options{RequireAuth: withAccount("acc_1")})
	if _, err := p.Publish([]Entry{
		{Key: "a", Value: 1, Conditions: []Condition{
			{Dimension: DimPlatform, Values: []string{"ios", "mac"}},
			{Dimension: DimRegion, Values: []string{"cn"}},
		}},
		{Key: "b", Value: 2}, // 无条件
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	p.mu.RLock()
	e0, e1 := p.items[0], p.items[1]
	p.mu.RUnlock()
	// 写时索引:条件切片已转为 map,维度与允许值集合一一对应。
	if len(e0.byDim) != 2 || len(e0.byDim[DimPlatform]) != 2 ||
		len(e0.byDim[DimRegion]) != 1 {
		t.Fatalf("byDim = %v", e0.byDim)
	}
	if !contains(e0.byDim[DimPlatform], "ios") || !contains(e0.byDim[DimRegion], "cn") {
		t.Fatalf("byDim 允许值集合错误: %v", e0.byDim)
	}
	if e1.byDim != nil {
		t.Fatalf("无条件条目 byDim 应为 nil: %v", e1.byDim)
	}
	// 读时分阶段过滤:请求维度缺省时跳过,提供时查表。
	get := func(query string) map[string]any {
		rec := call(t, p, http.MethodGet, "/v1/app/config", query)
		var got struct {
			Items map[string]any `json:"items"`
		}
		decodeData(t, rec, &got)
		return got.Items
	}
	if _, ok := get("?platform=mac")["a"]; !ok {
		t.Fatalf("platform=mac 应命中条目 a: %v", get("?platform=mac"))
	}
	if _, ok := get("?platform=android&region=cn")["a"]; ok {
		t.Fatalf("platform=android 不应命中条目 a: %v", get("?platform=android&region=cn"))
	}
}

// TestDimMatchEquivalence 等价性:按维度分阶段过滤与 scribe 逐条目 AND
// 短路对同一(条目条件,请求维度)对给出同一保留/丢弃结果。
func TestDimMatchEquivalence(t *testing.T) {
	// 条目条件:platform=ios AND region=cn。
	byDim := map[Dimension][]string{
		DimPlatform: {"ios"},
		DimRegion:   {"cn"},
	}
	// scribe.matchEntry 语义(逐条目条件,跳过未提供维度)。
	scribeMatch := func(dims map[Dimension]string) bool {
		conditions := []Condition{
			{Dimension: DimPlatform, Values: []string{"ios"}},
			{Dimension: DimRegion, Values: []string{"cn"}},
		}
		for _, c := range conditions {
			v, provided := dims[c.Dimension]
			if !provided {
				continue
			}
			if !contains(c.Values, v) {
				return false
			}
		}
		return true
	}
	cases := []struct {
		dims map[Dimension]string
	}{
		{nil}, // 无维度:全跳过
		{map[Dimension]string{DimPlatform: "ios"}},     // 只 platform 匹配
		{map[Dimension]string{DimPlatform: "android"}}, // platform 不匹配
		{map[Dimension]string{DimRegion: "cn"}},        // 只 region 匹配
		{map[Dimension]string{DimRegion: "us"}},        // region 不匹配
		{map[Dimension]string{DimPlatform: "ios", DimRegion: "cn"}},
		{map[Dimension]string{DimPlatform: "ios", DimRegion: "us"}},
		{map[Dimension]string{DimAppVersion: "1.0.0"}}, // 条目未声明的维度
	}
	for _, c := range cases {
		if got, want := dimsMatch(byDim, c.dims), scribeMatch(c.dims); got != want {
			t.Fatalf("dims = %v: dimsMatch=%v scribeMatch=%v", c.dims, got, want)
		}
	}
}

func TestConfigFailClosedWithoutAuth(t *testing.T) {
	p := New(Options{})
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
		t.Fatalf("equal min: %v", m)
	}
	if _, m := get("?appVersion=2.0.1"); m["forceUpdate"] != false {
		t.Fatalf("above min: %v", m)
	}
	if _, m := get(""); m["forceUpdate"] != false {
		t.Fatalf("缺省不判定: %v", m)
	}
	if _, m := get("?appVersion=1"); m["forceUpdate"] != true {
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
	var m2 map[string]any
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

	rec := call(t, p, http.MethodGet, "/v1/app/branding", "")
	var m map[string]any
	decodeData(t, rec, &m)
	if m["version"] != float64(0) || len(m) != 1 {
		t.Fatalf("initial branding = %v", m)
	}

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

func TestConfigVersionAccessor(t *testing.T) {
	p := New(Options{})
	if v := p.ConfigVersion(); v != 0 {
		t.Fatalf("初始版本 = %d, want 0", v)
	}
	v1, err := p.Publish([]Entry{{Key: "k", Value: "v"}})
	if err != nil || v1 != 1 {
		t.Fatalf("Publish = %d err=%v", v1, err)
	}
	if v := p.ConfigVersion(); v != v1 {
		t.Fatalf("ConfigVersion = %d, want %d", v, v1)
	}
}

func TestPublishInvalidValueRendersError(t *testing.T) {
	p := New(Options{})
	_, err := p.Publish([]Entry{{Key: "k", Value: make(chan int)}})
	if err == nil || err.Error() == "" {
		t.Fatalf("非法 JSON 值应报可读错误, got %v", err)
	}
	if _, err := p.Publish([]Entry{{Key: "k", Value: strings.Repeat("x", 33*1024)}}); err == nil {
		t.Fatal("超 32KiB 应拒")
	}
}
