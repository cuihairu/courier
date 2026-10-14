# Courier TODO — 需求清单

> 只列需求,一行一句;已完成项保留一行证据(commit + 测试)。F01–F76 逐项状态与落点见 [implementation-status.md](./research/implementation-status.md);阶段路线见 [roadmap](./roadmap.md);分层映射见 [layers.md](./layers.md)。

## 未完成

- [ ] F31 年龄门与同意管理(海外):首启年龄筛查 + VPC 家长同意;realname.md 已预留 consent 扩展位,出海需求拍板后立项
- [ ] F33 审计落库:仓内面已全落(事件面 + 日志半边 + HTTP 直写半边 + 第二实现同形状,批次 62/65/66);剩保留策略与查询面,归接入方存储实现(audit.go:3 已注)——卡点:接入方存储形态拍板(DB/SIEM/OSS + 保留天数 + 查询权限边界)

## 待议(边界未定,不立项)

- [ ] F14 分级读写权限:本人/公开/仅服务端分层待议(当前契约面无分层数据——档案仅 displayName/avatarUrl,无公开/仅服务端字段)
- [ ] F15 写锁与冲突合并:乐观锁/版本号待议(当前契约面无并发写冲突面——唯一写 PATCH displayName,服务端修剪,无版本号字段)
- [ ] F62 邮件/礼包码:与消息面相邻,按需优先评估(features.md §10 定性「值得优先评估」,需运营侧实际需求触发)

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
- [x] 批次 65 审计直写半边:warden HTTPSink(SIEM/自建落库代理,尽力交付不阻断请求)+ env 接线 + e2e 装配路径实证 | 35a13a4 | gateway -race 19 包 + e2e
- [x] 批次 66 审计面扩到第二实现:接缝抽共享包 gateway/audit(Provider 互不 import),warden 与 wardenalt 同形状注入同事件序列 + e2e 两实现同断言 | 0060ac9 | gateway -race 20 包 + e2e
- [x] 批次 67 player 域第二实现 archivist-alt(事件日志模型 vs 状态表,同契约异构)+ 一致性 e2e | 7a93441 | gateway -race 21 包 + e2e
- [x] 批次 68 messages 域第二实现 chirp-alt(共享环形日志+游标拉取 vs 每订阅者独立缓冲扇出,同契约异构)+ 一致性 e2e | 7c066d9 | gateway -race 22 包 + e2e
- [x] 批次 69 announcements 域第二实现 herald-alt(写时索引 vs 读时线性扫描,同契约异构)+ 一致性 e2e | 3323ed6 | gateway -race 23 包 + e2e
- [x] 批次 70 assistant 域第二实现 sage-alt(写时预计算规范化 vs 读时重复规范化,同契约异构)+ 一致性 e2e | 736fcc0 | gateway -race 24 包 + e2e
- [x] 批次 71 config 域第二实现 scribe-alt(写时维度索引 vs 读时逐条目 AND,同契约异构)+ 一致性 e2e + 维护门回调随 app primary 重指 | 24da6e2 | gateway -race 25 包 + e2e
- [x] 批次 72 payments 域第二实现 teller-alt(事件账本投影 vs 可变状态表,同契约异构)+ 一致性 e2e | 98ced31 | gateway -race 26 包 + e2e
- [x] 批次 73 support 域第二实现 croupier-alt(写时属主有序索引 + 键集游标 vs 读时全表扫描 + 偏移游标,同契约异构)+ 一致性 e2e + 跨实现分页等价证明 | 906f71d | gateway -race 27 包 + e2e
- [x] 批次 74 台账体检(28 项已完成条目回填 commit 证据)+ Go 工具链安全修补(go 1.24→1.27.2,gateway 9 个可达 stdlib 漏洞清零) | a7e367e 6448a63 | gateway -race 27 包 + govulncheck 双模块清零 + CI 全 job 绿
- [x] 批次 75 CI 漏洞扫描门禁(govulncheck@v1.9.0 进 tests.yml go job,根+gateway 双模块可达性扫描,防 stdlib 漏洞回归)+ README 本地门禁同步 | PENDING | govulncheck 双模块清零 + 全量门禁绿(Go -race 27 包 + node 3×136 + unity 164 + ue + examples 5 端)
