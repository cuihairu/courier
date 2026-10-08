import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Courier',
  description: 'Universal Game SDK — 为 Unity / Unreal / Cocos / Godot 等引擎提供统一的账号、认证、公告、客服、支付等游戏服务接口',
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
      { text: '五层架构', link: '/layers' },
      { text: '路线图', link: '/roadmap' },
    ],
    sidebar: [
      {
        text: '指南',
        items: [
          { text: '架构', link: '/architecture' },
          { text: '五层架构', link: '/layers' },
          { text: '路线图', link: '/roadmap' },
          { text: 'TODO', link: '/todo' },
        ]
      }
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/courier' }
    ]
  }
})
