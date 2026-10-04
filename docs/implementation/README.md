# Servify Implementation Backlogs

本目录按主题拆分架构实施任务。

目的：

- 让每份 backlog 保持可读
- 让 server、AI、SDK、协议接入各自可独立推进
- 支持中断恢复

文件说明：

- [总体架构设计](../ARCHITECTURE.md)
  - 宏观边界、运行时分层、未来扩展方向
- [当前架构分析](../current-architecture.md)
  - 当前真实架构快照、模块迁移状态与过渡风险
- [架构重设计计划](../architecture-redesign-plan.md)
  - 架构重设计计划（services→modules 迁移期产物，仅存档）
- [01-platform-and-runtime.md](./01-platform-and-runtime.md)
  - 入口、bootstrap、router、auth、event bus、realtime 等平台任务

- [02-ai-and-knowledge.md](./02-ai-and-knowledge.md)
  - provider、AI orchestration、tooling、knowledge indexing 等任务

- [03-business-modules.md](./03-business-modules.md)
  - conversation、routing、agent、ticket、customer、automation、analytics、voice

- [04-sdk-and-channel-adapters.md](./04-sdk-and-channel-adapters.md)
  - sdk core、web sdk、future api/app sdk 预留、channel adapters、SIP adapter
- [05-engineering-hardening.md](./05-engineering-hardening.md)
  - CI、测试金字塔、版本发布、文档站点
- [06-voice-and-protocol-expansion.md](./06-voice-and-protocol-expansion.md)
  - voice 协议入口深化、provider 落地、更多常见语音协议预留
- [07-sdk-multi-surface.md](./07-sdk-multi-surface.md)
  - web sdk 收口、future api/app sdk contract 深化、transport 演进
- [08-ai-provider-expansion.md](./08-ai-provider-expansion.md)
  - LLM/knowledge provider 扩展、编排层稳定性、AI 可观测性
- [09-runtime-and-repo-hygiene.md](./09-runtime-and-repo-hygiene.md)
  - 运行时产物清理、仓库卫生、ignore 策略、跨平台开发环境收口
- [10-service-to-module-migration.md](./10-service-to-module-migration.md)
  - 旧 services/handlers 向 modules 架构迁移、适配层与边界收口
- [10-migration-inventory.md](./10-migration-inventory.md)
  - 当前 handlers/services/modules 迁移盘点与优先级建议
- [10-migration-scorecard.md](./10-migration-scorecard.md)
  - 当前迁移完成度表与模块状态追踪
- [10-module-boundaries.md](./10-module-boundaries.md)
  - handlers/services/modules 迁移期边界规则与冻结策略
- [11-tenant-auth-and-audit.md](./11-tenant-auth-and-audit.md)
  - 多租户、权限模型、审计日志、配置边界
- [12-operator-observability.md](./12-operator-observability.md)
  - tracing、metrics、日志、告警、回放与运营诊断
- [13-ai-agent-loop.md](./13-ai-agent-loop.md)
  - P0：接通 AI agent loop（tool-calling 循环 + 流式），让智能客服多步行动

状态约定：

- `[ ]` 未开始
- `[-]` 进行中
- `[x]` 已完成

执行建议：

1. 先做 `01-platform-and-runtime`
2. 再做 `02-ai-and-knowledge`
3. 再拆 `03-business-modules`
4. 并行规划 `04-sdk-and-channel-adapters`（web sdk 已实现，Android/iOS 原生
   SDK 已随 M1–M3 落地；api/app sdk 仍为预留）
5. 第一阶段清零后，进入 `05` 到 `08` 的工程化与扩展阶段

当前进度（2026-10-04 更新，`v1.0.0` 已发布）：

- `01` 到 `08` 已全部清零
- `09` 已完成仓库卫生与运行时产物治理
- `10` services→modules 迁移已完成（`internal/services` 已整体移除，27 个
  一级模块封顶），配套 inventory / scorecard / boundaries 为迁移期档案
- `11` 和 `12` 主体已落地（租户/审计/安全基线接入管理面；tracing/metrics/
  告警通过 observability baseline 门禁）
- `13` AI agent loop 主循环已接通（tool-calling 循环 + 流式 + 三工具注册；
  工具 Port 数据源与权限校验接入为遗留项，状态见
  [13-ai-agent-loop.md](./13-ai-agent-loop.md) 头部核实注记）
- V1.0 收敛批次（B0–B4）已全部过闸，当前唯一活跃状态源是
  [todo.md](../../todo.md) 与 [验收矩阵](../acceptance-checklist.md)；后续
  演进见 [v1.0.0 发布说明](../release-notes-v1.0.0.md)「演进方向」

当前阅读顺序：

1. 先读 [当前架构分析](../current-architecture.md)，确认仓库真实状态。
2. 再读 [V1.0 收敛改造计划书](../v1-convergence-plan.md)与
   [v1.0.0 发布说明](../release-notes-v1.0.0.md)，确认当前产品/架构口径。
3. 历史实施主线（架构重设计计划、10-series 迁移档案）仅作迁移史参考。

配套专题：

- [版本发布策略](../release-versioning.md)
- [测试金字塔](../testing-pyramid.md)
- [Mermaid 兼容性](../MERMAID_COMPATIBILITY.md)
- [仓库卫生与生成物边界](../repo-hygiene.md)
