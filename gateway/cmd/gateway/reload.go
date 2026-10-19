// SIGHUP 配置热重载(F78 一期,批次 99;方案已代决:信号驱动,零新攻击面——
// 不设管理端点;文件 watch 与管理端点(前置=管理面鉴权契约)留二期)。
//
// 启动装配与 SIGHUP 重载共用同一条 mergeConfig + followTargets.apply 路径:
// 原启动代码只在首次读配置时做 default→alt 单向重指,重载回默认 primary
// 必须能指回,否则换表后验证器/推送源/维护门残留在旧实现上。
package main

import (
	"sync/atomic"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/providers/account"
	"github.com/cuihairu/courier/gateway/providers/accountalt"
	"github.com/cuihairu/courier/gateway/providers/chirp"
	"github.com/cuihairu/courier/gateway/providers/chirpalt"
	"github.com/cuihairu/courier/gateway/providers/scribe"
	"github.com/cuihairu/courier/gateway/providers/scribealt"
)

// atomicFn 原子函数指针:运行期重指的接缝(同 auth.Switchable 先例;
// 闭包捕获普通变量在运行期改写会数据竞争)。
type atomicFn[T any] struct{ v atomic.Pointer[T] }

func newAtomicFn[T any](initial T) *atomicFn[T] {
	a := &atomicFn[T]{}
	a.Set(initial)
	return a
}

func (a *atomicFn[T]) Set(f T) { a.v.Store(&f) }

func (a *atomicFn[T]) Get() T { return *a.v.Load() }

// followTargets 跟随对应能力域 primary 的运行期接缝:身份验证器(identity)、
// 推送源(messages)、维护门(app)。验证器与主存同源、推送源与流端点同源、
// 维护门读数同源——换 primary 不重指则新实现名存实亡(同启动装配注释口径)。
type followTargets struct {
	identity      *auth.Switchable
	idVerifier    auth.Verifier // courier-account
	idAltVerifier auth.Verifier // courier-account-alt
	publish       *atomicFn[func(string, any)]
	pubDefault    func(string, any) // chirp
	pubAlt        func(string, any) // chirp-alt
	maint         *atomicFn[func() bool]
	maintDefault  func() bool // scribe
	maintAlt      func() bool // scribe-alt
}

// apply 按用户路由表重指三接缝;只认两实现注册名——Disabled、空 primary、
// 未知名字一律不动(与启动装配同口径,fallbacks 不参与)。
func (ft *followTargets) apply(user providers.Config) {
	if cc, ok := user[providers.CapIdentity]; ok {
		switch cc.Primary {
		case accountalt.DefaultName:
			ft.identity.SetTarget(ft.idAltVerifier)
		case account.DefaultName:
			ft.identity.SetTarget(ft.idVerifier)
		}
	}
	if cc, ok := user[providers.CapMessages]; ok {
		switch cc.Primary {
		case chirpalt.DefaultName:
			ft.publish.Set(ft.pubAlt)
		case chirp.DefaultName:
			ft.publish.Set(ft.pubDefault)
		}
	}
	if cc, ok := user[providers.CapApp]; ok {
		switch cc.Primary {
		case scribealt.DefaultName:
			ft.maint.Set(ft.maintAlt)
		case scribe.DefaultName:
			ft.maint.Set(ft.maintDefault)
		}
	}
}

// mergeConfig 内置默认表 + 用户表覆盖(用户缺席的域保留默认)。
func mergeConfig(defaults, user providers.Config) providers.Config {
	merged := make(providers.Config, len(defaults)+len(user))
	for cap, cc := range defaults {
		merged[cap] = cc
	}
	for cap, cc := range user {
		merged[cap] = cc
	}
	return merged
}
