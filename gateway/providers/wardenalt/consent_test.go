// consent 组验收(realname.md v1.1,F31;冻结清单步骤 3 网关侧)——与 warden 同
// 场景同断言,证明两实现在 consent 面同契约异构(判定源/落库口径不同,consent
// 状态机与 wire 形状一致)。本实现不解析证件号,出生日期是 declare-age 的直接
// 输入,因此与 warden 本地解析口径在 consent 面重合。
package wardenalt

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/audit"
)

var consentNow = time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC)

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

func decodeConsentStatus(t *testing.T, env envelope) consentStatusView {
	t.Helper()
	var v consentStatusView
	if err := json.Unmarshal(env.Data, &v); err != nil {
		t.Fatalf("decode status: %v (data=%s)", err, env.Data)
	}
	return v
}

func newConsentProvider(clock func() time.Time, cfg *ConsentConfig, probe *consentProbe) *Provider {
	return New(Options{Clock: clock, RequireAuth: newAuth, S2SToken: "s2s-secret",
		Cloud: scriptedVerdicts(), Consent: cfg, AuditSink: probe, Notify: probe.notify})
}

func declare(t *testing.T, p *Provider, birthDate string) (int, consentStatusView) {
	t.Helper()
	code, env := post(t, p, "/v1/realname/consent/declare-age", "player",
		`{"birthDate":"`+birthDate+`"}`)
	return code, decodeConsentStatus(t, env)
}

func verdict(t *testing.T, p *Provider, action, accountID string) (int, consentStatusView) {
	t.Helper()
	code, env := post(t, p, "/v1/realname/consent/s2s/"+action, "s2s-secret",
		`{"accountId":"`+accountID+`"}`)
	return code, decodeConsentStatus(t, env)
}

// verdictCode 只回状态码(400 等错误响应无 data,不走解码)。
func verdictCode(t *testing.T, p *Provider, action, accountID string) int {
	t.Helper()
	code, _ := post(t, p, "/v1/realname/consent/s2s/"+action, "s2s-secret",
		`{"accountId":"`+accountID+`"}`)
	return code
}

const consentAcc = "acc_1"

func TestConsentDisabled_501(t *testing.T) {
	p := newProvider(nil) // 无 Consent 配置 = 能力关闭(与核验服务配置互不影响)
	for _, tc := range []struct {
		path, body string
		withAuth   bool
	}{
		{"/v1/realname/consent/status", "", true},
		{"/v1/realname/consent/declare-age", `{"birthDate":"2016-03-01"}`, true},
		{"/v1/realname/consent/parent-consent/request", "{}", true},
		{"/v1/realname/consent/s2s/grant", `{"accountId":"acc_1"}`, true},
	} {
		var code int
		var env envelope
		if tc.path == "/v1/realname/consent/status" {
			code, env = get(t, p, tc.path, "player")
		} else {
			code, env = post(t, p, tc.path, "s2s-secret", tc.body)
		}
		if code != http.StatusNotImplemented || env.Error == nil ||
			env.Error.Code != "COMMON_CAPABILITY_DISABLED" {
			t.Errorf("%s = %d %v, want 501 COMMON_CAPABILITY_DISABLED", tc.path, code, env.Error)
		}
	}
}

func TestConsentDeclare_Verdicts(t *testing.T) {
	for _, tc := range []struct {
		name, birthDate, want string
	}{
		{"adult 36 岁", "1990-01-01", "ADULT"},
		{"15 岁 ≥ 数字同意年龄 13", "2011-06-15", "GRANTED"},
		{"10 岁 < 数字同意年龄", "2016-03-01", "PENDING_PARENT"},
		{"边界:13 岁生日当天", "2013-10-08", "GRANTED"},
		{"边界:13 岁前一天", "2013-10-09", "PENDING_PARENT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
			code, got := declare(t, p, tc.birthDate)
			if code != http.StatusOK || got.State != tc.want {
				t.Fatalf("declare(%s) = %d %+v, want %s", tc.birthDate, code, got, tc.want)
			}
			if got.DeclaredAt == nil || *got.DeclaredAt != "2026-10-08T15:00:00.000Z" {
				t.Errorf("declaredAt = %v, want 申报时刻", got.DeclaredAt)
			}
			if got.GrantedAt != nil || got.RevokedAt != nil || got.RequestExpiresAt != nil {
				t.Errorf("非本态时间戳不应下发:%+v", got)
			}
		})
	}
}

func TestConsentDeclare_MinimumAgeDenied(t *testing.T) {
	p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
	code, got := declare(t, p, "2022-01-01")
	if code != http.StatusOK || got.State != "DENIED" {
		t.Fatalf("4 岁(min 8)= %d %s, want DENIED", code, got.State)
	}
	p2 := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
	code, got = declare(t, p2, "2018-10-08")
	if code != http.StatusOK || got.State != "PENDING_PARENT" {
		t.Fatalf("8 岁生日当天(min 8)= %d %s, want PENDING_PARENT", code, got.State)
	}
}

func TestConsentDeclare_BirthDateEdges(t *testing.T) {
	p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
	for _, bad := range []string{``, `2016/03/01`, `not-a-date`, `2027-01-01`} {
		code, env := post(t, p, "/v1/realname/consent/declare-age", "player",
			`{"birthDate":"`+bad+`"}`)
		if code != http.StatusBadRequest || env.Error == nil ||
			env.Error.Code != "COMMON_INVALID_ARGUMENT" {
			t.Errorf("birthDate %q = %d, want 400", bad, code)
		}
	}
	// 匿名 401 由真实认证栈承载(e2e conformance consent 场景断言);
	// 本文件测试中间件恒注入身份,走不到 401。
}

func TestConsentDeclare_RatchetOnlyTightens(t *testing.T) {
	t.Run("ADULT→未成年生效,回申报成年不生效", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		if code, got := declare(t, p, "1990-01-01"); code != 200 || got.State != "ADULT" {
			t.Fatalf("declare adult = %d %s", code, got.State)
		}
		code, got := declare(t, p, "2016-03-01")
		if code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare minor after adult = %d %s, want 生效", code, got.State)
		}
		code, got = declare(t, p, "1990-01-01")
		if code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare adult after pending = %d %s, want 不生效", code, got.State)
		}
		if got.DeclaredAt == nil || *got.DeclaredAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("declaredAt = %v, want 首次申报时刻", got.DeclaredAt)
		}
	})
	t.Run("GRANTED 申报成年不生效", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		if code, got := declare(t, p, "2011-06-15"); code != 200 || got.State != "GRANTED" {
			t.Fatalf("declare 15 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, p, "1990-01-01")
		if code != 200 || got.State != "GRANTED" {
			t.Fatalf("declare adult after granted = %d %s, want 不生效", code, got.State)
		}
	})
	t.Run("DENIED 申报成年不生效", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code, got := declare(t, p, "2022-01-01"); code != 200 || got.State != "DENIED" {
			t.Fatalf("declare 4 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, p, "1990-01-01")
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("declare adult after denied = %d %s, want 不生效", code, got.State)
		}
	})
	t.Run("PENDING_PARENT 申报更低龄趋严生效", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code, got := declare(t, p, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("declare 10 岁 = %d %s", code, got.State)
		}
		code, got := declare(t, p, "2022-01-01")
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("declare 4 岁 after pending = %d %s, want DENIED", code, got.State)
		}
	})
	t.Run("同态重申报无迁移无通知", func(t *testing.T) {
		cc := newConsentClock(consentNow)
		probe := &consentProbe{}
		p := newConsentProvider(cc.Now, &ConsentConfig{}, probe)
		if code, got := declare(t, p, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("first declare = %d %s", code, got.State)
		}
		_, env := get(t, p, "/v1/realname/consent/status", "player")
		first := decodeConsentStatus(t, env)
		if code, got := declare(t, p, "2016-02-01"); code != 200 || got.State != "PENDING_PARENT" {
			t.Fatalf("redeclare = %d %s", code, got.State)
		}
		_, env = get(t, p, "/v1/realname/consent/status", "player")
		second := decodeConsentStatus(t, env)
		if first.DeclaredAt == nil || second.DeclaredAt == nil ||
			*first.DeclaredAt != *second.DeclaredAt {
			t.Errorf("declaredAt 变动:%v → %v", first.DeclaredAt, second.DeclaredAt)
		}
		if _, sse := probe.snapshot(); len(sse) != 1 {
			t.Errorf("sse = %v, want 仅首次迁移 1 条", sse)
		}
	})
}

func TestConsentRequest_IdempotentWithinTTL(t *testing.T) {
	cc := newConsentClock(consentNow)
	p := newConsentProvider(cc.Now, &ConsentConfig{}, &consentProbe{})
	if code, got := declare(t, p, "2016-03-01"); code != 200 || got.State != "PENDING_PARENT" {
		t.Fatalf("setup declare = %d %s", code, got.State)
	}
	requestOnce := func(t *testing.T) consentRequestView {
		t.Helper()
		code, env := post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}")
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
	if r1.RequestExpiresAt != "2026-10-11T15:00:00.000Z" {
		t.Errorf("requestExpiresAt = %q, want 2026-10-11T15:00:00.000Z", r1.RequestExpiresAt)
	}
	cc.Advance(time.Hour)
	if r2 := requestOnce(t); r2.RequestID != r1.RequestID ||
		r2.RequestExpiresAt != r1.RequestExpiresAt {
		t.Errorf("未过期重发 = %+v, want 幂等同请求 %+v", r2, r1)
	}
	cc.Advance(70 * time.Hour)
	if r3 := requestOnce(t); r3.RequestID != r1.RequestID {
		t.Errorf("71h 重发 = %+v, want 同 requestId", r3)
	}
	cc.Advance(2 * time.Hour)
	r4 := requestOnce(t)
	if r4.RequestID == "" || r4.RequestID == r1.RequestID {
		t.Errorf("过期后 = %+v, want 新 requestId(≠%s)", r4, r1.RequestID)
	}
	if r4.RequestExpiresAt != "2026-10-14T16:00:00.000Z" {
		t.Errorf("过期后到期点 = %q, want 2026-10-14T16:00:00.000Z", r4.RequestExpiresAt)
	}
}

func TestConsentRequest_OnlyFromPendingParent(t *testing.T) {
	mk := func(t *testing.T, setup func(*Provider)) *Provider {
		t.Helper()
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{MinimumAge: 8}, &consentProbe{})
		setup(p)
		return p
	}
	for _, tc := range []struct {
		name  string
		setup func(*Provider)
	}{
		{"UNDECLARED", func(*Provider) {}},
		{"ADULT", func(p *Provider) { declare(t, p, "1990-01-01") }},
		{"GRANTED", func(p *Provider) { declare(t, p, "2011-06-15") }},
		{"DENIED", func(p *Provider) { declare(t, p, "2022-01-01") }},
		{"REVOKED", func(p *Provider) {
			declare(t, p, "2016-03-01")
			verdict(t, p, "revoke", consentAcc)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mk(t, tc.setup)
			code, env := post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}")
			if code != http.StatusBadRequest || env.Error == nil ||
				env.Error.Code != "COMMON_INVALID_ARGUMENT" {
				t.Errorf("%s 下请求 = %d %v, want 400", tc.name, code, env.Error)
			}
		})
	}
}

func TestConsentVerdict_S2SLifecycle(t *testing.T) {
	t.Run("PENDING_PARENT → grant → GRANTED", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p, "2016-03-01")
		if code, env := post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}"); code != 200 {
			t.Fatalf("request = %d %v", code, env.Error)
		}
		code, got := verdict(t, p, "grant", consentAcc)
		if code != 200 || got.State != "GRANTED" {
			t.Fatalf("grant = %d %+v", code, got)
		}
		if got.GrantedAt == nil || *got.GrantedAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("grantedAt = %v", got.GrantedAt)
		}
		if got.RequestExpiresAt != nil {
			t.Errorf("GRANTED 下不应有 requestExpiresAt:%+v", got)
		}
		code, got2 := verdict(t, p, "grant", consentAcc)
		if code != 200 || got2.State != "GRANTED" || got2.GrantedAt == nil ||
			*got2.GrantedAt != *got.GrantedAt {
			t.Errorf("re-grant = %d %+v, want 幂等", code, got2)
		}
	})
	t.Run("PENDING_PARENT → deny → DENIED", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p, "2016-03-01")
		code, got := verdict(t, p, "deny", consentAcc)
		if code != 200 || got.State != "DENIED" {
			t.Fatalf("deny = %d %+v", code, got)
		}
	})
	t.Run("GRANTED → revoke → REVOKED", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p, "2011-06-15")
		code, got := verdict(t, p, "revoke", consentAcc)
		if code != 200 || got.State != "REVOKED" {
			t.Fatalf("revoke = %d %+v", code, got)
		}
		if got.RevokedAt == nil || *got.RevokedAt != "2026-10-08T15:00:00.000Z" {
			t.Errorf("revokedAt = %v", got.RevokedAt)
		}
	})
	t.Run("在途请求 → revoke → 请求作废", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p, "2016-03-01")
		post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}")
		code, got := verdict(t, p, "revoke", consentAcc)
		if code != 200 || got.State != "REVOKED" || got.RequestExpiresAt != nil {
			t.Fatalf("revoke = %d %+v, want REVOKED 且无在途请求", code, got)
		}
	})
	t.Run("适用性边界", func(t *testing.T) {
		p := newConsentProvider(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		declare(t, p, "2022-01-01") // DENIED
		if code, got := verdict(t, p, "grant", consentAcc); code != 200 || got.State != "GRANTED" {
			t.Errorf("grant on DENIED = %d %s, want 翻案 GRANTED", code, got.State)
		}
		p2 := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		if code := verdictCode(t, p2, "grant", consentAcc); code != http.StatusBadRequest {
			t.Errorf("grant on UNDECLARED = %d, want 400", code)
		}
		declare(t, p2, "1990-01-01")
		if code := verdictCode(t, p2, "grant", consentAcc); code != http.StatusBadRequest {
			t.Errorf("grant on ADULT = %d, want 400", code)
		}
		p3 := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p3, "2011-06-15") // GRANTED
		if code := verdictCode(t, p3, "deny", consentAcc); code != http.StatusBadRequest {
			t.Errorf("deny on GRANTED = %d, want 400(用 revoke)", code)
		}
		p4 := newConsentProvider(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		declare(t, p4, "2022-01-01")
		if code, got := verdict(t, p4, "deny", consentAcc); code != 200 || got.State != "DENIED" {
			t.Errorf("deny on DENIED = %d %s, want 幂等", code, got.State)
		}
		p5 := newConsentProvider(newConsentClock(consentNow).Now,
			&ConsentConfig{MinimumAge: 8}, &consentProbe{})
		if code := verdictCode(t, p5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on UNDECLARED = %d, want 400", code)
		}
		declare(t, p5, "1990-01-01")
		if code := verdictCode(t, p5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on ADULT = %d, want 400", code)
		}
		declare(t, p5, "2016-03-01")
		verdict(t, p5, "deny", consentAcc)
		if code := verdictCode(t, p5, "revoke", consentAcc); code != http.StatusBadRequest {
			t.Errorf("revoke on DENIED = %d, want 400", code)
		}
		p6 := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
		declare(t, p6, "2011-06-15")
		verdict(t, p6, "revoke", consentAcc)
		if code, got := verdict(t, p6, "revoke", consentAcc); code != 200 || got.State != "REVOKED" {
			t.Errorf("revoke on REVOKED = %d %s, want 幂等", code, got.State)
		}
	})
}

func TestConsentS2S_Auth(t *testing.T) {
	p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, &consentProbe{})
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"无凭证", ""},
		{"错误凭证", "wrong-token"},
		{"玩家凭证不是 S2S 凭证", "player"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, env := post(t, p, "/v1/realname/consent/s2s/grant", tc.token,
				`{"accountId":"acc_1"}`)
			if code != http.StatusUnauthorized || env.Error == nil ||
				env.Error.Code != "COMMON_UNAUTHENTICATED" {
				t.Errorf("%s = %d, want 401", tc.name, code)
			}
		})
	}
	code, env := post(t, p, "/v1/realname/consent/s2s/grant", "s2s-secret", `{}`)
	if code != http.StatusBadRequest || env.Error == nil ||
		env.Error.Code != "COMMON_INVALID_ARGUMENT" {
		t.Errorf("空 accountId = %d, want 400", code)
	}
}

func TestConsentNotify_OnTransitions(t *testing.T) {
	probe := &consentProbe{}
	p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, probe)
	declare(t, p, "2016-03-01")                                               // → PENDING_PARENT
	declare(t, p, "2016-03-01")                                               // 同态:无
	declare(t, p, "1990-01-01")                                               // 放宽不生效:无
	post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}") // 非状态迁移:无
	verdict(t, p, "grant", consentAcc)                                        // → GRANTED
	verdict(t, p, "grant", consentAcc)                                        // 幂等:无
	verdict(t, p, "revoke", consentAcc)                                       // → REVOKED
	want := []string{
		"realname.consent.updated|PENDING_PARENT",
		"realname.consent.updated|GRANTED",
		"realname.consent.updated|REVOKED",
	}
	_, sse := probe.snapshot()
	if len(sse) != len(want) {
		t.Fatalf("sse = %v, want %v", sse, want)
	}
	for i, w2 := range want {
		if sse[i] != w2 {
			t.Errorf("sse[%d] = %q, want %q", i, sse[i], w2)
		}
	}
}

func TestConsentAudit_AccountDimensionOnly(t *testing.T) {
	probe := &consentProbe{}
	p := newConsentProvider(newConsentClock(consentNow).Now, &ConsentConfig{}, probe)
	declare(t, p, "2016-03-01")
	post(t, p, "/v1/realname/consent/parent-consent/request", "player", "{}")
	verdict(t, p, "grant", consentAcc)
	verdict(t, p, "revoke", consentAcc)
	events, _ := probe.snapshot()
	wantActions := []string{
		"realname.consent.declared",
		"realname.consent.parent_requested",
		"realname.consent.granted",
		"realname.consent.revoked",
	}
	if len(events) != len(wantActions) {
		t.Fatalf("audit = %d 条 %v, want %v", len(events), eventActions(events), wantActions)
	}
	for i, e := range events {
		if e.Action != wantActions[i] {
			t.Errorf("events[%d].Action = %q, want %q", i, e.Action, wantActions[i])
		}
		if e.AccountID != consentAcc {
			t.Errorf("events[%d].AccountID = %q, want %q", i, e.AccountID, consentAcc)
		}
		if strings.Contains(e.Outcome, "2016") || strings.Contains(e.Outcome, "birth") {
			t.Errorf("events[%d].Outcome 疑似携带出生信息:%q", i, e.Outcome)
		}
	}
}

func eventActions(events []audit.AuditEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}
