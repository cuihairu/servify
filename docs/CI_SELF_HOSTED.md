# GitHub Hosted CI

当前仓库已切换为 GitHub Hosted Runner，不再依赖仓库私有 self-hosted runner。

## 当前运行环境

- Runner: `ubuntu-latest`
- Workflow: `.github/workflows/ci.yml`
- 触发条件：
  - push 到 `main`
  - 提交到 `main` 的 pull request
  - 手动 `workflow_dispatch`

## 当前 CI 检查项

### Go checks

- `gofmt` 格式校验
- `go mod tidy` 漂移检查
- `go vet`
- 单元测试与覆盖率门槛检查
- 标准二进制构建
- WeKnora compatibility tag 构建

### Module checks

- `internal/modules/...` 构建与测试
- `internal/platform/...` 构建与测试
- `internal/handlers/...` 构建与测试
- `cmd/server` 入口构建

### SDK checks

- `sdk` 依赖安装
- SDK lint
- SDK test
- SDK build
- SDK 同步到 demo 后的工作区漂移检查

### Docs checks

- swag 重新生成并校验生成产物一致性
- VitePress 文档站构建

### Script checks

- `scripts/` 下脚本的语法与白名单校验（含测试）

### Smoke checks

- 服务冒烟启动与基础端点检查

### Android probe

- Android SDK 构建（AAR 产物 artifact：`servify-sdk-aar`）

### iOS checks

- `ios-swift`：Swift 源码测试 job
- `ios-macos`：macOS Darwin face 校验，产出 `servify-kit-xcframework` artifact 并过体积门禁（`check-ios-sdk-size.sh`）

### Admin checks

- `apps/admin` 构建与检查

### AI Golden Set（real LLM）

- job 名 `golden-real`，仅在手动 `workflow_dispatch` 触发时运行（依赖真实 LLM 凭证，push/PR 流水线跳过）

### Integration

- 使用 `docker compose` 叠加 base + weknora compatibility mock 双 overlay 拉起集成环境（`docker-compose.yml` + `docker-compose.weknora.yml`；不含 Dify 服务）
- 健康检查轮询
- 执行 `scripts/test-weknora-integration.sh`
- 迁移水位对账：动态取迁移链头部版本校验 `schema_migrations`（不硬编码）
- Backup & restore drill：pg_dump/pg_restore 演练
- 失败时输出 compose logs

CI 本身没有部署 job；发布面为手动 `release.yml`（`workflow_dispatch` 触发）。

## 设计取舍

- 不再要求维护额外 runner 主机，减少运维成本
- CI 改为显式失败，不再通过 `make test` 中的吞错逻辑掩盖问题
- 将 Go、SDK、worker、integration 拆成独立 job，便于定位失败点
- CI 只做检查与验证，不承担部署；发布走手动 release workflow

## 后续可继续补的检查

- `golangci-lint`
- Mermaid 最小渲染冒烟检查
- OpenAPI/contract drift check
- 更细粒度的 integration matrix
