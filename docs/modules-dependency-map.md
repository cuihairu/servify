# Modules 与 legacy services/models 依赖地图

> 数据口径：2026-09-15 对 `apps/server/internal/modules` 的全量扫描（命令见文末，可复现）。
> 用途：P1-7 边界收口的决策依据——先切哪里、按什么顺序切、切成什么样。

## 1. modules → internal/services（legacy 服务层）

**当前状态：0 直连（已收口）。**

本轮切法：契约类型下沉到各模块，legacy `internal/services` 通过**类型别名**反向引用同一份定义——两侧是同一个 Go 类型，不是两份平行 DTO，wire contract 与既有测试零漂移。

| 模块 | 契约类型 | 定义位置 | legacy 别名位置 |
| --- | --- | --- | --- |
| ai | `AIResponse` | `modules/ai/delivery/contract_types.go` | `services/ai.go` |
| ai | `AIMetrics`、`EnhancedAIResponse` | `modules/ai/delivery/contract_types.go` | `services/ai_enhanced.go` |
| customer | `CustomerCreateRequest` 等 9 个请求/响应类型 | `modules/customer/api/contract_types.go`（package `customerapi`） | `services/customer_service.go` |
| knowledge | `KnowledgeDocCreateRequest` 等 3 个请求类型 | `modules/knowledge/application/contract_types.go` | `services/knowledge_doc_service.go` |

说明：

- ai 的契约放在 delivery 而非 application，是因为 `modules/ai/application/types.go` 已有一个同名但 shape 不同的 `AIResponse`（orchestrator 内部用），避免重名冲突。
- customer 的契约放在 `modules/customer/api`（package `customerapi`）而非 application，是因为 swag（API 文档生成）按**包声明名**索引注解里的类型限定符，`application` 在多个模块下重名会歧义；`customer_handler.go` 的注解直接写 `customerapi.CustomerX`，是唯一能让 swag 无 flag 解析成功的挂法。该约束已推广：凡被 swag 注解引用（直接或经 `models.`/`services.` 别名链）的类型，其所在包声明名必须全仓唯一（quality 的 `qualitydomain` 即此例，实证注解可保持 `models.QualityReview` 不动、产物零漂移）；knowledge/ai 的 handler 无 swag 注解，不受此约束。
- 防回归门禁：`go test ./scripts -run TestModulesDoNotImportLegacyServices`（parser 级检查 import，含测试文件），已纳入 CI script-checks。

## 2. modules → internal/models（共享模型层）

**当前状态：15 个模块、约 60 个非测试文件直连 `internal/models`。** 按引用频次排前的类型：

| 类型 | 引用量 | 分类 |
| --- | --- | --- |
| `Ticket` / `Session` / `Agent` / `Message` / `User` / `Customer` | 35~260 | 共享领域核心（跨模块fan-out 最大） |
| `AgentGroup` / `CustomField` / `TicketComment` / `TicketStatus` / `TransferRecord` / `WaitingRecord` | 29~61 | 跨模块次核心 |
| `WebhookEndpoint` / `WebhookDelivery` | 65~75 | 模块自有（webhook，**已迁入** `modules/webhook/domain/models.go`，legacy 侧 `models.WebhookEndpoint` 等为别名，投递状态常量同步别名） |
| `AutomationTrigger` / `AutomationTimer` | 31~87 | 模块自有（automation，**已迁入** `modules/automation/domain/models.go`，含 `AutomationRun`，legacy 侧 `models.AutomationX` 为别名） |
| `QualityReview` | 62 | 模块自有（quality，**已迁入** `modules/quality/domain/models.go`，package 声明用唯一的 `qualitydomain`，legacy 侧 `models.QualityReview` 为别名） |
| `RemoteAssistSession` / `RemoteAssistAnnotation` | 33~46 | 模块自有（assist） |
| `VoiceCall` | 25 | 模块自有（voice，**已迁入** `modules/voice/infra/models.go`，legacy 侧 `models.VoiceCall` 为别名） |
| `KnowledgeDoc` | 32 | 模块自有（knowledge，已有 domain.Document 过渡层） |
| `DailyStats` | 21 | 统计聚合（analytics 读写、其他模块只读） |

分层分布（非测试文件数）：

| 模块 | application | delivery | infra |
| --- | --- | --- | --- |
| agent | 3 | 3 | 3 |
| ai | 1 | 1 | - |
| assist | 2 | 2 | 1 |
| automation | 3 | 2 | 1 |
| conversation | - | 2 | 1 |
| customer | 3 | 2 | 1 |
| knowledge | - | 2 | 1 |
| quality | 3 | 1 | 1 |
| routing | - | 4 | 1 |
| ticket | 1 | 3 | 1 |
| webhook | 2 | 2 | 1 |
| analytics / gamification / suggestion / voice | - | - | 各 1~3 |

## 3. 拆分策略（已定稿，解除 P1-7 阻塞项）

1. **共享领域核心保留为 shared kernel**：`User` / `Ticket` / `Session` / `Agent` / `Message` / `Customer` 等跨模块高频类型不拆散到单一模块，维持在共享模型层（当前 `internal/models`），但对 modules 的引用收口为"只读领域形状"，持久化细节（GORM scope、hook）由各模块 infra 层负责。
2. **模块自有类型逐步迁入所属模块**：webhook / automation / quality / assist / voice / knowledge 的自有模型，按"先 delivery/application 引用改为模块内类型 + legacy 别名过渡"的同一套切法迁移，优先级按"引用面小、无跨模块消费"排序：voice → gamification → suggestion → webhook → automation → quality → assist。
   - **迁移日志**：①2026-09-15 voice 完成——`VoiceCall` / `VoiceRecording` / `VoiceTranscript` 三个 GORM 模型迁入 `modules/voice/infra/models.go`（infra 是 voice 模块内唯一消费方，直接定义在 infra 消掉映射层），`internal/models` 保留类型别名，`bootstrap/migrate.go` 的 AutoMigrate 注册与 webhook 的快照只读经别名零改动；voice/infra 六个文件（含测试）全部去掉 `internal/models` import。②2026-09-15 webhook 完成——`models/webhook.go` 整文件迁入 `modules/webhook/domain/models.go`（`WebhookEndpoint` / `WebhookDelivery` + 四个投递状态常量；模块三层全消费故落 domain 而非 infra），`internal/models` 保留类型与常量别名，migrate 注册与 handlers 测试经别名零改动。③2026-09-15 automation 完成——`models/automation.go` 整文件迁入 `modules/automation/domain/models.go`（`AutomationTrigger` / `AutomationRun` / `AutomationTimer`，`AutomationRun.Trigger` 关联同文件类型随迁自洽），`internal/models` 保留类型别名，migrate 注册、legacy `services/automation_*` 与 handlers 测试经别名零改动。④2026-09-16 quality 完成——`QualityReview` 迁入 `modules/quality/domain/models.go`，包声明用唯一的 `qualitydomain`（首个被 swag 注解引用的迁移对象：`quality_handler.go` 的 `@Success 200 {object} models.QualityReview` 经 models 别名链解析，实证 swag 产物零漂移、29 字段完整），`internal/models` 保留类型别名，migrate 注册与 handlers 测试零改动。
   - **顺序纠偏（2026-09-15）**：gamification 与 suggestion 经逐类型核对**没有模块自有 models 类型**——gamification 只消费共享核心（`Ticket` / `CustomerSatisfaction` / `Agent` / `User`），suggestion 只消费 `Ticket` 与 knowledge 自有的 `KnowledgeDoc`（属 knowledge 迁移范围）。二者无需迁移，剩余顺序修正为：~~voice → gamification → suggestion →~~ webhook ✅ → automation ✅ → quality ✅ → assist → knowledge。
3. **契约类型已完成的迁移不再回退**：第 1 节的别名是过渡态，最终态是 legacy `internal/services` 里对应兼容层删除时别名一并删除（跟随 P3-2 services/modules 最终边界收口）。
4. **禁止新增**：modules 新代码不得 import `internal/services`（CI 门禁强制）；新增模块默认不引用 `internal/models` 中"模块自有"分类的类型。

## 4. 复现扫描命令

```bash
# modules -> services 直连（应为空）
grep -rln '"servify/apps/server/internal/services' apps/server/internal/modules --include='*.go'

# modules -> models 直连按模块计数
for d in apps/server/internal/modules/*/; do
  m=$(basename "$d")
  hits=$(grep -rln '"servify/apps/server/internal/models' "$d" --include='*.go' | grep -vc _test)
  echo "$m: $hits"
done

# models 类型引用频次
grep -rhoE 'models\.[A-Z][A-Za-z]+' apps/server/internal/modules --include='*.go' \
  | sort | uniq -c | sort -rn | head -25
```
