---
title: Documentation Home Notes
---

> The VitePress home entry has already migrated to [index.md](./index.md).
>
> The original content entry is kept here so that in-repo references to `docs/README.md` keep working.

## Product Overview

Servify's first version focuses first on delivering web-based intelligent customer service for corporate websites, brand standalone sites, SaaS marketing sites, and documentation sites.

It prioritizes completing the core customer service chain first, with platform-wide compatibility deferred:

`Web 接入 -> AI 首答 -> 人工接管 -> 转接协作 -> 工单全流程`

The current documentation site's focus should likewise be understanding the product around this chain, rather than starting from the implementation backlog.

Remote assistance remains an enhancement direction worth keeping, but in the V1 phase it must not outweigh the core capabilities: the website customer service entry, AI first answer, human handover, and the full ticket workflow.

## Why Servify

### Self-hosted Deployment First

Servify fits enterprise customer service scenarios that need self-hosted deployment, controlled integration, and continuous evolution. It serves a single enterprise team by default, rather than putting multi-tenant platform capabilities at the very center of the product.

### Make the Core Chain Solid First

Many customer service systems spread many feature surfaces early on, but what V1 truly needs to establish first is: website integration, knowledge Q&A, human takeover, and continued ticket handling.

The current repository already has the real-time foundation needed for remote assistance, including web sessions, messages, and WebSocket / WebRTC related chains; but the documentation deliberately avoids overstating this as "having fully delivered some heavyweight remote-control product".

### AI and Humans Are Not Separate

AI handles first answers, clarification, knowledge recall, and suggestions; humans can take over at any time, continue handling, and collaborate, keeping the whole customer service experience continuous.

## Recommended Reading

1. [V1 Product Scope](/v1-product-scope)
2. [Web Embedding Integration Guide](/embedding-guide)
3. [Agent Workspace](/agent-workspace)
4. [Ticket Workflow](/ticket-workflow)
5. [Deployment Guide](/deployment)
6. [Operator Runbook](/operator-runbook)
7. [Current Delivery Priorities](/delivery-priorities)
8. [Implementation Plan Index](/zh/implementation/)

## Where You Can Continue From Here

Grouped by usage task, consistent with the docs site navigation (`docs/.vitepress/site-structure.ts`).

### Evaluation and Getting Started

- [V1 Product Scope](/v1-product-scope)
- [Current Delivery Priorities](/delivery-priorities)
- [Local Development](/local-development)
- [Contributing Guide](/zh/contributing)

### Embedding and Access

- [Web Embedding Integration Guide](/embedding-guide)
- [Mobile SDK Integration](/mobile-sdk-integration)
- [Realtime Speech Translation](/realtime-translation-design)
- [Twilio PSTN Access](/voice-pstn-twilio)

### Agents and Tickets

- [Agent Workspace](/agent-workspace) (human handover queue, takeover, replies, reassignment, session history)
- [Ticket Workflow](/ticket-workflow) (creation, assignment, follow-up, status transitions, statistics export)

### Deployment and Operations

- [Deployment Guide](/deployment)
- [Operator Runbook](/operator-runbook)
- [WebRTC/TURN Deployment](/webrtc-deployment)
- [Backup and Recovery](/backup-and-recovery)
- [Metrics Conventions](/metrics-glossary)

### Security and Compliance

- [Security Baseline for Operations](/security-baseline-operations)
- [Configuration Scope Rules](/configuration-scopes)
- [Token Lifecycle and Key Rotation](/token-lifecycle-and-key-rotation)
- [Public API Security Checklist](/public-surface-security-checklist)

### AI and Knowledge Base

- [WeKnora Integration](/WEKNORA_INTEGRATION) (for the selection landscape see [KNOWLEDGE_BASE_LANDSCAPE](/KNOWLEDGE_BASE_LANDSCAPE))
- [RAGFlow Integration](/RAGFLOW_INTEGRATION)
- [AI Fallback Behavior](/ai-fallback-behavior)

### Development and Implementation Details

- [Overall Architecture Design](/ARCHITECTURE)
- [Current Architecture Analysis](/current-architecture)
- [Implementation Backlog Index](/zh/implementation/)
- [Testing Pyramid](/testing-pyramid)
- [CI / GitHub Hosted Runner](/CI_SELF_HOSTED)
- [Release Versioning Strategy](/release-versioning)
- [Planning and History Archive](/architecture-redesign-plan)

## Common Entry Points

- Local self-check: `make local-check`
- Security check: `make security-check CONFIG=./config.yml`
- Observability check: `make observability-check CONFIG=./config.yml`
- Release check: `make release-check CONFIG=./config.yml`
