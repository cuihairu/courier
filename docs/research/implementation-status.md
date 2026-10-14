# 实现状态汇总(features.md 76 项对照)

> **口径**(2026-10-11,批次 45):对 [features.md](./features.md) 全量 76 项逐条核对仓库证据;任务条目与状态注记在 [todo.md](../todo.md) 批次 45,本表是汇总视图。
> **更新**(2026-10-12,批次 51):F03 / F35 / F43 由 ❌ → ✅(契约 Frozen v1.1 → 网关 → SDK 六端 → e2e 四批落地,证据 commit 见行内);F11 错误码表 v4→v5。
> **更新**(2026-10-12,批次 52):F01 由 ◑ → ✅(oauth.md Frozen v1 → 网关 → SDK 六端 → e2e 四批落地,证据 commit 见行内);F11 错误码表 v5→v6(+OAUTH 3 码);手机号登录另提同构域,不在本项口径。
> **更新**(2026-10-12,批次 53):F44 由 ❌ → ✅(support.md 附件节 Frozen v1.1 → 网关 → SDK 六端 → e2e 四批落地,证据 commit 见行内);F11 错误码表 v6→v7(+SUPPORT 附件 2 码)。
> **更新**(2026-10-13,批次 54):F36 由 ◑ → ✅(跑马灯呈现落 UI 包 `8127ba9`,零新契约;◑ 清零,统计行顺修批次 53 起的差 1)。
> **更新**(2026-10-13,批次 55):交付面,无 F 项变动——六端 `examples/` + quickstart 实测落地(`425fe55`;附带修网关 `/v1/<cap>` 无斜杠根 307 丢 Authorization → 内部重派 `c5c4814`),六端 README 增「快速开始」;选装清单 `docs/sdk-selection.md`(实测字节 + Provider 降级语义)。
> **更新**(2026-10-13,批次 57):F76 由 ❌ → ✅(announcement.md Frozen v1.1 多语内容节 → 网关 herald Language/GroupKey → SDK 六端 CourierConfig.language 自动附带 → e2e,证据 commit 见行内);零新错误码(errors.md 仍 v8)。
> **更新**(2026-10-13,批次 62):F33 维持 ❌ 但事件面半边已落(网关 `warden.AuditSink` 注入 + `LogSink` 日志半边 + 红线测试,零新错误码);剩余 = 落库 sink 随部署面。
> **更新**(2026-10-13,批次 65):F33 直写半边已落(网关 `warden.HTTPSink`,SIEM 摄取端 / 自建落库代理按 `COURIER_REALNAME_AUDIT_WEBHOOK`(+ `_TOKEN`)注入;尽力交付不阻断实名请求;e2e 经完整装配面实证,零新错误码);剩余 = 保留策略与查询面,归接入方存储实现。
> **更新**(2026-10-13,批次 66):F33 审计面扩到第二实现——接缝抽到共享包 `gateway/audit`(Provider 互不 import,不能挂任一 Provider 下),warden 与 warden-alt 同形状注入 `AuditSink`、同事件序列(verify/curfew/charge_check/playtime_report,status 纯读不留痕);main.go 同源接线,e2e 两实现同断言;零新错误码。
> **更新**(2026-10-14,对账):过期阻塞销项——F13(player.md:5,11 红线「进度不经 Courier」)、F58/F59/F60/F61/F63(features.md §10 定性「玩法系统非服务递送」)决策已落,由「待议」移「不做」;F33 仓内面已全落(批次 62/65/66),余保留策略与查询面归接入方存储(audit.go:3 已注);F14/F15 注明当前契约面无适用面。待议 9→3、不做 7→13。
> **更新**(2026-10-14,批次 67):player 域第二实现 archivist-alt(事件日志模型:变更追加事件、读=投影重放,与 archivist 状态表同契约异构)落地——单测含追加只读/投影重放/注销弃历史异构面,e2e 一致性(两实现同 wire 场景同断言),main 同源接线;零新错误码。
> **更新**(2026-10-14,批次 68):messages 域第二实现 chirp-alt(共享环形日志 + 每连接游标拉取,与 chirp 每订阅者独立缓冲扇出同契约异构)落地——单测含环形淘汰/慢消费者跳窗(at-most-once 缺失段不补发)/订阅点语义,e2e 一致性(两实现同 wire 场景同断言),main 注册 + Notify 钩子随 messages primary 重指(同身份源先例);零新错误码。
> **更新**(2026-10-14,批次 70):assistant 域第二实现 sage-alt(写时预计算规范化文本:AddEntry 时规范化一次,查询零重复计算,与 sage 读时对每个条目重复规范化同契约异构)落地——单测含预计算字段/大小写与首尾空白不敏感/空关键词语义/幂等性,e2e 一致性(两实现同 wire 场景同断言),main 注册;零新错误码。
> **更新**(2026-10-14,批次 71):config 域第二实现 scribe-alt(写时维度索引:Publish 时条件切片转维度→允许值索引,投影按请求维度分阶段过滤,与 scribe 读时逐条目条件 AND 短路同契约异构)落地——单测含索引结构/分阶段过滤/两求值方向等价显式证明,e2e 一致性(两实现同 wire 场景同断言),main 注册 + 维护门回调跟随 app 域 primary 重指(同推送源先例);零新错误码。
> **更新**(2026-10-14,批次 72):payments 域第二实现 teller-alt(追加式事件账本:状态转移追加不可变事件,status/时间戳=投影重放,与 teller 可变状态表同契约异构)落地——单测含账本追加只读/投影重放/两模型操作序列等价显式证明,e2e 一致性(两实现同 wire 场景同断言),main 注册;零新错误码。
> **更新**(2026-10-14,批次 73):support 域第二实现 croupier-alt(写时属主有序索引:建单/追加/回复/关单/附件登记即时重排,游标=`<毫秒>|<工单ID>` 键集锚点,与 croupier 读时全表扫描+偏移游标同契约异构)落地——单测含索引重排/整轮翻页/游标校验/锚点失效空页 + 跨实现分页等价显式证明(同脚本两实现同页序),e2e 一致性(两实现同 wire 场景同断言),main 注册;九域全部两实现,阶段 C「每域至少两种实现」收口;零新错误码。
> **更新**(2026-10-13,批次 56):F45 由 ❌ → ✅(support.md v1.1 举报与处罚申诉节 Frozen → 网关 → SDK 六端 → e2e 分批落地,证据 commit 见行内);F11 错误码表 v7→v8(+SUPPORT 举报/申诉 2 码,42 码)。
> **核对方法**:「已有」声称引用批次号+commit(来自 todo 各批验收记录);「缺口」声称以 grep 机器核对为准(零命中/命中行注明;批次 45 时点 announcement.md `filter` 0 命中、support.md `context` 0 命中、全契约 `language/locale` 0 命中,已分别于批次 51/57 落地);auth.md:10 与 realname.md:100 为契约明示的边界预留。
> **图例**:✅ 已落地(仓库内有契约+实现+测试/e2e 证据,部署面残留另行注明)· ◑ 部分(核心面已落,剩余明示)· ❌ 缺口(仓库内未立项/未实现)· 🔌 Provider(不进核心契约)· ⏸ 待议 · ⛔ 不做。
> **统计**:核心 47 = ✅ 45 + ❌ 2 + ◑ 0(批次 57:F76 落地;批次 54 起◑清零);🔌 13;⏸ 9;⛔ 7。

## 核心面(47 项)

| ID | 功能 | 状态 | 证据 |
| --- | --- | --- | --- |
| F01 | 登录方式聚合 | ✅ | 游客+邮箱密码 auth.md Frozen v1(批次 3,`3c24cfc`/`39b5007`)+ OAuth oauth.md Frozen v1(exchange/bind/unbind/bindings;网关双实现同口径 `689ece9` + SDK 六端 `e22e3d2` + 一致性 e2e ×2 实现;批次 52 契约 `6d2ff22`);手机号另提同构域(不在本项) |
| F02 | 绑定/解绑 | ✅ | auth.md bind/解绑;批次 3/18(`d0936eb`);多正式账号合并不在 v1 |
| F03 | 注销与数据删除 | ✅ | auth.md Frozen v1.1(delete/cancel + 宽限期 + 受理即吊销 + OnAccountDeleted 钩子);网关双实现同口径(`f20e9ab`)+ SDK 六端(`53eaa7c`)+ 一致性 e2e ×2 实现;批次 51(`5317802`) |
| F04 | 跨平台身份贯通 | ✅ | 多设备会话+设备列表 auth.md;批次 3/18 |
| F05 | SDK 不带 UI | ✅ | UPM 四包+TS 三端可选包;批次 5/17/41 |
| F06 | 匿名/静默登录 | ✅ | 游客设备登录 e2e 全链路;批次 3/4(`8b9ed8a`) |
| F08 | 令牌轮换 | ✅ | 单飞刷新+重放一次;批次 3/4/12/14/32;primitives v1.1 并发纪律 |
| F09 | 服务端验签 | ✅ | auth Bearer(批次 3)+回调 HMAC fail-closed(批次 10 `ddcbd03`) |
| F10 | scope 隔离 | ✅ | scope.md Frozen+网关 scope 中间件;批次 2(`cdd391d`) |
| F11 | 统一错误码 | ✅ | errors.md v8 42 码六端生成对差零漂移(v8 举报/申诉 2 码);批次 1/29/51/56(`5317802`/`d02ddcc`) |
| F12 | 凭据管理 | ✅ | 客户端零 Provider 凭证(architecture.md 安全边界) |
| F16 | 玩家档案 | ✅ | player.md Frozen+archivist;批次 8(`72bf15c`);第二实现 archivist-alt(事件日志,同契约可换 + 一致性 e2e)批次 67 |
| F18 | 服务端权威写入 | ✅ | Gateway 唯一入口(批次 2)+支付服务端定价(批次 10) |
| F19 | 商品列表/渠道拉起 | ✅ | payment.md SKUs+payToken 归渠道 SDK;批次 10;真实渠道随部署面 |
| F20 | 收据验真 | ✅ | 回调签名先于 body 解析 fail-closed;批次 10;商店 API 真核验随部署面 |
| F21 | 幂等防重放 | ✅ | 回调幂等不二次发货;批次 10 单测+e2e |
| F22 | 商品一致性 | ✅ | 服务端定价防伪造金额;批次 10 e2e |
| F24 | 恢复购买 | ✅ | `GET /payments/orders`(payment.md:64);批次 10 |
| F25 | 流水对账 | ✅ | Reconcile(payment.md:91);批次 10/11(`79c5bce`);持久化随部署面;第二实现 teller-alt(事件账本投影,同契约可换 + 一致性 e2e)批次 72 |
| F26 | 掉单补发 | ✅ | 断单停 PAID+对账重发 e2e;批次 10/11 |
| F27 | 实名认证 | ✅ | realname.md Frozen+warden;批次 7(`e926626`) |
| F28 | 时段限制 | ✅ | curfew F28 窗口;批次 7 |
| F29 | 充值限额 | ✅ | charge-check F29 分龄限额;批次 7 |
| F30 | 防沉迷上报 | ✅ | playtime-report S2S 幂等月累计;批次 7 |
| F31 | 年龄门/同意(海外) | ❌ | realname.md:100 明示 v1.1 预留 consent 组(出海需求触发再冻结);未立项 |
| F33 | 审计留痕 | ❌(仓内面 ✅) | trace 贯穿已有(layers.md L5);事件面已落(共享接缝 `gateway/audit.AuditSink` 注入,nil = 不产出;事件只含账号维度元数据,身份输入红线不进事件,批次 62);内置两个接收面:`LogSink`(`COURIER_REALNAME_AUDIT=log`)+ `HTTPSink`(`COURIER_REALNAME_AUDIT_WEBHOOK`(+ `_TOKEN`),SIEM/自建落库代理,尽力交付不阻断请求,批次 65);两实现(warden / warden-alt)同形状注入、同事件序列(批次 66);保留策略与查询面归接入方存储实现(audit.go:3 已注)——仓内面已全落,余归接入方 |
| F34 | 公告系统 | ✅ | announcement.md Frozen+herald;批次 6(`38fe759`);第二实现 herald-alt(写时索引,同契约可换 + 一致性 e2e)批次 69 |
| F35 | 公告定向过滤 | ✅ | announcement.md Frozen v1.1 `filters` 开集 map + 四查询维度;herald 匹配 + SDK list 参数(`53eaa7c`)+ e2e 命中/未命中/详情 404;批次 51(`5317802`/`f20e9ab`);herald-alt 匹配同源 + 两实现一致性 e2e 批次 69 |
| F36 | 跑马灯 | ✅ | 数据面零新契约(severity 字段 announcement.md:32);跑马灯呈现随批次 54 落 UI 包(TS 三端 `onMarqueeRendered`/`formatMarquee` + Unity `MarqueeRendered`/`AnnouncementText`,packcheck 结构守卫,`8127ba9`) |
| F41 | 工单 | ✅ | support.md Frozen+croupier;批次 6;第二实现 croupier-alt(写时属主有序索引 + 键集游标,同契约可换 + 一致性 e2e)批次 73 |
| F42 | FAQ 自助 | ✅ | FAQ+sage 检索;批次 6/9(`2481383`);第二实现 sage-alt(写时预计算规范化,同契约可换 + 一致性 e2e)批次 70 |
| F43 | 上下文附带 | ✅ | support.md Frozen v1.1 `context` 开集 map 服务端零解释透传;croupier 校验 + SDK 六端(`53eaa7c`)+ e2e 回显与 400;批次 51(`5317802`/`f20e9ab`) |
| F44 | 反馈+日志上传 | ✅ | support.md 附件节 Frozen v1.1(批次 53 `9954281`:单端点申请直传 + 预签名 URL + 限额/allowlist/归属/状态 + fail-closed 存储);croupier `Signer` 接口 + 详情 `attachments[]` 加法(`035125d`)+ SDK 六端(`6dbfca0`)+ e2e(`804e93d`);diagnostics 自有面不变;第二实现 croupier-alt(附件面逐字同源,登记即重排属主索引)批次 73 |
| F45 | 举报/申诉 | ✅ | support.md v1.1 举报与处罚申诉节 Frozen(批次 56 `d02ddcc`:reports/penalties/appeal 四端点;**判定与执行全在管理面**,玩家侧只读投影 + 申诉入口;进展不向举报人反馈);croupier `PenaltyStore` 投影源 + fail-closed 501 缺省 + e2e(`96d8295`)+ SDK 六端(`de4dc3d`);errors v8 +2 码;第二实现 croupier-alt(写时属主索引 + 键集游标,同契约可换 + 一致性 e2e)批次 73 |
| F46 | 客服补单 | ✅ | Reconcile 补发通道;批次 10/11;工具面归 croupier |
| F47 | 远程配置 | ✅ | config.md Frozen+scribe 条件投影热生效;批次 8(`33c5c18`);第二实现 scribe-alt(写时维度索引,同契约可换 + 一致性 e2e)批次 71 |
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
| F76 | 多语言本地化 | ✅ | announcement.md Frozen v1.1 多语内容节(批次 57 `65d9971`:单语分条模型契约化,行内 `language`(RFC 5646)/`groupKey` 可选元数据,零新错误码;可见性仍由 filters 决定)+ herald 两字段回显 + e2e(`65d9971`)+ SDK 六端 `CourierConfig.language` 自动附带(显式覆写,缺省不附带)(`478c194`) |

## Provider 面(13 项,不进核心契约)

| ID | 功能 | 落点口径 |
| --- | --- | --- |
| F07 | 账号风控与防刷 | 登录限流内建(批次 3);分级风控归 Provider;支付风控为预留钩子 |
| F17 | 二进制文件存储 | Provider 化;support 附件同口径 |
| F23 | 订阅状态管理 | 随真实渠道接入(部署面) |
| F32 | 数据最小化/共享清单 | 红线内建契约(默认关闭/字段最小化);清单输出归 Provider |
| F37 | 远程推送 | 应用内 SSE 已落(批次 6/35-40 六端流读);第二实现 chirp-alt(环形日志+游标,同契约可换 + 一致性 e2e)批次 68;APNs/FCM 随部署面 |
| F38 | 按账号/标签定向推送 | Provider |
| F39 | 本地通知/定时提醒 | Provider |
| F40 | 触达效果统计 | Provider |
| F49 | A/B 实验 | Provider;契约不建(features.md §14) |
| F52 | 埋点与行为分析 | Provider;不自建分析产品 |
| F53 | 付费与广告收入统计 | Provider |
| F55 | 崩溃与性能监控 | diagnostics 可选包直发自托管端点(Sentry/GlitchTip/OTLP) |
| F57 | 数据导出 | Provider |

## 待议(3 项)与不做(13 项)

| ID | 功能 | 处置 |
| --- | --- | --- |
| F14/F15 | 分级权限/写锁合并 | ⏸ 待议——当前契约面无适用面(档案仅 displayName/avatarUrl 无分层;唯一写 PATCH displayName 无并发冲突面),边界待议 |
| F13 | 云存档/进度同步 | ⛔ 不做——player.md:5,11 红线「进度不经 Courier,角色数据归各游戏服」,边界已定 |
| F58/F59/F60/F61/F63 | 排行榜/成就/Stats/赛季/活动 | ⛔ 不做——features.md §10 定性「玩法系统非服务递送」,防范围失控(§10 结论⑤),边界已定 |
| F62 | 邮件/礼包码 | ⏸ 待议优先评估——与消息面相邻 |
| F51 | GM/运营工具界面 | ⛔ 不做界面——croupier API 面保留,界面归 Provider 管理面 |
| F54 | 买量归因 | ⛔ 不做——垂直领域 |
| F64–F68 | 社交与实时(好友/聊天/匹配/语音/公会) | ⛔ 不做——roadmap 非目标,与 Nakama/Agones 划界 |

## 缺口汇总(❌ 2 项;截至批次 57,F03/F35/F43/F01/F44/F36/F45/F76 已落地移出)

| ID | 缺口 | 建议落点 |
| --- | --- | --- |
| F31 | 年龄门/家长同意(海外) | realname v1.1 consent 组(realname.md:100 已预留) |
| F33 | 审计落库 | 仓内面已全落(批次 62/65/66:`gateway/audit.AuditSink` + `LogSink` + `HTTPSink`,两实现同形状);剩保留策略与查询面,归接入方存储实现(audit.go:3 已注)——卡接入方存储形态拍板 |
