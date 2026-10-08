# 功能调研:市面上游戏 SDK 的常见功能清单

> **口径**:样本见 §0;检索与核对时间 **2026-10-08**,结论均可回溯到文末链接。
> 本文是**功能维度**的颗粒度盘点——竞品取位、星数与三条结论见 [competitive.md](./competitive.md),本文只补它的下一层:这个品类里「一个游戏 SDK 到底该有哪些功能」。
> **常见度**为定性三档:`必备`(联运/云服务类样本几乎都有,缺了接不了或上不了架)/ `常见`(多数综合型 SDK 提供,但按产品分层)/ `加分`(少数或垂直产品才有)。
> **Courier 取舍**是调研建议,对照 [roadmap 非目标](../roadmap.md) 与五层架构,不是承诺。

## 0. 样本与方法

| 样本 | 类型 | 覆盖的功能面 |
| --- | --- | --- |
| [Nakama](https://github.com/heroiclabs/nakama) | 开源游戏服务器 | 账号链接、存档、排行榜、内购验真(详见 [competitive.md](./competitive.md)) |
| [Firebase for Games](https://firebase.google.com/docs/games/setup) | 移动通用 BaaS | 认证、远程配置、推送、应用内消息、崩溃 |
| [Unity Gaming Services](https://docs.unity.com/en-us/services) | 引擎厂商 LiveOps | 认证、云存档、经济、排行榜、推送、远程配置 |
| [Epic Online Services](https://github.com/api-evangelist/epic-games/blob/main/apis.yml) | 跨平台在线服务 | 身份、社交、进度、存储、合规(制裁)、电商 |
| [Google Play Games services](https://support.google.com/googleplay/android-developer/answer/2990418) | 平台方 SDK | 成就、排行榜、云存档、身份 |
| [AccelByte AGS](https://accelbyte.io/) | 商业模块化后端 | 账号、云存档、商城、赛季、遥测 |
| [腾讯 MSDK](https://docs.msdk.qq.com/v5/zh-CN/Module/Notice.html) | 国内联运聚合 | 公告、推送、模块化选装 |
| [XDSDK / TapTap TapSDK](https://sdk-docs.xindong.com/) | 发行平台 SDK | 登录、支付、实名防沉迷、数据、客服 |
| [QuickSDK](https://www.quicksdk.com/) | 渠道聚合 | 登录、充值、服务端验签 |
| [GamePush](https://gamepush.com/en/backend/) / [BACKND](https://backnd.com/en/) | 轻量游戏 BaaS | 云存档、支付、排行榜、公告、工单、推送 |
| [Helpshift](https://www.helpshift.com/blog/mobile-game-support-sdk/) / [Zendesk for Unity](https://www.zendesk.com/blog/unity-zendesk-partnership/) | 客服垂直 | 游戏内工单、FAQ、上下文透传 |
| [GameAnalytics](https://docs.gameanalytics.com/event-tracking-and-integrations/advanced-tracking/purchase-validation/) / [ByteBrew](https://github.com/ByteBrewIO/ByteBrewAndroidSDK) / [友盟](https://devs.umeng.com/) | 数据归因垂直 | 埋点、付费验真、归因、远程配置、A/B |
| 行业口径:[腾讯云《手游 SDK 能做什么》](https://cloud.tencent.com/developer/article/1856528)、[《版本运营与 GM 工具》](https://cloud.tencent.com/developer/article/1978643) | 中文行业资料 | 联运 SDK 必备项、运营工具面 |
| 合规口径:[TapTap 合规认证](https://developer.taptap.cn/docs/sdk/anti-addiction/features/)、[COPPA/GDPR 实务](https://www.strapdata.com/gdpr-coppa-compliance-gaming/)、[GameAnalytics 隐私口径](https://www.gameanalytics.com/trust/privacy-faq) | 合规 | 实名、防沉迷、同意与注销 |

**方法**:先按「玩家从装游戏到流失」的生命周期切面枚举功能,再回查每个样本是否提供,最后标注常见度与 Courier 取舍。样本共 14 类,单个功能至少有 2 个独立来源才会入表。

---

## 1. 账号与身份

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F01 | 登录方式聚合 | 游客、自建账号、手机号/邮箱、第三方 OAuth(Apple / Google / Steam / 微信 / QQ / Facebook);对游戏暴露统一登录接口 | 必备 | [Nakama 身份链接](https://deepwiki.com/heroiclabs/nakama-godot/3-nakama-game-backend)、[XDSDK 账户管理](https://sdk-docs.xindong.com/)、[MSDK 模块](https://docs.msdk.qq.com/v5/zh-CN/Module/Modules.html) | ✅ 核心 |
| F02 | 账号绑定 / 解绑 / 合并 | 一个账号挂多个平台身份(`link_device/email/steam...`),游客身份升级合并为正式账号 | 必备 | Nakama `link_*`、[XDSDK 绑定解绑](https://sdk-docs.xindong.com/) | ✅ 核心 |
| F03 | 账号注销与数据删除 | 完整注销流程 + 下游数据级联删除(GDPR `erasure`、应用商店均要求**游戏内可达**的入口) | 必备 | [XDSDK 注销流程](https://sdk-docs.xindong.com/)、[商店政策口径](https://privacyterms.io/privacy-policy-for-mobile-game) | ✅ 核心 |
| F04 | 跨平台身份贯通 | 同一玩家在 PC/主机/手机共享进度与身份,以平台 ID 为 key 回捞存档 | 常见 | [EOS Connect](https://github.com/api-evangelist/epic-games/blob/main/apis.yml)、[PGS 质量清单 1.4](https://developer.android.com/games/pgs/quality)、[AccelByte 15+ 平台](https://accelbyte.io/) | ✅ 核心 |
| F05 | 游戏自绘登录 UI / SDK 不带 UI | SDK 只给数据与回调,UI 由游戏实现;或提供可选默认 UI | 常见 | [XDSDK UI 剥离趋势](https://sdk-docs.xindong.com/)、[MSDK 公告只给数据不给 UI](https://docs.msdk.qq.com/v5/zh-CN/Module/Notice.html) | ✅ 核心(UI 永远可选) |
| F06 | 会话/设备历史与账号状态 | 多端会话记录、活跃/闲置/封禁状态、异地登录提示 | 加分 | [自建后端能力清单](https://sumcircle.com/custom-gaming-backend-solution) | 🔌 Provider |
| F07 | 账号风控与防刷 | 设备指纹、异常登录检测、封号/解封、权限(封玩法/封排行榜)分级 | 常见 | [运营工具处罚类型](https://cloud.tencent.com/developer/article/1978643)、[BACKND 举报与处罚](https://backnd.com/en/) | 🔌 Provider |

**Courier 取舍**:F01–F05 是 M1 认证契约的正身(账号是公司级实体、`player` 是 game 维度实体,见 [scope.md](../contract/scope.md));F06/F07 走 Provider,不进核心契约。

## 2. 会话、令牌与调用安全

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F08 | access / refresh token 与自动续期 | 短期访问令牌 + 会话令牌续签,过期静默刷新,不打断游戏 | 必备 | [Unity Authentication 令牌](https://docs.unity.com/en-us/services-web-apis/client-auth)、Nakama session refresh | ✅ 核心 |
| F09 | 服务端验签(客户端凭证不可信) | 客户端拿到的 token/票据必须由服务端验签后才能发奖;密钥走服务端 | 必备 | [QuickSDK 服务端验证口径](https://www.quicksdk.com/)、[AWS 后端校验客户端 token](https://d1.awsstatic.com/events/Summits/reinvent2023/GAM302-R_Build-scalable-cross-platform-game-backends-on-AWS-REPEAT.pdf) | ✅ 核心 |
| F10 | scope / 多环境隔离 | 按 `game_id + env` 隔离资源,token 绑定 scope,越权显式报错 | 常见 | AccelByte namespace + OAuth2、[Courier scope 契约](../contract/scope.md) | ✅ 核心 |
| F11 | 统一错误码与可重试语义 | 客户端可判定「重试/降级/终止」的错误分类 | 常见 | [Courier errors 契约](../contract/errors.md)、EOS REST 错误分类 | ✅ 核心 |
| F12 | 客户端凭据与密钥管理 | SDK 内嵌的 game key 只做准入,不做授权;真凭证在服务端 | 必备 | [EOS Developer Portal 客户端凭证](https://github.com/api-evangelist/epic-games/blob/main/apis.yml) | ✅ 核心 |

**Courier 取舍**:整节都是核心——这正是 [competitive.md](./competitive.md) 结论 3 里「客户端凭证必须服务端验证」的同一口径。

## 3. 玩家数据与存档

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F13 | 云存档 / 进度同步 | 跨设备存进度,离线先写本地、上线补同步 | 必备 | [UGS Cloud Save](https://docs.unity.com/en-us/cloud-save/get-started)、[PGS saved games](https://support.google.com/googleplay/android-developer/answer/2990418)、[GamePush 云存档](https://gamepush.com/en/backend/) | ❓ 待议 |
| F14 | 分级读写权限 | 按可见性分层:本人可读写 / 公开只读 / 仅服务端可写(反作弊数据) | 常见 | [UGS 四级访问](https://docs.unity.com/en-us/cloud-save/get-started)(Default/Public/Protected/Custom) | ❓ 待议 |
| F15 | 写锁与冲突合并 | 并发写入的乐观锁/版本号,避免两端互相覆盖 | 加分 | UGS `SaveItem` write-lock | ❓ 待议 |
| F16 | 玩家档案 | 昵称、头像、自定义元数据,可公开检索 | 必备 | [EOS UserInfo](https://github.com/api-evangelist/epic-games/blob/main/apis.yml)、AccelByte Player Profiles | ✅ 核心(玩家) |
| F17 | 二进制文件存储 | 存档文件、截图、UGC 附件的对象存储 | 常见 | UGS Cloud Save Files、[EOS Title/Player Data Storage](https://github.com/api-evangelist/epic-games/blob/main/apis.yml) | 🔌 Provider |
| F18 | 服务端权威写入 | 影响数值的数据只能由服务端/云函数写,客户端只写偏好类 | 常见 | [UGS Cloud Code 服务端模块](https://cloud-code-sdk-documentation.cloud.unity3d.com/)、[Nakama 服务端逻辑](https://github.com/heroiclabs/nakama) | ✅ 核心(Gateway 侧) |

**Courier 取舍**:F16 / F18 进「玩家」面核心;F13–F15 存档面留给 Provider,不做统一钱包式假设。

## 4. 支付与商业化

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F19 | 支付渠道拉起与商品列表 | Apple IAP / Google Billing / 微信 / 支付宝 / 网页第三方;服务端下发商品 | 必备 | [XDSDK 内购支付](https://sdk-docs.xindong.com/)、[Unity IAP](https://docs.unity.com/en-us/iap/payment-providers/purchases-sdk) | ✅ 核心(递送) |
| F20 | 服务端收据验真 | 收据/票据一律回服务端,向商店 API(App Store Server API / Google Play Developer API)核验后才发奖 | 必备 | [Nakama IAP validation](https://heroiclabs.com/docs/nakama/concepts/iap-validation/)、[GameAnalytics 购买验真](https://docs.gameanalytics.com/event-tracking-and-integrations/advanced-tracking/purchase-validation/) | ✅ 核心(Payment Provider) |
| F21 | 幂等与防重放 | 同一收据/`purchaseToken` 只能入账一次,记录 `seenBefore`;绑定提交账号防跨账号注入 | 必备 | Nakama `seenBefore`、[transactionId/purchaseToken 去重口径](https://www.opoinstall.com/blog/track-optimize-in-app-purchases) | ✅ 核心 |
| F22 | 商品一致性与订单履约 | 校验 product ID 与实付金额,匹配后才授予权益(防「买便宜货解锁贵奖励」) | 必备 | Nakama product mismatch 防护、[Unity IAP fulfillment](https://docs.unity.com/en-us/iap/payment-providers/purchases-sdk) | ✅ 核心 |
| F23 | 订阅状态管理 | 到期/续订/退款状态跟随商店服务端通知更新,支持恢复 | 常见 | Nakama subscriptions + provider 通知、StoreKit 2 JWS | 🔌 Provider |
| F24 | 跨设备恢复购买 | 换设备后拉取历史订单恢复权益 | 必备 | [Unity IAP 后端 fulfillment](https://docs.unity.com/en-us/iap/payment-providers/purchases-sdk) | ✅ 核心 |
| F25 | 交易流水与对账 | 全量交易落库,供客服补单、退款对账、财务审计 | 必备 | [Nakama 交易记录](https://heroiclabs.com/docs/nakama/concepts/iap-validation/)、[运营工具个人充值记录](https://cloud.tencent.com/developer/article/2226974) | ✅ 核心 |
| F26 | 支付回调与补发(掉单处理) | 支付成功但发货中断时的查询、补发、客服补偿入口 | 必备 | [GamePush 支付补发](https://gamepush.com/en/backend/)、[客服补偿购买](https://theymes.com/for/gaming) | ✅ 核心 |

**Courier 取舍**:整节是 Courier 的差异化正身——**「支付递送」= 拉起 + 验真 + 幂等 + 履约 + 补发**,形态开放(不假设统一钱包),实现全部走可插拔 Payment Provider。

## 5. 合规与未成年人保护

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F27 | 实名认证 | 对接官方实名系统(国内中宣部防沉迷实名认证),以开发者主体上报 | 必备(国内) | [TapTap 合规认证](https://developer.taptap.cn/docs/sdk/anti-addiction/features/)、[MSDK 实名制](https://sdk.kurogames.com/p/sdk_list.html)、[Courier realname 契约](../contract/realname.md) | ✅ 核心 |
| F28 | 游戏时长与时段限制 | 未成年人仅周五/六/日及法定节假日 20:00–21:00 可玩;云端查询可玩时段 | 必备(国内) | [TapSDK 防沉迷](https://developer.taptap.cn/docs/sdk/anti-addiction/features/) | ✅ 核心 |
| F29 | 充值额度限制 | <8 岁禁充;8–16 岁单次 ≤50/月 ≤200 元;16–18 岁单次 ≤100/月 ≤400 元 | 必备(国内) | TapSDK 合规认证、[XDSDK 防沉迷金额上报](https://sdk-docs.xindong.com/anti-addict) | ✅ 核心 |
| F30 | 防沉迷数据上报 | 时长、消费金额上报(优先服务端 S2S 上报,避免客户端重复上报) | 必备(国内) | [XDSDK Server-to-Server 上报](https://sdk-docs.xindong.com/anti-addict)、[小米联运 SDK 时长采集](https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1376) | ✅ 核心 |
| F31 | 年龄门与同意管理 | 首启年龄筛查;低于数字同意年龄(GDPR 13–16)走可验证家长同意(VPC);同意记录单独留存 | 必备(海外) | [COPPA/GDPR 架构实务](https://www.strapdata.com/gdpr-coppa-compliance-gaming/)、[GameAnalytics 同意流程](https://www.gameanalytics.com/trust/privacy-faq) | ✅ 核心 |
| F32 | 数据最小化与第三方共享清单 | 每个内置 SDK 的采集项、目的、权限清单;非必要 SDK 默认关闭,按年龄分档启用 | 必备(海外) | [第三方个人信息共享清单实例](https://sdk.kurogames.com/p/sdk_list.html)、[隐私政策口径](https://privacyterms.io/privacy-policy-for-mobile-game) | 🔌 Provider(契约化输出) |
| F33 | 审计与合规留痕 | 实名/同意/支付限额判定的可追溯日志,满足监管与 SOC 2 式举证 | 常见 | [合规留痕要求](https://www.strapdata.com/gdpr-coppa-compliance-gaming/) | ✅ 核心 |

**Courier 取舍**:国内合规(实名/时长/额度/上报)是**必备项而非可选包**——印证 [competitive.md](./competitive.md) 对 XDSDK 的判断;海外合规(年龄门/同意/注销)与 F03 一起构成账号面的默认能力。

## 6. 消息与触达:公告 / 推送 / 应用内消息

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F34 | 公告系统 | 文本/图片/网页链接三形态;分类、分组、起止时间、语种、排序;后台配置下发 | 必备 | [MSDK 公告模块](https://docs.msdk.qq.com/v5/zh-CN/Module/Notice.html)、[BACKND 公告](https://backnd.com/en/) | ✅ 核心 |
| F35 | 公告定向过滤 | 按 OS、渠道、openid、设备 ID、版本号、大区过滤下发;客户端只拿到命中项 | 常见 | [MSDK 公告过滤字段](https://docs.msdk.qq.com/v5/zh-CN/Module/Notice.html) | ✅ 核心 |
| F36 | 跑马灯 / 滚服播报 | 全服滚动消息(中奖、维护、活动),与公告同源不同呈现 | 常见 | [运营工具:公告/跑马灯/全服邮件](https://cloud.tencent.com/developer/article/1978643) | ✅ 核心(消息) |
| F37 | 远程推送 push | APNs / FCM / 国内厂商通道;本地+远程两类;通知栏与应用内消息两种形态 | 必备 | [MSDK 推送模块](https://docs.msdk.qq.com/v5/zh-CN/Module/Push.html)、[UGS Push](https://docs.unity.com/en-us/push-notifications/get-started) | 🔌 Provider(默认 chirp,可换) |
| F38 | 按账号/标签定向推送 | 设备注册之外再绑账号,支持标签(性别/年龄/偏好)分群投放 | 常见 | MSDK `setAccount` + 标签推送、[UGS campaign](https://docs.unity.com/en-us/push-notifications/get-started) | 🔌 Provider |
| F39 | 本地通知 / 定时提醒 | 客户端本地排程(体力恢复、活动倒计时),不依赖网络 | 常见 | MSDK 本地推送、UGS Mobile Notifications | 🔌 Provider |
| F40 | 触达效果统计 | 发送 / 展示 / 点击漏斗,回流归因到活动 | 常见 | [FCM 报表口径](https://firebase.google.com/posts/2019/03/everything-we-announced-at-game/)、UGS `notificationOpened` 事件 | 🔌 Provider |

**Courier 取舍**:公告/跑马灯是 Courier 的**核心正身**(「公告递送到游戏客户端」),只给数据不带 UI;推送整条链路是 Provider,且**不自建长连接通道**(roadmap 非目标)。

## 7. 客服与玩家反馈

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F41 | 游戏内客服会话 / 工单 | 不离开客户端即可提问、提交工单、查看处理进度 | 常见 | [Helpshift 游戏内 SDK](https://www.helpshift.com/blog/mobile-game-support-sdk/)、[Zendesk for Unity](https://www.zendesk.com/blog/unity-zendesk-partnership/)、[BACKND 工单](https://backnd.com/en/) | ✅ 核心 |
| F42 | FAQ / 知识库自助 | 客户端内检索与浏览,命中即免工单(deflection) | 常见 | Zendesk 帮助中心、Helpshift Quicksearch | ✅ 核心 |
| F43 | 上下文自动附带 | 自动带上 player ID、平台、build 号、关卡、购买历史,省去「你的 ID 是多少」 | 常见 | [Theyymes 上下文透传](https://theymes.com/for/gaming)、[Helpshift 上下文](https://www.helpshift.com/blog/mobile-game-support-sdk/) | ✅ 核心 |
| F44 | 玩家意见反馈与日志上传 | 一键反馈 + 附带截图与客户端日志,便于复现 | 常见 | [MSDK Bugly 系模块](https://docs.msdk.qq.com/v5/zh-CN/Module/Modules.html)、[GamePush 玩家反馈](https://gamepush.com/en/backend/) | ✅ 核心 |
| F45 | 举报与处罚申诉 | 玩家举报、封禁通知、申诉入口与处理闭环 | 常见 | [EOS Reports / Sanctions appeal](https://github.com/EpicGames/EOS-Getting-Started)、[BACKND 举报处罚](https://backnd.com/en/) | ✅ 核心 |
| F46 | 客服侧补单 / 补偿 | 客服按账号查询交易、补发道具、退款处理 | 常见 | [客服补偿购买场景](https://theymes.com/for/gaming)、[玩家支持工具对比](https://playerdriven.io/blog/best-player-support-tools-for-game-studios-in-2026) | ✅ 核心 |

**Courier 取舍**:整节是 Courier 的**核心正身**(客服递送),且与 F25 交易流水天然咬合:工单自动带上下文 + 补单走同一套支付 Provider。

## 8. 运营配置与实验

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F47 | 远程配置 / Feature Flag | 不发版改数值、开关玩法;支持灰度放量与**秒级回滚** | 必备 | [Firebase Remote Config](https://firebase.google.com/docs/remote-config)、[UGS Remote Config](https://cloud-code-sdk-documentation.cloud.unity3d.com/) | ✅ 核心 |
| F48 | 分群与条件定向 | 按版本、语言、受众、设备、自定义条件下发不同配置 | 常见 | [Firebase 分群与条件](https://firebase.google.com/docs/remote-config)、MSDK 公告过滤同思路 | ✅ 核心 |
| F49 | A/B 实验 | 对照组自动生成、指标显著性判定,配合 Crashlytics/分析看稳定性 | 常见 | [Firebase A/B Testing](https://firebase.google.com/products/remote-config)、[ByteBrew A/B](https://github.com/ByteBrewIO/ByteBrewAndroidSDK) | 🔌 Provider |
| F50 | 配置版本历史与回滚 | 保留多版本模板,一键回退;记录改动人 | 常见 | [Remote Config 变更历史(300 版/90 天)](https://firebase.google.com/posts/2018/08/in-app-messaging-crashlytics) | ✅ 核心 |
| F51 | GM / 运营工具面 | 用户信息查询、资源发放、开/关服与维护、白名单、功能开关、处罚 | 必备(运营侧) | [《版本运营与 GM 工具》](https://cloud.tencent.com/developer/article/1978643)、[BACKND 管理端](https://backnd.com/en/) | ⬜ 不做界面(默认 Croupier,API 保留) |

**Courier 取舍**:配置/开关的**下发链路**是核心(Gateway 只递送数据,UI 永远可选);运营**界面**不自建,默认 Provider Croupier——与 roadmap 非目标一致。

## 9. 数据、崩溃与归因

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F52 | 埋点与行为分析 | 会话、关卡起止、漏斗、留存 D1/D7/D30、分群下钻 | 必备 | [GameAnalytics](https://www.gameanalytics.com/blog/how-to-integrate-analytics-into-a-game)、[友盟 U-Game](https://devs.umeng.com/)、[ByteBrew](https://github.com/ByteBrewIO/ByteBrewAndroidSDK) | 🔌 Provider |
| F53 | 付费与广告收入统计 | 付费率、ARPU/ARPDAU、内购+广告收入合并口径 | 必备 | [ByteBrew 变现追踪](https://github.com/ByteBrewIO/ByteBrewAndroidSDK)、[友盟](https://devs.umeng.com/) | 🔌 Provider |
| F54 | 买量归因 | 渠道安装归因、SKAN、深链/延迟深链回传 | 常见 | AppsFlyer / Adjust(AppsFlyer/Adjust 为行业通识,见 [归因口径](https://newagesysit.com/mobile-gaming-application-development-services/)) | ⬜ 不做(垂直领域) |
| F55 | 崩溃与性能监控 | 崩溃聚类、面包屑、按 build/版本分布、告警到 Slack/Jira | 必备 | [Firebase Crashlytics](https://firebase.google.com/docs/games/setup)、[XDSDK 集成 Crashlytics](https://sdk-docs.xindong.com/) | 🔌 Provider |
| F56 | 服务端/SDK 遥测 | 调用量、延迟、状态码、SDK 自身健康度 | 常见 | [EOS QoS metrics](https://endlessrunner.co.uk/privacy-policies/epic-online-services-developer-terms)、[AccelByte Game Telemetry](https://github.com/api-evangelist/accelbyte/blob/main/README.md) | ✅ 核心(diagnostics 契约) |
| F57 | 数据导出 | 导出到 BigQuery/S3/自建仓,支持审计与二次分析 | 加分 | [Crashlytics 导出 BigQuery](https://firebase.google.com/posts/2018/08/in-app-messaging-crashlytics)、[AIS 数据仓库](https://www.metaplay.io/comparisons/metaplay-vs-accelbyte) | 🔌 Provider |

**Courier 取舍**:Courier **不自建分析产品**(F52–F55 全部 Provider 化,或干脆交给游戏已有的分析栈);只保证 F56 的诊断契约——SDK 自身可观测,才能谈「递送成功」。

## 10. 进度、成就与留存玩法

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F58 | 排行榜 | 全局/好友/周期榜(日周赛季),自定义排序、分页、按 owner 过滤 | 必备 | [Nakama 排行榜](https://deepwiki.com/heroiclabs/nakama-godot/3-nakama-game-backend)、[EOS Leaderboards](https://github.com/api-evangelist/epic-games/blob/main/apis.yml)、[UGS Leaderboards](https://docs.unity.com/en-us/services) | ❓ 待议 |
| F59 | 成就 / 任务 | 一次性与累积式(incremental)成就、隐藏成就、进度可视化 | 必备 | [PGS 成就质量清单](https://developer.android.com/games/pgs/quality)、EOS Achievements | ❓ 待议 |
| F60 | 数值统计(Stats) | 服务端保存的玩家统计项,供成就/排行榜消费 | 常见 | EOS Stats、AccelByte Statistics | ❓ 待议 |
| F61 | 赛季 / 通行证 / 签到 | 活动周期、奖励轨道、每日奖励 | 常见 | AccelByte Season Pass、[GamePush 奖励调度](https://gamepush.com/en/backend/) | ❓ 待议 |
| F62 | 邮件 / 礼包码 | 全服邮件、单人邮件、兑换码,常与运营工具同源 | 必备(运营侧) | [运营工具:全服&单人邮件](https://cloud.tencent.com/developer/article/1978643)、[MSDK 礼包券](https://cloud.tencent.com/developer/article/1856528) | ❓ 待议(属「消息」近邻) |
| F63 | 活动与限时运营 | 活动日程、倒计时、开服预热 | 常见 | [BACKND 活动与公告](https://backnd.com/en/)、[SpellSync 活动](https://spellsync.com/game-backend/) | ❓ 待议 |

**Courier 取舍**:本节多数是**玩法系统**而非「服务递送」,除 F62 邮件与消息面相邻、值得优先评估外,其余列入待议,避免把 Courier 做成第二个 Nakama(见 [competitive.md](./competitive.md) 结论 3)。

## 11. 社交与实时(边界参考,列而不做)

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F64 | 好友与在线状态 | 好友列表、在线/离线、屏蔽名单 | 常见 | [EOS Friends/Presence/Blocklist](https://github.com/api-evangelist/epic-games/blob/main/apis.yml)、Nakama friends | ⬜ 不做 |
| F65 | 聊天 / 私信 | 频道、私聊、实时消息 | 常见 | [SpellSync 聊天](https://spellsync.com/game-backend/)、Nakama chat | ⬜ 不做 |
| F66 | 匹配 / 大厅 / 会话 | 匹配池、规则、延迟感知、专属服编排 | 常见 | [AccelByte Matchmaking](https://docs.accelbyte.io/gaming-services/modules/multiplayer/matchmaking/unity-integrating-matchmaking/)、Nakama | ⬜ 不做 |
| F67 | 实时语音 | 房间语音、降噪、权限 | 加分 | EOS Voice、TapSDK 实时语音 | ⬜ 不做 |
| F68 | 公会 / 组队 | 组织、成员、权限、公会战 | 常见 | Nakama groups、AccelByte Guilds | ⬜ 不做 |

**Courier 取舍**:整节是 roadmap [非目标](../roadmap.md),与 Nakama/Agones 划清边界;Courier 只做它们与玩家世界之间的**递送层**,两者可共存。

## 12. SDK 工程与交付

| ID | 功能 | 典型能力 | 常见度 | 代表实现 | 取舍 |
| --- | --- | --- | --- | --- | --- |
| F69 | 多引擎一等公民 | Unity / Unreal / Godot / 原生 / C++ 各自 SDK,契约行为跨端一致 | 必备 | [Nakama 多引擎 SDK](https://github.com/heroiclabs/nakama)、[EOS C/C# + Unity/Unreal 插件](https://onlineservices.epicgames.com/sdk?lang=en-US)、[AWS Unity/Unreal/Godot](https://d1.awsstatic.com/events/Summits/reinvent2023/GAM302-R_Build-scalable-cross-platform-game-backends-on-AWS-REPEAT.pdf) | ✅ 核心(五层中的 SDK Contract) |
| F70 | 模块化按需选装 | 只引需要的模块,可拆可删,不为用不到的功能付体积 | 必备 | [MSDK 模块选装](https://docs.msdk.qq.com/v5/zh-CN/Module/Modules.html)、[AccelByte 模块分层](https://www.metaplay.io/comparisons/metaplay-vs-accelbyte) | ✅ 核心(Provider/可选包) |
| F71 | UI 可选 / 游戏自绘 | SDK 无强制 UI;默认 UI 作为可选包 | 常见 | XDSDK UI 剥离、MSDK 公告只给数据 | ✅ 核心 |
| F72 | 初始化与生命周期 | 单次 `Init({gameId, env, endpoint})`;启动、登录态变化、切后台的确定回调 | 必备 | [Courier scope 契约](../contract/scope.md)、UGS 初始化要求 | ✅ 核心 |
| F73 | 弱网重试与离线降级 | 超时/断网的重试与退避;公告等只读内容的本地缓存兜底 | 常见 | [离线-同步口径](https://developer.android.com/games/pgs/quality)(本地先存、认证后同步) | ✅ 核心 |
| F74 | 日志与诊断开关 | 可调日志级别、请求追踪 ID、可上报 SDK 自身错误 | 必备 | [EOS SDK 日志分析](https://onlineservices.epicgames.com/sdk?lang=en-US)、[Courier diagnostics 契约](../contract/diagnostics.md) | ✅ 核心 |
| F75 | 沙箱 / 测试环境 | dev/staging/prod 隔离;商店沙箱收据;mock 与 fixtures 供 CI | 必备 | [Courier scope 三环境](../contract/scope.md)、商店 sandbox 验真 | ✅ 核心 |
| F76 | 多语言与本地化 | 内容按语种下发,SDK 文案可本地化 | 常见 | [MSDK 公告 language 字段](https://docs.msdk.qq.com/v5/zh-CN/Module/Notice.html)、[多语言客服](https://theymes.com/for/gaming) | ✅ 核心 |

**Courier 取舍**:本节几乎全是核心——[competitive.md](./competitive.md) 结论 2 明确要抄 Nakama 的工程面与 MSDK 的插件化,这六条就是那两条结论的落地清单。

---

## 13. 横向结论

**① 必备项集中在五条链路。** 把 `必备` 标记铺开看,游戏 SDK 的刚性功能只有五条:登录与绑定(F01/F02)、服务端验签(F09/F20/F21)、实名合规(F27–F30)、公告与推送(F34/F37)、支付履约与补发(F19/F22/F24/F25/F26)。**其中四条正好落在 Courier 的定位里**——这不是巧合,是「服务递送」这个品类的边界本来就由这些链路划出。

**② 「递送」类功能几乎都是「数据 + 回调」,UI 一律后置。** MSDK 公告明说只给数据、UI 自定;XDSDK 把登录 UI 剥给游戏自绘;Helpshift/Zendesk 的客服 SDK 反而带 UI(因为客服 UI 是它们的产品本体)。**规律:越是后端递送型功能,SDK 越不该带 UI;越是垂直 SaaS,SDK 越靠 UI 绑定。** Courier 属于前者,支持 [competitive.md](./competitive.md) 的「UI 永远可选」。

**③ 合规不是可选包,是分地区的默认项。** 国内实名/时长/额度/上报是上架硬门槛,海外年龄门/同意/注销是商店政策硬门槛。任何「通用游戏 SDK」都必须把它做成默认路径而非扩展模块——这也是 XDSDK 把防沉迷剥离给 TapSDK 后仍要在文档首位强调它的原因。

**④ 垂直领域明确不自建:分析、归因、客服产品、运营界面。** F52–F55(分析/崩溃/归因)、F51(运营界面)、F54(买量归因)行业里已有成熟专门厂商,Courier 只留 F56 的诊断契约与 F51 的 API 面(界面归 Croupier)。**做递送层的人,不该同时做所有层的界面。**

**⑤ 待议区(第 10 节)是最大的范围风险。** 排行榜/成就/赛季这些功能在样本里出现频率极高(因为它们是**玩法**而非**服务**),很容易被「别家都有」推着做进去。建议维持 roadmap 口径:先只评估 F62 邮件/礼包码,其余不动。

### 与已有文档的关系

- 竞品取位、星数、三条结论 → [competitive.md](./competitive.md)(本文不重复)。
- 非目标与里程碑 → [roadmap.md](../roadmap.md)。
- 跨端契约基元 → [contract/](../contract/index.md)(scope / auth / errors / diagnostics / realname)。
