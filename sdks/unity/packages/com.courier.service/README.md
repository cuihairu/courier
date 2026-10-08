# Courier Service

能力服务包(L4 消费侧,`com.courier.service`),依赖 `com.courier.core`。

M2 批次落位:

- 公告域(herald 默认 Provider)
- 客服域(croupier 默认 Provider)
- 推送域(chirp 默认 Provider;关闭 = 仅拉取兜底)

Provider 原则:**默认提供,可换可关**(docs/layers.md);本包只消费 L1 契约,不感知供应商实现。
