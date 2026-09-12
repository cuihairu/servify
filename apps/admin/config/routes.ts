/**
 * Servify Admin 路由定义
 * 对齐后端 29 个 Handler 的全部管理能力
 */
export default [
  { path: '/login', layout: false, component: './Login' },
  { path: '/login/oidc/callback', layout: false, component: './Login/oidcCallback' },
  { path: '/', redirect: '/dashboard' },

  // 仪表板
  {
    path: '/dashboard',
    name: '仪表板',
    icon: 'DashboardOutlined',
    component: './Dashboard',
  },

  // 工单管理
  {
    path: '/ticket',
    name: '工单管理',
    icon: 'FileTextOutlined',
    routes: [
      { path: '/ticket/list', name: '工单列表', component: './Ticket/list' },
      {
        path: '/ticket/detail/:id',
        name: '工单详情',
        component: './Ticket/detail',
        hideInMenu: true,
      },
    ],
  },

  // 会话管理
  {
    path: '/conversation',
    name: '会话管理',
    icon: 'MessageOutlined',
    component: './Conversation',
  },

  // 客户管理
  {
    path: '/customer',
    name: '客户管理',
    icon: 'UserOutlined',
    routes: [
      {
        path: '/customer/list',
        name: '客户列表',
        component: './Customer/list',
      },
      {
        path: '/customer/detail/:id',
        name: '客户详情',
        component: './Customer/detail',
        hideInMenu: true,
      },
    ],
  },

  // 客服管理
  {
    path: '/agent',
    name: '客服管理',
    icon: 'TeamOutlined',
    routes: [
      {
        path: '/agent/list',
        name: '客服列表',
        component: './Agent/list',
      },
      {
        path: '/agent/detail/:id',
        name: '客服详情',
        component: './Agent/detail',
        hideInMenu: true,
      },
    ],
  },

  // 路由管理
  {
    path: '/routing',
    name: '路由管理',
    icon: 'SwapOutlined',
    component: './Routing',
  },

  // 知识库
  {
    path: '/knowledge',
    name: '知识库',
    icon: 'BookOutlined',
    routes: [
      {
        path: '/knowledge/list',
        name: '文档管理',
        component: './Knowledge/list',
      },
      {
        path: '/knowledge/detail/:id',
        name: '文档详情',
        component: './Knowledge/detail',
        hideInMenu: true,
      },
    ],
  },

  // AI 管理
  {
    path: '/ai',
    name: 'AI 管理',
    icon: 'RobotOutlined',
    component: './AI',
  },

  // 自动化
  {
    path: '/automation',
    name: '自动化',
    icon: 'ThunderboltOutlined',
    component: './Automation',
  },

  // 宏与模板
  {
    path: '/macro',
    name: '宏与模板',
    icon: 'SnippetsOutlined',
    component: './Macro',
  },

  // SLA 管理
  {
    path: '/sla',
    name: 'SLA 管理',
    icon: 'SafetyCertificateOutlined',
    routes: [
      { path: '/sla/configs', name: 'SLA 配置', component: './SLA/configs' },
      {
        path: '/sla/violations',
        name: '违约记录',
        component: './SLA/violations',
      },
    ],
  },

  // 班次管理
  {
    path: '/shift',
    name: '班次管理',
    icon: 'ScheduleOutlined',
    component: './Shift',
  },

  // 满意度
  {
    path: '/satisfaction',
    name: '满意度',
    icon: 'SmileOutlined',
    routes: [
      {
        path: '/satisfaction/overview',
        name: '满意度概览',
        component: './Satisfaction/overview',
      },
      {
        path: '/satisfaction/surveys',
        name: '调查管理',
        component: './Satisfaction/surveys',
      },
    ],
  },

  // 语音管理
  {
    path: '/voice',
    name: '语音管理',
    icon: 'PhoneOutlined',
    component: './Voice',
  },

  // 应用市场
  {
    path: '/app-market',
    name: '应用市场',
    icon: 'AppstoreOutlined',
    component: './AppMarket',
  },

  // 自定义字段
  {
    path: '/custom-field',
    name: '自定义字段',
    icon: 'FormOutlined',
    component: './CustomField',
  },

  // 游戏化
  {
    path: '/gamification',
    name: '游戏化',
    icon: 'TrophyOutlined',
    component: './Gamification',
  },

  // 审计日志
  {
    path: '/audit',
    name: '审计日志',
    icon: 'FileSearchOutlined',
    component: './Audit',
  },

  // 质检抽检工作台
  {
    path: '/quality',
    name: '质检抽检',
    icon: 'AuditOutlined',
    component: './Quality',
  },

  // 安全管理
  {
    path: '/security',
    name: '安全管理',
    icon: 'LockOutlined',
    component: './Security',
  },

  // 系统设置
  {
    path: '/settings',
    name: '系统设置',
    icon: 'SettingOutlined',
    component: './Settings',
  },

  // 开放平台（Webhook 订阅 / 投递日志 / API Keys；Email 渠道经 config.yml 配置）
  {
    path: '/open-platform',
    name: '开放平台',
    icon: 'ApiOutlined',
    component: './OpenPlatform',
  },
];
