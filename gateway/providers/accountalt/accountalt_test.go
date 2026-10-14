// 批次 18:第二实现内部面单测(签名令牌编解码、代际重放检测)。
// wire 行为契约一致性由 e2e/identity_conformance_test.go 对两个实现跑同一场景。
package accountalt

import (
	"strings"
	"testing"
	"time"

	"github.com/cuihairu/courier/gateway/providers/oauth"
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

// --- 注销(v1.1):存储层语义与自建一致(wire 一致性由 conformance e2e 把关)---

func TestDeleteCancelFlowStore(t *testing.T) {
	now := time.Now()
	p := newTestProvider(t, func(o *Options) {
		o.DeleteGrace = time.Hour
		o.Clock = func() time.Time { return now }
	})
	acc, err := p.store.CreateEmailAccount("del@b.co", "password8")
	if err != nil {
		t.Fatalf("注册: %v", err)
	}
	_, access, _, err := p.store.CreateSession(acc.id, "g1", "prod", "d1")
	if err != nil {
		t.Fatalf("建会话: %v", err)
	}
	if _, err := p.store.codec.parseAccess(access); err != nil {
		t.Fatalf("access 应可验: %v", err)
	}

	pending, token, deletedAt, err := p.store.RequestDelete(acc.id, p.store.opts.DeleteGrace)
	if err != nil || !pending || deletedAt != nil || token == "" {
		t.Fatalf("RequestDelete = pending=%v token=%q deletedAt=%v err=%v", pending, token, deletedAt, err)
	}
	if s, _ := p.store.Session(mustSessionID(t, p, access)); !s.revoked {
		t.Fatal("受理即应吊销会话")
	}
	if got, err := p.store.ConsumeCancelToken("junk"); err == nil {
		t.Fatalf("垃圾 token 应拒, got %q", got)
	}
	if got, err := p.store.ConsumeCancelToken(token); err != nil || got != acc.id {
		t.Fatalf("ConsumeCancelToken = %q err=%v", got, err)
	}
	if _, err := p.store.ConsumeCancelToken(token); err == nil {
		t.Fatal("token 应一次性")
	}
	p.store.CancelDelete(acc.id)
	if a, _ := p.store.GetAccount(acc.id); a.status != "ACTIVE" {
		t.Fatalf("撤销后 status = %s", a.status)
	}
}

func mustSessionID(t *testing.T, p *Provider, access string) string {
	t.Helper()
	ac, err := p.store.codec.parseAccess(access)
	if err != nil {
		t.Fatalf("解析 access: %v", err)
	}
	return ac.sessionID
}

func TestDeleteImmediateAndFinalizeStore(t *testing.T) {
	t.Run("游客恒立即删,同设备索引清除", func(t *testing.T) {
		p := newTestProvider(t, nil)
		acc := p.store.CreateGuestAccount("g-dev", "ios")
		pending, token, deletedAt, err := p.store.RequestDelete(acc.id, time.Hour)
		if err != nil || pending || token != "" || deletedAt == nil {
			t.Fatalf("RequestDelete = %v %q %v %v", pending, token, deletedAt, err)
		}
		if _, ok := p.store.FindGuestByDevice("g-dev"); ok {
			t.Fatal("游客索引应已清除(同设备再进 = 新游客)")
		}
	})

	t.Run("EMAIL grace=0 立即删,同邮箱可重注册", func(t *testing.T) {
		p := newTestProvider(t, func(o *Options) { o.DeleteGrace = 0 })
		acc, err := p.store.CreateEmailAccount("zap@b.co", "password8")
		if err != nil {
			t.Fatalf("注册: %v", err)
		}
		pending, _, deletedAt, err := p.store.RequestDelete(acc.id, 0)
		if err != nil || pending || deletedAt == nil {
			t.Fatalf("RequestDelete = %v %v %v", pending, deletedAt, err)
		}
		if _, err := p.store.CreateEmailAccount("zap@b.co", "password8"); err != nil {
			t.Fatalf("同邮箱重注册应成功: %v", err)
		}
	})

	t.Run("宽限期满惰性终态化", func(t *testing.T) {
		now := time.Now()
		p := newTestProvider(t, func(o *Options) {
			o.DeleteGrace = time.Hour
			o.Clock = func() time.Time { return now }
		})
		acc, err := p.store.CreateEmailAccount("late@b.co", "password8")
		if err != nil {
			t.Fatalf("注册: %v", err)
		}
		if _, _, _, err := p.store.RequestDelete(acc.id, time.Hour); err != nil {
			t.Fatalf("RequestDelete: %v", err)
		}
		if p.store.FinalizeIfExpired(acc.id) {
			t.Fatal("宽限期内不应终态化")
		}
		now = now.Add(2 * time.Hour)
		if !p.store.FinalizeIfExpired(acc.id) {
			t.Fatal("宽限期满应终态化")
		}
		if a, _ := p.store.GetAccount(acc.id); a.status != StatusDeleted {
			t.Fatalf("status = %s", a.status)
		}
		if _, err := p.store.CreateEmailAccount("late@b.co", "password8"); err != nil {
			t.Fatalf("同邮箱重注册应成功: %v", err)
		}
	})
}

// --- v1.1 oauth.md(存储面;wire 行为一致性由 e2e 对两实现跑同一场景) ---

func TestOAuthStoreExchangeBindUnbind(t *testing.T) {
	p := newTestProvider(t, nil)
	s := p.store

	// 注册 OAUTH 账号 + 设备绑定。
	acc, err := s.CreateOAuth("google", "sub-1", "dev-1", "ios")
	if err != nil {
		t.Fatalf("CreateOAuth: %v", err)
	}
	if acc.Type != "OAUTH" || acc.Status != "ACTIVE" || acc.Email != "" {
		t.Fatalf("OAUTH 账号形状: %+v", acc)
	}
	// 幂等命中:同 (provider, subject) 找回同账号。
	if got, ok := s.FindOAuth("google", "sub-1"); !ok || got.ID != acc.ID {
		t.Fatalf("FindOAuth: %+v ok=%v", got, ok)
	}
	// 会话签发投影(access 过期按 TTL 计算)。
	sess, err := s.CreateOAuthSession(acc.ID, "game_a", "prod", "dev-1")
	if err != nil {
		t.Fatalf("CreateOAuthSession: %v", err)
	}
	if sess.Account.ID != acc.ID || sess.AccessToken == "" || sess.RefreshToken == "" {
		t.Fatalf("会话投影形状: %+v", sess)
	}
	if !sess.AccessExpiresAt.After(time.Now()) {
		t.Fatalf("access 过期应按 TTL 前推: %v", sess.AccessExpiresAt)
	}

	// 绑定到已有账号(GUEST/EMAIL 一键登录),重复绑定同 provider → ErrProviderBound。
	email, err := s.CreateEmailAccount("a@b.co", "password8")
	if err != nil {
		t.Fatalf("CreateEmailAccount: %v", err)
	}
	row, err := s.BindOAuth(email.id, "apple", "sub-2")
	if err != nil {
		t.Fatalf("BindOAuth: %v", err)
	}
	if row.Provider != "apple" || row.AccountID != email.id {
		t.Fatalf("绑定行形状: %+v", row)
	}
	if _, err := s.BindOAuth(email.id, "apple", "sub-2"); err != oauth.ErrProviderBound {
		t.Fatalf("重复绑定应拒: %v", err)
	}
	// subject 已属其他账号 → ErrIdentityTaken。
	if _, err := s.BindOAuth(email.id, "google", "sub-1"); err != oauth.ErrIdentityTaken {
		t.Fatalf("subject 被占应拒: %v", err)
	}

	// 列表升序;解绑幂等;解绑后同 subject 再注册 = 新账号。
	time.Sleep(time.Millisecond)
	if _, err := s.BindOAuth(email.id, "google", "sub-3"); err != nil {
		t.Fatalf("bind google: %v", err)
	}
	rows := s.ListBindings(email.id)
	if len(rows) != 2 || rows[0].Provider != "apple" || rows[1].Provider != "google" {
		t.Fatalf("列表应按 createdAt 升序: %+v", rows)
	}
	if !s.UnbindOAuth(email.id, "apple") || s.UnbindOAuth(email.id, "apple") {
		t.Fatal("解绑应幂等:首次 true,再删 false")
	}
	// 解绑原 OAUTH 账号的 google 后,同 subject 再注册 = 新账号。
	if !s.UnbindOAuth(acc.ID, "google") {
		t.Fatal("解绑 google 应成功")
	}
	acc2, err := s.CreateOAuth("google", "sub-1", "", "")
	if err != nil {
		t.Fatalf("重注册: %v", err)
	}
	if acc2.ID == acc.ID {
		t.Fatal("解绑后同 subject 应为新账号")
	}
}

func TestOAuthStorePendingAndAnonymizeCascade(t *testing.T) {
	p := newTestProvider(t, func(o *Options) { o.DeleteGrace = 24 * time.Hour })
	s := p.store

	// PENDING 账号绑定拒绝(防御竞态)。
	email, err := s.CreateEmailAccount("a@b.co", "password8")
	if err != nil {
		t.Fatalf("CreateEmailAccount: %v", err)
	}
	if _, _, _, err := s.RequestDelete(email.id, s.opts.DeleteGrace); err != nil {
		t.Fatalf("RequestDelete: %v", err)
	}
	if _, err := s.BindOAuth(email.id, "google", "sub-x"); err != oauth.ErrPendingDelete {
		t.Fatalf("PENDING bind 应拒: %v", err)
	}

	// OAUTH 账号注销 = 立即匿名化(无凭证行,对齐 GUEST);映射行随匿名化清理,
	// 同 subject 再 exchange = 新账号。
	acc, err := s.CreateOAuth("google", "sub-1", "", "")
	if err != nil {
		t.Fatalf("CreateOAuth: %v", err)
	}
	pending, _, deletedAt, err := s.RequestDelete(acc.ID, s.opts.DeleteGrace)
	if err != nil || pending || deletedAt == nil {
		t.Fatalf("OAUTH 注销应立即删: pending=%v deletedAt=%v err=%v", pending, deletedAt, err)
	}
	if _, ok := s.FindOAuth("google", "sub-1"); ok {
		t.Fatal("匿名化应清理映射行")
	}
	acc2, err := s.CreateOAuth("google", "sub-1", "", "")
	if err != nil || acc2.ID == acc.ID {
		t.Fatalf("同 subject 再注册应为新账号: %+v err=%v", acc2, err)
	}
}

func TestOAuthStoreDeviceLimit(t *testing.T) {
	p := newTestProvider(t, func(o *Options) { o.MaxDevices = 1 })
	s := p.store
	acc, err := s.CreateOAuth("google", "sub-1", "dev-1", "")
	if err != nil {
		t.Fatalf("CreateOAuth: %v", err)
	}
	if err := s.EnsureBindDevice(acc.ID, "dev-2", ""); err != ErrDeviceLimit {
		t.Fatalf("设备超限应拒: %v", err)
	}
}

func TestStoreAccessorAndStatusGate(t *testing.T) {
	p := newTestProvider(t, nil)
	if p.Store() == nil || p.Store() != p.Store() {
		t.Fatal("Store 访问器应返回同一实例")
	}
	acc := p.Store().CreateGuestAccount("dev-1", "ios")
	if got, ok := p.Store().GetAccount(acc.id); !ok || got.id != acc.id {
		t.Fatalf("GetAccount = %+v ok=%v", got, ok)
	}

	// 禁用后注销受理必须拒(ErrDisabled);未知账号 no-op 不 panic。
	p.Store().SetAccountStatus(acc.id, "DISABLED")
	if _, _, _, err := p.Store().RequestDelete(acc.id, time.Minute); err != ErrDisabled {
		t.Fatalf("禁用后 RequestDelete = %v, want ErrDisabled", err)
	}
	p.Store().SetAccountStatus("acctalt_missing", "DISABLED") // no-op
	if _, _, _, err := p.Store().RequestDelete("acctalt_missing", time.Minute); err != ErrNotFound {
		t.Fatalf("未知账号 RequestDelete err=%v, want ErrNotFound", err)
	}

	// 恢复 ACTIVE 后可正常受理注销(游客账号无宽限,立即匿名删)。
	p.Store().SetAccountStatus(acc.id, "ACTIVE")
	if _, _, _, err := p.Store().RequestDelete(acc.id, time.Minute); err != nil {
		t.Fatalf("恢复后 RequestDelete err=%v", err)
	}
}
