// Package session 网关会话:token 轮换与吊销的网关侧配合点(M1,批次 3)。
//
// 职责:会话状态解析与吊销信号;数据事实留在 AccountProvider 实现侧,网关不落业务数据。
// 契约在 docs/contract/auth.md 冻结后在此实现。批次 2 仅立目录。
package session
