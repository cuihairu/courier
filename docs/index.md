---
layout: home

hero:
  name: Courier
  text: Universal Game SDK
  tagline: A universal SDK for integrating common game services across engines and platforms —— 为 Unity、Unreal、Cocos、Godot 等引擎提供统一的账号、认证、玩家、消息、公告、客服、支付、实名、远程配置、品牌与诊断等能力接口
  actions:
    - theme: brand
      text: 契约宪法
      link: /contract/
    - theme: alt
      text: 架构
      link: /architecture
    - theme: alt
      text: 路线图
      link: /roadmap

features:
  - title: 账号与会话
    details: 注册/登录、token 轮换、设备绑定;一个账号登录所有游戏
  - title: 玩家
    details: 账号 ↔ 游戏角色映射与档案接口,角色数据归各游戏
  - title: 消息与公告
    details: 实时推送与公告、活动、维护通知的拉取/订阅,通道可换可关
  - title: 客服
    details: 工单提交/查询、FAQ 检索;面板属可选 UI 包
  - title: 实名(可选,合规)
    details: Provider 热插拔(降级链+熔断);默认关闭,开启明示数据范围
  - title: 远程配置与品牌
    details: 按 game_id/env/platform/version/region 匹配;品牌素材下发,UI 包消费
  - title: 诊断(可选)
    details: 崩溃/错误追踪、OTel Trace、性能指标、埋点;默认全关,未配置即 no-op
  - title: 支付(仅契约)
    details: Order/Purchase/Receipt;账号钱包/游戏钱包/直购/外部支付四形态可选
---
