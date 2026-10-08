# 品牌定制契约(Branding)

> 状态:Draft v0 · 里程碑 M3(与 Remote Config 同管道)。
> 目的:可选 UI 包展示**接入方**的品牌,而不是 Courier 的标。**Core 不含品牌逻辑**——契约只有字段,消费全在 UI 包。

## 资源模型

```json
{
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

- 颜色一律 `#RRGGBB`;URL 均为 CDN/对象存储直链(HTTPS)。
- 字段全部可选,见「兜底」。

## 下发与缓存

- 端点:`GET /v1/app/branding`(按 scope 隔离,同一契约面归入 App 域路由)。
- 响应带 `version`(单调递增)与 ETag;客户端缓存,`version` 变化即热生效(轮询或 `config.updated` 推送,见 events.md)。
- 链路:`BrandingProvider`(默认自建;素材可存对象存储)→ Gateway 缓存下发 → SDK 透传 → UI 包消费。与 Remote Config 同管道、同缓存语义。

## 兜底规则

- 任何字段缺失 → UI 包使用该字段的 Courier 内置默认。
- 整能力未配置/未开启 → 全默认,Courier 自己的标兜底。
- Core 对字段零解释、零渲染、零缓存策略(缓存归平台 Adapter 的存储实现)。

## 构建期物料附录(指引,非契约)

UI 包运行时消费上面下发的品牌;应用图标等构建期资产无法热下发,各引擎模板工程须提供替换指引:

| 平台 | 清单 |
| --- | --- |
| Unity | Player Settings 图标(逐尺寸)、启动画面、包名相关品牌位 |
| iOS | AppIcon 全尺寸(20/29/40/60/76/83.5 pt 的 @2x/@3x)、启动屏 |
| Android | `mipmap-mdpi … xxxhdpi` launcher 图标 + 自适应图标(foreground/background)、应用名 |
| 小程序/小游戏 | 平台后台配置项(图标、名称、简介)核对单 |
| UE / Godot / Layabox | 各自打包设置中的图标与启动图清单 |

模板工程放置占位资源 + `BRANDING.md` 勾选清单,保证「接入方 logo 替换」是流程而非考古。
