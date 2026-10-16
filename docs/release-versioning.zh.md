> [English](release-versioning.md) | **中文**

# 发布版本管理

## 服务端版本注入

- 唯一事实来源：`apps/server/internal/version`
- 构建元数据由根目录 `Makefile` 通过 `-ldflags` 注入
- 注入字段：
  - `Version`
  - `Commit`
  - `BuildTime`
- `servify` 服务端 handler 与 `servify-cli version` 读取的是同一个包

## SDK workspace 版本策略

- `sdk/package.json` 是 workspace 版本的事实来源
- 可发布的包与 workspace 根版本完全一致：
  - `@servify/core`
  - `@servify/react`
  - `@servify/vue`
  - `@servify/vanilla`
- 包间内部依赖钉定到完全相同的版本，而非宽松范围
- `@servify/api-client` 与 `@servify/app-core` 当前共享 workspace 版本（`1.0.0`），没有独立的发布入口：两者都未声明 `private`，也没有任何发布流程以它们为目标（`private: true` 由 `sdk` workspace 根以及 `sdk/packages/transport-http` 和 `sdk/packages/transport-websocket` 设置）
- 使用：
  - `npm -C sdk run version:sync`
  - `npm -C sdk run version:check`

## 变更日志与发布流程

- 草稿变更日志的生成由 `scripts/generate-changelog.sh` 提供
- 本地草稿文件生成由 `make release-changelog` 提供
- 本地发布草稿输出默认为 `./.runtime/release/RELEASE_CHANGELOG.md`
- 手动发布准备在 `.github/workflows/release.yml` 中预留
- 当前发布 workflow 只准备元数据与产物，不自动发布包
- `RELEASE_CHANGELOG.md` 被视为发布产物，而非提交入库的生成资产
