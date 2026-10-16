// 导航与侧边栏按使用任务分组：开始（评估与上手）→ 接入（把 Servify 嵌进自己的站/端）
// → 部署运维 → 安全合规 → AI 知识库 → 远程协助 → 架构与研发治理 → 规划与历史存档。
// implementation/ 有独立侧边栏。新增页面须归入对应分组，避免出现导航孤儿。
//
// i18n 布局遵循 VitePress 官方目录约定：英文（root locale）在 docs/，中文在 docs/zh/。
// 两个 locale 的 nav / sidebar 各自独立维护，不共用同一份对象——中文侧的链接必须带
// /zh 前缀，且只列出 docs/zh/ 下真实存在的页面；尚未产出英文译版的页面不进英文侧栏，
// 缺口登记在 todo.md。

// 界面原型是静态 HTML（构建时由 scripts 复制进 dist/design/prototypes-v1/），
// 不随 locale 复制，两侧统一指向根路径。
const PROTOTYPES_LINK = '/design/prototypes-v1/';

// ── English (root locale, docs/) ────────────────────────────────────────────
// 28 篇子目录英文正文已落地（2026-10-16），英文侧栏与 zh 侧栏逐组对齐。
const startPagesEn = [
  { text: 'Competitive Survey', link: '/research/competitive-survey' },
  { text: 'UI Prototypes v1', link: PROTOTYPES_LINK },
  '/v1-product-scope',
  '/delivery-priorities',
  '/local-development',
  '/contributing',
];

const integratePagesEn = [
  '/web-sdk',
  '/embedding-guide',
  '/mobile-sdk-integration',
  '/realtime-translation-design',
  '/voice-pstn-twilio',
  '/voice-protocol-template',
];

const workbenchPagesEn = ['/agent-workspace', '/ticket-workflow'];

const operatePagesEn = [
  '/deployment',
  '/operator-runbook',
  '/webrtc-deployment',
  '/TURN_DEPLOYMENT',
  '/backup-and-recovery',
  '/multi-instance-boundary',
  '/metrics-glossary',
  '/metrics-spec',
  '/design/metrics-snapshot-read-exit',
  '/design/tenant-quota-policy',
  '/perf-baseline',
  '/ai-observability-policy',
];

const securityPagesEn = [
  '/security-baseline-operations',
  '/configuration-scopes',
  '/token-lifecycle-and-key-rotation',
  '/public-surface-security-checklist',
  '/auth-surface-policy',
  '/audit-log-policy',
  '/tenant-workspace-boundaries',
];

const aiPagesEn = [
  '/WEKNORA_INTEGRATION',
  '/RAGFLOW_INTEGRATION',
  '/KNOWLEDGE_BASE_LANDSCAPE',
  '/design/knowledge-provider-selection-policy',
  '/ai-fallback-behavior',
];

const remoteAssistPagesEn = [
  '/remote-assistance',
  '/remote-assistance-status',
  '/remote-assistance-mvp',
  '/remote-assistance-current-state',
];

const engineeringPagesEn = [
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
  '/审计-文档一致性',
];

const archivePagesEn = [
  '/architecture-review-2026',
  '/architecture-redesign-plan',
  '/v1-convergence-plan',
  '/mobile-sdk-cocoapods-evaluation',
  '/release-0.1.0-acceptance',
  '/review/chatgpt-review-2026-10-04',
  '/review/chatgpt-review-2026-10-04-verification',
  '/superpowers/plans/2025-05-01-pgvector-knowledge-base',
  '/superpowers/specs/2025-05-01-pgvector-knowledge-base-design',
];

const implementationPagesEn = [
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

const migrationGovernancePagesEn = [
  '/implementation/10-service-to-module-migration',
  '/implementation/10-migration-inventory',
  '/implementation/10-migration-scorecard',
  '/implementation/10-module-boundaries',
];

export const docsNavEn = [
  { text: 'Home', link: '/' },
  { text: 'Product', link: '/v1-product-scope' },
  { text: 'Web SDK', link: '/web-sdk' },
  { text: 'Web Embed', link: '/embedding-guide' },
  { text: 'Agent Workspace', link: '/agent-workspace' },
  { text: 'Deployment', link: '/deployment' },
  { text: 'Operations', link: '/operator-runbook' },
  { text: 'Mobile SDK', link: '/mobile-sdk-integration' },
  {
    text: 'Runtime & Security',
    items: [
      { text: 'Security Baseline', link: '/security-baseline-operations' },
      { text: 'Configuration Scopes', link: '/configuration-scopes' },
      { text: 'Token Lifecycle', link: '/token-lifecycle-and-key-rotation' },
      { text: 'Public Surface Checklist', link: '/public-surface-security-checklist' },
      { text: 'Backup & Recovery', link: '/backup-and-recovery' },
    ],
  },
  {
    text: 'Engineering',
    items: [
      { text: 'UI Prototypes v1', link: PROTOTYPES_LINK },
      { text: 'Competitive Survey', link: '/research/competitive-survey' },
      { text: 'Implementation', link: '/implementation/' },
      { text: 'Architecture', link: '/ARCHITECTURE' },
      { text: 'Current Architecture', link: '/current-architecture' },
      { text: 'WeKnora Integration', link: '/WEKNORA_INTEGRATION' },
      { text: 'Testing Pyramid', link: '/testing-pyramid' },
      { text: 'CI / Runner', link: '/CI_SELF_HOSTED' },
      { text: 'Versioning', link: '/release-versioning' },
    ],
  },
];

export const docsSidebarEn = {
  '/implementation/': [
    {
      text: 'Implementation Track',
      items: ['/implementation/', ...implementationPagesEn],
    },
    {
      text: 'Module Migration Governance',
      items: migrationGovernancePagesEn,
    },
  ],
  '/': [
    { text: 'Start', items: ['/', ...startPagesEn] },
    { text: 'Integration', items: integratePagesEn },
    { text: 'Agents & Tickets', items: workbenchPagesEn },
    { text: 'Deployment & Operations', items: operatePagesEn },
    { text: 'Security & Compliance', items: securityPagesEn },
    { text: 'AI & Knowledge', items: aiPagesEn },
    { text: 'Remote Assistance', items: remoteAssistPagesEn },
    { text: 'Architecture & Engineering', items: engineeringPagesEn },
    { text: 'Planning & Archive', items: archivePagesEn },
    {
      text: 'Implementation',
      items: ['/implementation/', ...implementationPagesEn, ...migrationGovernancePagesEn],
    },
  ],
};

// ── 简体中文（locale key: zh, docs/zh/）────────────────────────────────────
// 侧栏 key 必须带 /zh 前缀：VitePress 用含 locale 的 relativePath 做前缀匹配。

const implementationPagesZh = [
  '/zh/implementation/01-platform-and-runtime',
  '/zh/implementation/02-ai-and-knowledge',
  '/zh/implementation/03-business-modules',
  '/zh/implementation/04-sdk-and-channel-adapters',
  '/zh/implementation/05-engineering-hardening',
  '/zh/implementation/06-voice-and-protocol-expansion',
  '/zh/implementation/07-sdk-multi-surface',
  '/zh/implementation/08-ai-provider-expansion',
  '/zh/implementation/09-runtime-and-repo-hygiene',
  '/zh/implementation/11-tenant-auth-and-audit',
  '/zh/implementation/11-tenant-scope-inventory',
  '/zh/implementation/12-operator-observability',
  '/zh/implementation/13-ai-agent-loop',
];

const migrationGovernancePagesZh = [
  '/zh/implementation/10-service-to-module-migration',
  '/zh/implementation/10-migration-inventory',
  '/zh/implementation/10-migration-scorecard',
  '/zh/implementation/10-module-boundaries',
];

const startPagesZh = [
  { text: '竞品调研（同类客服产品）', link: '/zh/research/competitive-survey' },
  { text: '界面原型 v1 设计说明', link: PROTOTYPES_LINK },
  '/zh/v1-product-scope',
  '/zh/delivery-priorities',
  '/zh/local-development',
  '/zh/contributing',
];

const integratePagesZh = [
  '/zh/web-sdk',
  '/zh/embedding-guide',
  '/zh/mobile-sdk-integration',
  '/zh/realtime-translation-design',
  '/zh/voice-pstn-twilio',
  '/zh/voice-protocol-template',
];

const workbenchPagesZh = ['/zh/agent-workspace', '/zh/ticket-workflow'];

const operatePagesZh = [
  '/zh/deployment',
  '/zh/operator-runbook',
  '/zh/webrtc-deployment',
  '/zh/TURN_DEPLOYMENT',
  '/zh/backup-and-recovery',
  '/zh/multi-instance-boundary',
  '/zh/metrics-glossary',
  '/zh/metrics-spec',
  '/zh/design/metrics-snapshot-read-exit',
  '/zh/design/tenant-quota-policy',
  '/zh/perf-baseline',
  '/zh/ai-observability-policy',
];

const securityPagesZh = [
  '/zh/security-baseline-operations',
  '/zh/configuration-scopes',
  '/zh/token-lifecycle-and-key-rotation',
  '/zh/public-surface-security-checklist',
  '/zh/auth-surface-policy',
  '/zh/audit-log-policy',
  '/zh/tenant-workspace-boundaries',
];

const aiPagesZh = [
  '/zh/WEKNORA_INTEGRATION',
  '/zh/RAGFLOW_INTEGRATION',
  '/zh/KNOWLEDGE_BASE_LANDSCAPE',
  '/zh/design/knowledge-provider-selection-policy',
  '/zh/ai-fallback-behavior',
];

const remoteAssistPagesZh = [
  '/zh/remote-assistance',
  '/zh/remote-assistance-status',
  '/zh/remote-assistance-mvp',
  '/zh/remote-assistance-current-state',
];

const engineeringPagesZh = [
  '/zh/ARCHITECTURE',
  '/zh/current-architecture',
  '/zh/modules-dependency-map',
  '/zh/testing-pyramid',
  '/zh/CI_SELF_HOSTED',
  '/zh/release-versioning',
  '/zh/surface-naming',
  '/zh/generated-assets',
  '/zh/repo-hygiene',
  '/zh/demo-and-mock-boundaries',
  '/zh/acceptance-checklist',
  '/zh/acceptance-weknora-docker',
  '/zh/mobile-sdk-design',
  '/zh/mobile-sdk-platform-spec',
  '/zh/MERMAID_COMPATIBILITY',
  '/zh/审计-文档一致性',
];

const archivePagesZh = [
  '/zh/architecture-review-2026',
  '/zh/architecture-redesign-plan',
  '/zh/v1-convergence-plan',
  '/zh/mobile-sdk-cocoapods-evaluation',
  '/zh/release-0.1.0-acceptance',
  '/zh/review/chatgpt-review-2026-10-04',
  '/zh/review/chatgpt-review-2026-10-04-verification',
  '/zh/superpowers/plans/2025-05-01-pgvector-knowledge-base',
  '/zh/superpowers/specs/2025-05-01-pgvector-knowledge-base-design',
];

export const docsNavZh = [
  { text: '首页', link: '/zh/' },
  { text: '产品', link: '/zh/v1-product-scope' },
  { text: 'Web SDK', link: '/zh/web-sdk' },
  { text: 'Web 嵌入', link: '/zh/embedding-guide' },
  { text: '坐席工作台', link: '/zh/agent-workspace' },
  { text: '部署', link: '/zh/deployment' },
  { text: '运维', link: '/zh/operator-runbook' },
  { text: '移动端 SDK', link: '/zh/mobile-sdk-integration' },
  {
    text: '运行与安全',
    items: [
      { text: '安全基线', link: '/zh/security-baseline-operations' },
      { text: '配置作用域', link: '/zh/configuration-scopes' },
      { text: 'Token 生命周期', link: '/zh/token-lifecycle-and-key-rotation' },
      { text: '开放接口安全清单', link: '/zh/public-surface-security-checklist' },
      { text: '备份与恢复', link: '/zh/backup-and-recovery' },
    ],
  },
  {
    text: '研发附录',
    items: [
      { text: '竞品调研', link: '/zh/research/competitive-survey' },
      { text: '界面原型 v1', link: PROTOTYPES_LINK },
      { text: '实施计划', link: '/zh/implementation/' },
      { text: '总体架构', link: '/zh/ARCHITECTURE' },
      { text: '当前架构分析', link: '/zh/current-architecture' },
      { text: 'WeKnora 集成', link: '/zh/WEKNORA_INTEGRATION' },
      { text: '测试金字塔', link: '/zh/testing-pyramid' },
      { text: 'CI / Runner', link: '/zh/CI_SELF_HOSTED' },
      { text: '版本发布', link: '/zh/release-versioning' },
    ],
  },
];

export const docsSidebarZh = {
  '/zh/implementation/': [
    {
      text: '实施主线',
      items: ['/zh/implementation/', ...implementationPagesZh],
    },
    {
      text: '模块迁移治理',
      items: migrationGovernancePagesZh,
    },
  ],
  '/zh/': [
    { text: '开始', items: ['/zh/', ...startPagesZh] },
    { text: '接入与集成', items: integratePagesZh },
    { text: '坐席与工单', items: workbenchPagesZh },
    { text: '部署与运维', items: operatePagesZh },
    { text: '安全与合规', items: securityPagesZh },
    { text: 'AI 与知识库', items: aiPagesZh },
    { text: '远程协助', items: remoteAssistPagesZh },
    { text: '架构与研发治理', items: engineeringPagesZh },
    { text: '规划与历史存档', items: archivePagesZh },
    {
      text: '实施计划',
      items: ['/zh/implementation/', ...implementationPagesZh, ...migrationGovernancePagesZh],
    },
  ],
};
