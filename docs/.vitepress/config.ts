import { defineConfig } from 'vitepress';
import { withMermaid } from 'vitepress-plugin-mermaid';
import { docsNavEn, docsNavZh, docsSidebarEn, docsSidebarZh } from './site-structure';

const docsBase =
  process.env.SERVIFY_DOCS_BASE ??
  (process.env.GITHUB_ACTIONS === 'true' ? '/servify/' : '/');

// i18n 按 VitePress 官方约定：root locale 的文件放 srcDir/，其余 locale 放
// srcDir/<localeKey>/。因此英文在 docs/，中文在 docs/zh/，中文路由为 /zh/...。
// 语言切换器由主题根据 locales 自动渲染，无需自定义组件。
const config = defineConfig({
  srcDir: '.',
  title: 'Servify Docs',
  description:
    'Servify product docs for intelligent customer service, remote assistance, deployment, and operations',
  base: docsBase,
  head: [['meta', { name: 'theme-color', content: '#0f172a' }]],
  // 仅放行文档里的 localhost 示例地址；站内死链仍然 build 即断。
  ignoreDeadLinks: [/^https?:\/\/localhost/],

  // 无对应中文版且未入任何导航的存档评审页：不入站，避免官方语言切换器产生死链。
  srcExclude: ['review/luna-review-2026-10-18.md', 'review/sdk-kb-audit-2026-10-19.md'],

  locales: {
    root: {
      label: 'English',
      lang: 'en',
      title: 'Servify Docs',
      description:
        'Servify product docs for intelligent customer service, remote assistance, deployment, and operations',
      themeConfig: {
        nav: docsNavEn,
        sidebar: docsSidebarEn,
        editLink: {
          pattern: 'https://github.com/cuihairu/servify/edit/main/docs/:path',
          text: 'Edit this page on GitHub',
        },
        lastUpdated: {
          text: 'Last updated',
          formatOptions: {
            dateStyle: 'short',
            timeStyle: 'short',
          },
        },
        docFooter: {
          prev: 'Previous page',
          next: 'Next page',
        },
        outline: {
          label: 'On this page',
          level: [2, 3],
        },
      },
    },
    zh: {
      label: '简体中文',
      lang: 'zh-CN',
      title: 'Servify 文档',
      description: 'Servify 产品文档：智能客服、远程协助、部署与运维',
      themeConfig: {
        nav: docsNavZh,
        sidebar: docsSidebarZh,
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
      },
    },
  },

  // 语言无关的项放在根 themeConfig，会被每个 locale 继承。
  themeConfig: {
    logo: '/icon.png',
    search: {
      provider: 'local',
    },
  },
});

export default withMermaid(config);
