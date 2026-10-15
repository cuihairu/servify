# 租户级配额限流口径（translation.quota 的 unit/window）

> 状态：**已代决待核**（2026-10-15 巡检代决，用户可推翻；推翻只需改本文，
> 无代码耦合）。对应 todo「Phase 3 语音 → 租户级配额限流」卡点
> 「unit/window 口径未注册，需产品拍板」。本文给出对比表与单个明确推荐；
> enforcement 实现另有硬缺口（见 §4），本轮不落地。

## 1. 现状（2026-10-15 实测）

- `translation.quota` **只有方向登记，无任何 schema**：全仓 grep `Quota`
  在 Go/YAML/SQL/TS 零命中；唯一出处是
  `docs/realtime-translation-design.md` §3.2（「按租户配置开关与配额
  （复用 configscope 租户覆盖模式）」）与 §4.6（「按租户配额限流，
  超配额 429」）。
- 仓库现有两套「窗口」词汇，均无 `unit`/`window` 字符串枚举：
  - 限流器：`RateLimitingConfig.RequestsPerMinute + Burst`
    （`apps/server/internal/config/config.go:474-495`），窗口隐式为 1 分钟
    （`ratePerSec: rpm/60.0`），token bucket，按 IP/API-Key 键控，
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
| A. 复用限流器口径（`requests_per_minute + burst`） | 现行 `RateLimitingConfig` 同形 | 零新概念，复用现成 middleware | token bucket 是平滑速率语义，不是「预算 + 周期重置」的配额账本；只支持分钟窗；无 token 用量维度；按 IP/API-Key 键控非租户 |
| B. 单位后缀 int 字段（`limit` + `window_minutes/hours/days`） | 沿用 session-risk 命名法 | 仓库内 5 处先例 | 计量单位（次数 vs token）与时间窗口正交，后缀法表达不了二维；想加窗口档位就加字段，字段爆炸 |
| C. `limit + unit + window` 显式枚举 | `unit: requests\|tokens`，`window: hour\|day\|month`，固定滚动窗口 | 计量单位 × 时间窗口正交表达完整；枚举可校验可注册（对齐 config_template_lint 门禁）；天然挂 configscope 租户覆盖 | 需新建枚举与校验；窗口锚点需要共享存储（见 §4） |

## 3. 推荐（已代决）

**方案 C**。口径定义：

```yaml
translation:
  quota:
    enabled: false        # 默认关；开启前需完成 §4 三项缺口
    limit: 100000         # 每租户每窗口配额量
    unit: tokens          # requests | tokens（tokens 取 LLM TokenUsage 累计）
    window: day           # hour | day | month，固定滚动窗口，超配额 429
```

- `unit` 与 `window` 正交：`requests` 按翻译调用次数计，`tokens` 按
  `TokenUsage` 累计计；窗口固定滚动语义（不做 token bucket 平滑）。
- 默认 `enabled: false`：口径先注册，enforcement 缺口补齐前不产生行为。
- 与 `docs/realtime-translation-design.md` §3.2/§4.6 对齐：configscope
  租户覆盖（tenant/workspace 层覆盖 system 值），超配额 429。

## 4. enforcement 缺口（本轮不落地，写明缺什么）

按推荐直接落地 **只有 schema 注册**——会产出无消费者的死配置，违反
「demo/mock 边界清理」同类纪律，故不落。完整 enforcement 需要三项硬缺口
补齐：

1. **租户维度用量记账存储**：无 usage 表/迁移；Prometheus 计数器无
   tenant 标签且按进程累计（多实例不可汇总，违反
   `docs/multi-instance-boundary.md` 口径）。需新表（或 configscope
   存储模式同构新列）+ 列级守卫迁移。
2. **租户键控的配额检查点**：现行 `RateLimitMiddleware` 按 IP/API-Key
   键控，非租户维度；需在 translation 路径加 per-tenant 预算检查
   （configscope resolver 出配额值 + middleware 查账）。
3. **窗口锚点存储**：固定滚动窗口需 Redis/DB 窗口锚点；进程内存版在
   多实例部署下窗口各自为政，口径不成立。

## 5. 推翻方式

口径推翻 = 改本文 + 改 `todo.md` 对应注记；无代码回滚成本。
