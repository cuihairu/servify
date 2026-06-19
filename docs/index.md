---
layout: home

hero:
  name: Servify
  text: 面向企业独立部署的智能客服系统
  tagline: Web 优先，AI 首答，人工接管，工单闭环
  actions:
    - theme: brand
      text: 查看 V1 目标
      link: /v1-product-scope
    - theme: alt
      text: 查看部署文档
      link: /deployment

features:
  - title: Web 接入
    details: 以网站客服入口为起点，把访客接待、实时会话和后续处理串成统一链路。
  - title: AI 首答与知识问答
    details: 通过知识库和模型能力完成首答、澄清和建议，让 AI 成为客服流程里的协作角色。
  - title: 人工接管与转接
    details: 坐席可以随时接管 AI 会话，并继续转接、协作和推进复杂问题。
  - title: 工单闭环
    details: 无法即时解决的问题进入工单继续跟进，让服务从聊天延伸到持续处理。
  - title: 知识库管理
    details: 把 FAQ、帮助文档、产品说明和售后政策沉淀成 AI 可用的知识资产。
  - title: 后台运营
    details: 在统一后台管理坐席、知识库、权限、安全策略和产品运行配置。
---

## 产品概览

Servify 第一版先针对企业官网、品牌独立站、SaaS 官网和文档站提供 Web 智能客服能力。

它不是先做平台再兼容客服，而是先把客服主链路做完整：

`Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单闭环`

当前文档站的重点，也应该围绕这条链路来理解产品，而不是先从 implementation backlog 开始读。

远程协助仍然是一个值得保留的增强方向，但在 V1 阶段，它不应压过网站客服入口、AI 首答、转人工和工单闭环这些主能力。

## 为什么是 Servify

### 独立部署优先

Servify 适合需要独立部署、可控接入和持续演化的企业客服场景。它默认服务的是单个企业团队，而不是把多租户平台化能力放在产品正中央。

### 先把主链路做扎实

很多客服系统会在早期同时铺很多功能面，但 V1 真正需要先成立的是：网站接入、知识问答、人工接管和工单继续处理。

当前仓库已经具备远程协助所需的实时基础，包括 Web 会话、消息、WebSocket / WebRTC 相关链路；但文档表达会刻意避免把它夸大成“已完整交付某种重型远控产品”。

### AI 和人工不是割裂关系

AI 负责首答、澄清、知识召回和建议，人工可以随时接管、继续处理和协作，让整个客服体验保持连续。

## 推荐阅读

1. [V1 产品收敛](/v1-product-scope)
2. [总体架构设计](/ARCHITECTURE)
3. [当前架构分析](/current-architecture)
4. [部署指南](/deployment)
5. [当前交付优先级](/delivery-priorities)
6. [实施计划索引](/implementation/)

## 你可以从这里继续

### 快速了解产品

- [V1 产品收敛](/v1-product-scope)
- [总体架构设计](/ARCHITECTURE)
- [当前架构分析](/current-architecture)
- [架构重设计计划](/architecture-redesign-plan)
- [远程协助](/remote-assistance)
- [部署指南](/deployment)
- [本地开发](/local-development)

### 深入运行与安全

- [运行安全基线](/security-baseline-operations)
- [配置作用域规则](/configuration-scopes)
- [Token 生命周期与密钥轮换](/token-lifecycle-and-key-rotation)
- [开放接口安全清单](/public-surface-security-checklist)
- [当前交付优先级](/delivery-priorities)

### 继续看研发与实施细节

- [实施 Backlog 索引](/implementation/)
- [模块迁移计划](/implementation/10-service-to-module-migration)
- [模块迁移完成度](/implementation/10-migration-scorecard)
- [外部知识库集成指南：Dify 优先，WeKnora 兼容](/WEKNORA_INTEGRATION)
- [v0.1.0 Release Notes](/release-notes-v0.1.0)
- [版本发布策略](/release-versioning)
- [测试金字塔](/testing-pyramid)
- [CI / GitHub Hosted Runner](/CI_SELF_HOSTED)
- [Mermaid 兼容性](/MERMAID_COMPATIBILITY)

## 常用入口

- 本地自检：`make local-check`
- 安全检查：`make security-check CONFIG=./config.yml`
- 可观测性检查：`make observability-check CONFIG=./config.yml`
- 发布检查：`make release-check CONFIG=./config.yml`
