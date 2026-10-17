---
layout: home

hero:
  name: Servify
  text: An intelligent customer service system for self-hosted enterprise deployment
  tagline: Web-first, AI first response, human takeover, end-to-end ticketing
  actions:
    - theme: brand
      text: View V1 Goals
      link: /v1-product-scope
    - theme: alt
      text: View Deployment Docs
      link: /deployment

features:
  - title: Web Access
    details: Starting from the website support entry point, it chains visitor reception, real-time conversations, and follow-up handling into one unified flow.
  - title: AI First Response and Knowledge Q&A
    details: Uses the knowledge base and model capability to deliver first responses, clarification, and suggestions, making AI a collaborative role in the support workflow.
  - title: Human Takeover and Transfer
    details: Agents can take over an AI conversation at any time and keep transferring, collaborating, and pushing complex issues forward.
  - title: End-to-End Ticketing
    details: Issues that cannot be resolved immediately flow into tickets for continued follow-up, extending service from chat into ongoing handling.
  - title: Knowledge Base Management
    details: Turns FAQs, help docs, product descriptions, and after-sales policies into knowledge assets that AI can use.
  - title: Back-Office Operations
    details: Manage agents, knowledge bases, permissions, security policies, and product runtime configuration in one unified admin console.
---

<script setup lang="ts">
// 首次访问按浏览器语言选择文档语言（vitepress 无此能力，GitHub Pages 不支持 _redirects）
// 优先级：localStorage 手动选择 > navigator.language > 保持当前 locale
import { onBeforeMount } from 'vue'
const KEY = 'servify-lang-auto-picked'
const BASE = '/servify/'
const ROOT_PATH = BASE
const ZH_PATH = BASE + 'zh/'
onBeforeMount(() => {{
  if (typeof window === 'undefined') return
  const p = window.location.pathname
  if (p !== ROOT_PATH && p !== ROOT_PATH.slice(0, -1)) return
  const picked = localStorage.getItem(KEY)
  if (picked) {{ if (picked !== p) window.location.replace(picked); return }}
  const langs = (navigator.languages?.length ? navigator.languages : [navigator.language]).map(l => l.toLowerCase())
  if (langs.some(l => l.startsWith('zh'))) window.location.replace(ZH_PATH)
}})
if (typeof window !== 'undefined') {{
  window.addEventListener('click', (e) => {{
    const a = (e.target as HTMLElement)?.closest?.('a') as HTMLAnchorElement | null
    const href = a?.getAttribute('href') || ''
    if (href.startsWith(ZH_PATH) || href.startsWith('zh/')) localStorage.setItem(KEY, ZH_PATH)
    else if (href === ROOT_PATH || href === ROOT_PATH.slice(0, -1)) localStorage.setItem(KEY, ROOT_PATH)
  }}, true)
}}
</script>


## Product Overview

The first release of Servify delivers Web-based intelligent customer service for corporate websites, brand standalone storefronts, SaaS marketing sites, and documentation sites.

It prioritizes completing the core support chain, with platform-level compatibility deferred:

`Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单全流程`

The focus of this documentation site should likewise be on understanding the product around this chain, rather than starting from the implementation backlog.

Remote assistance remains an enhancement direction worth keeping, but in the V1 phase it must not outweigh the core capabilities: the website support entry, AI first response, human handoff, and end-to-end ticketing.

## Why Servify

### Self-Hosted Deployment First

Servify fits enterprise support scenarios that need self-hosted deployment, controllable integration, and continuous evolution. It serves a single enterprise team by default, rather than putting multi-tenant platform capability at the very center of the product.

### Solidify the Core Chain First

Many customer service systems spread across many feature surfaces early on, but what V1 truly needs to establish first is: website access, knowledge Q&A, human takeover, and continued ticket handling.

The repository already has the realtime foundations needed for remote assistance, including Web sessions, messages, and the WebSocket / WebRTC related paths; but the documentation deliberately avoids overstating this as "having fully delivered some heavyweight remote-control product".

### AI and Humans Are Not Separate Silos

AI handles first responses, clarification, knowledge recall, and suggestions; humans can take over, continue, and collaborate at any time, keeping the entire support experience continuous.

## Recommended Reading

1. [V1 Product Scope](/v1-product-scope)
2. [Web Embedding Integration Guide](/embedding-guide)
3. [Agent Workspace](/agent-workspace)
4. [Ticket End-to-End Workflow](/ticket-workflow)
5. [Deployment Guide](/deployment)
6. [Operator Runbook](/operator-runbook)
7. [Current Delivery Priorities](/delivery-priorities)
8. [Implementation Plan Index](/zh/implementation/)

## Where You Can Go From Here

Grouped by usage task, consistent with the left navigation.

### Evaluation and Getting Started

- [V1 Product Scope](/v1-product-scope)
- [Current Delivery Priorities](/delivery-priorities)
- [Local Development](/local-development)
- [Contributing Guide](/zh/contributing)

### Embedding and Integration

- [Web SDK](/web-sdk) (full tables of core / react / vue / vanilla APIs and configuration)
- [Web Embedding Integration Guide](/embedding-guide) (website support widget, session interop, ticket context, event callbacks, full theme tables)
- [Mobile SDK Integration](/mobile-sdk-integration)
- [Realtime Speech Translation](/realtime-translation-design)
- [Twilio PSTN Integration](/voice-pstn-twilio)

### Agents and Tickets

- [Agent Workspace](/agent-workspace) (human handoff queue, takeover, replies, reassignment, conversation transcripts)
- [Ticket End-to-End Workflow](/ticket-workflow) (creation, assignment, follow-up, status transitions, statistics and export)

### Deployment and Operations

- [Deployment Guide](/deployment)
- [Operator Runbook](/operator-runbook)
- [WebRTC/TURN Deployment](/webrtc-deployment)
- [Backup and Recovery](/backup-and-recovery)
- [Metrics Glossary](/metrics-glossary)

### Security and Compliance

- [Runtime Security Baseline](/security-baseline-operations)
- [Configuration Scope Rules](/configuration-scopes)
- [Token Lifecycle and Key Rotation](/token-lifecycle-and-key-rotation)
- [Public Surface Security Checklist](/public-surface-security-checklist)

### AI and Knowledge Base

- [WeKnora Integration](/WEKNORA_INTEGRATION) (for the knowledge provider selection landscape, see [KNOWLEDGE_BASE_LANDSCAPE](/KNOWLEDGE_BASE_LANDSCAPE))
- [RAGFlow Integration](/RAGFLOW_INTEGRATION)
- [AI Fallback Behavior](/ai-fallback-behavior)

### Development and Implementation Details

- [Overall Architecture Design](/ARCHITECTURE)
- [Current Architecture Analysis](/current-architecture)
- [Implementation Backlog Index](/zh/implementation/)
- [Testing Pyramid](/testing-pyramid)
- [CI / GitHub Hosted Runner](/CI_SELF_HOSTED)
- [Release Versioning Policy](/release-versioning)
- [Planning and Archived History](/architecture-redesign-plan) (historical documents such as the v0.1.0 Release Notes are in the last sidebar group)

## Common Entry Points

- Local self-check: `make local-check`
- Security check: `make security-check CONFIG=./config.yml`
- Observability check: `make observability-check CONFIG=./config.yml`
- Release check: `make release-check CONFIG=./config.yml`
