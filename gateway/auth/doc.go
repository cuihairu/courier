// Package auth 网关认证:凭证 → 身份(M1,批次 3)。
//
// 职责:校验 access token / 游客凭证,向请求上下文注入身份;
// 边界:注册/登录业务经 AccountProvider 接入(providers/),scope 校验见 scope/、middleware/,
// 契约在 docs/contract/auth.md 冻结后在此实现。批次 2 仅立目录。
package auth
