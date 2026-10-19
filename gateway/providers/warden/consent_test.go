// consent 组验收(realname.md v1.1,F31;冻结清单步骤 3 网关侧):
// 默认关闭 501(能力关闭语义,不进报错路径);六态状态机与防回拨(仅趋严生效);
// VPC 请求 TTL 幂等(未过期同 requestId,过期换新);S2S 判定面(Provider 运维
// 通道,玩家凭证不可见,契约外);审计五事件(账号维度,出生日期永不进事件);
// SSE realname.consent.updated 仅状态迁移时推送;同意记录与身份数据分存储
// (未核验账号可独立走 consent 全流程)。
package warden

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/audit"
)

// 基准时钟(与 warden_test.go 同点):2026-10-08 周四 15:00。
var consentNow = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

// consentClock 可推进时钟(VPC TTL 过期/换新断言用)。
type consentClock struct {
	mu sync.Mutex
	t  time.Time
}

func newConsentClock(t time.Time) *consentClock { return &consentClock{t: t} }

func (c *consentClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }

func (c *consentClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// consentProbe 审计 + SSE 双桩:AuditSink 直通,notify 记 "type|state"。
type consentProbe struct {
	mu     sync.Mutex
	events []audit.AuditEvent
	sse    []string
}

func (p *consentProbe) Record(e audit.AuditEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
}

func (p *consentProbe) notify(eventType string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m, _ := data.(map[string]any)
	s, _ := m["state"].(string)
	p.sse = append(p.sse, eventType+"|"+s)
}

func (p *consentProbe) snapshot() ([]audit.AuditEvent, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]audit.AuditEvent(nil), p.events...), append([]string(nil), p.sse...)
}

type consentEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func cdo(t *testing.T, h http.Handler, req *http.Request) (int, consentEnvelope) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env consentEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env
}

func cpost(t *testing.T, h http.Handler, path, token, body string) (int, consentEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return cdo(t, h, req)
}

func cget(t *testing.T, h http.Handler, path, token string) (int, consentEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return cdo(t, h, req)
}

// consentStatusView status DTO 镜像(测试侧;与 wire 字段一一对应)。
type consentStatusView struct {
	State            string  `json:"state"`
	DeclaredAt       *string `json:"declaredAt"`
	GrantedAt        *string `json:"grantedAt"`
	RevokedAt        *string `json:"revokedAt"`
	RequestExpiresAt *string `json:"requestExpiresAt"`
}

type consentRequestView struct {
	RequestID        string `json:"requestId"`
	RequestExpiresAt string `json:"requestExpiresAt"`
}

func decodeConsentStatus(t *testing.T, env consentEnvelope) consentStatusView {
	t.Helper()
	var v consentStatusView
	if err := json.Unmarshal(env.Data, &v); err != nil {
		t.Fatalf("decode status: %v (data=%s)", err, env.Data)
	}
	return v
}

func newConsentWarden(clock func() time.Time, cfg *ConsentConfig, probe *consentProbe) *Warden {
	return New(Options{Clock: clock, RequireAuth: newAuthW, S2SToken: "s2s-secret",
		Consent: cfg, AuditSink: probe, Notify: probe.notify})
}

// declare 助手:POST declare-age 并解码 status 视图。
func declare(t *testing.T, w *Warden, birthDate string) (int, consentStatusView) {
	t.Helper()
	code, env := cpost(t, w, "/v1/realname/consent/declare-age", "player",
		`{"birthDate":"`+birthDate+`"}`)
	return code, decodeConsentStatus(t, env)
}

func consentGet(t *testing.T, w *Warden) (int, consentStatusView) {
	t.Helper()
	code, env := cget(t, w, "/v1/realname/consent/status", "player")
	return code, decodeConsentStatus(t, env)
}

// verdict 助手:S2S 判定(grant/deny/revoke),接入方服务端凭证。
func verdict(t *testing.T, w *Warden, action, accountID string) (int, consentStatusView) {
	t.Helper()
	code, env := cpost(t, w, "/v1/realname/consent/s2s/"+action, "s2s-secret",
		`{"accountId":"`+accountID+`"}`)
	return code, decodeConsentStatus(t, env)
}

// verdictCode 只回状态码(400 等错误响应无 data,不走解码)。
func verdictCode(t *testing.T, w *Warden, action, accountID string) int {
	t.Helper()
	code, _ := cpost(t, w, "/v1/realname/consent/s2s/"+action, "s2s-secret",
		`{"accountId":"`+accountID+`"}`)
	return code
}

const consentAcc = "acc_1" // newAuthW 固定账号

func TestConsentDisabled_501(t *testing.T) {
	w := newWarden(fixedClock(consentNow), "s2s-secret") // 无 Consent 配置 = 能力关闭
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/realname/consent/status", ""},
		{http.MethodPost, "/v1/realname/consent/declare-age", `{"birthDate":"2016-03-01"}`},
		{http.MethodPost, "/v1/realname/consent/parent-consent/request", "{}"},
		{http.MethodPost, "/v1/realname/consent/s2s/grant", `{"accountId":"acc_1"}`},
	} {
		var code int
		var env consentEnvelope
		if tc.method == http.MethodGet {
			code, env = cget(t, w, tc.path, "player")
		} else {
			code, env = cpost(t, w, tc.path, "s2s-secret", tc.body)
		}
		if code != http.StatusNotImplemented || env.Error == nil ||
			env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Errorf("%s %s = %d %v, want 501 COMMON_CAPABILITY_DISABLED(默认关闭)",
				tc.method, tc.path, code, env.Error)
		}
	}
}

func TestConsentDeclare_Verdicts(t *testing.T) {
	for _, tc := range []struct {
		name      string
		birthDate string
		want      string
	}{
		{"adult 36 岁", "1990-01-01", "ADULT"},
		{"15 岁 ≥ 数字同意年龄 13", "2011-06-15", "GRANTED"},
		{"10 岁 < 数字同意年龄", "2016-03-01", "PENDING_PARENT"},
		{"边界:13 岁生日当天即本人同意", "2013-10-08", "GRANTED"},
		{"边界:13 岁前一天", "2013-10-09", "PENDING_PARENT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
			code, got := declare(t, w, tc.birthDate)
			if code != http.StatusOK || got.State != tc.want {
				t.Fatalf("declare(%s) = %d %+v, want %s", tc.birthDate, code, got, tc.want)
			}
			// 申报即得判定:declaredAt 同响应下发。
			if got.DeclaredAt == nil || *got.DeclaredAt != "2026-10-08T15:00:00.000Z" {
				t.Errorf("declaredAt = %v, want 申报时刻", got.DeclaredAt)
			}
			// 判定经 200 载荷,零新增错误码。
			if got.GrantedAt != nil || got.RevokedAt != nil || got.RequestExpiresAt != nil {
				t.Errorf("非本态时间戳不应下发:%+v", got)
			}
		})
	}
}

func TestConsentDeclare_MinimumAgeDenied(t *testing.T) {
	// minimumAge 归 Provider 配置(契约 v1.1「法域中立」):低于下限恒 DENIED。
	w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
	code, got := declare(t, w, "2022-01-01") // 4 岁
	if code != http.StatusOK || got.State != "DENIED" {
		t.Fatalf("4 岁(min 8)= %d %s, want DENIED", code, got.State)
	}
	// 边界:恰好下限生日当天不算「低于」→ 走正常分档(8 岁 <13 → PENDING_PARENT)。
	w2 := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
	code, got = declare(t, w2, "2018-10-08")
	if code != http.StatusOK || got.State != "PENDING_PARENT" {
		t.Fatalf("8 岁生日当天(min 8)= %d %s, want PENDING_PARENT", code, got.State)
	}
}

func TestConsentDeclare_BirthDateEdges(t *testing.T) {
	w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
	for _, bad := range []string{``, `2016/03/01`, `not-a-date`, `2027-01-01` /*未来*/} {
		body := `{"birthDate":"` + bad + `"}`
		code, env := cpost(t, w, "/v1/realname/consent/declare-age", "player", body)
		if code != http.StatusBadRequest || env.Error == nil ||
			env.Error.Code != "COMMON_INVALID_ARGUMENT" {
			t.Errorf("birthDate %q = %d, want 400 COMMON_INVALID_ARGUMENT", bad, code)
		}
	}
	// 匿名 401 由真实认证栈承载(e2e conformance consent 场景断言);
	// 本文件测试中间件恒注入身份,走不到 401。
}

// TestConsentDeclare_RatchetOnlyTightens 防回拨(契约 v1.1:成年→未成年可,反向
// 不行;纠错走运维面)。场景链在同一 warden 内按趋严方向推进。
func TestConsentDeclare_RatchetOnlyTightens(t *testing.T) {
	t.Run("ADULT→未成年生效,回申报成年不生效", func(t *testing.T) {
		cc := newConsentClock(consentNow)
		probe := &consentProbe{}
		w := newConsentWarden(cc.Now, &ConsentConfig{}, probe)
		if code, got := declare(t, w, "1990-01-01"); code != 200 || got.State != "ADULT" {
			t.Fatalf("declare adult = %d %s", code, got.State)
		}
		code, got := declare(t, w, "2016-03-01") // 趋严:ADULT(0)→PENDING_PARENT(2)
		if code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare minor after adult = %d %s, want 生效", code, got.State)
		}
		code, got = declare(t, w, "1990-01-01") // 放宽:不生效
		if code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare adult after pending = %d %s, want 不生效", code, got.State)
		}
		// declaredAt 保持首次申报时刻(放宽申报不留痕)。
		if got.DeclaredAt == nil || *got.DeclaredAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("declaredAt = %v, want 首次申报时刻", got.DeclaredAt)
		}
	})
	t.Run("GRANTED 申报成年不生效", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		if code, got := declare(t, w, "2011-06-15"); code != 200 || got.State != "GRANTED" {
			t.Fatalf("declare 15 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, w, "1990-01-01")
		if code != 200 || got.State != "GRANTED" {
			t.Fatalf("declare adult after granted = %d %s, want 不生效", code, got.State)
		}
	})
	t.Run("DENIED 申报成年不生效(纠错走运维面)", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code, got := declare(t, w, "2022-01-01"); code != 200 || got.State != "DENIED" {
			t.Fatalf("declare 4 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, w, "1990-01-01")
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("declare adult after denied = %d %s, want 不生效", code, got.State)
		}
	})
	t.Run("PENDING_PARENT 申报更低龄趋严生效", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code, got := declare(t, w, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare 10 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, w, "2022-01-01") // 4 岁:DENIED 更严
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("declare 4 岁 after pending = %d %s, want DENIED", code, got.State)
		}
	})
	t.Run("同态重申报无迁移无通知", func(t *testing.T) {
		cc := newConsentClock(consentNow)
		probe := &consentProbe{}
		w := newConsentWarden(cc.Now, &ConsentConfig{}, probe)
		if code, got := declare(t, w, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("first declare = %d %s", code, got.State)
		}
		_, first := consentGet(t, w)
		if code, got := declare(t, w, "2016-02-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("redeclare = %d %s", code, got.State)
		}
		_, second := consentGet(t, w)
		if first.DeclaredAt == nil || second.DeclaredAt == nil ||
			*first.DeclaredAt != *second.DeclaredAt {
			t.Errorf("declaredAt 变动:%v → %v, want 保持首次", first.DeclaredAt, second.DeclaredAt)
		}
		if _, sse := probe.snapshot(); len(sse) != 1 {
			t.Errorf("sse = %v, want 仅首次迁移 1 条", sse)
		}
	})
}

func TestConsentRequest_IdempotentWithinTTL(t *testing.T) {
	cc := newConsentClock(consentNow)
	w := newConsentWarden(cc.Now, &ConsentConfig{}, &consentProbe{})
	if code, got := declare(t, w, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
		t.Fatalf("setup declare = %d %s", code, got.State)
	}

	requestOnce := func(t *testing.T) consentRequestView {
		t.Helper()
		code, env := cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}")
		if code != http.StatusOK || env.Error != nil {
			t.Fatalf("request = %d %v", code, env.Error)
		}
		var v consentRequestView
		if err := json.Unmarshal(env.Data, &v); err != nil {
			t.Fatalf("decode request: %v (data=%s)", err, env.Data)
		}
		return v
	}

	r1 := requestOnce(t)
	if r1.RequestID == "" {
		t.Fatal("requestId 为空")
	}
	// TTL 缺省 72h:到期点 = 申报后 72 小时。
	if r1.RequestExpiresAt != "2026-10-11T15:00:00.000Z" {
		t.Errorf("requestExpiresAt = %q, want 2026-10-11T15:00:00.000Z", r1.RequestExpiresAt)
	}
	cc.Advance(time.Hour)
	if r2 := requestOnce(t); r2.RequestID != r1.RequestID ||
		r2.RequestExpiresAt != r1.RequestExpiresAt {
		t.Errorf("未过期重发 = %+v, want 幂等同请求 %+v", r2, r1)
	}
	cc.Advance(70 * time.Hour) // 71h < 72h,仍幂等
	if r3 := requestOnce(t); r3.RequestID != r1.RequestID {
		t.Errorf("71h 重发 = %+v, want 同 requestId", r3)
	}
	cc.Advance(2 * time.Hour) // 73h > 72h,过期换新
	r4 := requestOnce(t)
	if r4.RequestID == "" || r4.RequestID == r1.RequestID {
		t.Errorf("过期后 = %+v, want 新 requestId(≠%s)", r4, r1.RequestID)
	}
	if r4.RequestExpiresAt != "2026-10-14T16:00:00.000Z" {
		t.Errorf("过期后到期点 = %q, want 2026-10-14T16:00:00.000Z", r4.RequestExpiresAt)
	}
}

// TestConsentRequest_OnlyFromPendingParent 非 PENDING_PARENT 一律 400
// (现有码 COMMON_INVALID_ARGUMENT,零新增错误码)。
func TestConsentRequest_OnlyFromPendingParent(t *testing.T) {
	mk := func(t *testing.T, setup func(*Warden)) *Warden {
		t.Helper()
		w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
		setup(w)
		return w
	}
	for _, tc := range []struct {
		name  string
		setup func(*Warden)
	}{
		{"UNDECLARED", func(*Warden) {}},
		{"ADULT", func(w *Warden) { declare(t, w, "1990-01-01") }},
		{"GRANTED", func(w *Warden) { declare(t, w, "2011-06-15") }},
		{"DENIED", func(w *Warden) { declare(t, w, "2022-01-01") }},
		{"REVOKED", func(w *Warden) {
			declare(t, w, "2016-03-01")
			verdict(t, w, "revoke", consentAcc)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := mk(t, tc.setup)
			code, env := cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}")
			if code != http.StatusBadRequest || env.Error == nil ||
				env.Error.Code != "COMMON_INVALID_ARGUMENT" {
				t.Errorf("%s 下请求 = %d %v, want 400", tc.name, code, env.Error)
			}
		})
	}
}

func TestConsentVerdict_S2SLifecycle(t *testing.T) {
	t.Run("PENDING_PARENT → grant → GRANTED", func(t *testing.T) {
		cc := newConsentClock(consentNow)
		probe := &consentProbe{}
		w := newConsentWarden(cc.Now, &ConsentConfig{}, probe)
		declare(t, w, "2016-03-01")
		// 先有在途请求:grant 后应清(PENDING_PARENT 之外不下发 requestExpiresAt)。
		if code, env := cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}"); code != 200 {
			t.Fatalf("request = %d %v", code, env.Error)
		}
		code, got := verdict(t, w, "grant", consentAcc)
		if code != 200 || got.State != "GRANTED" {
			t.Fatalf("grant = %d %+v", code, got)
		}
		if got.GrantedAt == nil || *got.GrantedAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("grantedAt = %v, want 判定时刻", got.GrantedAt)
		}
		if got.RequestExpiresAt != nil {
			t.Errorf("GRANTED 下不应有 requestExpiresAt:%+v", got)
		}
		// 幂等:重复 grant 不迁移不覆盖 grantedAt。
		code, got2 := verdict(t, w, "grant", consentAcc)
		if code != 200 || got2.State != "GRANTED" || got2.GrantedAt == nil ||
			*got2.GrantedAt != *got.GrantedAt {
			t.Errorf("re-grant = %d %+v, want 幂等", code, got2)
		}
	})
	t.Run("PENDING_PARENT → deny → DENIED", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, w, "2016-03-01")
		code, got := verdict(t, w, "deny", consentAcc)
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("deny = %d %+v", code, got)
		}
	})
	t.Run("GRANTED → revoke → REVOKED", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, w, "2011-06-15") // 15 岁 → GRANTED
		code, got := verdict(t, w, "revoke", consentAcc)
		if code != 200 || got.State != "REVOKED" {
			t.Fatalf("revoke = %d %+v", code, got)
		}
		if got.RevokedAt == nil || *got.RevokedAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("revokedAt = %v, want 判定时刻", got.RevokedAt)
		}
	})
	t.Run("在途请求 → revoke → REVOKED(请求一并作废)", func(t *testing.T) {
		w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, w, "2016-03-01")
		cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}")
		code, got := verdict(t, w, "revoke", consentAcc)
		if code != 200 || got.State != "REVOKED" || got.RequestExpiresAt != nil {
			t.Fatalf("revoke = %d %+v, want REVOKED 且无在途请求", code, got)
		}
	})
	t.Run("适用性边界", func(t *testing.T) {
		// grant:DENIED 可翻案(运维面);UNDECLARED/ADULT 不可。
		w := newConsentWarden(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		declare(t, w, "2022-01-01") // DENIED
		if code, got := verdict(t, w, "grant", consentAcc); code != 200 || got.State != "GRANTED" {
			t.Errorf("grant on DENIED = %d %s, want 翻案 GRANTED", code, got.State)
		}
		w2 := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		if code := verdictCode(t, w2, "grant", consentAcc); code != http.StatusBadRequest {
			t.Errorf("grant on UNDECLARED = %d, want 400", code)
		}
		declare(t, w2, "1990-01-01")
		if code := verdictCode(t, w2, "grant", consentAcc); code != http.StatusBadRequest {
			t.Errorf("grant on ADULT = %d, want 400", code)
		}
		// deny:GRANTED 下应走 revoke;DENIED 幂等。
		w3 := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, w3, "2011-06-15") // GRANTED
		if code := verdictCode(t, w3, "deny", consentAcc); code != http.StatusBadRequest {
			t.Errorf("deny on GRANTED = %d, want 400(用 revoke)", code)
		}
		w4 := newConsentWarden(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		declare(t, w4, "2022-01-01")
		if code, got := verdict(t, w4, "deny", consentAcc); code != 200 || got.State != "DENIED" {
			t.Errorf("deny on DENIED = %d %s, want 幂等", code, got.State)
		}
		// revoke:UNDECLARED/ADULT/DENIED 不可;REVOKED 幂等。
		w5 := newConsentWarden(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code := verdictCode(t, w5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on UNDECLARED = %d, want 400", code)
		}
		declare(t, w5, "1990-01-01")
		if code := verdictCode(t, w5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on ADULT = %d, want 400", code)
		}
		declare(t, w5, "2016-03-01") // ADULT→趋严 PENDING_PARENT
		verdict(t, w5, "deny", consentAcc)
		if code := verdictCode(t, w5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on DENIED = %d, want 400", code)
		}
		w6 := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, w6, "2011-06-15")
		verdict(t, w6, "revoke", consentAcc)
		if code, got := verdict(t, w6, "revoke", consentAcc); code != 200 || got.State != "REVOKED" {
			t.Errorf("revoke on REVOKED = %d %s, want 幂等", code, got.State)
		}
	})
}

func TestConsentS2S_Auth(t *testing.T) {
	w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"无凭证", ""},
		{"错误凭证", "wrong-token"},
		{"玩家凭证不是 S2S 凭证", "player"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, env := cpost(t, w, "/v1/realname/consent/s2s/grant", tc.token,
				`{"accountId":"acc_1"}`)
			if code != http.StatusUnauthorized || env.Error == nil ||
				env.Error.Code != "COMMON_UNAUTHENTICATED" {
				t.Errorf("%s = %d, want 401", tc.name, code)
			}
		})
	}
	// accountId 缺失 → 400。
	code, env := cpost(t, w, "/v1/realname/consent/s2s/grant", "s2s-secret", `{}`)
	if code != http.StatusBadRequest || env.Error == nil ||
		env.Error.Code != "COMMON_INVALID_ARGUMENT" {
		t.Errorf("空 accountId = %d, want 400", code)
	}
}

func TestConsentNotify_OnTransitions(t *testing.T) {
	cc := newConsentClock(consentNow)
	probe := &consentProbe{}
	w := newConsentWarden(cc.Now, &ConsentConfig{}, probe)
	declare(t, w, "2016-03-01")                                                // → PENDING_PARENT
	declare(t, w, "2016-03-01")                                                // 同态重申报:无
	declare(t, w, "1990-01-01")                                                // 放宽不生效:无
	cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}") // 请求非状态迁移:无
	verdict(t, w, "grant", consentAcc)                                         // → GRANTED
	verdict(t, w, "grant", consentAcc)                                         // 幂等:无
	verdict(t, w, "revoke", consentAcc)                                        // → REVOKED
	want := []string{
		"realname.consent.updated|PENDING_PARENT",
		"realname.consent.updated|GRANTED",
		"realname.consent.updated|REVOKED",
	}
	_, sse := probe.snapshot()
	if len(sse) != len(want) {
		t.Fatalf("sse = %v, want %v", sse, want)
	}
	for i, want2 := range want {
		if sse[i] != want2 {
			t.Errorf("sse[%d] = %q, want %q", i, sse[i], want2)
		}
	}
}

func TestConsentAudit_AccountDimensionOnly(t *testing.T) {
	probe := &consentProbe{}
	w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, probe)
	declare(t, w, "2016-03-01")
	cpost(t, w, "/v1/realname/consent/parent-consent/request", "player", "{}")
	verdict(t, w, "grant", consentAcc)
	verdict(t, w, "revoke", consentAcc)
	events, _ := probe.snapshot()
	wantActions := []string{
		"realname.consent.declared",
		"realname.consent.parent_requested",
		"realname.consent.granted",
		"realname.consent.revoked",
	}
	if len(events) != len(wantActions) {
		t.Fatalf("audit = %d 条 %v, want %v", len(events), actions(events), wantActions)
	}
	for i, e := range events {
		if e.Action != wantActions[i] {
			t.Errorf("events[%d].Action = %q, want %q", i, e.Action, wantActions[i])
		}
		if e.AccountID != consentAcc {
			t.Errorf("events[%d].AccountID = %q, want %q", i, e.AccountID, consentAcc)
		}
		// 出生日期永不进事件(结构上 AuditEvent 无该字段;此处守护口径不被旁路
		// 成自由文本 outcome)。
		if strings.Contains(e.Outcome, "2016") || strings.Contains(e.Outcome, "birth") {
			t.Errorf("events[%d].Outcome 疑似携带出生信息:%q", i, e.Outcome)
		}
	}
}

func actions(events []audit.AuditEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}

// TestConsent_IndependentOfIdentityStorage consent 与实名核验分存储(契约 v1.1
// 「同意记录与身份数据分存储」):未核验账号可独立走 consent 全流程,
// consent 状态不反向污染实名状态。
func TestConsent_IndependentOfIdentityStorage(t *testing.T) {
	w := newConsentWarden(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
	// 实名侧未提交:UNVERIFIED。
	code, env := cget(t, w, "/v1/realname/status", "player")
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	var rn struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(env.Data, &rn); err != nil || rn.State != "UNVERIFIED" {
		t.Fatalf("实名初始 = %s, want UNVERIFIED", rn.State)
	}
	// consent 侧独立走通:申报 → 请求 → 判定。
	if code, got := declare(t, w, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
		t.Fatalf("未核验账号申报 = %d %s, want 独立生效", code, got.State)
	}
	if code, got := verdict(t, w, "grant", consentAcc); code != 200 || got.State != "GRANTED" {
		t.Fatalf("未核验账号 grant = %d %s", code, got.State)
	}
	// consent 判定不改变实名状态。
	_, env = cget(t, w, "/v1/realname/status", "player")
	_ = json.Unmarshal(env.Data, &rn)
	if rn.State != "UNVERIFIED" {
		t.Errorf("consent 判定后实名状态 = %s, want 仍 UNVERIFIED", rn.State)
	}
}
