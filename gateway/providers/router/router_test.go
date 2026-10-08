// 治理引擎验收:降级链顺序、熔断开路/半开恢复、业务结果不计熔断、
// 安全态(PENDING_REVIEW / 不可玩 / 不放行)、路由表原子热重载(在途按旧表完成)。
// 对齐 docs/contract/realname.md「Provider 热插拔」与 architecture.md「Provider 治理」。
package router

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fake 可编程供应商:err 非 nil = 调用失败(计熔断);block 非 nil 时挂起直到关闭。
type fake struct {
	name  string
	mu    sync.Mutex
	err   error // mu 保护(nil = 健康)
	calls atomic.Int32
	block chan struct{}
}

func newFake(name string) *fake {
	f := &fake{name: name, block: make(chan struct{})}
	close(f.block) // 默认不挂起
	return f
}

func (f *fake) failWith(err error) { f.mu.Lock(); f.err = err; f.mu.Unlock() }

func (f *fake) failed() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

func (f *fake) callCount() int32 { return f.calls.Load() }

func (f *fake) Name() string { return f.name }
func (f *fake) Verify(string, IdentityInput) (VerifyOutcome, error) {
	f.calls.Add(1)
	<-f.block
	if e := f.failed(); e != nil {
		return VerifyOutcome{}, e
	}
	return VerifyOutcome{State: "VERIFIED"}, nil
}
func (f *fake) Query(string) (StatusOutcome, error) {
	f.calls.Add(1)
	<-f.block
	if e := f.failed(); e != nil {
		return StatusOutcome{}, e
	}
	return StatusOutcome{State: "VERIFIED", HasVerified: true, VerifiedAt: time.Unix(0, 0)}, nil
}
func (f *fake) Curfew(string, time.Time) (CurfewOutcome, error) {
	f.calls.Add(1)
	if e := f.failed(); e != nil {
		return CurfewOutcome{}, e
	}
	return CurfewOutcome{Playable: true}, nil
}
func (f *fake) ChargeCheck(string, int) (ChargeOutcome, error) {
	f.calls.Add(1)
	if e := f.failed(); e != nil {
		return ChargeOutcome{}, e
	}
	return ChargeOutcome{Allowed: true}, nil
}
func (f *fake) HealthCheck() bool { return true }

var errDown = errors.New("provider down")

func TestPrimaryHealthy_AllCallsGoPrimary(t *testing.T) {
	p, f1 := newFake("primary"), newFake("fb1")
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{f1}})

	out := r.Verify("acc_1", IdentityInput{Name: "张三", IDNumber: "110101199001011234"})

	if out.State != "VERIFIED" {
		t.Fatalf("state = %q, want VERIFIED", out.State)
	}
	if got := p.callCount(); got != 1 {
		t.Errorf("primary calls = %d, want 1", got)
	}
	if got := f1.callCount(); got != 0 {
		t.Errorf("fallback calls = %d, want 0(主健康不碰 fallback)", got)
	}
}

func TestPrimaryFails_FallbackTriedInOrder(t *testing.T) {
	p, f1, f2 := newFake("primary"), newFake("fb1"), newFake("fb2")
	p.failWith(errDown)
	f1.failWith(errDown)
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{f1, f2}})

	out := r.Query("acc_1")

	if out.State != "VERIFIED" {
		t.Fatalf("state = %q, want VERIFIED(第二 fallback 命中)", out.State)
	}
	for name, want := range map[string]int32{"primary": 1, "fb1": 1, "fb2": 1} {
		var got int32
		switch name {
		case "primary":
			got = p.callCount()
		case "fb1":
			got = f1.callCount()
		case "fb2":
			got = f2.callCount()
		}
		if got != want {
			t.Errorf("%s calls = %d, want %d", name, got, want)
		}
	}
}

func TestAllFail_VerifyFallsToPendingReview(t *testing.T) {
	p, f1 := newFake("primary"), newFake("fb1")
	p.failWith(errDown)
	f1.failWith(errDown)
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{f1}})

	out := r.Verify("acc_1", IdentityInput{})

	// 契约安全态:不通过也不拒绝放量,转人工/延迟复核。
	if out.State != "PENDING_REVIEW" {
		t.Fatalf("state = %q, want PENDING_REVIEW", out.State)
	}
	if r.Curfew("acc_1", time.Now()).Playable {
		t.Error("全链失败 curfew 应不可玩(保守面)")
	}
	if r.ChargeCheck("acc_1", 100).Allowed {
		t.Error("全链失败 charge 应不放行")
	}
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	p, fb := newFake("primary"), newFake("fb")
	p.failWith(errDown)
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{fb},
		Breaker: BreakerConfig{Failures: 3, OpenCalls: 100}})

	for i := 0; i < 3; i++ {
		r.Verify("acc_1", IdentityInput{})
	}
	if got := p.callCount(); got != 3 {
		t.Fatalf("primary calls = %d, want 3(阈值内每链都打到主)", got)
	}
	// 已连续 3 次失败 → 开路;后续调用不再打主,直接 fallback。
	r.Verify("acc_1", IdentityInput{})
	r.Query("acc_1")
	if got := p.callCount(); got != 3 {
		t.Errorf("开路后 primary calls = %d, want 3(不再增长)", got)
	}
	if got := fb.callCount(); got != 5 {
		t.Errorf("fallback calls = %d, want 5", got)
	}
}

func TestBreakerHalfOpenRecoversOnSuccess(t *testing.T) {
	p, fb := newFake("primary"), newFake("fb")
	p.failWith(errDown)
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{fb},
		Breaker: BreakerConfig{Failures: 2, OpenCalls: 3}})

	for i := 0; i < 2; i++ {
		r.Verify("acc_1", IdentityInput{})
	} // 连续 2 次失败 → 开路;p.calls = 2
	r.Query("acc_1")
	r.Query("acc_1") // 开路期 tick 1、2:拒绝,不打主
	if got := p.callCount(); got != 2 {
		t.Fatalf("开路期 tick<阈值不应打主:calls = %d, want 2", got)
	}

	p.failWith(nil) // 主恢复;下一次调用 = tick 3 = 半开探测
	out := r.Query("acc_1")

	if got := p.callCount(); got != 3 {
		t.Fatalf("半开探测应打主一次:calls = %d, want 3", got)
	}
	if out.State != "VERIFIED" {
		t.Fatalf("探测成功应回 closed:state = %q", out.State)
	}
	// 恢复后:再次调用直接走主。
	r.Verify("acc_1", IdentityInput{})
	if got := p.callCount(); got != 4 {
		t.Errorf("恢复后应直连主:calls = %d, want 4", got)
	}
}

func TestBusinessResultNotCountedAsFailure(t *testing.T) {
	p := newFake("primary")
	r := New(Config{Primary: p, Breaker: BreakerConfig{Failures: 2, OpenCalls: 100}})
	// 业务「不通过」用 error 表达会被误计;契约规定业务结果经 Outcome 返回、
	// error 只留调用失败——err=nil 的业务结果冲 5 次,不应开路。
	for i := 0; i < 5; i++ {
		out := r.Query("acc_1")
		if out.State != "VERIFIED" {
			t.Fatalf("state = %q", out.State)
		}
	}
	if got := p.callCount(); got != 5 {
		t.Errorf("业务成功不应开路:calls = %d, want 5", got)
	}
}

func TestSnapshotReflectsChain(t *testing.T) {
	p, f1, dup := newFake("primary"), newFake("fb1"), newFake("primary")
	r := New(Config{Primary: p, Fallbacks: []RealNameProvider{f1, dup}})

	got := r.Snapshot()
	if len(got) != 2 || got[0] != "primary" || got[1] != "fb1" {
		t.Errorf("snapshot = %v, want [primary fb1](同名去重)", got)
	}
}

func TestReloadAtomic_InFlightCompletesOnOldTable(t *testing.T) {
	old := newFake("old-primary")
	old.block = make(chan struct{}) // 挂起在途
	r := New(Config{Primary: old})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		out := r.Query("acc_1")
		if out.State != "VERIFIED" {
			t.Errorf("在途请求应按旧表完成:state = %q", out.State)
		}
		close(release)
	}()

	// 等在途已进入 old(计数已 +1)后热重载。
	for old.callCount() == 0 {
		time.Sleep(time.Millisecond)
	}
	r.Reload(Config{Primary: newFake("new-primary")})
	if got := r.Snapshot(); len(got) != 1 || got[0] != "new-primary" {
		t.Fatalf("snapshot after reload = %v", got)
	}
	close(old.block) // 放行在途
	<-release
	wg.Wait()

	// 新请求走新表(不再打 old)。
	r.Query("acc_2")
	if got := old.callCount(); got != 1 {
		t.Errorf("old calls = %d, want 1(重载后不再触达)", got)
	}
}

func TestEmptyChain_AllSafeStates(t *testing.T) {
	r := New(Config{})
	if got := r.Verify("a", IdentityInput{}).State; got != "PENDING_REVIEW" {
		t.Errorf("empty verify = %q, want PENDING_REVIEW", got)
	}
	if r.Curfew("a", time.Now()).Playable {
		t.Error("empty curfew 应不可玩")
	}
	if r.ChargeCheck("a", 1).Allowed {
		t.Error("empty charge 应不放行")
	}
}
