# 05 Engineering Hardening

范围：

- CI 质量门禁
- 覆盖率与稳定性
- 生成物一致性
- 发布与版本控制
- 文档站点自动化

## H1 ci-quality-gates

- [x] 为 Go 增加 `go test -race` 分层执行策略
- [x] 为 Go 增加覆盖率产物输出
- [x] 为 SDK 增加 `typecheck` 独立 job
- [x] 为 docs/VitePress 增加构建校验 job
- [x] 为 shell/script 增加最小静态检查

验收：

- CI 不只验证能否编译，还能验证并发安全、类型稳定性和文档可构建性

## H2 generated-assets-and-lockfiles

- [x] 固化需要提交的生成物清单
- [x] 增加 lockfile 一致性检查
- [x] 增加 demo SDK 同步结果校验
- [x] 增加 API 文档生成/漂移检查

验收：

- 本地与 CI 不会因为“忘记提交生成物”而长期漂移

## H3 release-versioning

- [x] 设计 server 版本号注入
- [x] 设计 SDK workspace version strategy
- [x] 预留 changelog 生成脚本
- [x] 预留 tag/release action

验收：

- 后续发布 server 和 SDK 时不需要重新设计版本链路

## H4 test-pyramid

- [x] 盘点当前 integration build tag 覆盖面
- [x] 为 `voice` 补 handler 到 runtime 的端到端测试
- [x] 为 `ai` 补 provider fallback 集成测试
- [x] 为 `sdk` 补 examples smoke tests
- [x] 为关键模块定义最小冒烟用例集合

验收：

- 单测、集成测试、冒烟测试职责清晰

## H5 docs-delivery

- [x] 补 VitePress 站点配置骨架
- [x] 补 docs 导航与侧边栏生成策略
- [x] 增加 GitHub Pages 或 artifact 发布流程
- [x] 增加 Mermaid 渲染兼容性约束说明

验收：

- `docs/` 可以稳定构建并部署，而不是只有 Markdown 文件
