# 租户级配额限流口径（translation.quota 的 unit/window）

> 状态：**已代决待核**（2026-10-15 巡检代决，用户可推翻；推翻只需改本文，
> 无代码耦合）。对应 todo「Phase 3 语音 → 租户级配额限流」卡点
> 「unit/window 口径未注册，需产品拍板」。本文给出对比表与单个明确推荐；
> enforcement 已按 §4 落地（2026-10-15，默认 `enabled: false`）；租户/
> 工作区覆盖限额链按 §4.4 落地（2026-10-16，同轮代决）。

## 1. 现状（2026-10-15 实测）

- `translation.quota` **只有方向登记，无任何 schema**：全仓 grep `Quota`
  在 Go/YAML/SQL/TS 零命中；唯一出处是
  `docs/realtime-translation-design.md` §3.2（「按租户配置开关与配额
  （复用 configscope 租户覆盖模式）」）与 §4.6（「按租户配额限流，
  超配额 429」）。
- 仓库现有两套「窗口」词汇，均无 `unit`/`window` 字符串枚举：
  - 限流器：`RateLimitingConfig.RequestsPerMinute + Burst`
    （`apps/server/internal/config/config.go:474-495`），窗口隐式为 1 分钟
    （`ratePerSec: rpm/60.0`），token bucket，按 socket 对端 IP 键控，
    system scope。
  - 会话风险窗口：`*_window_{minutes|hours|days}` int 字段后缀
    （`config.go:439-443`，5 处先例），单位即字段名后缀。
- 计量现状：翻译 token 用量已有观测面（`TranslateResult.TokenUsage` →
  `ai_llm_tokens_total{provider, token_type}`、
  `voice_translation_sentences_total{outcome}`），但 **无 tenant 标签**，
  且 Prometheus 计数器按进程累计、多实例间不可汇总。
- 无任何用量记账表/迁移（23 个迁移中无 quota/usage 相关）。

## 2. 对比表

| 方案 | 形态 | 优点 | 缺点 |
| --- | --- | --- | --- |
| A. 复用限流器口径（`requests_per_minute + burst`） | 现行 `RateLimitingConfig` 同形 | 零新概念，复用现成 middleware | token bucket 是平滑速率语义，不是「预算 + 周期重置」的配额账本；只支持分钟窗；无 token 用量维度；按 socket 对端 IP 键控非租户 |
| B. 单位后缀 int 字段（`limit` + `window_minutes/hours/days`） | 沿用 session-risk 命名法 | 仓库内 5 处先例 | 计量单位（次数 vs token）与时间窗口正交，后缀法表达不了二维；想加窗口档位就加字段，字段爆炸 |
| C. `limit + unit + window` 显式枚举 | `unit: requests\|tokens`，`window: hour\|day\|month`，固定滚动窗口 | 计量单位 × 时间窗口正交表达完整；枚举可校验可注册（对齐 config_template_lint 门禁）；天然挂 configscope 租户覆盖 | 需新建枚举与校验；窗口锚点需要共享存储（见 §4） |

## 3. 推荐（已代决）

**方案 C**。口径定义：

```yaml
translation:
  quota:
    enabled: false        # 默认关；开启即生效（§4 已落地）
    limit: 100000         # 每租户每窗口配额量
    unit: tokens          # requests | tokens（tokens 取 LLM TokenUsage 累计）
    window: day           # hour | day | month，固定滚动窗口，超配额 429
```

- `unit` 与 `window` 正交：`requests` 按翻译调用次数计，`tokens` 按
  `TokenUsage` 累计计；窗口固定滚动语义（不做 token bucket 平滑）。
- **Token 限额是软阈值，不是硬成本上限。** 用量在 provider 成功后记账，故并发
  调用及单次调用本身都可能使累计值越过 `limit`；provider 不回 usage 或计量器
  故障时也无法记账。当前结果契约没有 token usage 的非空批量翻译在调用 provider
  前返回 `ErrTranslationQuotaUsageUnavailable`。
  此策略是滥用/成本保护，不是计费口径或严格预算。
- 默认 `enabled: false`：口径先注册，enforcement 缺口补齐前不产生行为。
- 与 `docs/realtime-translation-design.md` §3.2/§4.6 对齐：configscope
  租户覆盖（tenant/workspace 层覆盖 system 值，§4.4 已落地），超配额 429。

**计量存储选型（2026-10-15 已代决）**：Redis 固定窗，不建 DB 用量表。
理由：本仓生产配置本就钉 redis（config.yml 启动校验拒绝 inmemory），
免迁移、免列守卫；窗口状态天然瞬态，redis 重启窗口归零重计的后果只是
多放行一窗请求（429 保护面，非计费面），不产生资损。窗口锚点入键名
（`servify:quota:translation:{window}:{anchor}:{tenant}`），TTL 兜底
自清理。meter 故障时 fail-open（放行当次并保持翻译主链路可用——保护面
不该反过来打死主面）。

## 4. enforcement（2026-10-15 已落地）

三缺口按 §3 决议补齐（默认 `enabled: false`，开启需同步 redis 可用）：

1. **租户维度用量记账** → `internal/platform/quotameter`：Redis 固定窗
   计量器（不建 DB 用量表，§3 计量存储选型决议）；`Usage` 读当前窗口
   累计、`Add` 原子累加（IncrBy + ExpireNX 兜底 TTL），窗口归零重计
   容忍边界见 §3 决议段。
2. **租户键控检查点** → `translation/delivery` `QuotaGate` 装饰器：装配层
   把它包在翻译门面外（REST translate / 实时自动翻译 / 历史批量标注 /
   语音通道统一过门），租户从 ctx 认证信息提取（`platformauth`），
   无租户静默跳过；`ErrTranslationQuotaExceeded` → HTTP 429。`requests`
   单位先 +1 再比限（被拒调用也占窗，免 check-then-act 竞态）；`tokens`
   单位事前 `Usage ≥ limit` 拒（不消耗计量）、成功后按
   `TokenUsage.TotalTokens` 记账（provider 未回传 usage 不记）；这是**软阈值**，
   因调用完成后才知道用量，并发请求可超限。非空批量标注在 `tokens` 单位下以
   `ErrTranslationQuotaUsageUnavailable` 拒绝，且不调用 provider，因
   批量结果没有 usage 面；空批量不触发 provider，可透传。这仍是保护阈值，不是
   计费硬上限。
   meter 故障一律 fail-open（放行 + Warn 日志）。
3. **共享窗口锚点** → 键名编锚点
   `servify:quota:translation:{window}:{anchor}:{tenant}`（hour→整点、
   day→当日、month→当月），多实例共享同一 redis 键自然对齐；TTL =
   剩余窗口 + 10min 宽限，旧键兜底自清理。

**装配校验**（`buildTranslationQuotaGate`）：enabled=true 时校验
unit/window/limit 与 redis 可用性，非法即启动失败（fail-fast）。四份
config 模板已同步（schema 注册过 config_template_lint 门禁）。

### 4.4 configscope 租户覆盖（2026-10-16 已代决落地）

`§3` 与 realtime-translation-design §3.2 的「configscope 租户覆盖」对齐
承诺补齐（原缺口记为「待真实需求排期」，本轮按代决权限直接落地，用户可
推翻——推翻只改本文与本节实现）：

1. **存储**：迁移 `000024_translation_quota_scoped_config` 给
   `tenant_configs` / `workspace_configs` 各加一列
   `translation_quota_json` text（对齐 `rag_flow_json` /
   `session_risk_json` 列形态，sqlite 走 AutoMigrate），与
   `models.TenantConfig.TranslationQuotaJSON` /
   `models.WorkspaceConfig.TranslationQuotaJSON` 逐列对齐（列级对账
   守卫 `TestMigrationChainCoversAllModelColumns` 通过）。列内仅存
   `{limit: N}`（`config.TranslationQuotaOverrideConfig`）。
2. **解析**：`configscope.Resolver.ResolveTranslationQuotaLimit(ctx)` 三层
   合并——system 默认（`translation.quota.limit`）+ 租户覆盖 + 工作区
   覆盖（工作区优先，与 portal/openai/session_risk 的层级口径一致）；
   覆盖值 `<= 0` 视为未设置回落上一层，provider 出错或无覆盖行同样回落
   （保护面不因覆盖读取失败误拒服务）。租户与工作区身份取自
   `platformauth` ctx，provider 形态复用现有 gorm provider。
3. **检查点**：`QuotaGate` 增 `QuotaLimitSource func(ctx) int64` 端口，
   每请求解析当次限额（`NewQuotaGate` 保持静态限额语义，
   `NewQuotaGateWithLimitSource` 接解析器）；`Translate` /
   `BatchTranslate` 两面统一走 `limitFor(ctx)`。装配层
   `buildTranslationQuotaGate` 在有主库时构造 resolver 注入，无主库部署
   形态传 nil（只按系统默认限额把关，零行为差异）。
4. **只覆盖 `limit`**：`enabled` / `unit` / `window` 保持系统级——
   `window` 已进入 Redis 计量键名，按租户切窗需另一组计量键与键空间
   治理，收益不足以抵复杂度；`unit` 与计量语义绑定（requests 预占窗 /
   tokens 事后记账），租户级混用会产生口径歧义。

**本轮明确不做**（后续缺口）：

- **管理端写面**：覆盖值目前只有读路径生效（覆盖行需直接写
  tenant_configs / workspace_configs）；管理端 scope 表单、字段校验与
  审计段需 `scoped_config_handler` 新 scope 分支 + admin 前端表单，属
  产品面而非 enforcement 缺口，单独立项。
- **实时/历史路径的租户 plumbing**：这些路径的 ctx 尚未携带租户时按
  无租户跳过（过门但不计量）；REST translate 端点已带认证租户全量生效。

## 5. 推翻方式

口径推翻 = 改本文 + 改 `todo.md` 对应注记 + `translation.quota.enabled`
回 false（默认即关，无数据回滚成本；meter 键靠 TTL 自清理）。覆盖限额
链推翻（§4.4）= 清空 `translation_quota_json` 覆盖行即回到系统单值口径
（列可留，无覆盖行=无覆盖；migration 为纯加列，无回滚需求）。
