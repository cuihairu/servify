# Audit Log Policy

本文件定义当前审计日志的最小查询与保留策略。

## 查询入口

- 管理面查询：`GET /api/audit/logs`
- 管理面单条查询：`GET /api/audit/logs/:id`
- 管理面差异预览：`GET /api/audit/logs/:id/diff`

支持过滤：

- `action`
- `resource_type`
- `resource_id`
- `principal_kind`
- `actor_user_id`
- `success=true|false`
- `from=<RFC3339>`
- `to=<RFC3339>`
- `page`
- `page_size`
- 以及通过统一 request scope 投影的 `tenant_id` / `workspace_id`

## 当前记录范围

当前统一覆盖：

- 管理面 `/api/*` 的成功写请求
- 记录主体类型、操作者、动作、资源、请求元数据、请求载荷

当前未完全覆盖：

- 匿名 public surface
- 仅只读查询请求
- 每个 handler 的精细 `before/after` 业务快照
- 审计查询导出的更高级格式或离线任务化能力

差异预览说明：

- `GET /api/audit/logs/:id/diff` 会在当前 request scope 下读取单条审计记录
- 若 `before_json` / `after_json` 可解析为 JSON，对象字段级变更会返回 `changed_paths` 与 `changes`
- 当前为通用 JSON diff，适合排障与快速预览；更细的领域级 diff 仍可按资源类型继续增强

导出说明：

- `GET /api/audit/logs/export` 复用与列表接口一致的过滤参数，并额外支持 `limit`
- `limit` 默认 `1000`，最大 `5000`；超过上限会被服务端截断，避免管理面直接做无限量导出
- 当前导出格式为 CSV，适合临时排查、留档或交给运营侧做轻量二次分析
- 若后续导出量继续增大，建议补异步导出任务、对象存储落盘和权限水印

## 保留策略

当前建议基线：

- 默认保留 180 天在线审计日志
- 超过 180 天的记录应归档或清理
- 涉及安全事件、权限变更、关键配置变更的日志应支持更长保留期

当前仓库状态：

- 已具备持久化模型与查询接口
- 已接入定时清理 worker，默认按 180 天保留窗口删除过期记录
- 清理逻辑按批次扫描并删除 `created_at < cutoff` 的记录；命中保留边界时不会误删等于 cutoff 的日志
- 已接入冷热分层归档（见下节），未配置归档目录时保持纯删除行为

## 冷热分层归档

- 配置 `security.audit.archive_dir` 指向持久化目录后，清理 worker 在删除每批过期记录前先把整批行落为 gzip JSON 快照（冷层）；目录为空（默认）时不归档，行为与既往一致
- 快照文件名形如 `audit-archive-<UTC时间戳>-<首行ID>-<末行ID>.json.gz`，按时间与 id 天然有序，可直接按文件对账
- 行内完整保留 `prev_hash` / `entry_hash`——冷层数据与热层同一条哈希链，归档后仍可用链式校验对账，不出现「冷层不可审计」的断链
- 归档失败即中断本轮清理且不删除：热层清库先归档后删除，不存在「未归档先删除」的数据丢失窗口；故障排除后下一轮自动重试
- 归档写入采用临时文件 + rename 原子落盘，进程中断不会留下半截快照被误当完整冷层数据
- 后续可将 `archive_dir` 指向对象存储挂载点，或在此基础上再补外部归档任务，不需要改动清理语义

## 与配置回滚的配合

- `scoped_config` 的 `update` / `rollback` 会把 before / after 快照写入审计库，供 `history`、`diff` 和回滚预览复用
- 高风险 rollback 仍需显式 `confirm=true`、`change_ref`、`reason`，并在命中高风险规则时补真实 `approval_ref`
- rollback 执行后应继续通过 `POST /api/security/config/{scope}/verify/:audit_id` 回填验证结论；审计导出不承担治理职能

## 后续实现建议

1. 冷热分层归档已落地（`security.audit.archive_dir`）；权限变更、配置变更等高价值事件如需更长保留期，可基于冷层快照再建独立的长期归档策略
2. 为权限变更、配置变更扩展更稳定的长期归档策略，而不是只依赖在线库
3. 为大体量导出补异步任务、对象存储落盘和权限水印
