# 知识库选型全景与适配规划

> 状态：调研规划稿（2026-09-20）。落地以选型决策为准。
> 背景：Dify 严格说是 LLM 应用编排平台，知识库（dataset）只是其附属能力——当初把 Dify 作为「知识库插件」适配（`difykp` driver）在定位上不够严谨。本文档盘点当前主流知识库产品，评估其与 servify 知识源契约的适配成本，给出后续路线建议。

## 1. servify 知识源契约现状

统一契约位于 `apps/server/internal/platform/knowledgeprovider/provider.go`：

| 操作 | 语义 |
| --- | --- |
| `Search` | 检索：请求 → 命中列表（含得分/引用） |
| `UpsertDocument` | 文档写入/更新，返回外部文档 ID |
| `DeleteDocument` | 按 ID 删除 |
| `HealthCheck` | 可用性探测（启动期 require/降级判断依据） |
| `RebuildIndex`（可选扩展） | 索引重建 |

现有三个 driver：

| Driver | 适配对象 | 定位评估 |
| --- | --- | --- |
| `weknorakp` | WeKnora（腾讯开源知识库） | **正统知识库**，主推方向之一，见 `WEKNORA_INTEGRATION.md` |
| `pgvector` | 进程内 pgvector（DB + embedding 直配） | 轻量自托管默认路径，无外部依赖 |
| `difykp` | Dify dataset API | **定位不严谨**：Dify 是编排平台，dataset 是附属功能。作为兼容路径保留，不宜作为知识库主推 |

选择链由启动/请求级共用的 `selectKnowledgeSource` 门面收敛（刀 23）：dify（健康检查、可降级）→ weknora（健康检查）→ pgvector 直配。新增 driver 只需实现契约并接入该门面，选择优先级与装配为既有代码路径。

## 2. 选型维度

1. **API 完整度**：REST API 是否覆盖契约四操作（检索 / 上传 / 删除 / 健康检查）——决定 driver 适配成本
2. **部署形态**：自托管（Docker Compose / K8s）契合 servify 的私有化交付模式；SaaS-only 一票否决
3. **文档解析能力**：复杂 PDF / 合同 / 表格的解析质量直接决定检索质量上限
4. **检索能力**：混合检索（向量 + 全文）、rerank、引用溯源
5. **租户 / 权限模型**：多租户知识隔离能力（对齐 servify 的 tenant/workspace scope 治理方向）
6. **许可证与社区**：开源协议友好度、活跃度、长期维护风险
7. **资源占用**：embedding/解析服务的 GPU/CPU 要求

## 3. 主流知识库产品盘点（2026-09）

综合社区横评与官方文档（来源见文末）：

### 3.1 RAGFlow（InfiniFlow）——首选候选

- **定位**：开源企业级 RAG 引擎，GitHub 18k+ star，以 DeepDoc 深度文档理解（复杂 PDF/表格/版式解析）与检索精度著称
- **API**：RESTful API 完整（知识库创建、文档上传、检索等，API Key 认证），契约四操作可一一映射
- **部署**：Docker Compose 自托管，契合私有化交付
- **适配评估**：四操作映射最直接；检索质量上限高；资源占用偏高（解析服务较重）
- **风险**：版本迭代快（当前 0.x），API 稳定性需在接入时锁定版本

### 3.2 MaxKB（飞致云 / 1Panel）——次选候选

- **定位**：企业知识服务与业务落地导向，开箱即用，零编码嵌入第三方系统
- **API**：支持文档上传/在线文档爬取、自动拆分向量化、RAG 检索；API 面向问答场景为主
- **部署**：Docker 自托管，资源要求温和
- **适配评估**：落地快、运维轻；但 API 以「问答」为中心，`Search` 契约需映射其检索接口，细粒度文档管理 API 面较弱
- **风险**：深度文档解析弱于 RAGFlow

### 3.3 FastGPT（labring）

- **定位**：简易式 RAG + 工作流编排，上传即自动拆分/嵌入/索引，上手最快
- **适配评估**：与 Dify 同属「编排平台带知识库」形态——若按「Dify 定位不严谨」的口径，FastGPT 作为知识库插件存在同样的定位问题；更适合作为对照而非主推
- **风险**：定位重复（servify 已有编排层诉求由 AI 模块自身承担）

### 3.4 QAnything（网易有道）

- **定位**：轻量本地知识库问答，「子曰」模型 + 两阶段检索
- **适配评估**：轻量易部署；社区活跃度近年趋缓，长期维护风险需跟进确认
- **风险**：维护节奏与多租户能力待验证

### 3.5 AnythingLLM

- **定位**：个人 / 小团队本地知识库，轻量部署
- **适配评估**：面向个人场景，企业级多租户 / 权限 / 审计能力与 servify 交付场景不匹配，不推荐

### 3.6 Dify——定位澄清（现有兼容保留）

- **定位**：LLM 应用编排平台（工作流 / Agent / 应用发布），知识库为其附属子系
- **适配评估**：现有 `difykp` 经 dataset API 适配为统一契约，兼容保留；但其 dataset 能力（解析、检索配置）弱于专职知识库，且把编排平台当作知识库依赖会放大耦合面（升级编排平台可能波及知识检索）
- **建议**：文档定位从「知识库插件」修正为「编排平台集成（附带 dataset 兼容）」；新客户默认不主推

### 3.7 WeKnora（腾讯）——已适配

- 已有完整 driver 与集成文档（`WEKNORA_INTEGRATION.md`），作为正统知识库主推方向之一
- 与 RAGFlow 的差异化：WeKnora 部署较轻、中文生态融合好；RAGFlow 文档解析更深。两者可并存为双路径

### 3.8 向量数据库层（不属于知识库，但相关）

- pgvector（已适配，进程内直配）、Milvus、Qdrant、Weaviate、Chroma 是「知识库引擎」之下的存储/检索层
- servify 的 `pgvector` driver 走的是「自建检索层」路线（DB + embedding 直配），适合零外部依赖的默认路径；专职知识库产品则把解析/分块/rerank 一并托管

## 4. 适配评估矩阵

| 维度 | RAGFlow | MaxKB | WeKnora（现有） | Dify（现有） | pgvector（现有） |
| --- | --- | --- | --- | --- | --- |
| 契约四操作 API 映射 | 完整 | 问答为主，文档管理面较弱 | 完整 | dataset API（附属能力） | 直配（进程内实现） |
| 深度文档解析 | 强（DeepDoc） | 中 | 中 | 中 | 依赖自建分块 |
| 部署形态 | Docker 自托管 | Docker 自托管 | Docker 自托管 | Docker 自托管 | 无外部依赖 |
| 多租户隔离 | 知识库级 | 知识库级 | 知识库级 | dataset 级 | 由 servify 数据层承担 |
| 资源占用 | 偏高 | 中 | 中 | 中高 | 最低 |
| 定位严谨性 | 知识库 | 知识库 | 知识库 | 编排平台（附属） | 检索层自建 |

> API 端点细节以各官方文档实测为准；接入前按契约四操作逐项 spike 验证。

## 5. 建议路线

1. **RAGFlow driver 优先立项**：REST API 最完整、文档解析最深、自托管契合——作为知识库主推方向的补强（与 WeKnora 形成双路径：深度解析场景走 RAGFlow，轻量中文生态走 WeKnora）
2. **MaxKB 备选观察**：客户已有 MaxKB 存量时按需接入，不做默认主推
3. **Dify 定位降级**：文档与产品口径从「知识库插件」修正为「编排平台集成（dataset 兼容保留）」；`selectKnowledgeSource` 门面中 dify 分支保留兼容，不新增能力投入
4. **pgvector 保持默认轻量路径**：零外部依赖场景的默认选择不变

## 6. 落地路径（新 driver 模式）

新增 driver 参照 `difykp` / `weknorakp` 既有模式：

1. `platform/knowledgeprovider/<driver>/`：`Provider` 实现四操作 + `HealthCheck`（HTTP client + API Key 认证）
2. `mapping.go`：外部命中/文档模型 → `KnowledgeHit` / `KnowledgeDocument` 映射
3. `config` 增配置块（BaseURL / API Key / Timeout），对齐 secure-config 白名单门禁（新增 gate 必改三个白名单测试）
4. `selectKnowledgeSource` 门面接入选择链（健康检查 / 降级语义沿用现行契约）
5. swag 注释与文档同步；验收脚本对齐 `acceptance-weknora-docker.md` 模式

## 7. 参考来源

- [MaxKB 与 RAGFlow 企业级平台对比](https://www.maxkb.cn/maxkb-vs-ragflow)
- [5 大开源 RAG 平台深度横评（知乎）](https://zhuanlan.zhihu.com/p/1921226488444880527)
- [五大 AI 应用框架深度对比：Dify、FastGPT、RagFlow（百度智能云）](https://cloud.baidu.com/article/4510271)
- [RAGFlow、Dify、FastGPT、Cherry Studio、AnythingLLM 比较及部署（掘金）](https://juejin.cn/post/7520286373594234880)
- [RAG+AI 工作流+Agent：LLM 框架全面对比（腾讯云）](https://cloud.tencent.com/developer/article/2441799)
- [开源知识库 RAG 项目最全细节对比（QAnything/RAGFlow/Dify/FastGPT 等）](https://hustyichi.github.io/2024/07/08/compare)
- [RAGFlow HTTP API 官方文档](https://ragflow.io)
- [开源 RAG 框架汇总（火山引擎）](https://developer.volcengine.com/articles/7392432442768326667)
