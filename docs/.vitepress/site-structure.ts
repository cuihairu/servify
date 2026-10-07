// 导航与侧边栏按使用任务分组：开始（评估与上手）→ 接入（把 Servify 嵌进自己的站/端）
// → 部署运维 → 安全合规 → AI 知识库 → 远程协助 → 架构与研发治理 → 规划与历史存档。
// implementation/ 有独立侧边栏。新增页面须归入对应分组，避免出现导航孤儿。

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
  '/implementation/11-tenant-scope-inventory',
  '/implementation/12-operator-observability',
  '/implementation/13-ai-agent-loop',
];

const migrationGovernancePages = [
  '/implementation/10-service-to-module-migration',
  '/implementation/10-migration-inventory',
  '/implementation/10-migration-scorecard',
  '/implementation/10-module-boundaries',
];

const startPages = [
  '/v1-product-scope',
  '/release-notes-v1.0.0',
  '/delivery-priorities',
  '/local-development',
  '/contributing',
];

const integratePages = [
  '/embedding-guide',
  '/mobile-sdk-integration',
  '/realtime-translation-design',
  '/voice-pstn-twilio',
  '/voice-protocol-template',
];

const workbenchPages = ['/agent-workspace', '/ticket-workflow'];

const operatePages = [
  '/deployment',
  '/operator-runbook',
  '/webrtc-deployment',
  '/TURN_DEPLOYMENT',
  '/backup-and-recovery',
  '/multi-instance-boundary',
  '/metrics-glossary',
  '/metrics-spec',
  '/perf-baseline',
  '/ai-observability-policy',
];

const securityPages = [
  '/security-baseline-operations',
  '/configuration-scopes',
  '/token-lifecycle-and-key-rotation',
  '/public-surface-security-checklist',
  '/auth-surface-policy',
  '/audit-log-policy',
  '/tenant-workspace-boundaries',
];

const aiPages = [
  '/WEKNORA_INTEGRATION',
  '/RAGFLOW_INTEGRATION',
  '/KNOWLEDGE_BASE_LANDSCAPE',
  '/ai-fallback-behavior',
];

const remoteAssistPages = [
  '/remote-assistance',
  '/remote-assistance-status',
  '/remote-assistance-mvp',
  '/remote-assistance-current-state',
];

const engineeringPages = [
  '/ARCHITECTURE',
  '/current-architecture',
  '/modules-dependency-map',
  '/testing-pyramid',
  '/CI_SELF_HOSTED',
  '/release-versioning',
  '/surface-naming',
  '/generated-assets',
  '/repo-hygiene',
  '/demo-and-mock-boundaries',
  '/acceptance-checklist',
  '/acceptance-weknora-docker',
  '/mobile-sdk-design',
  '/mobile-sdk-platform-spec',
  '/MERMAID_COMPATIBILITY',
];

const archivePages = [
  '/architecture-review-2026',
  '/architecture-redesign-plan',
  '/v1-convergence-plan',
  '/mobile-sdk-cocoapods-evaluation',
  '/release-notes-v0.1.0',
  '/release-0.1.0-acceptance',
  '/review/chatgpt-review-2026-10-04',
  '/review/chatgpt-review-2026-10-04-verification',
  '/superpowers/plans/2025-05-01-pgvector-knowledge-base',
  '/superpowers/specs/2025-05-01-pgvector-knowledge-base-design',
];

export const docsNav = [
  { text: '首页', link: '/' },
  { text: '产品', link: '/v1-product-scope' },
  { text: 'Web 嵌入', link: '/embedding-guide' },
  { text: '坐席工作台', link: '/agent-workspace' },
  { text: '部署', link: '/deployment' },
  { text: '运维', link: '/operator-runbook' },
  { text: '移动端 SDK', link: '/mobile-sdk-integration' },
  {
    text: '运行与安全',
    items: [
      { text: '安全基线', link: '/security-baseline-operations' },
      { text: '配置作用域', link: '/configuration-scopes' },
      { text: 'Token 生命周期', link: '/token-lifecycle-and-key-rotation' },
      { text: '开放接口安全清单', link: '/public-surface-security-checklist' },
      { text: '备份与恢复', link: '/backup-and-recovery' },
    ],
  },
  {
    text: '研发附录',
    items: [
      { text: '实施计划', link: '/implementation/' },
      { text: '总体架构', link: '/ARCHITECTURE' },
      { text: '当前架构分析', link: '/current-architecture' },
      { text: 'WeKnora 集成', link: '/WEKNORA_INTEGRATION' },
      { text: '测试金字塔', link: '/testing-pyramid' },
      { text: 'CI / Runner', link: '/CI_SELF_HOSTED' },
      { text: '版本发布', link: '/release-versioning' },
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
    { text: '开始', items: ['/', ...startPages] },
    { text: '接入与集成', items: integratePages },
    { text: '坐席与工单', items: workbenchPages },
    { text: '部署与运维', items: operatePages },
    { text: '安全与合规', items: securityPages },
    { text: 'AI 与知识库', items: aiPages },
    { text: '远程协助', items: remoteAssistPages },
    { text: '架构与研发治理', items: engineeringPages },
    { text: '规划与历史存档', items: archivePages },
    { text: '实施计划', items: ['/implementation/', ...implementationPages, ...migrationGovernancePages] },
  ],
};
