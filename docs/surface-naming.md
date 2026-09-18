# 公开命名治理：legacy/compat 混名清理与保留登记（P3-1）

P3-1 的裁决口径：**内部兼容层继续保留，但对外命名统一到产品语义**。本文登记两类决定——已清理的混名，与"看起来像混名、实为产品语义或存储契约、刻意保留"的名字。后续遇到同类名字先查此表，避免重复裁决或误清理。

## 已清理（2026-09，P3-1）

| 清理项 | 位置 | 说明 |
| --- | --- | --- |
| `reconnectAttempts` / `reconnectDelay` 配置字段 | `sdk/packages/core`（`ServifyConfig`、`WebSocketManagerOptions`） | `reconnectPolicy` 早已并存且语义更完整；旧字段是同一行为的旧名。SDK 未发布 npm，仓库内（examples/demo-sdk/admin）无消费者，删除不构成对外 breaking |
| `LegacyReconnectOptions` 与 `normalizeReconnectPolicy` 第二参数 | `sdk/packages/core/src/contracts/reconnect.ts` | 旧字段的兼容 shim，随旧字段一起删除 |
| `EnableWeKnora` / `DisableWeKnora` handler 包装 | `apps/server/internal/handlers/ai_handler.go` | 仅旧路由时代的方法名包装，无生产路由挂载；新路由 `/api/v1/ai/knowledge-provider/enable|disable` 与 `EnableKnowledgeProvider`/`DisableKnowledgeProvider` 已存在并被测试覆盖 |

SDK 侧登记见 `sdk/SURFACE_GOVERNANCE.md`（Breaking Change Checklist 要求移除项留痕）。

## 刻意保留（非混名，勿清理）

| 保留项 | 位置 | 保留理由 |
| --- | --- | --- |
| `weknora.*` 配置键（`ai.weknora`、`health_checks.weknora`、`WEKNORA_*` 环境变量） | `apps/server/internal/config`、`bootstrap/config.go` | P0-5 裁决：WeKnora 是 knowledge provider 的**产品实名**（兼容接入路径），不是旧名；`knowledge.provider` 是开关面，`weknora.*` 是该 provider 的配置命名空间 |
| `weknora_usage_count` / `weknora_latency`（`AIMetrics` JSON 字段、`servify_ai_weknora_usage_total` 指标） | `modules/ai/delivery/contract_types.go`、`handlers/ai_handler.go` | 与 `knowledge_provider_usage_count` 是**父子维度**而非新旧名：总量按 active provider 分派计数（dify/weknora 各自的子计数与延迟）。删除会丢失 per-provider 观测维度 |
| `WeKnoraUsageToday`（analytics `weknora_usage_today`） | `modules/analytics/{contract,application}/types.go` | 同上：per-provider 统计维度，与 `knowledge_provider_usage_today` 并存 |
| `service_type='weknora'` 枚举值 | `scripts/init-db.sql`、knowledge 映射模块 | 存储契约：DB 行里的 provider 实名标识 |
| `servify_weknora_mappings` 表 | `scripts/init-db.sql` | 存储契约：表名含 provider 实名，改名即迁移，无混名收益 |
| `fallback.legacy_kb_enabled` | `config.go` | P0-5 裁决：语义就是"legacy 内置知识库开关"，`legacy` 是准确描述而非混名 |
| configscope section key `weknora` | `platform/configscope/gorm_store.go` | 持久化契约：作用域配置的 section 标识落库 |
| `apps/demo-sdk/widget.js` 的 `reconnectAttempts` | demo 自实现重连逻辑 | 与 SDK 无关的内部计数器，仅名字撞车，非 SDK legacy 选项 |

## 判定口径（供后续同类裁决复用）

1. **同一行为的两个名字并存**（如 reconnectPolicy vs reconnectAttempts）→ 清理旧名；仓库内未发布的包不构成对外 breaking。
2. **provider 实名或子策略维度**（WeKnora 作为被兼容接入的具体产品）→ 保留；"统一到产品语义"不等于抹掉具体 provider 的名字。
3. **存储契约**（DB 列/表名/枚举值/落库 section key）→ 改名需要迁移且无消费者收益，默认保留并在此登记。
