# 品牌定制契约(Branding)

> 状态:**Frozen v1** · 里程碑 M3(与 Remote Config 同管道)· 冻结日期 2026-10-09。
> 目的:可选 UI 包展示**接入方**的品牌,而不是 Courier 的标。**Core 不含品牌逻辑**——契约只有字段,消费全在 UI 包。域前缀已注册([errors.md](./errors.md):本域无专属错误码);默认供应商 **scribe**(与 config/app 同管道)。

## 资源模型

```json
{
  "version": 3,
  "companyName": "示例互娱",
  "productName": "幻想大陆",
  "logo": {
    "svg": "https://…/logo.svg",
    "png": { "1x": "https://…/logo@1x.png", "2x": "https://…/logo@2x.png", "3x": "https://…/logo@3x.png" }
  },
  "icon": { "android": "https://…/icon.png", "ios": "https://…/icon.png", "web": "https://…/icon.png" },
  "theme": { "primaryColor": "#4C8DFF", "secondaryColor": "#1F2937" },
  "about": { "title": "关于我们", "body": "…" },
  "supportEntry": { "label": "联系客服", "url": "https://…/support" }
}
```

- `version`:品牌物料版本,单调递增(热生效判据);与 config 的 `configVersion` 相互独立。
- 颜色一律 `#RRGGBB`;URL 均为 CDN/对象存储直链(HTTPS)。
- 业务字段全部可选,见「兜底规则」;除 `version` 外字段名不与任何契约语义冲突(管理面禁止占用 `version` 键)。

## 下发与缓存

- 端点:`GET /v1/app/branding`(**匿名可**——登录页就要显示品牌;带有效 Bearer 亦可),挂 App 域路由(`/v1/app/*`,维护门白名单内),scope header 要求同 [auth.md](./auth.md)。
- 未设置过品牌 → `200 {"version": 0}`(空字段,UI 全默认兜底);空集不是错误。
- 响应整体缓存(`version` 变化即失效重拉);缓存策略归平台 Adapter 的存储实现,Core 不定缓存。
- 链路:`BrandingProvider`(默认 scribe;素材存对象存储)→ Gateway 透传投影 → SDK 透传 → UI 包消费。与 Remote Config 同管道。
- 热生效:经 [messages.md](./messages.md) 通道 `branding.updated` 事件,data 只带 `{"version": n}`(广播;客户端收后重拉比对,相同即忽略)。

## 兜底规则

- 任何字段缺失 → UI 包使用该字段的 Courier 内置默认。
- 整能力未配置/未开启(`501 COMMON_CAPABILITY_DISABLED`)→ SDK 返回「未启用」态(null),UI 全默认,Courier 自己的标兜底。
- Core 与 Service 对字段零解释、零渲染(透传原始 JSON);缓存归平台 Adapter 的存储实现。

## 错误码(域表)

本域**无新增错误码**:参数/认证/限流/能力关闭一律通用码([errors.md](./errors.md))。

## 客户端要求(各端 SDK)

- SDK 透传原始 JSON(`version` 之外的字段原样交给 UI 包),不做字段校验与类型收窄。
- `branding.updated` 事件到达后重拉比对 `version`,相同即忽略(不重渲染)。
- UI 包持有 Courier 默认标常量,作为全部字段的兜底(消费示例:Unity UI 包 BrandingCatalog)。

## 构建期物料附录(指引,非契约)

UI 包运行时消费上面下发的品牌;应用图标等构建期资产无法热下发,各引擎模板工程须提供替换指引:

| 平台 | 清单 |
| --- | --- |
| Unity | Player Settings 图标(逐尺寸)、启动画面、包名相关品牌位 |
| iOS | AppIcon 全尺寸(20/29/40/60/76/83.5 pt 的 @2x/@3x)、启动屏 |
| Android | `mipmap-mdpi … xxxhdpi` launcher 图标 + 自适应图标(foreground/background)、应用名 |
| 小程序/小游戏 | 平台后台配置项(图标、名称、简介)核对单 |
| UE / Godot / Layabox | 各自打包设置中的图标与启动图清单 |

模板工程放置占位资源 + `BRANDING.md` 勾选清单,保证「接入方 logo 替换」是流程而非考古(模板工程落地随各端模板批次,本附录即指引落档)。
