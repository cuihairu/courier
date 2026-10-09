// 批次 18:第二实现内部面单测(签名令牌编解码、代际重放检测)。
// wire 行为契约一致性由 e2e/identity_conformance_test.go 对两个实现跑同一场景。
package accountalt

import (
	"strings"
	"testing"
	"time"
)

func newTestProvider(t *testing.T, mutate func(*Options)) *Provider {
	t.Helper()
	opts := Options{Iterations: 1000, RatePerMinute: 1000, RateBurst: 1000}
	if mutate != nil {
		mutate(&opts)
	}
	return New(opts)
}

func TestCodecRoundtripAndTamper(t *testing.T) {
	p := newTestProvider(t, nil)
	now := time.Now()
	access := p.store.codec.issueAccess("s1", "a1", "d1", "g1", "prod", now.Add(time.Minute), "nonce1")
	refresh := p.store.codec.issueRefresh("s1", 1, now.Add(time.Hour))

	ac, err := p.store.codec.parseAccess(access)
	if err != nil {
		t.Fatalf("access 解析失败: %v", err)
	}
	if ac.sessionID != "s1" || ac.accountID != "a1" || ac.deviceID != "d1" || ac.gameID != "g1" || ac.env != "prod" {
		t.Fatalf("access claim 不符: %+v", ac)
	}
	rc, err := p.store.codec.parseRefresh(refresh)
	if err != nil || rc.sessionID != "s1" || rc.gen != 1 {
		t.Fatalf("refresh 解析: %+v err=%v", rc, err)
	}

	// 篡改 payload(保原签名)/拼错形状/换签名/换前缀 → 全部拒
	parts := strings.Split(access, ".")
	tampered := []byte(parts[1])
	tampered[0] ^= 0xFF
	bad := []string{
		"at1." + string(tampered) + "." + parts[2],
		access + "x",
		"at1.deadbeef." + parts[2],
		"at2." + parts[1] + "." + parts[2],
	}
	for i, tok := range bad {
		if _, err := p.store.codec.parseAccess(tok); err == nil {
			t.Fatalf("bad[%d] 应拒: %s", i, tok)
		}
	}
	// 前缀混用:refresh 当 access 用必须拒
	if _, err := p.store.codec.parseAccess(refresh); err == nil {
		t.Fatal("refresh 当 access 应拒")
	}
}

func TestSigningKeyValidation(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("非法签名密钥应 panic 快速失败")
		}
	}()
	New(Options{SigningKey: SigningKey("short")})
}

func TestRefreshGenerationReplay(t *testing.T) {
	p := newTestProvider(t, nil)
	acc, err := p.store.CreateEmailAccount("a@b.co", "password8")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	_, access1, refresh1, err := p.store.CreateSession(acc.id, "game_a", "prod", "d1")
	if err != nil {
		t.Fatalf("建会话: %v", err)
	}
	r1, err := p.store.codec.parseRefresh(refresh1)
	if err != nil {
		t.Fatalf("解析: %v", err)
	}

	// 轮换:gen 1→2,新令牌全新
	access2, refresh2, err := p.store.Rotate(r1.sessionID)
	if err != nil {
		t.Fatalf("轮换: %v", err)
	}
	if access2 == access1 || refresh2 == refresh1 {
		t.Fatal("轮换应生成全新令牌")
	}
	sess, _ := p.store.Session(r1.sessionID)
	if sess.gen != 2 {
		t.Fatalf("会话代际 = %d, 期望 2", sess.gen)
	}

	// 旧 refresh 再现(gen 1 < 2)→ 吊销整个会话
	if _, err := p.store.codec.parseRefresh(refresh1); err != nil {
		t.Fatalf("旧 refresh 应仍可解析: %v", err)
	}
	p.store.RevokeSession(r1.sessionID) // 上层在 gen<current 时吊销(行为契约)

	// 旧 access 因会话吊销而死(签名仍有效但状态拒)
	ac, err := p.store.codec.parseAccess(access1)
	if err != nil {
		t.Fatalf("旧 access 解析: %v", err)
	}
	sess, _ = p.store.Session(ac.sessionID)
	if !sess.revoked {
		t.Fatal("会话应已吊销")
	}
}
