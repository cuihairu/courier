import { defineConfig } from 'vitepress'

export default defineConfig({
  // 官方 i18n：root = English（根路径），zh = 简体中文（/zh/）。
  // 中文源文档位于 docs/zh/，英文源文档位于 docs/（由 *.en.md 提为根路径）。
  locales: {
    root: { label: 'English', lang: 'en' },
    zh: {
      label: '简体中文',
      lang: 'zh-CN',
      link: '/zh/',
      title: 'Courier — 通用游戏 SDK',
      description: '通用游戏 SDK — 为 Unity / Unreal / Cocos / Godot / 微信小游戏 / LayaBox 提供统一的账号、认证、公告、客服、推送、支付、实名、远程配置、品牌与诊断等游戏服务接口',
      themeConfig: {
        nav: [
          { text: '首页', link: '/zh/' },
          { text: '架构', link: '/zh/architecture' },
          { text: '契约', link: '/zh/contract/' },
          { text: '路线图', link: '/zh/roadmap' },
          { text: '竞品调研', link: '/zh/research/competitive' },
          { text: '功能调研', link: '/zh/research/features' },
        ],
        sidebar: [
          {
            text: '指南',
            items: [
              { text: '架构', link: '/zh/architecture' },
              { text: '五层架构', link: '/zh/layers' },
              { text: '路线图', link: '/zh/roadmap' },
              { text: 'TODO', link: '/zh/todo' },
              { text: '选装清单', link: '/zh/sdk-selection' },
              { text: 'Provider 接入指南', link: '/zh/provider-guide' },
              { text: '竞品调研', link: '/zh/research/competitive' },
              { text: '功能调研', link: '/zh/research/features' },
              { text: '决策记录', link: '/zh/research/decisions' },
            ],
          },
          {
            text: '契约',
            items: [
              { text: '总纲', link: '/zh/contract/' },
              { text: '基元', link: '/zh/contract/primitives' },
              { text: '错误', link: '/zh/contract/errors' },
              { text: 'Scope', link: '/zh/contract/scope' },
              { text: '版本', link: '/zh/contract/versioning' },
              { text: '事件', link: '/zh/contract/events' },
              { text: '认证(M1)', link: '/zh/contract/auth' },
              { text: '公告(M2)', link: '/zh/contract/announcement' },
              { text: '客服(M2)', link: '/zh/contract/support' },
              { text: '推送(M2)', link: '/zh/contract/messages' },
              { text: '实名', link: '/zh/contract/realname' },
              { text: '品牌', link: '/zh/contract/branding' },
              { text: '配置(M3)', link: '/zh/contract/config' },
              { text: '应用(M3)', link: '/zh/contract/app' },
              { text: '档案(M3)', link: '/zh/contract/player' },
              { text: '诊断', link: '/zh/contract/diagnostics' },
              { text: '助手(M4)', link: '/zh/contract/assistant' },
              { text: '支付(M5)', link: '/zh/contract/payment' },
              { text: 'Fixtures(机器可读快照)', link: '/zh/contract/fixtures/' },
            ],
          },
        ],
      },
    },
  },

  title: 'Courier — Universal Game SDK',
  description: 'Universal Game SDK — unified account, auth, announcements, support, push, payments, real-name, remote config, branding and diagnostics game-service interfaces for Unity / Unreal / Cocos / Godot / WeChat mini-game / LayaBox',
  base: '/courier/',
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/courier/logo.svg' }]],
  markdown: {
    lineNumbers: true
  },
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Home', link: '/' },
      { text: 'Architecture', link: '/architecture' },
      { text: 'Contracts', link: '/contract/' },
      { text: 'Roadmap', link: '/roadmap' },
      { text: 'Competitive Research', link: '/research/competitive' },
      { text: 'Feature Research', link: '/research/features' },
    ],
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Architecture', link: '/architecture' },
          { text: 'Five-Layer Architecture', link: '/layers' },
          { text: 'Roadmap', link: '/roadmap' },
          { text: 'TODO', link: '/todo' },
          { text: 'SDK Selection', link: '/sdk-selection' },
          { text: 'Provider Integration Guide', link: '/provider-guide' },
          { text: 'Competitive Research', link: '/research/competitive' },
          { text: 'Feature Research', link: '/research/features' },
          { text: 'Decision Record', link: '/research/decisions' },
        ],
      },
      {
        text: 'Contracts',
        items: [
          { text: 'Constitution', link: '/contract/' },
          { text: 'Primitives', link: '/contract/primitives' },
          { text: 'Errors', link: '/contract/errors' },
          { text: 'Scope', link: '/contract/scope' },
          { text: 'Versioning', link: '/contract/versioning' },
          { text: 'Events', link: '/contract/events' },
          { text: 'Auth (M1)', link: '/contract/auth' },
          { text: 'Announcements (M2)', link: '/contract/announcement' },
          { text: 'Support (M2)', link: '/contract/support' },
          { text: 'Messages (M2)', link: '/contract/messages' },
          { text: 'Real Name', link: '/contract/realname' },
          { text: 'Branding', link: '/contract/branding' },
          { text: 'Config (M3)', link: '/contract/config' },
          { text: 'App (M3)', link: '/contract/app' },
          { text: 'Player (M3)', link: '/contract/player' },
          { text: 'Diagnostics', link: '/contract/diagnostics' },
          { text: 'Assistant (M4)', link: '/contract/assistant' },
          { text: 'Payments (M5)', link: '/contract/payment' },
          { text: 'Fixtures (machine-readable snapshot)', link: '/contract/fixtures/' },
        ],
      },
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/courier' }
    ]
  }
})