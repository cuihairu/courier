import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Courier',
  description: '玩家服务 SDK + 轻量 API 网关',
  base: '/courier/',
  markdown: {
    lineNumbers: true
  },
  themeConfig: {
    nav: [
      { text: '首页', link: '/' },
      { text: '架构', link: '/architecture' },
      { text: '路线图', link: '/roadmap' },
    ],
    sidebar: [
      {
        text: '指南',
        items: [
          { text: '架构', link: '/architecture' },
          { text: '路线图', link: '/roadmap' },
        ]
      }
    ],
    socialLinks: [
      { icon: 'github', link: 'https://github.com/cuihairu/courier' }
    ]
  }
})
