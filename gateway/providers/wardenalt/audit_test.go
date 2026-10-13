// 审计留痕(F33)wardenalt 侧验收:与 warden 同形状留痕(共享 gateway/audit 接缝)——
// 关键动作产出事件、nil sink 零产出、S2S 鉴权失败与入参非法不留痕、红线(事件
// 永不携带姓名/证件号明文或指纹)、sink 失败不反向打断合规动作。
// 与 warden/audit_test.go 同构:两实现事件面一致是「同契约可换」的行为面之一。
package wardenalt

import (
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/cuihairu/courier/gateway/audit"
)

// captureSink 捕获事件留断言用。
type captureSink struct{ events []audit.AuditEvent }

func (s *captureSink) Record(e audit.AuditEvent) { s.events = append(s.events, e) }

func newProviderWithSink(sink audit.AuditSink) *Provider {
	return New(Options{Clock: fixedClock(nowThursday), RequireAuth: newAuth,
		Cloud: scriptedVerdicts(), S2SToken: "s2s-secret", AuditSink: sink})
}

// hitAll 走一遍四个留痕端点(verify 用未成年证件触发 curfew/charge 的 denied 面)。
func hitAll(t *testing.T, p *Provider) {
	t.Helper()
	code, _ := post(t, p, "/v1/realname/verify", "tok", verifyBody(minorID))
	if code != 200 {
		t.Fatalf("verify: %d", code)
	}
	if code, _ := get(t, p, "/v1/realname/curfew", "tok"); code != 200 {
		t.Fatalf("curfew: %d", code)
	}
	if code, _ := post(t, p, "/v1/realname/charge-check", "tok", `{"amountCents":100}`); code != 200 {
		t.Fatalf("charge-check: %d", code)
	}
	if code, _ := post(t, p, "/v1/realname/playtime-report", "s2s-secret",
		`{"accountId":"acc_1","date":"2026-10-08","playMinutes":30,"chargeAmountCents":0}`); code != 200 {
		t.Fatalf("playtime-report: %d", code)
	}
}

func TestAudit_KeyActionsEmitted(t *testing.T) {
	sink := &captureSink{}
	p := newProviderWithSink(sink)
	hitAll(t, p)

	want := []struct{ action, outcome string }{
		{audit.AuditActionVerify, stateVerified},
		{audit.AuditActionCurfew, audit.AuditOutcomeDenied}, // 未成年 @周四 15:00 → 不可玩
		{audit.AuditActionChargeCheck, audit.AuditOutcomeAllowed},
		{audit.AuditActionPlaytimeReport, audit.AuditOutcomeOK},
	}
	if len(sink.events) != len(want) {
		t.Fatalf("事件数: %d, want %d (%v)", len(sink.events), len(want), sink.events)
	}
	for i, e := range sink.events {
		if e.Action != want[i].action || e.Outcome != want[i].outcome {
			t.Errorf("事件[%d] = %s/%s, want %s/%s", i, e.Action, e.Outcome, want[i].action, want[i].outcome)
		}
		if e.AccountID != "acc_1" {
			t.Errorf("事件[%d] 账号 = %q", i, e.AccountID)
		}
		if !e.At.Equal(nowThursday) {
			t.Errorf("事件[%d] At = %v, want Clock 口径 %v", i, e.At, nowThursday)
		}
		if e.TraceID == "" {
			t.Errorf("事件[%d] TraceID 空", i)
		}
	}
}

func TestAudit_StatusIsPureRead_NoEvent(t *testing.T) {
	sink := &captureSink{}
	p := newProviderWithSink(sink)
	if code, _ := get(t, p, "/v1/realname/status", "tok"); code != 200 {
		t.Fatalf("status: %d", code)
	}
	if len(sink.events) != 0 {
		t.Fatalf("纯读不应留痕: %v", sink.events)
	}
}

func TestAudit_NilSink_NoPanicNoEvents(t *testing.T) {
	p := newProviderWithSink(nil)
	hitAll(t, p) // nil sink:不产出也不 panic
}

func TestAudit_AuthFailureAndInvalidInput_NoEvent(t *testing.T) {
	sink := &captureSink{}
	p := newProviderWithSink(sink)
	// S2S 凭证错误 → 401,动作未发生,不留痕。
	if code, _ := post(t, p, "/v1/realname/playtime-report", "wrong",
		`{"accountId":"acc_1","date":"2026-10-08","playMinutes":1}`); code != 401 {
		t.Fatalf("s2s 401: %d", code)
	}
	// 入参非法挡在边缘(400):不产生审计事件。
	if code, _ := post(t, p, "/v1/realname/verify", "tok", `{"name":"","idNumber":"x"}`); code != 400 {
		t.Fatalf("verify 400: %d", code)
	}
	if code, _ := post(t, p, "/v1/realname/charge-check", "tok", `{"amountCents":-1}`); code != 400 {
		t.Fatalf("charge-check 400: %d", code)
	}
	if len(sink.events) != 0 {
		t.Fatalf("失败路径不应留痕: %v", sink.events)
	}
}

// 红线(realname.md):审计事件只含账号维度元数据——姓名/证件号明文不得出现在
// 事件及其序列化形状里。
func TestAudit_RedLine_NeverCarriesIdentityInput(t *testing.T) {
	sink := &captureSink{}
	p := newProviderWithSink(sink)
	if code, _ := post(t, p, "/v1/realname/verify", "tok", verifyBody(minorID)); code != 200 {
		t.Fatalf("verify: %d", code)
	}
	if len(sink.events) != 1 {
		t.Fatalf("事件数: %d", len(sink.events))
	}
	encoded, err := json.Marshal(sink.events[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	shape := string(encoded)
	for _, banned := range []string{"张三", minorID, "idNumber", "name"} {
		if strings.Contains(shape, banned) {
			t.Errorf("事件携带违禁内容 %q: %s", banned, shape)
		}
	}
}

// sink 失败不得反向打断合规动作:verify 在 sink 报错时仍 200。
func TestAudit_FailingSink_DoesNotBreakVerifyPath(t *testing.T) {
	p := New(Options{
		Clock: fixedClock(nowThursday), RequireAuth: newAuth, Cloud: scriptedVerdicts(),
		S2SToken:  "s2s-secret",
		AuditSink: audit.HTTPSink{Endpoint: "http://127.0.0.1:1/x", Logger: log.New(io.Discard, "", 0)},
	})
	if code, _ := post(t, p, "/v1/realname/verify", "tok", verifyBody(minorID)); code != 200 {
		t.Fatalf("sink 失败不应影响 verify: %d", code)
	}
}
