# Courier TODO — 需求清单

> 只列需求,一行一句;已完成项保留一行证据(commit + 测试)。F01–F76 逐项状态与落点见 [implementation-status.md](./research/implementation-status.md);阶段路线见 [roadmap](./roadmap.md);分层映射见 [layers.md](./layers.md)。

## 未完成

- [ ] F31 年龄门与同意管理(海外):首启年龄筛查 + VPC 家长同意;realname.md 已预留 consent 扩展位,出海需求拍板后立项
- [ ] F33 审计落库:事件面与日志半边已落(批次 62),剩 DB/SIEM 直写 sink 与保留策略,随部署面凭据到位

## 待议(边界未定,不立项)

- [ ] F13 云存档/进度同步:存档面留 Provider,不做统一钱包式假设
- [ ] F14 分级读写权限:本人/公开/仅服务端分层待议
- [ ] F15 写锁与冲突合并:乐观锁/版本号待议
- [ ] F58 排行榜:玩法非服务,按需评估
- [ ] F59 成就/任务:玩法非服务,按需评估
- [ ] F60 数值统计:玩法非服务,按需评估
- [ ] F61 赛季/通行证/签到:玩法非服务,按需评估
- [ ] F62 邮件/礼包码:与消息面相邻,按需优先评估
- [ ] F63 活动与限时运营:玩法非服务,按需评估

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

## 已完成(批次台账:一句话 | commit | 测试)

- [x] 批次 0 更名 Concierge→Courier、定位 Universal Game SDK、五层架构设计 | 仓库首批 | —
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
- [x] 批次 20 cocos 诊断可选包 | 同上批结构 | node 102
- [x] 批次 21 维护态自定位:告警清零/导航补缺/漂移修正/vet 门禁 | 7bd102a 621c5dc 等 | 全量门禁绿
- [x] 批次 22 巡检续批:README 门禁章节/工作流统一/测试数刷新 | d979b08 d9e61cf 67edbce | cocos 102 / miniprogram 83 / laybox 82
- [x] 批次 23 巡检续批:layers/README 漂移两处 | e0ac63c | CI 绿
- [x] 批次 24 巡检续批:WebSocket→SSE 四处矛盾修复 | 9823261 | CI 绿
- [x] 批次 25 巡检续批:layers 九处同步 + 审计中间件声称修正 | 0eb1503 055bd29 | CI 绿
- [x] 批次 26 声称抽查:33 端点全实装 + 公告详情/FAQ 补 e2e | ad5bac4 | gateway 17 包 -race
- [x] 批次 27 声称抽查:事件表/端点表 vs SDK 零漂移 | 零修复 | —
- [x] 批次 28 README 双语对等零漂移 + fixtures 页入导航 | 6aff436 | vitepress 构建绿
- [x] 批次 29 错误码 v4 六端零漂移 + e2e 覆盖矩阵 33/33 | aa436ec | CI 绿
- [x] 批次 30 DTO 六端跨端对照:六处不一致全修 + 二进制卫生 | 469e809 b250622 | 六端全矩阵绿
- [x] 批次 31 tools/tsync 三端守同 + CI 收编 packcheck/tsync | 同批 | 102/83/82 + tools
- [x] 批次 32 Godot M1 core 落位 + contractgen GDScript 修复 | 同批 | godot 三套
- [x] 批次 33 Godot 服务域 M2–M5 | 同批 | godot 五套
- [x] 批次 34 Godot 真传输 + 持久令牌存储 | 同批 | godot 六套
- [x] 批次 35 Godot SSE 流读 | 同批 | godot 七套
- [x] 批次 36 Godot 前后台生命周期适配 | 同批 | godot 八套
- [x] 批次 37 Unity SSE 流读 + CI unity-dotnet job | 同批 | dotnet 8+48+77+11
- [x] 批次 38 TS 三端 + UE messages SSE 流客户端 | 同批 | cocos 111 / laybox 91 / miniprogram 92 / UE 七套
- [x] 批次 39 手写 UTF-8 解码器 + 微信 enableChunked 流源 | 同批 | cocos 123 / miniprogram 104 / laybox 98
- [x] 批次 40 laya XHR 流源(六端流源全景收口) | 同批 | laybox 104 / cocos 123 / miniprogram 104
- [x] 批次 41 ui/diagnostics 可选包三端对齐 | 同批 | 三端各 123
- [x] 批次 42 Godot 绑定"规划中"四处清账 + tsync 收编 ui/diagnostics | 文档+工具 | godot 八套
- [x] 批次 43 契约分框缺口补登:messages.md v1.1 传输分框硬要求 | 契约加注 | 全量健康检查绿
- [x] 批次 44 竞品调研功能点立项(示例/选装/quickstart/对标) | 归批次 55 落地 | —
- [x] 批次 45 features.md 76 项全量入账 + implementation-status 落档 | 文档 | —
- [x] 批次 46 外部架构评审要点归档 | 文档 | —
- [x] 批次 47 契约 v1.1 三提案(auth 注销/公告过滤/工单上下文)+ 七域扩展预留 | 文档 | —
- [x] 批次 48 契约 v1.1 两提案(多语内容/举报申诉) | 文档 | —
- [x] 批次 49 OAuth 域契约草案 | 文档 | —
- [x] 批次 50 F44 附件提案 + 扩展预留收口 + 机械核对 | 文档 | —
- [x] 批次 51 F03+F35+F43 冻结到实现(契约/网关/SDK 六端/e2e) | f20e9ab 53eaa7c f2de30e | node 128×3 / unity 51+80+8+11 / ue 7
- [x] 批次 52 F01 OAuth 冻结到实现(errors v6) | 689ece9 e22e3d2 | node 130×3 / unity 55+8+80+11 / ue 7
- [x] 批次 53 F44 附件冻结到实现(errors v7,直传零网关流量) | 035125d 6dbfca0 804e93d | node 131×3 / unity 8+55+11+81 / ue 7
- [x] 批次 54 F36 跑马灯 UI 呈现(severity=CRITICAL 复用) | 8127ba9 | node 132×3 / unity 84+8+55+11
- [x] 批次 55 六端示例 + quickstart 实测 + 选装清单 + 对标刷新 | 425fe55 c5c4814 | 六端示例实测同输出 + node 132×3 / unity 8+55+84+11 / ue 7
- [x] 批次 56 F45 举报与处罚申诉冻结到实现(errors v8,42 码) | d02ddcc 96d8295 de4dc3d | node 134×3 / unity 86+55+8+11 / ue 7
- [x] 批次 57 F76 多语内容冻结到实现(单语分条,零新错误码) | 65d9971 478c194 | node 136×3 / unity 88+55+8+11 / ue 7 / godot +1
- [x] 批次 58 Provider 接入指南(阶段 D 收尾) | 文档 | 逐条对码核验
- [x] 批次 59 F31/F33 触发条件复核(只读) | 零增量 | CI 绿
- [x] 批次 60 选装清单字节与测试计数复测 | d0049d9 | tools + vitepress 绿
- [x] 批次 61 敏感域切换策略 + 账号侧并发换源测试 | 同批 | gateway -race 18 包
- [x] 批次 62 F33 审计事件面半边(AuditSink + 日志半边) | 同批 | gateway -race 18 包
- [x] 批次 63 跨引擎一致性:Unity 并发刷新单飞修复 + UE 重入防护 + 切号补齐 | 同批 | unity 164 / ue 7 / godot 8
- [x] 批次 64 realname 第二实现 warden-alt(托管核验)+ 一致性 e2e | 53f4194 | gateway -race 19 包
