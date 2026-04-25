# Local Development

本文件约定 Servify 在 Windows PowerShell、WSL、Linux 与 CI 下的最小开发流程，目标是减少“同一命令在不同环境下行为不同”的问题。

## 推荐方式

- Windows 用户：
  - 推荐在 PowerShell 中执行 Git 与编辑器操作
  - 推荐在 WSL Ubuntu 中执行仓库内的 `sh` / `bash` 脚本
- Linux 用户：
  - 直接使用仓库内 `make`、`go`、`npm`、`sh` 入口
- CI：
  - 以 `.github/workflows/ci.yml` 为准，默认使用 Ubuntu + bash

## 统一入口

- 构建：
  - `make build`
  - `make build-knowledge-provider`
  - `build-knowledge-provider` 是 `build-weknora` 的通用 alias，用于外部 knowledge provider compatibility 构建
- 运行服务：
  - `make run`
  - `make run-cli CONFIG=./config.yml`
  - `make run-knowledge-provider CONFIG=./config.weknora.yml`
  - `make run-weknora CONFIG=./config.weknora.yml`
  - `run-knowledge-provider` / `run-weknora` 都仅用于 WeKnora compatibility / mock 回归；日常知识库链路优先使用 `Dify`
- 测试：
  - `./scripts/run-tests.sh`
  - `./scripts/run-go-race-tests.sh`
  - `./scripts/run-smoke-tests.sh`
  - `make dify-acceptance`
  - `make weknora-acceptance`
  - `make auth-session-acceptance`
  - `make validate-acceptance-manifest MANIFEST=./scripts/test-results/<provider>/<mode>/manifest.json`
  - `make check-acceptance-evidence`
  - `make knowledge-provider-acceptance`
  - `dify-acceptance` 用于 Dify 主路径验收；默认 `DIFY_ACCEPTANCE_MODE=mock`
  - `weknora-acceptance` 用于 WeKnora compatibility 验收；默认 `WEKNORA_ACCEPTANCE_MODE=mock`
  - `auth-session-acceptance` 用于认证与自助会话链路验收；默认 `AUTH_ACCEPTANCE_MODE=real`
  - 这些脚本都会把证据写到 `./scripts/test-results/...`
  - `scripts/test-results/**/manifest.json` 可以作为正式验收索引纳入 git；其他响应文件默认仍保持忽略，避免把本地产物整包提交
  - 真实环境验收前需显式覆盖 `SERVIFY_URL`、provider 地址与数据集/凭证
- 生成物与仓库卫生：
  - `make demo-sync-sdk`
  - `make generated-assets`
  - `make release-changelog FROM=<previous-tag-or-commit> TO=HEAD`
  - `make repo-hygiene`
  - `make local-check`
  - `make release-check CONFIG=./config.yml`
  - `make clean-runtime`
  - `./scripts/verify-generated-assets.sh`

## Windows 与 WSL 约定

- 如果你在 PowerShell 中调用 `bash` 或 `sh`，请确认它实际指向的是可用环境
- 如果系统里的 `bash.exe` 指向旧 WSL 入口但没有正确绑定发行版，优先使用：
  - `wsl -d Ubuntu -- bash -lc "<command>"`
- 如果 `sh` 来自 Git Bash / scoop shim，可直接执行简单脚本校验

## Git Safe Directory

当 WSL 中的 Git 提示 `detected dubious ownership` 时，在 WSL 里执行：

```bash
git config --global --add safe.directory /mnt/c/Users/cui/Workspaces/servify
```

如果仓库路径变化，请替换为实际路径。

## 换行符与权限位

- 仓库通过 `.gitattributes` 统一文本换行策略
- shell 脚本默认使用 LF
- CI 中会用 `bash -n` 校验脚本语法
- 需要执行权限的脚本，在 CI 或 shell 中显式 `chmod +x` 后运行

## 运行时文件位置

- 本地运行时输出默认放在 `./.runtime/`
- 不要把上传目录、临时输出、二进制放到仓库根目录
- 构建产物优先输出到 `./bin/`
- 需要清理本地运行时目录时，执行 `make clean-runtime`

## 最小跨平台检查清单

- 推荐先跑：
  - `make local-check`
  - `make dify-acceptance`
  - `make weknora-acceptance`
  - 发布前再补：`make release-check CONFIG=./config.yml`
- `go test ./apps/server/internal/config ./apps/server/internal/handlers`
- `go test ./scripts -run "Test(WeKnoraIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence)|DifyIntegrationScript(RealModeRejectsLocalHost|MockModeWritesEvidence))$"`
- `sh scripts/check-repo-hygiene.sh`
- 如果改了生成物流程，再补：
  - `sh scripts/regenerate-generated-assets.sh`
  - `sh scripts/verify-generated-assets.sh`

`make local-check` 会输出：

- 当前 `sh` / `bash` / `node` / `npm` 的解析路径
- repo hygiene 与 generated assets 校验结果
- `safe.directory` 是否已包含当前仓库
- WSL 是否可用，以及当前发行版列表

## 已知差异

- 某些 Windows 环境中的 `bash` 可能默认走旧 WSL 入口，导致 `/bin/bash` 不可用
- WSL 中的 `npm` / `node` 可能误连到 Windows 侧安装路径，需要单独检查 `which node`、`which npm`
- PowerShell、Git Bash、WSL 对路径格式和引号处理并不完全一致，复杂命令优先写入仓库脚本
