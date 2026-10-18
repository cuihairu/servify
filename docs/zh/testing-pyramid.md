# 测试金字塔

## Integration 构建标签覆盖范围

当前 `integration` 覆盖集中在 `apps/server/internal/handlers`（HTTP handler 到运行时装配），并分布在 `apps/server/internal/modules/*` 的 infra/application/delivery 各包以及 `internal/platform/realtime` —— 41 个测试文件分布在 15 个包中。

已覆盖区域：

- 智能体
- AI
- 应用市场
- 自动化
- 自定义字段
- 客户
- 游戏化
- 知识库文档
- 宏
- 满意度
- 会话转接
- 排班
- SLA
- 统计
- 建议
- 工单
- WebSocket
- 工作空间

专用 integration 标签：

- `integration && sqlite_integration`
  - `apps/server/internal/modules/ticket/infra/gorm_repository_test.go`
  - `apps/server/internal/modules/ticket/infra/gorm_repository_scope_integration_test.go`

此前缺失于 integration 层、现已覆盖：

- `voice` 协议 HTTP 运行时路径
- 编排式 `ai` provider 回落路径

## 测试分层职责

- 单元测试
  - 纯模块逻辑、适配器、DTO 映射、提示词构建、重试策略、SDK 包行为
- 集成测试
  - HTTP handler 到运行时装配、基于数据库的服务流程、provider 回落装配、协议路由
- 冒烟测试
  - 小而高信号量的检查，保护关键面免受明显回归

## 最小冒烟套件

运行方式：

- `./scripts/run-smoke-tests.sh`

当前冒烟集合（权威清单见 `scripts/run-smoke-tests.sh`）：

- conversation handler / application / infra 状态映射
- ticket handler：创建/获取/列表/分配，自定义字段 + 导出，关联会话
- voice handler 集成路径
- 编排式 AI provider 回落（`./apps/server/internal/modules/ai/delivery`）
- 全项目 `go build ./...`
- `npm -C sdk run test:examples`

这些项进入冒烟集合的原因：

- `conversation`/`ticket`
  - 客户侧主链路：会话、消息、工单分配
- `voice`
  - 验证协议路由注册与有状态运行时行为
- `ai`
  - 验证编排式 provider 失败时仍能安全降级到回落
- `sdk examples`
  - 确保示例入口仍指向受支持的 SDK 能力面
