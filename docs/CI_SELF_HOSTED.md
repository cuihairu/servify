# GitHub Hosted CI

当前仓库已切换为 GitHub Hosted Runner，不再依赖仓库私有 self-hosted runner。

## 当前运行环境

- Runner: `ubuntu-latest`
- Workflow: `.github/workflows/ci.yml`
- 触发条件：
  - push 到 `main`
  - 提交到 `main` 的 pull request

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
- `internal/services/...` 构建与测试
- `internal/handlers/...` 构建与测试

### SDK checks

- `sdk` 依赖安装
- SDK lint
- SDK test
- SDK build
- SDK 同步到 demo 后的工作区漂移检查

### Integration

- 使用 `docker compose` 拉起 Dify 优先、WeKnora compatibility mock 集成环境
- 健康检查轮询
- 执行 `scripts/test-weknora-integration.sh`
- 失败时输出 compose logs

### Deploy jobs

以下任务仅在 `main` 分支运行，且依赖 Cloudflare secrets：

- Cloudflare Workers 部署
- Cloudflare Pages 部署

## 需要的仓库 Secrets

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`

可选仓库 Variable：

- `CF_PAGES_PROJECT`

## 设计取舍

- 不再要求维护额外 runner 主机，减少运维成本
- CI 改为显式失败，不再通过 `make test` 中的吞错逻辑掩盖问题
- 将 Go、SDK、worker、integration 拆成独立 job，便于定位失败点
- 保留部署 job，但通过 secrets gate 和变更检测避免无效发布

## 后续可继续补的检查

- `golangci-lint`
- Mermaid 最小渲染冒烟检查
- OpenAPI/contract drift check
- 更细粒度的 integration matrix
