# 知识库选型口径（立项口径 / 选择链优先级 / 新客户默认）

> 状态：**已代决待核**（2026-10-15 巡检代决，用户可推翻）。对应
> todo「知识库选型决策：RAGFlow 立项口径、选择链优先级、新客户默认
> 口径（待产品确认）」。本文把散在
> `docs/KNOWLEDGE_BASE_LANDSCAPE.md` §3-§6 与 `docs/deployment.md`
> 「新客户交付默认知识库口径（2026-09-21 拍板）」的口径收口为单一决策
> 记录。**结论与现行代码一致，零代码改动**；唯一残留缺口见 §5。

## 1. 现状（2026-10-15 实测）

- 启动优先级（`apps/server/internal/app/server/ai_runtime.go:91-106`）：
  `pgvector` 直配 → `local` → 外部链 → 无知识源。pgvector/local 是
  全局声明、优先于外部链（`:89-95` 注释明确）。
- 外部选择链（`app/server/knowledge_source.go:64-162`）：
  **`ragflow → dify → weknora`**，逐跳 `Enabled` 门控 + 可选健康检查
  + 降级；`requireHealthy` 时无可用源则启动失败。
- 代码默认：`Knowledge.Provider: ""`（`config/config.go:1287-1292`），
  三个外部 provider 默认 `Enabled: false` —— 全新安装得到「无知识源」
  基座装配；`config.yml`（dev）单独 ship `knowledge.provider: "pgvector"`。
- provider 能力矩阵：pgvector（自托管进程内，需 pg 扩展 + embedding）、
  local（零外部依赖，sqlite/postgres 双轨）、ragflow（外部服务，
  API 锚定 v0.27.x）、dify（编排平台的 dataset 附属能力）、
  weknora（外部服务，唯一 `RebuildableProvider`）。

## 2. 对比表

### 2.1 新客户默认口径

| 方案 | 形态 | 优点 | 缺点 |
| --- | --- | --- | --- |
| A. 代码默认改 `pgvector` | 改 `config.Default()` | 首启即有知识源 | pgvector 需 pg 扩展 + embedding 服务，sqlite 部署形态直接破；隐式引入外部依赖，违「零外部配置」口径 |
| B. 维持代码默认 `""`，交付模板/文档声明 `pgvector` | 现状 | 安全默认（不意外外呼）；sqlite/评估形态可用；拍板口径落在交付面 | 交付侧需主动声明，文档口径必须跟着交付走 |
| C. 代码默认改 `local` | 改 `config.Default()` | 零外部依赖、sqlite 可用 | 偏离 2026-09-21 拍板（拍板说的是 pgvector 起步）；向量检索质量弱于外部引擎 |

### 2.2 选择链优先级

| 方案 | 顺序 | 优点 | 缺点 |
| --- | --- | --- | --- |
| A. 现行 `ragflow → dify → weknora` | 代码现状 | 与 LANDSCAPE §3.1（RAGFlow 首选）/§3.6（Dify 降级）一致；已有 7 个选择链单测锁定 | Dify 排在 weknora 之前（历史兼容位） |
| B. `ragflow → weknora → dify` | dify 沉底 | 与「Dify 定位降级、不新增投入」更一致 | 改动 7 个单测与三处文档；dify 是存量兼容路径，沉底会破坏存量客户的降级预期 |

### 2.3 立项口径

| 方向 | 结论 | 依据 |
| --- | --- | --- |
| RAGFlow | **重解析增强路径，已立项落地**（driver + 验收 + scoped config） | LANDSCAPE §3.1：REST API 最完整、DeepDoc 解析最深、Docker 自托管 |
| WeKnora | **轻量中文生态路径**（已适配，双路径之一） | LANDSCAPE §3.7、§5.1 |
| Dify | **兼容保留，不新增投入**（定位=编排平台，dataset 是附属能力） | LANDSCAPE §3.6、§5.3 |
| MaxKB | 备选观察（客户存量时按需接入） | LANDSCAPE §3.2、§5.2 |
| FastGPT / QAnything / AnythingLLM | 不立项（定位重复或不推荐） | LANDSCAPE §3.3-§3.5 |

## 3. 推荐（已代决）

1. **新客户默认口径 = 方案 B**：代码默认保持 `""`（无知识源基座），
   `pgvector 起步` 作为**交付面口径**由交付模板与 `docs/deployment.md`
   承担；客户有知识库运营诉求时按解析深度选 WeKnora（轻量中文）或
   RAGFlow（重解析）增强。理由：pgvector 的 pg 扩展 + embedding 前置
   条件不能进入隐式代码默认，安全默认（无知识源而非意外外呼）优先。
2. **选择链优先级 = 方案 A（现行序）**：`pgvector 直配 → local →
   ragflow → dify → weknora → 无知识源`。dify 留在 weknora 之前是存量
   兼容位，不与「不新增投入」冲突。
3. **立项口径 = 推荐口径**：RAGFlow 为唯一新增投入方向（重解析），
   WeKnora 双路径维持，Dify 冻结能力、MaxKB 观察。

## 4. 落地状态

推荐口径 **与现行代码完全一致**，本轮零代码改动；本文是决策收口与
引用锚点（原口径散在 LANDSCAPE §3-§6、deployment.md 拍板段两处）。

## 5. 残留缺口

**真实环境验收证据**：Dify / WeKnora / RAGFlow 的 real 模式端到端证据
仍依赖外部服务与凭证（见 todo「real 模式知识库验收」，脚本入口
`*_ACCEPTANCE_MODE=real` 已就绪）；mock 模式证据已入库
（`docs/acceptance-checklist.md:79-81,107-114`）。**缺的是外部服务
地址与凭证，不是仓内动作。**

## 6. 推翻方式

口径推翻 = 改本文 + `docs/deployment.md` 拍板段 + `todo.md` 注记；
若选方案 A/C（改代码默认）才需要动 `config.Default()` 与验收基线。
