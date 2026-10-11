# 实现状态汇总(features.md 76 项对照)

> **口径**(2026-10-11,批次 45):对 [features.md](./features.md) 全量 76 项逐条核对仓库证据;任务条目与状态注记在 [todo.md](../todo.md) 批次 45,本表是汇总视图。
> **核对方法**:「已有」声称引用批次号+commit(来自 todo 各批验收记录);「缺口」声称以 grep 机器核对为准(零命中/命中行注明,如 announcement.md `filter` 0 命中、support.md `context` 0 命中、auth.md 仅 `DELETE /devices`、全契约 `language/locale` 0 命中);auth.md:10 与 realname.md:100 为契约明示的边界预留。
> **图例**:✅ 已落地(仓库内有契约+实现+测试/e2e 证据,部署面残留另行注明)· ◑ 部分(核心面已落,剩余明示)· ❌ 缺口(仓库内未立项/未实现)· 🔌 Provider(不进核心契约)· ⏸ 待议 · ⛔ 不做。
> **统计**:核心 47 = ✅ 37 + ❌ 10;🔌 13;⏸ 9;⛔ 7。

## 核心面(47 项)

| ID | 功能 | 状态 | 证据 |
| --- | --- | --- | --- |
| F01 | 登录方式聚合 | ◑ | 游客+邮箱密码落地:auth.md Frozen v1,批次 3(`3c24cfc`/`39b5007`);第三方 OAuth/手机号 auth.md:10 明示不覆盖(后续域契约扩展);OAuth 新域草案已写(批次 49,oauth.md Draft),手机号另提同构域 |
| F02 | 绑定/解绑 | ✅ | auth.md bind/解绑;批次 3/18(`d0936eb`);多正式账号合并不在 v1 |
| F03 | 注销与数据删除 | ❌ | 仅设备解绑(auth.md:24);账号注销端点未建——v1.1 提案 Draft 已写(批次 47) |
| F04 | 跨平台身份贯通 | ✅ | 多设备会话+设备列表 auth.md;批次 3/18 |
| F05 | SDK 不带 UI | ✅ | UPM 四包+TS 三端可选包;批次 5/17/41 |
| F06 | 匿名/静默登录 | ✅ | 游客设备登录 e2e 全链路;批次 3/4(`8b9ed8a`) |
| F08 | 令牌轮换 | ✅ | 单飞刷新+重放一次;批次 3/4/12/14/32;primitives v1.1 并发纪律 |
| F09 | 服务端验签 | ✅ | auth Bearer(批次 3)+回调 HMAC fail-closed(批次 10 `ddcbd03`) |
| F10 | scope 隔离 | ✅ | scope.md Frozen+网关 scope 中间件;批次 2(`cdd391d`) |
| F11 | 统一错误码 | ✅ | errors.md v4 33 码六端生成对差零漂移;批次 1/29 |
| F12 | 凭据管理 | ✅ | 客户端零 Provider 凭证(architecture.md 安全边界) |
| F16 | 玩家档案 | ✅ | player.md Frozen+archivist;批次 8(`72bf15c`) |
| F18 | 服务端权威写入 | ✅ | Gateway 唯一入口(批次 2)+支付服务端定价(批次 10) |
| F19 | 商品列表/渠道拉起 | ✅ | payment.md SKUs+payToken 归渠道 SDK;批次 10;真实渠道随部署面 |
| F20 | 收据验真 | ✅ | 回调签名先于 body 解析 fail-closed;批次 10;商店 API 真核验随部署面 |
| F21 | 幂等防重放 | ✅ | 回调幂等不二次发货;批次 10 单测+e2e |
| F22 | 商品一致性 | ✅ | 服务端定价防伪造金额;批次 10 e2e |
| F24 | 恢复购买 | ✅ | `GET /payments/orders`(payment.md:64);批次 10 |
| F25 | 流水对账 | ✅ | Reconcile(payment.md:91);批次 10/11(`79c5bce`);持久化随部署面 |
| F26 | 掉单补发 | ✅ | 断单停 PAID+对账重发 e2e;批次 10/11 |
| F27 | 实名认证 | ✅ | realname.md Frozen+warden;批次 7(`e926626`) |
| F28 | 时段限制 | ✅ | curfew F28 窗口;批次 7 |
| F29 | 充值限额 | ✅ | charge-check F29 分龄限额;批次 7 |
| F30 | 防沉迷上报 | ✅ | playtime-report S2S 幂等月累计;批次 7 |
| F31 | 年龄门/同意(海外) | ❌ | realname.md:100 明示 v1.1 预留 consent 组(出海需求触发再冻结);未立项 |
| F33 | 审计留痕 | ❌ | trace 贯穿已有(layers.md L5);审计落库随部署面 |
| F34 | 公告系统 | ✅ | announcement.md Frozen+herald;批次 6(`38fe759`) |
| F35 | 公告定向过滤 | ❌ | v1 无 filters;v1.1 提案 Draft 已写(批次 47,复用 config 投影语义) |
| F36 | 跑马灯 | ◑ | 数据面零新契约(severity 字段 announcement.md:32);跑马灯呈现归 UI 包未做 |
| F41 | 工单 | ✅ | support.md Frozen+croupier;批次 6 |
| F42 | FAQ 自助 | ✅ | FAQ+sage 检索;批次 6/9(`2481383`) |
| F43 | 上下文附带 | ❌ | v1.1 提案 Draft 已写(批次 47);现 extras 承载 |
| F44 | 反馈+日志上传 | ❌ | support.md:11 明示附件后续扩展;v1.1 提案 Draft 已写(批次 50,presigned 直传面);diagnostics 自有面不变 |
| F45 | 举报/申诉 | ❌ | 无契约面——v1.1 提案 Draft 已写(批次 48,reports/penalties/appeal 四端点) |
| F46 | 客服补单 | ✅ | Reconcile 补发通道;批次 10/11;工具面归 croupier |
| F47 | 远程配置 | ✅ | config.md Frozen+scribe 条件投影热生效;批次 8(`33c5c18`) |
| F48 | 条件定向 | ✅ | 五维匹配;批次 8 |
| F50 | 版本/回滚 | ✅ | 版本单调+回滚=历史内容再发布(config.md:11);批次 8 |
| F56 | SDK 遥测 | ✅ | diagnostics.md Frozen+可选包默认全关;批次 8/20/41 |
| F69 | 多引擎一等公民 | ✅ | contractgen 六端生成+CI 门禁;批次 1/19 |
| F70 | 模块化选装 | ✅ | UPM 四包+三端可选包对齐;批次 5/17/41;选装呈现=批次 44 |
| F71 | UI 可选 | ✅ | 同 F05;批次 5/17/41 |
| F72 | 初始化/生命周期 | ✅ | 状态机九态十触发六端同构;批次 4/13/14/32 |
| F73 | 弱网/离线降级 | ✅ | 退避重试+config 缓存+拉取兜底;批次 4/6/12 |
| F74 | 日志诊断开关 | ✅ | diagnostics 开关+traceId;批次 8/20 |
| F75 | 沙箱环境 | ✅ | scope 三环境+fixtures;批次 1/2 |
| F76 | 多语言本地化 | ❌ | 契约无语种/locale 字段(grep 零命中);v1.1 提案 Draft 已写(批次 48,单语分条契约化) |

## Provider 面(13 项,不进核心契约)

| ID | 功能 | 落点口径 |
| --- | --- | --- |
| F07 | 账号风控与防刷 | 登录限流内建(批次 3);分级风控归 Provider;支付风控为预留钩子 |
| F17 | 二进制文件存储 | Provider 化;support 附件同口径 |
| F23 | 订阅状态管理 | 随真实渠道接入(部署面) |
| F32 | 数据最小化/共享清单 | 红线内建契约(默认关闭/字段最小化);清单输出归 Provider |
| F37 | 远程推送 | 应用内 SSE 已落(批次 6/35-40 六端流读);APNs/FCM 随部署面 |
| F38 | 按账号/标签定向推送 | Provider |
| F39 | 本地通知/定时提醒 | Provider |
| F40 | 触达效果统计 | Provider |
| F49 | A/B 实验 | Provider;契约不建(features.md §14) |
| F52 | 埋点与行为分析 | Provider;不自建分析产品 |
| F53 | 付费与广告收入统计 | Provider |
| F55 | 崩溃与性能监控 | diagnostics 可选包直发自托管端点(Sentry/GlitchTip/OTLP) |
| F57 | 数据导出 | Provider |

## 待议(9 项)与不做(7 项)

| ID | 功能 | 处置 |
| --- | --- | --- |
| F13/F14/F15 | 云存档/分级权限/写锁合并 | ⏸ 待议——存档面留 Provider,不做统一钱包式假设 |
| F58/F59/F60/F61/F63 | 排行榜/成就/Stats/赛季/活动 | ⏸ 待议——玩法非服务,防范围失控(features.md §13 结论⑤) |
| F62 | 邮件/礼包码 | ⏸ 待议优先评估——与消息面相邻 |
| F51 | GM/运营工具界面 | ⛔ 不做界面——croupier API 面保留,界面归 Provider 管理面 |
| F54 | 买量归因 | ⛔ 不做——垂直领域 |
| F64–F68 | 社交与实时(好友/聊天/匹配/语音/公会) | ⛔ 不做——roadmap 非目标,与 Nakama/Agones 划界 |

## 缺口汇总(❌ 10 项;截至批次 49 契约面提案全部已写 Draft,进入评审与冻结阶段)

| ID | 缺口 | 建议落点 |
| --- | --- | --- |
| F01 | 第三方 OAuth/手机号登录 | 独立域 oauth.md(提案 Draft 已写,批次 49);手机号另提同构域 |
| F03 | 账号注销+数据级联删除 | auth 契约 v1.1(商店政策硬门槛;提案 Draft 已写,批次 47) |
| F31 | 年龄门/家长同意(海外) | realname v1.1 consent 组(realname.md:100 已预留) |
| F33 | 审计落库 | 部署面;trace 已贯穿 |
| F35 | 公告定向过滤 | announcement v1.1 `filters`(提案 Draft 已写,批次 47) |
| F36 | 跑马灯呈现 | UI 包 severity 渲染(零新契约) |
| F43 | 工单上下文附带 | support v1.1 `context`(提案 Draft 已写,批次 47) |
| F44 | 反馈+日志上传 | support v1.1 presigned 直传(提案 Draft 已写,批次 50;diagnostics 联动不在契约面) |
| F45 | 举报/处罚申诉 | support v1.1 端点组(提案 Draft 已写,批次 48) |
| F76 | 多语言与本地化 | announcement v1.1 语种维度(提案 Draft 已写,批次 48,单语分条契约化) |
