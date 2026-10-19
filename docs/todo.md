
# Courier TODO — 需求清单

> 只列需求,一行一句;已完成项保留一行证据(commit + 测试)。F01–F76 逐项状态与落点见 [implementation-status.md](./research/implementation-status.md);阶段路线见 [roadmap](./roadmap.md);分层映射见 [layers.md](./layers.md)。

> **复核**(2026-10-16,巡检 P1016AM):逐条复核未完成与待议 3 项(F31/F33/F62),解锁条件均未解除、无合法开工项;`docs/roadmap.md` 仍无未勾选项;不重复长报告——F31 缺出海立项拍板,F33 缺接入方存储形态+保留天数+查询权限边界,F62 缺运营侧实际需求触发。已裁决项未重开。
> **复核**(2026-10-16,巡检 PATROL1016):3 条未完成项逐条复核解锁条件,结论全部仍成立,无合法开工项——F31 `contract/realname.md:100/171` v1.1 预留「只加不改」口径未变、仓内无出海立项决策;F33 `gateway/audit/audit.go:3` 落库归实现方口径未变、无接入方存储形态输入;F62 `research/implementation-status.md:104` 仍 `⏸ 待议`、无运营需求触发。已裁决项未重开(卡点类型与解锁条件由上轮 498b3ae 就地标注)。
>
> **复核**(2026-10-16,巡检 FW1016E):3 条未完成项逐条复核,结论仍成立,无合法开工项(上轮 FW1016D 代决后状态复核)——F31 `contract/realname.md:100/171` v1.1 预留「出海需求触发再冻结」口径未变、仓内仍无出海立项决策(设计面已由 FW1016D 落草案,冻结与实现待拍板);F33 `gateway/audit/audit.go:3`「落库(保留策略 / 查询 / 合规导出)由实现方负责」口径未变、无接入方存储形态(DB/SIEM/OSS)输入(`provider-guide.md:74` 仅描述现有 sink 注入面,非新决策);F62 `research/implementation-status.md:105` 仍 `✅ 已代决`(评估完成,不立项)、无运营需求触发;`docs/roadmap.md` 无未勾选项。已裁决项未重开(FW1016D 代决结论沿用,推翻路径见各自仓内记录)。

## 未完成

- [ ] F31 实现面(网关两实现 + 六端 SDK + e2e):consent 组契约已于 2026-10-18 代决冻结为 realname v1.1(批次 94,只加不改、零新增错误码);按 research/consent-v1.1-draft.md 冻结清单 2–4 步落地——卡点类型=等开工拍板;解锁条件=接入方确认真接入 consent 流程或用户指示开工(无接入方开工会产生无人验收的实现维护面)
- [ ] F33 审计落库:仓内面已全落(事件面 + 日志半边 + HTTP 直写半边 + 第二实现同形状,批次 62/65/66);剩保留策略与查询面,归接入方存储实现(audit.go:3 已注)——卡点类型=等接入方;解锁条件=接入方定存储形态(DB/SIEM/OSS)+ 保留天数 + 查询权限边界
- [x] 文档双语:根目录 8 篇(eef3c95)+ 子目录 25 篇(contract 20 + research 5,批次 93)英文版配对 + 双向切换器 50 文件全覆盖;public/ 仅 logo 无文档

## 待议(边界未定,不立项)

- [ ] F62 邮件/礼包码:与消息面相邻,按需优先评估(features.md §10 定性「值得优先评估」,需运营侧实际需求触发);卡点类型=等运营需求;解锁条件=运营侧实际需求触发、按评估草图冻结立项;FW1016D 已代决(评估):评估完成(research/mail-cdk-evaluation.md)——契约面同构度高、按草图低成本可立;运营需求未触发维持不立项,解锁条件不变
- [ ] F77 支付订单持久化与重启恢复(2026-10-18 审核表三评估;一期 2026-10-19 已代决落地,二期仍需用户拍板):现状订单全进程内存(teller/telleralt `orders` map),重启 = 未完结订单丢失、回调来单 404;payment.md 未把重启恢复写进契约,属实现方部署语义。**一期已代决**(巡检推进单授权,取 ledger 建议方案:零外部依赖):`Options.StateFile` opt-in(空 = 纯内存,历史行为零变化),每次订单状态变更原子写全量快照(临时文件 + rename,无停机钩子依赖),构造时回放;快照损坏 → HealthCheck fail-closed → 支付域 503,不静默从空态启动丢单;SKU 价格表不快照(管理面启动登记);teller(可变状态表)/ telleralt(事件账本)同构落地,main.go 经 `COURIER_PAYMENTS_STATE` / `COURIER_PAYMENTS_STATE_ALT` 注入。**二期待议**:按接入方存储形态注入 Options Store 接缝——需用户拍板(涉部署形态选型)
- [ ] F78 Provider 配置热重载管理入口(2026-10-18 审核表三评估):**一期已代决落地**(2026-10-19 巡检推进单代决条款,取台账分期建议:零新攻击面)——`routing.Hot` 全域路由表原子换表(在途按旧表完成),`SIGHUP` 重读 COURIER_GATEWAY_CONFIG、失败保留旧表 fail-safe;跟随 primary 三接缝(身份验证器/推送源/维护门)随重载双向重指(原启动单向重指重构为启动/重载共用 apply 路径,指回默认不再残留旧实现);architecture/layers/provider-guide(zh+en)同步。**二期待议**:管理端点(前置=管理面鉴权契约)与文件 watch——需用户拍板
- [x] F79 多实例共享限流(2026-10-18 审核表三评估):**已代决不立项**(2026-10-19 巡检推进单代决条款,取台账建议方案:接入方设施优先)——多实例部署时限流经接入方 Redis/LB 设施承担,仓内不引入外部依赖做共享限流(单实例部署本就无影响);rates 语义红线(单机阈值)已在 architecture/middleware 注明。若接入方无 Redis/LB 且需多实例,重新立项再议
- [ ] F80 UE/Cocos 持久化令牌存储适配器(2026-10-18 审核表三评估):**已代决不提前做**(2026-10-19 巡检推进单代决条款,取台账建议方案)——随引擎集成面(UE HttpModule 后续)一并落位,不单独提前;现状=仓内仅 MemoryTokenStore、接入方可自实现 TokenStore 接缝补齐(layers.md 已注)。卡点类型=等引擎集成面;解锁条件=UE HttpModule 集成启动或接入方自适配
- 审核表三另两项已有归属,不另开:F31(consent 端到端,未完成节,卡点=等开工拍板)、F33(审计可靠交付/保留/查询,未完成节,卡点=等接入方存储形态)——未重开已裁决条目

## Provider 口径(能力归 Provider,不进核心契约)

- F07 账号风控与防刷(登录限流已内建,支付风控为预留钩子)
- F17 二进制文件存储(对象存储归 Provider)
- F23 订阅状态管理(随真实渠道接入)
- F32 数据最小化与第三方共享清单(清单输出归 Provider)
- F37 远程推送 APNs/FCM(应用内 SSE 已落,厂商通道不自建)
- F38 按账号/标签定向推送
- F39 本地通知/定时提醒
- F40 触达效果统计
- F49 A/B 实验(不自建,契约不建)
- F52 埋点与行为分析(归游戏自有分析栈)
- F53 付费与广告收入统计
- F55 崩溃与性能监控(diagnostics 可选包直发,不经网关)
- F57 数据导出

## 明确不做

实时多人/匹配/大厅;渠道包聚合联运;运营端界面;自建长连接;统一钱包假设。
F13 云存档/进度同步——player.md:5,11 红线「进度不经 Courier,角色数据归各游戏服」,边界已定(非待议);F58/F59/F60/F61/F63 排行榜/成就/Stats/赛季/活动——features.md §10 定性「玩法系统非服务递送」,防范围失控(§10 结论⑤),边界已定。

## 已完成(批次台账:一句话 | commit | 测试)

- [x] 批次 0 更名 Concierge→Courier、定位 Universal Game SDK、五层架构设计 | 2a754f3 ff9b64c 78589c9 | —
- [x] 批次 1 M0 契约立宪:五基元 Frozen v1 + realname/branding/diagnostics 初稿 + 契约测试骨架 | d5dbebf a60d8ed | unity 8 / TS 6×3 / ue 绿
- [x] 批次 2 网关瘦身骨架:Provider 注册表 + 配置路由 + 能力降级 + 中间件链 | cdd391d | go test -race
- [x] 批次 3 账号域自建实现 + auth 契约冻结:邮箱/游客/轮换/吊销/设备 | 3c24cfc 39b5007 | go test -race + e2e
- [x] 批次 4 Unity Core:门面 + 生命周期状态机 + 注入接口 | 8b9ed8a | xunit 45
- [x] 批次 5 Unity Adapter + UPM 三包骨架:安全存储/传输/前后台信号 | 516c1a0 | Core 45+ + packcheck
- [x] 批次 6 公告/客服/推送 Provider + UI 包:herald/croupier/chirp + SSE | 8ec5702 38fe759 a4ce1eb | e2e 四例 + dotnet 8 + TS 6×3 + UE 枚举
- [x] 批次 7 实名域:warden 自建核验 + 治理引擎(降级链/熔断/热重载)+ 默认关闭 | e835ff2 e926626 a3dc311 | router 9(-race)+ warden 14 + e2e 6 + ServiceTests 36
- [x] 批次 8 配置/档案/应用/品牌/诊断五域:scribe/archivist + UI 品牌 + 诊断可选包 | 569fc8b 33c5c18 72bf15c c7fdf63 43ed745 等 | e2e 五例 + DiagnosticsTests 11
- [x] 批次 9 助手域:sage 检索问答 + 转人工 | 1104081 2481383 9b45f9e 1047bb1 | e2e 两例
- [x] 批次 10 支付域:teller 沙箱渠道 + 回调 HMAC fail-closed + 对账 | 9df7ba7 ddcbd03 fff7125 | e2e 四例 + ServiceTests 6
- [x] 批次 11 支付推送事件 v1.1:paid/delivered 经 chirp 广播 | 60b5d22 79c5bce | e2e SSE
- [x] 批次 12 cocos core + service:FetchTransport + 九域 | 315717c db69d8e | node 41
- [x] 批次 13 生命周期状态机 + 微信小游戏适配器 | ce974f7 e6cf491 | node 83
- [x] 批次 14 UE core + service:纯 ISO C++17 header-only | 225ad01 f5b160e 3cc859a | UE 六套
- [x] 批次 15 miniprogram core + service + wx 适配 | 33325e8 | node 83
- [x] 批次 16 laybox core + service + laya 适配 | 59a8e02 | node 82
- [x] 批次 17 cocos UI 可选包:品牌兜底链 + 三面板 | 1fb4790 | node 91
- [x] 批次 18 账号第二实现 accountalt + SwitchableVerifier + 一致性 e2e + primitives v1.1 | d0936eb | gateway 17 包
- [x] 批次 19 CI 测试门禁:tests.yml 三栈全仓回归 | d44e23a | 首跑全绿
- [x] 批次 20 cocos 诊断可选包 | 0af3e2f | node 102
- [x] 批次 21 维护态自定位:告警清零/导航补缺/漂移修正/vet 门禁 | 7bd102a 621c5dc 等 | 全量门禁绿
- [x] 批次 22 巡检续批:README 门禁章节/工作流统一/测试数刷新 | d979b08 d9e61cf 67edbce | cocos 102 / miniprogram 83 / laybox 82
- [x] 批次 23 巡检续批:layers/README 漂移两处 | e0ac63c | CI 绿
- [x] 批次 24 巡检续批:WebSocket→SSE 四处矛盾修复 | 9823261 | CI 绿
- [x] 批次 25 巡检续批:layers 九处同步 + 审计中间件声称修正 | 0eb1503 055bd29 | CI 绿
- [x] 批次 26 声称抽查:33 端点全实装 + 公告详情/FAQ 补 e2e | ad5bac4 | gateway 17 包 -race
- [x] 批次 27 声称抽查:事件表/端点表 vs SDK 零漂移 | 516a94a(零漂移审计) | —
- [x] 批次 28 README 双语对等零漂移 + fixtures 页入导航 | 6aff436 | vitepress 构建绿
- [x] 批次 29 错误码 v4 六端零漂移 + e2e 覆盖矩阵 33/33 | aa436ec | CI 绿
- [x] 批次 30 DTO 六端跨端对照:六处不一致全修 + 二进制卫生 | 469e809 b250622 | 六端全矩阵绿
- [x] 批次 31 tools/tsync 三端守同 + CI 收编 packcheck/tsync | ac2cfb6 a2d0fd6 | 102/83/82 + tools
- [x] 批次 32 Godot M1 core 落位 + contractgen GDScript 修复 | 1e697ce 6b0fcab 07a7e40 | godot 三套
- [x] 批次 33 Godot 服务域 M2–M5 | 2a574d2 cf9f1fe | godot 五套
- [x] 批次 34 Godot 真传输 + 持久令牌存储 | 033fb00 5546f6a | godot 六套
- [x] 批次 35 Godot SSE 流读 | 4d03c11 9a86d32 | godot 七套
- [x] 批次 36 Godot 前后台生命周期适配 | afc8b75 8baa248 | godot 八套
- [x] 批次 37 Unity SSE 流读 + CI unity-dotnet job | 0118303 959bf72 7b18a22 cf82d1a | dotnet 8+48+77+11
- [x] 批次 38 TS 三端 + UE messages SSE 流客户端 | c220011 d2899ff fc2f95a 057a7d0 | cocos 111 / laybox 91 / miniprogram 92 / UE 七套
- [x] 批次 39 手写 UTF-8 解码器 + 微信 enableChunked 流源 | ecabccf 84d2ed5 7dc7771 | cocos 123 / miniprogram 104 / laybox 98
- [x] 批次 40 laya XHR 流源(六端流源全景收口) | be2da66 dfc85c8 | laybox 104 / cocos 123 / miniprogram 104
- [x] 批次 41 ui/diagnostics 可选包三端对齐 | 7457bfe 7632a06 | 三端各 123
- [x] 批次 42 Godot 绑定"规划中"四处清账 + tsync 收编 ui/diagnostics | 791fe57 1ab399f a873aaf | godot 八套
- [x] 批次 43 契约分框缺口补登:messages.md v1.1 传输分框硬要求 | 99dc7e1 | 全量健康检查绿
- [x] 批次 44 竞品调研功能点立项(示例/选装/quickstart/对标) | 2201809(立项;落地见批次 55) | —
- [x] 批次 45 features.md 76 项全量入账 + implementation-status 落档 | a90196e | —
- [x] 批次 46 外部架构评审要点归档 | 0fdb4e2 | —
- [x] 批次 47 契约 v1.1 三提案(auth 注销/公告过滤/工单上下文)+ 七域扩展预留 | 45bfeaf | —
- [x] 批次 48 契约 v1.1 两提案(多语内容/举报申诉) | 50c4d04 | —
- [x] 批次 49 OAuth 域契约草案 | 5bfadab | —
- [x] 批次 50 F44 附件提案 + 扩展预留收口 + 机械核对 | db5c3ef | —
- [x] 批次 51 F03+F35+F43 冻结到实现(契约/网关/SDK 六端/e2e) | f20e9ab 53eaa7c f2de30e | node 128×3 / unity 51+80+8+11 / ue 7
- [x] 批次 52 F01 OAuth 冻结到实现(errors v6) | 689ece9 e22e3d2 | node 130×3 / unity 55+8+80+11 / ue 7
- [x] 批次 53 F44 附件冻结到实现(errors v7,直传零网关流量) | 035125d 6dbfca0 804e93d | node 131×3 / unity 8+55+11+81 / ue 7
- [x] 批次 54 F36 跑马灯 UI 呈现(severity=CRITICAL 复用) | 8127ba9 | node 132×3 / unity 84+8+55+11
- [x] 批次 55 六端示例 + quickstart 实测 + 选装清单 + 对标刷新 | 425fe55 c5c4814 | 六端示例实测同输出 + node 132×3 / unity 8+55+84+11 / ue 7
- [x] 批次 56 F45 举报与处罚申诉冻结到实现(errors v8,42 码) | d02ddcc 96d8295 de4dc3d | node 134×3 / unity 86+55+8+11 / ue 7
- [x] 批次 57 F76 多语内容冻结到实现(单语分条,零新错误码) | 65d9971 478c194 | node 136×3 / unity 88+55+8+11 / ue 7 / godot +1
- [x] 批次 58 Provider 接入指南(阶段 D 收尾) | ad91846 | 逐条对码核验
- [x] 批次 59 F31/F33 触发条件复核(只读) | 71170e1(复核记录) | CI 绿
- [x] 批次 60 选装清单字节与测试计数复测 | d0049d9 | tools + vitepress 绿
- [x] 批次 61 敏感域切换策略 + 账号侧并发换源测试 | 054a59e 161c246 | gateway -race 18 包
- [x] 批次 62 F33 审计事件面半边(AuditSink + 日志半边) | c37ef71 | gateway -race 18 包
- [x] 批次 63 跨引擎一致性:Unity 并发刷新单飞修复 + UE 重入防护 + 切号补齐 | 16e04b1 | unity 164 / ue 7 / godot 8
- [x] 批次 64 realname 第二实现 warden-alt(托管核验)+ 一致性 e2e | 53f4194 | gateway -race 19 包
- [x] 批次 65 审计直写半边:warden HTTPSink(SIEM/自建落库代理,尽力交付不阻断请求)+ env 接线 + e2e 装配路径实证 | 3b80159 | gateway -race 19 包 + e2e
- [x] 批次 66 审计面扩到第二实现:接缝抽共享包 gateway/audit(Provider 互不 import),warden 与 wardenalt 同形状注入同事件序列 + e2e 两实现同断言 | 0060ac9 | gateway -race 20 包 + e2e
- [x] 批次 67 player 域第二实现 archivist-alt(事件日志模型 vs 状态表,同契约异构)+ 一致性 e2e | 7a93441 | gateway -race 21 包 + e2e
- [x] 批次 68 messages 域第二实现 chirp-alt(共享环形日志+游标拉取 vs 每订阅者独立缓冲扇出,同契约异构)+ 一致性 e2e | 7c066d9 | gateway -race 22 包 + e2e
- [x] 批次 69 announcements 域第二实现 herald-alt(写时索引 vs 读时线性扫描,同契约异构)+ 一致性 e2e | 3323ed6 | gateway -race 23 包 + e2e
- [x] 批次 70 assistant 域第二实现 sage-alt(写时预计算规范化 vs 读时重复规范化,同契约异构)+ 一致性 e2e | 736fcc0 | gateway -race 24 包 + e2e
- [x] 批次 71 config 域第二实现 scribe-alt(写时维度索引 vs 读时逐条目 AND,同契约异构)+ 一致性 e2e + 维护门回调随 app primary 重指 | 24da6e2 | gateway -race 25 包 + e2e
- [x] 批次 72 payments 域第二实现 teller-alt(事件账本投影 vs 可变状态表,同契约异构)+ 一致性 e2e | 98ced31 | gateway -race 26 包 + e2e
- [x] 批次 73 support 域第二实现 croupier-alt(写时属主有序索引 + 键集游标 vs 读时全表扫描 + 偏移游标,同契约异构)+ 一致性 e2e + 跨实现分页等价证明 | 906f71d | gateway -race 27 包 + e2e
- [x] 批次 74 台账体检(28 项已完成条目回填 commit 证据)+ Go 工具链安全修补(go 1.24→1.27.2,gateway 9 个可达 stdlib 漏洞清零) | a7e367e 6448a63 | gateway -race 27 包 + govulncheck 双模块清零 + CI 全 job 绿
- [x] 批次 75 CI 漏洞扫描门禁(govulncheck@v1.9.0 进 tests.yml go job,根+gateway 双模块可达性扫描,防 stdlib 漏洞回归)+ README 本地门禁同步 | be52c98 | govulncheck 双模块清零 + 全量门禁绿(Go -race 27 包 + node 3×136 + unity 164 + ue + examples 5 端)
- [x] 批次 76 台账 hash 可达性核验(76 项逐条比对 git 历史,批次 65 的 35a13a4 因历史重写悬空改为可达的 3b80159) | 4ccc324 | 76/76 hash 可达 + docs:build 绿
- [x] F14 分级读写权限 | 已代决(巡检 R1) | 当前契约即本人/仅服务端两层(Bearer 自持),公开层与社交非目标冲突,不立项
- [x] F15 写锁与冲突合并 | 已代决(巡检 R1) | 唯一写字段 displayName 服务端修剪,无并发写冲突面,版本号待第二写字段出现再评估
- [x] 批次 77 CI 竞态检测对齐(Tests go job 双模块 go test 加 -race,对齐台账批次 62 起的本地验证口径;并发面 SSE/单飞/换源回归自动拦截)+ README 本地门禁同步 | 2f80204 | 全量门禁绿(Go -race 双模块 + node 3×136 + unity 164 + ue + examples 5 端)
- [x] 批次 78 Provider 单元测试补口(oauth 注册表优先级/同名覆盖/显式注销;croupier fail-closed+无身份写路径+CreateTicket 运维入口+坐席回复关单;herald 分页参数校验+fail-closed;accountalt Store 访问器+SetAccountStatus 禁用闸;7 个零覆盖函数补齐至 100%) | 1bc1058 | gateway -race 30 包全绿 + vet/gofmt 绿
- [x] 批次 79 网关零覆盖函数清零(unit+e2e 合并 profile 全模块核验,余 16 个零覆盖函数全补:prune 清退/满表放行、archivist 注销级联、croupieralt 无身份写路径+运维入口、Breaker.IsOpen、scribe/scribealt ConfigVersion 访问器+非法值错误渲染、teller/telleralt 建单坏体拒、warden/wardenalt 无身份写路径+core 健康判定、wardenalt 降级链热重载;模块函数级零覆盖归零) | 54a429f | gateway -race 30 包全绿 + union profile 0 零覆盖函数 + vet/gofmt 绿
- [x] 批次 80 contractgen 生成链回环测试(checkOrWrite 临时根 -write 落盘后复核一致,fixture 解析→8 端生成器→写盘全链首次入测;覆盖率 81.7%→90.1%) | 776a0fb | go test -race ./tools/... 绿 + vet 绿
- [x] 批次 81 Unity 前后台适配层可测化(路线图阶段 B 开放项收口:MonoBehaviour 只留引擎回调转发,信号去重/成对恢复/被拒不丢挂起/未绑定 no-op 拆到零引擎依赖的 LifecycleSignalRelay;8 例单测入 CoreTests~,与 godot test_adapter.gd 逐条对齐;packcheck Adapter 符号门禁与 dotnet include 门禁同步改为逐文件零平台引用校验) | 377b4ea | dotnet test 四工程全绿(65/88/11/8) + go test -race ./tools/... 绿 + docs:build 绿
- [x] 批次 82 TS 三端传输层零覆盖路径补口(coreTransport.test.ts 新增 13 例:仓内此前只有脚本化 FakeTransport,默认传输实现 FetchTransport 全无单测覆盖——method/headers/jsonBody 透传、响应头键小写归一、timeoutMs 非正回落 10s、TransportError 双形(Error 与非 Error)、省略 fetchImpl 绑定 globalThis.fetch;FetchSseConnection 行读边界:CRLF 容忍/流尾残余半行/流读失败归一并记忆/无 body 拒/close 幂等;SessionService 边界:未接线守卫、clear、无会话 refresh 不发包、非安全事件错误上抛不误清场、设备列表与解绑 URL 编码;ui.test.ts 新增 5 例:两面板未装配 no-op、提单限流/详情 404/FAQ 断网/转人工失败四条 onError 面、品牌兜底链三段、转人工标题 120 截断。transport.ts 65%→100%(分支 25%→100%)、session.ts 88.79%→100%、sseStream.ts 92.22%→100%、assistantPanel 84.72%→100%、customerServicePanel 84.48%→100%;模块 94.33%→96.82%) | 22e176e | node 3 端各 154/154 + tsync 三端守同绿
- [x] 批次 83 文档计数漂移收口(layers.md node:test 136→154 分解含 coreTransport 13/ui 14 同步批次 82;审计台账干净区块快照追注 CoreTests 57→65、node 136→154 两处漂移移出并标时点) | b7de88d | docs:build 绿 + UE 七套全绿
- [x] 批次 84 错误码接线闭环门禁 + 保留码代决销项(新门禁 TestErrorCodesWiredOrReserved:errors.json 每个码要么在网关非测试代码被引用、要么在 errors.md「注册保留码」注记点名,集合须相等;门禁首跑即抓出 3 处审计当时漏掉的悄悄保留码 COMMON_PERMISSION_DENIED/CONFIG_NOT_FOUND/APP_VERSION_UNSUPPORTED,保留注记 6→9 条;C-02/C-03 代决——判定走 200 载荷为唯一真相源、码保留不发(判定端点回 403 会把查询变异常路径,且载荷能同表五种实名态而单码不能),SESSION_CONFLICT 属玩家可见登录策略不代决为网关行为;按「修复以码为准」对齐 realname.md 域表与 app.md:39 两处超前主张) | 837171a | go test -race ./tools/... 绿(双分支负向验证:摘注记/加引用各失败一次)+ docs:build 绿
- [x] 批次 85 UE 端覆盖率收口(gcov --coverage 七套跨 TU 归并实测:生产代码行覆盖 96.9%→98.37%,未覆盖可执行行归零,余 2 行为 gcov 收尾花括号不可执行;缺口集中在 api_client 整条韧性链——fallback_wire 状态映射表/Retry-After 负值/空 body 与非 JSON body 兜底/空 5xx 退避重试/默认真实 sleep/AUTH_TOKEN_EXPIRED 重放与 refresh 拒绝/无本地会话拒绝 refresh;json 序列化与解析规范路径——布尔与控制字符转义、2 字节 UTF-8 往返、非对象 set 无副作用、字面量与分隔符与转义与代理对与指数的拒绝面;lifecycle 状态名与触发名全表、表外迁移一律拒绝、无订阅者 emit;service 域解码器与错误面——decode_app_maintenance/decode_app_environment 成功路径、bind_character、config region 维度与未取缓存、oauth_exchange 可选字段、十域 typed 错误原样上抛、FAQ keywords 非字符串跳过、公告 filters 非数组成员跳过;SSE 解析与流客户端——无冒号行按字段名处理、头键小写归一、读失败走重连、缺省 sleep 占位) | babab36 | sh sdks/ue/run_tests.sh 七套全绿 + gcov 归并 98.37% + docs:build 绿
- [x] 批次 86 TS 共享面覆盖残留收口
- [x] 批次 87 wechat 适配器覆盖收口(sseSource/tokenStore 100% 行与函数;补 4 例——sseSource 预取消拒连与 wx.request 同步抛错归一(Error 与非 Error)、settled 后重复 success 忽略且 task.abort 抛错被吞(fail 照常收场)、CRLF 行剥除与空块/关流后 push 忽略及 close 后再 fail 守卫、tokenStore storage 读取异常安全降级;FakeChunkedWx header 透传使 lowercased(undefined) 覆盖;模块 99.18%→99.90% 行覆盖;layers.md 计数 171→175 同步;miniprogram 同步(laybox laya 适配器另批次,不属本项)) | be86d2d | node 3 端 175/175+171/171 + tsync 守同绿
- [x] 批次 88 laya 适配器覆盖收口(补 6 例——laya sseSource 已取消拒连、工厂同步抛错归一(Error 与非 Error)、send 同步抛错归一、CRLF 行剥除与 progress 携 DONE 交流尾及 abort/close 抛错被吞、事件回调内循环顶取消净停(不重连不发终态)、ontimeout 归一断流重连、无 headers 建连(headers ?? {} 空头集,source 只透传不拼 Bearer);tokenStore storage 读取异常安全降级;FakeLayaXhr 补 timedOut 手动驱动;laya lifecycleMonitor/sseSource/tokenStore 行/分支/函数三项 100%;sseStreamClient 余 183-185 为 defaultSleep 入口已取消分支经 run() 不可达,transport 余 85.71% 分支为未命名残留;laybox node:test 171→177;批次 87 登记项完成) | 1dc7b61 | node laybox 177/177
- [x] 批次 89 平台适配器 transport 分支收口(wx/laya 两端 8 文件行/分支/函数三项全 100%;wx 补缺省响应头(header ?? {})与 null 响应体空串归一;laya 补响应体缺失(responseText ?? "")与 timeoutMs 缺省归 0 不设超时;TS 平台适配层覆盖收口完成,余共享面分支残留见批次 86 记录;laybox node:test 177→178,layers.md 计数 175→176 同步) | 0d4e39b | node 3 端 176/176/178 + tsync 守同绿(node --test --experimental-test-coverage 实测模块 96.89%→99.18%,补 17 例——courierClient init 复核守卫与 registerAsync 直连、identity.refresh 直连、utf8 2 字节完成与 3 字节超长/代理区、sseStreamClient source/request 守卫/退避 sleep 被拒净停/非 TransportError 上抛/defaultSleep 定时放行、support listTickets cursor 编码、player bindCharacter 与四域非降级错误照常抛、payments listOrders、branding cachedBranding 访问器、diagnostics anyEnabled 与 reporter 空参守卫/span 去 query、ui getProductTitle 回退与公告面板 onError;共享面 10 文件行覆盖 100%,sseStreamClient 余 183-185 为 defaultSleep 入口已取消分支经 run() 不可达;wechat 平台适配器残留待后续批次;layers.md 计数 154→171 同步) | 0d1e9a2 | node 3 端各 171/171 + tsync 守同绿
- [x] 批次 90 TS 共享面分支/函数残留收口(apiClient 修复 ErrorSpecs 查表键型:表按枚举名做键而 wire 直查恒 miss,信封缺省 retryable 的冻结映射兜底从未生效——经 errorCodeOf 映射后查表;补 23 例——parseEnvelope 空体状态矩阵全分支(401/403/404/429/501/其他)与信封缺字段降级(空 error/无 message/无 retryable 走登记 spec 双向验证)、Retry-After 非法值降级基础间隔、defaultSleep 真实定时器、token 预检与无 refreshHook 过期直抛、refresh 重放边界(二次过期不再 refresh/token 为空 Bearer 不动/匿名请求 hook 生效)、transport 抛 CourierApiError 原样透传与非 Error 值归一、非 JSON 404 不可重试;session REVOKED 同走安全清场与网络错误非安全事件上抛会话保留;sseParser null/undefined 行归一、纯 id 帧不出事件、无冒号行按字段名;app 无参版本查询空 query、config region 维度、assistant 非降级错误照常抛;stream 非法 Retry-After 落退避、request/conn 头缺省归一空头集、读循环中取消净停、断流 TransportError 重连、abort 与断流竞态净停、default 退避入口已取消即拒、建连返回前取消、缺省构造绑全局 fetch;diagnostics noop jank/rtt 与全字段 resource crash wire 携 osVersion/deviceModel;ui wireOf 非 Courier 错误落 UNKNOWN 与详情视图空列表防御分支;utf8 4 字节超长与越界码点降级;结构性与跨进程残留定性:apiClient request 循环尾 throw 后空区间与 session tryRefresh wire 提取 `: ""`(apiClient 归一使非 wire 错误不可达)与 utf8 2 字节超长侧(0xC0/0xC1 起始已拦,≥0xC2 必 ≥0x80)为不可达防御,生命周期 emit 循环体/FetchSseSource fetch ?? 两支/sseStreamClient attemptOnce 非 TransportError 上抛为跨进程覆盖合并伪影(相应测试文件已覆盖);node:test 176→199,layers.md 同步) | 472d3df | node 3 端 199/199/201 + tsync 守同绿 + V8 覆盖残余归零(6 项定性:3 不可达 + 3 伪影)
- [x] 批次 91 contractgen 入口覆盖收口(fatal 加可测性接缝 var exit = os.Exit,生产默认即 os.Exit 行为不变;fatal 真退进程致子进程覆盖不并入父 profile,改进程内:记录型 exit + 双管道 captureIO,断言退出码 1 与 stderr contractgen: 前缀;补 fatal 调用点——readJSON 缺失/损坏 fixture、checkOrWrite 缺文件+漂移同轮双支与汇总致命信息、generate fixture 不完整双支(version<1/码表空前缀空与 dto 缺 key);main 进程内 -write 驱动;零覆盖函数全仓归零:todo 三功能项(F31/F33/F62)均外部卡点,本项为覆盖率收尾——tools/contractgen 模块 90.1%→96.9%,根模块归并 profile 零覆盖函数 0(原 2:fatal/main);余 camelName 空段 continue(需畸形码形,合法 fixture 不可达)与 checkOrWrite 权限类 fatal(需 chmod/root)为防御分支,批次 79 函数级零覆盖口径达标) | 7941938 | go test -race ./tools/... 绿 + 根/网关双 vet 绿 + gateway -race 全绿 + 归并 profile 零覆盖函数 0
- [x] 批次 92 docs 根目录双语配对(8 篇中文文档英文版 + 双向切换器;README.md 中文正文红线修复——「本地测试口径」节译英) | eef3c95 | docs:build 绿(16 页)
- [x] 批次 93 docs 子目录双语配对(contract 20 篇 + research 5 篇英文版;双语切换器 50 文件全覆盖;英文页互链改指 .en.md;代码围栏/JSON 样例数据/SQL 注释按判据原样保留) | 2f59807 | docs:build 绿(66 页) + go test -race ./tools/... 绿(errors.md 为 contractgen 测试输入,实证无漂移)
- [x] 批次 94 F31 契约面代决立项:consent 组按草案冻结为 realname v1.1(realname.md 新增「同意管理」节,只加不改、零新增错误码;errors.md 与 fixtures 未动;实现面登记为新待办行) | f7a14a0 | docs-only:中英 8 文件同步改;本地无 node_modules,docs:build 由 CI Deploy Docs 兜底,仓内链接完整性检查绿
- [x] 批次 95 2026-10-18 独立审核整改:表一 6 缺陷全修(#1 webhook 强制 HTTPS+重定向 fail-closed;#2 请求体 1MiB 上限中间件+decode MaxBytesError 分流;#3 http.Server 显式读侧超时、写 0 保 SSE;#4 限流桶上限加最久未活跃淘汰;#5 订单 ID hexfnv→UUIDv7+锁内查重防碰撞覆盖,等价测试改去 id 比对;#6 删除 warden 无比对用途 SHA-256 指纹);表二 5 项全消(2 项被 #5/#1 消解,3 项以代码为准改 architecture.md/en——未配置 Provider=全挂载 501 降级、token 存储四端落地 UE/Cocos 内存态、App 门面无 GetVersion);表三 6 项只评估不实现(F77–F80 新登记待议节,F31/F33 已有归属) | e39b328 | cd gateway && go test -race ./... 30 包全绿 + middleware/cmd/teller/telleralt/warden/wardenalt 新测试 8 例 + docs:build 绿
- [x] 批次 96 F77 一期(已代决)支付订单快照与重启恢复:teller/telleralt 新增 `Options.StateFile`(opt-in,空 = 纯内存历史行为零变化);每次订单状态变更(下单/回调/关单/对账)锁内原子写全量快照(CreateTemp + rename,无停机钩子依赖);构造时回放重建 orders/byOwner;快照损坏 → HealthCheck fail-closed → 支付域 503(不静默从空态启动丢单),stateErr 置位后停写待运维;SKU 价格表不快照;main.go 注入 `COURIER_PAYMENTS_STATE` / `COURIER_PAYMENTS_STATE_ALT`;provider-guide.md/en 登记 env 与 fail-closed 口径 | 24a0e0b | cd gateway && go test -race -count=1 ./... 30 包全绿(teller/telleralt 各 +3 例:三代重启恢复含回调续完成、无 StateFile 纯内存回归、损坏快照 fail-closed)
- [x] 批次 97 文档时效性清理(2026-10-18 用户令):23 处文档↔代码漂移修正,全部以代码为准、业务代码零改动——unity README 测试数(core 48→65/service 77→88,`dotnet test` 实测)与目录树(删不存在的 tests/ 节点)、cocos/miniprogram 123→199 例、laybox 123→201 例(`node --test` 实测);sdk-selection 字节复测(unity core 77,851→79,759 B、TS core 44,448→44,570 B、UE 241,095→279,852 B,口径注刷新 2026-10-19);architecture/layers(zh+en)补 audit/ 目录与 1MiB MaxBody 最外层、HealthCheck 改逐请求探活、敏感端点列表按 account.go 改 register/login/guest/bind/refresh;events.md/en 删幽灵行 app.maintenance_changed/force_update(加更正注)并补 branding.updated 与 realname.consent.updated、信封加更正注指向 messages.md SSE 帧、index(zh+en)realname→v1.1/payment→v1.1、realname consent 节标注冻结≠实现(F31)、implementation-status(zh+en)标题计数改 3 已代决;六端命令行示例实测全绿、examples/README godot 节补 COURIER_ENDPOINT(默认 :8080 与本机 nginx 冲突) | 293e8c2 | docs:build
- [x] 批次 99 F78 一期 SIGHUP 配置热重载(已代决:取台账分期建议的一期方案,零新攻击面):routing/hot.go 新增 Hot(atomic.Pointer 整体换 root handler,在途按旧表完成,已注册 Provider 不重建只重排降级链);main.go 重构——notifyTarget/maintenanceSource 由闭包捕获变量改原子函数指针(运行期重指不再数据竞争),启动装配与 SIGHUP 重载共用 reload.go 的 mergeConfig+followTargets.apply(双向重指,Disabled/未知 primary/缺席键不动,与启动口径一致);SIGHUP 失败保留旧表只记日志;e2e 实测改配置→HUP→501↔401 换表与损坏文件保留旧表;architecture/layers/provider-guide(zh+en)治理现状同步 | 4a240fe | go test ./... 30 包全绿(含 -race 并发换表用例)
 绿 + 六端示例实测(cocos/miniprogram/laybox node 199/199/201 例全绿、unity dotnet 8/65/88/11 全绿、ue g++ 实跑、godot headless 实跑)+ go test ./tools/... 绿
- [x] 批次 98 契约事件登记表同步:messages.md/en 按「新事件随域契约冻结在此登记」规则补登 realname.consent.updated(realname v1.1 追加段:注 2026-10-18 冻结、未实现、实现面 F31、同 config.updated 模式最小字段+客户端重拉、出生日期等敏感数据不进事件)——消解批次 97 events.md 更正注「登记以 messages.md 为准」与权威登记表缺行的自相矛盾;至此 messages.md 与 events.md 两域登记齐平(branding.updated 批次 96 已在) | accf1db | docs:build 绿
