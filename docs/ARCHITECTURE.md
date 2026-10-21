# Architecture Overview

Servify stays a modular monolith at the current stage, hardening step by step around the unified runtime, AI orchestration, business modules, the voice protocol entry, and the SDK surface.

## Reading Entry Points

- V1 product scope: [V1 Product Scope](./v1-product-scope.md)
- Root document: the repository root `ARCHITECTURE.md`
- Current real state: [Current Architecture Analysis](./current-architecture.md)
- Next round plan: [Architecture Redesign Plan](./architecture-redesign-plan.md)
- Follow-up enhancement direction: [Remote Assistance](./remote-assistance.md)
- Deployment guide: [Deployment Guide](./deployment.md)
- Implementation backlog: [./implementation/README.md](/zh/implementation/README.md)
- Engineering hardening: [./implementation/05-engineering-hardening.md](/zh/implementation/05-engineering-hardening.md)

## Corresponding Topics in the Current Documentation Site

- [Implementation Plan](/zh/implementation/)

## Site Conventions

- The architecture overview page carries only navigation and reading order, and does not duplicate the long root document
- Detailed design still defers to the repository root `ARCHITECTURE.md`
- Current implementation state defers to `docs/current-architecture.md`, to avoid misreading the target state as already fully landed
- The documentation site pages are responsible for organizing the implementation, engineering, and topic documents into a deployable site
