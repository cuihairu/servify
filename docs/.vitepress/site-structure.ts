const implementationPages = [
  '/implementation/01-platform-and-runtime',
  '/implementation/02-ai-and-knowledge',
  '/implementation/03-business-modules',
  '/implementation/04-sdk-and-channel-adapters',
  '/implementation/05-engineering-hardening',
  '/implementation/06-voice-and-protocol-expansion',
  '/implementation/07-sdk-multi-surface',
  '/implementation/08-ai-provider-expansion',
  '/implementation/09-runtime-and-repo-hygiene',
  '/implementation/11-tenant-auth-and-audit',
  '/implementation/12-operator-observability',
];

const migrationGovernancePages = [
  '/implementation/10-service-to-module-migration',
  '/implementation/10-migration-inventory',
  '/implementation/10-migration-scorecard',
  '/implementation/10-module-boundaries',
];

const productPages = [
  '/v1-product-scope',
  '/ARCHITECTURE',
  '/current-architecture',
  '/architecture-redesign-plan',
  '/remote-assistance',
  '/deployment',
  '/local-development',
];

const operationsPages = [
  '/security-baseline-operations',
  '/configuration-scopes',
  '/token-lifecycle-and-key-rotation',
  '/public-surface-security-checklist',
];

const appendixPages = [
  '/ARCHITECTURE',
  '/WEKNORA_INTEGRATION',
  '/CI_SELF_HOSTED',
  '/release-versioning',
  '/testing-pyramid',
  '/MERMAID_COMPATIBILITY',
];

export const docsNav = [
  { text: '首页', link: '/' },
  { text: '产品', link: '/v1-product-scope' },
  { text: '架构', link: '/ARCHITECTURE' },
  { text: '部署', link: '/deployment' },
  {
    text: '运行与安全',
    items: [
      { text: '安全基线', link: '/security-baseline-operations' },
      { text: '配置作用域', link: '/configuration-scopes' },
      { text: 'Token 生命周期', link: '/token-lifecycle-and-key-rotation' },
      { text: '开放接口安全清单', link: '/public-surface-security-checklist' },
    ],
  },
  {
    text: '研发附录',
    items: [
      { text: '实施计划', link: '/implementation/' },
      { text: '当前架构分析', link: '/current-architecture' },
      { text: '架构重设计计划', link: '/architecture-redesign-plan' },
      { text: 'WeKnora 集成', link: '/WEKNORA_INTEGRATION' },
      { text: 'CI / Runner', link: '/CI_SELF_HOSTED' },
      { text: '版本发布', link: '/release-versioning' },
      { text: '测试金字塔', link: '/testing-pyramid' },
      { text: 'Mermaid 兼容性', link: '/MERMAID_COMPATIBILITY' },
    ],
  },
];

export const docsSidebar = {
  '/implementation/': [
    {
      text: '实施主线',
      items: ['/implementation/', ...implementationPages],
    },
    {
      text: '模块迁移治理',
      items: migrationGovernancePages,
    },
  ],
  '/': [
    {
      text: '产品与上手',
      items: ['/', ...productPages],
    },
    {
      text: '运行与安全',
      items: operationsPages,
    },
    {
      text: '研发附录',
      items: [
        '/implementation/',
        ...implementationPages,
        ...migrationGovernancePages,
        ...appendixPages.filter((page) => page !== '/ARCHITECTURE'),
      ],
    },
  ],
};
