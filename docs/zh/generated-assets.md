# Generated Assets

本文件说明 Servify 中需要提交到仓库的生成物、对应生成入口、以及 CI 如何校验它们。

## 当前受控生成物

`generated-assets.manifest` 当前包含：

- `apps/demo-sdk/servify-sdk.esm.js`
- `apps/demo-sdk/servify-sdk.umd.js`
- `apps/demo-sdk/index.d.ts`
- `docs/generated/api/docs.go`
- `docs/generated/api/swagger.json`
- `docs/generated/api/swagger.yaml`

## 统一重建入口

推荐使用：

```bash
sh scripts/regenerate-generated-assets.sh
```

它会按顺序执行：

1. 同步 demo SDK 产物
2. 重新生成 API 文档
3. 校验 `generated-assets.manifest`

## 分项生成入口

### Demo SDK

命令：

```bash
make demo-sync-sdk
```

或：

```bash
sh scripts/sync-sdk-to-demo.sh
```

来源：

- `sdk/packages/vanilla/dist/index.esm.js`
- `sdk/packages/vanilla/dist/index.js`
- `sdk/packages/vanilla/dist/index.d.ts`

输出：

- `apps/demo-sdk/servify-sdk.esm.js`
- `apps/demo-sdk/servify-sdk.umd.js`
- `apps/demo-sdk/index.d.ts`

### API Docs

命令：

```bash
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g apps/server/cmd/server/main.go -o docs/generated/api
```

输出：

- `docs/generated/api/docs.go`
- `docs/generated/api/swagger.json`
- `docs/generated/api/swagger.yaml`

### Release Changelog Draft

命令：

```bash
make release-changelog FROM=<previous-tag-or-commit> TO=HEAD
```

或：

```bash
sh scripts/generate-changelog.sh <previous-tag-or-commit> HEAD > ./.runtime/release/RELEASE_CHANGELOG.md
```

输出：

- `./.runtime/release/RELEASE_CHANGELOG.md`
- GitHub release workflow artifact 中的 `RELEASE_CHANGELOG.md`

规则：

- 这是发布产物，不是受控提交生成物
- 不进入 `generated-assets.manifest`
- 默认写入 `.runtime/` 或 workflow artifact，不写回仓库根目录

## 校验入口

存在性与 Git 跟踪校验：

```bash
sh scripts/verify-generated-assets.sh
```

工作区漂移校验：

- CI 中会在生成后执行带说明的漂移检查，并打印：
  - 哪些路径发生了变化
  - `git diff --stat`
  - 推荐的重建命令
- 本地可在重建后执行：

```bash
git diff --exit-code -- apps/demo-sdk docs/generated/api
```

## 新增生成物时的规则

- 必须能稳定重建
- 必须有明确生成命令
- 必须决定是否纳入 `generated-assets.manifest`
- 必须补对应文档说明
- 如果 CI 需要阻止漂移，必须补校验步骤
- 如果只是发布过程中的临时产物，应明确写入 `.runtime/` 或 artifact，而不是提交入库
