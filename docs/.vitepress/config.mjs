import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Courier — Universal Game SDK',
  description: 'Universal Game SDK — 为 Unity / Unreal / Cocos / Godot 等引擎提供统一的账号、认证、公告、客服、支付、实名、远程配置、品牌与诊断等游戏服务接口',
  base: '/courier/',
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/courier/logo.svg' }]],
  markdown: {
    lineNumbers: true
  },
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: '首页', link: '/' },
      { text: '架构', link: '/architecture' },
      { text: '契约', link: '/contract/' },
      { text: '路线图', link: '/roadmap' },
      { text: '竞品调研', link: '/research/competitive' },
      { text: '功能调研', link: '/research/features' },
    ],
    sidebar: [
      {
        text: '指南',
        items: [
          { text: '架构', link: '/architecture' },
          { text: '五层架构', link: '/layers' },
          { text: '路线图', link: '/roadmap' },
          { text: 'TODO', link: '/todo' },
          { text: '竞品调研', link: '/research/competitive' },
          { text: '功能调研', link: '/research/features' },
        ]
      },
      {
        text: '契约',
        items: [
          { text: '总纲', link: '/contract/' },
          { text: '基元', link: '/contract/primitives' },
          { text: '错误', link: '/contract/errors' },
          { text: 'Scope', link: '/contract/scope' },
          { text: '版本', link: '/contract/versioning' },
          { text: '事件', link: '/contract/events' },
          { text: '认证(M1)', link: '/contract/auth' },
          { text: '公告(M2)', link: '/contract/announcement' },
          { text: '客服(M2)', link: '/contract/support' },
          { text: '推送(M2)', link: '/contract/messages' },
          { text: '实名', link: '/contract/realname' },
          { text: '品牌', link: '/contract/branding' },
          { text: '诊断', link: '/contract/diagnostics' },
        ]
      }
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/courier' }
    ]
  }
})
