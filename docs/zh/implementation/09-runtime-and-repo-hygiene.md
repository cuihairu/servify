# 09 Runtime And Repo Hygiene

范围：

- 仓库卫生
- 运行时产物边界
- ignore 策略
- 本地开发环境一致性
- 生成物与缓存治理

## R1 tracked-runtime-artifacts-cleanup

- [x] 盘点误提交的二进制、上传目录、测试残留、缓存文件
- [x] 清理 `server`、`server.exe`、运行时 upload 样本等非源码产物
- [x] 为测试中需要的样本文件建立明确 fixture 目录
- [x] 将运行时输出目录迁出源码树或改为临时目录
- [x] 补充清理脚本或测试 teardown，避免再次污染工作区

验收：

- 仓库中不再长期保留运行时脏产物
- 测试样本与真实运行时输出边界清晰

## R2 ignore-and-boundary-policy

- [x] 统一根目录、`sdk/`、`docs/`、`apps/*` 的 ignore 策略
- [x] 区分必须提交的 generated assets 与禁止提交的 build/runtime/cache 文件
- [x] 为二进制、日志、覆盖率、临时数据库、上传目录建立规则
- [x] 为 generated assets manifest 增加文档说明和维护规则
- [x] 为新增目录定义默认 ignore 约定

验收：

- 新人可以快速判断“哪些文件该提交，哪些不该提交”
- CI 与本地不会因边界不清反复漂移

## R3 local-dev-environment-convergence

- [x] 盘点 Windows PowerShell、WSL、Linux、CI 的命令差异
- [x] 为常用流程提供统一入口，例如 `make` / `npm scripts` / shell wrappers
- [x] 解决 Git safe directory、换行符、权限位、Node/Go 路径差异
- [x] 补充 WSL 与 Ubuntu 初始化文档
- [x] 为跨平台开发建立最小验证清单

验收：

- 同一套开发流程可以在 Windows + WSL + Linux 下稳定执行
- 常见环境问题有明确文档和修复路径

## R4 generated-assets-governance

- [x] 盘点所有需要提交的生成物与对应生成入口
- [x] 盘点所有不应提交的构建产物与缓存产物
- [x] 为 demo SDK、docs API、release/changelog 统一生成规则
- [x] 为生成物增加一键重建入口
- [x] 为 CI 增加边界漂移说明输出，降低排障成本

验收：

- 生成物治理不再是零散脚本集合，而是统一规则
- 任何一类生成物都可以快速重建和验证

## R5 repo-hygiene-guardrails

- [x] 增加仓库卫生检查，例如禁止提交二进制、运行时目录、超大文件
- [x] 增加 fixture / testdata / generated 目录命名约定
- [x] 为 PR 模板或贡献文档补充提交前自检规则
- [x] 为根目录文件增加职责说明，避免继续堆杂项文件

验收：

- 仓库结构长期可维护，不会因临时文件逐步劣化
