import { defineConfig } from 'vitepress';
import { withMermaid } from 'vitepress-plugin-mermaid';
import { docsNav, docsSidebar } from './site-structure';

const docsBase =
  process.env.SERVIFY_DOCS_BASE ??
  (process.env.GITHUB_ACTIONS === 'true' ? '/servify/' : '/');

const config = defineConfig({
  srcDir: '.',
  lang: 'zh-CN',
  title: 'Servify Docs',
  description: 'Servify product docs for intelligent customer service, remote assistance, deployment, and operations',
  base: docsBase,
  head: [['meta', { name: 'theme-color', content: '#0f172a' }]],
  ignoreDeadLinks: true,

  themeConfig: {
    logo: '/icon.png',
    nav: docsNav,
    sidebar: docsSidebar,
    editLink: {
      pattern: 'https://github.com/cuihairu/servify/edit/main/docs/:path',
      text: '在 GitHub 上编辑此页',
    },
    lastUpdated: {
      text: '最后更新',
      formatOptions: {
        dateStyle: 'short',
        timeStyle: 'short',
      },
    },
    docFooter: {
      prev: '上一页',
      next: '下一页',
    },
    outline: {
      label: '页面导航',
      level: [2, 3],
    },
    search: {
      provider: 'local',
    },
  },
});

export default withMermaid(config);
