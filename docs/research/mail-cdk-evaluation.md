[English](./mail-cdk-evaluation.en.md) | [中文](./mail-cdk-evaluation.md)

# F62 邮件/礼包码:评估结论

> 状态:**评估完成,不立项** · 2026-10-16 巡检 FW1016D 代决 · 依据 [features.md §10](./features.md)「仅 F62 邮件/礼包码值得优先评估」。
> 结论先行:**契约面与既有面同构度高,立项成本低;但运营需求未触发,维持不立项。解锁条件不变(运营侧实际需求触发)。** 评估落地后,立项 = 按草图冻结,非从零设计。

## 代决记录(2026-10-16,巡检 FW1016D)

- **决定**:完成评估(本档),不立项。
- **理由**:F62 属「需求取舍」卡点(需运营需求触发);features.md §10 结论「本节是玩法不是服务,全列待议防范围失控」是既有裁决,本评估不重开它——评估本身是该节明示的合法动作(「仅 F62 值得优先评估」)。
- **推翻路径**:运营需求落地 → 按本档草图冻结立项;拍板人亦可直接否决该方向,本档归档即可。

## 需求拆解(运营侧发,玩家侧收)

| 能力 | 说明 | 与既有面关系 |
| --- | --- | --- |
| 全服邮件 | 运营群发公告型 / 带附件邮件 | 公告(herald)同构:发布一次,玩家侧按游标拉取 |
| 单人邮件 | 定向补偿 / 客服附函 | 工单附件(croupier)同构:属主索引 + 附件 |
| 礼包码(兑换码) | 一码一换 / 一码限次 | 幂等领取与支付域(payment)同构 |

## 契约草图(评估用,未冻结)

```text
mail 域(挂载 /v1/mail/*):
  GET  /v1/mail              游标分页 + unread 计数
  GET  /v1/mail/{id}         详情
  POST /v1/mail/{id}/claim   附件领取(幂等:重复领取同结果)
  DELETE /v1/mail/{id}       已读/删除(软删,游标语义不变)
  推送事件 mail.updated       复用推送通道(同 config.updated / realname.consent.updated 形状)

cdk 域(挂载 /v1/cdk/*):
  POST /v1/cdk/redeem        { code } → 兑换结果(幂等键 = account + code)
```

- 全服邮件**不做服务端扇出**:玩家侧拉取时按 audience(all | segment)过滤,公告同款;避免 O(全服) 写放大。
- 附件模型与 payment 域货币/物品语义对齐,**不引入第二套价值单位**(价值发放仍经支付域语义,邮件只是载体)。
- 兑换码防刷:既有全局限流 + 失败退避覆盖;code 一次性(原子核销)或限次(计数),语义归 Provider。

## 同构度(立项成本)

| 立项要素 | 复用 | 新增面 |
| --- | --- | --- |
| 分页 | primitives 游标(cursor) | 无 |
| 推送 | chirp SSE / 公告推送通道 | `mail.updated` 事件名 |
| 幂等领取 | payment claim 语义 | mail/claim 幂等键 |
| 审计 | `gateway/audit.AuditSink` 接缝 | mail.claimed / cdk.redeemed 事件类型 |
| 六端生成 | tools/contractgen 全自动 | fixture 增补 |
| 两实现收口 | 九域两实现模式 | 每域 2 个 Provider 实现 |

## 边界(不做的部分)

- **发放 / 运营面不进网关**:运营端任何界面是 [roadmap 非目标](../roadmap.md);发信、码批次生成、核销报表归运营工具 / 接入方,经 Provider S2S 或直写其存储。
- 邮件不做富文本渲染规范(客户端 UI 自由);不做跨端已读同步之外的消息语义(与 messages/chirp 划界:mail 是运营触达存储面,chirp 是会话推送通道)。

## 立项触发后的步骤清单

1. 草图评审 → mail.md / cdk.md 冻结(错误码进 errors.md v9)。
2. fixtures → `go run ./tools/contractgen -write` 六端再生成。
3. 网关两 Provider 实现同形状 + 审计事件。
4. 六端 SDK + UI(邮件列表 / 领取 / 兑换入口)+ e2e。
5. 台账按批次惯例分批落地。
