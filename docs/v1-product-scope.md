# V1 产品收敛：Web 独立站智能客服

本文定义 Servify 第一版产品目标。它优先约束产品边界和文档叙事，不替代具体 implementation backlog。

## 一句话目标

V1 先服务一个明确场景：

> 给企业官网、品牌独立站、SaaS 官网或文档站嵌入一个 Web 智能客服入口，让访客能即时提问，AI 基于站点知识首答，必要时无缝转人工，并把复杂问题沉淀为工单继续处理。

这意味着第一版不是平台化客服中台，也不是全渠道联络中心，而是一个可独立部署、Web 优先、AI 协同的智能客服产品。

## AI 更新后的设计判断

截至 2026-06-19，OpenAI 最新模型指南已把 `gpt-5.5` 作为最新模型，并建议推理、工具调用、多轮任务优先使用 Responses API。官方能力也继续强调结构化输出、prompt caching、hosted tools、file search、Agents SDK 的编排、handoff、guardrails 与 tracing 能力。

这些更新对 Servify 的影响不是“V1 应该做更复杂的 Agent 平台”，而是：

- V1 的 AI 层应该更薄：让模型承担更强的理解、改写、总结和工具选择，业务系统只保留清晰 contract。
- V1 的输出应该更结构化：意图识别、是否转人工、知识来源、置信度、工单摘要应尽量走结构化结果，而不是靠自然语言解析。
- V1 的知识链路应该更可替换：可以参考 OpenAI file search / hosted tools 的能力形态，但 Servify 核心仍应保持 `KnowledgeProvider` 抽象，服务独立部署和私有知识库。
- V1 不需要先做多 Agent 工作流：Agents SDK 的 handoff、guardrails、tracing 思路值得借鉴，但第一版产品不应把“多 Agent 编排平台”作为用户可见能力。
- V1 应控制成本和延迟：访客首答优先低延迟、短回答；复杂总结、转人工建议、工单归纳再使用更高 reasoning 或更强模型。
- V1 的 prompt 应更短、更 outcome-first：不再堆叠冗长流程提示，而是明确目标、边界、可用证据和输出格式。

参考资料：

- [OpenAI latest model guide](https://developers.openai.com/api/docs/guides/latest-model.md)
- [Migrate to the Responses API](https://developers.openai.com/api/docs/guides/migrate-to-responses)
- [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs)
- [File search](https://developers.openai.com/api/docs/guides/tools-file-search)
- [Prompt guidance](https://developers.openai.com/api/docs/guides/prompt-guidance)
- [Agents SDK](https://developers.openai.com/api/docs/guides/agents)

## V1 推荐 AI 运行模式

第一版更合理的 AI 设计不是“多 Agent 编排平台”，而是“一个主编排器 + 少量明确工具 + 结构化输出”：

1. 一个主对话编排器负责理解访客问题、检索知识、调用必要工具、产出结构化决策。
2. 工具保持很少：
   - 检索知识
   - 查询当前会话/客户上下文
   - 创建或建议创建工单
   - 请求人工接管
3. 模型输出尽量结构化，至少包括：
   - `answer`
   - `citations`
   - `confidence`
   - `next_action`
   - `handoff_reason`
   - `ticket_summary`
4. `next_action` 只保留少数稳定状态：
   - `answer`
   - `clarify`
   - `handoff`
   - `ticket`
5. 访客首答优先使用较低推理成本；需要总结、归因、长文本归纳时再提高 reasoning。

这套设计比“多个专用 agent 相互转来转去”更适合 V1，因为它：

- 更容易调试和评估
- 更容易做可观测性和审计
- 更容易控制延迟和成本
- 更符合网站客服的高频短会话形态

## V1 保留的核心功能

### 1. Web 嵌入入口

目标是让一个独立站可以用最小成本接入：

- 一段 script 或 SDK 配置即可挂载客服入口
- 支持访客会话、消息收发、实时状态、基础访客信息
- 支持页面上下文透传，例如当前 URL、页面标题、语言、来源入口
- 支持品牌基础配置，例如名称、欢迎语、主题色、离线提示
- 支持公开配置和公开知识入口，但不能暴露管理能力

### 2. AI 首答与知识问答

AI 在 V1 里的角色是“客服协同助手”，不是完全自治的业务执行者：

- 根据知识库回答产品、使用、售后、文档类问题
- 不确定时先澄清，而不是编造答案
- 回答应带可追溯来源或至少保留来源引用给客服侧查看
- 对明显需要人工的问题给出转人工建议
- 对敏感、越权、无知识依据的问题走拒答或降级策略
- 对长对话生成面向坐席的摘要，而不是让人工从头读完整历史

### 3. 知识库管理

V1 需要把知识库做成客服首答的核心资产：

- 支持文档创建、更新、删除（当前 HTTP 仅暴露 CRUD；索引任务管理与搜索预览为 P1 增强项）
- 支持站点 FAQ、产品说明、帮助文档、售后政策等内容沉淀
- 支持 provider 可替换，不把核心逻辑绑定到 Dify、WeKnora、pgvector 或某个模型厂商
- 支持索引状态、检索命中、fallback 情况的可观测信息

### 4. 人工接管与坐席工作台

AI 解决不了的问题必须能回到人：

- 坐席可以查看会话上下文和历史消息
- 坐席可以接管当前会话并继续回复
- 支持转派给其他坐席或队列
- 支持基础在线状态、负载和分配策略
- AI 应能生成给坐席看的摘要和建议，而不是替坐席隐式执行高风险动作

### 5. 工单闭环

V1 的“解决问题”不能停在聊天窗口：

- 会话可以升级或沉淀为工单
- 工单保留客户、会话、AI 摘要、处理人、状态、评论
- 坐席能继续跟进、关闭、回看关联会话
- 后台能看到基础统计和处理状态

### 6. 后台运营与安全基线

第一版后台只保留交付必要能力：

- 坐席、客户、会话、知识库、工单的基础管理
- 登录、权限、token 失效、审计日志
- AI provider、knowledge provider、fallback、健康检查和指标入口
- 明确 dev/demo/mock 与 production 配置边界

## 能力优先级

为了避免 V1 再次发散，能力按下面三层收口：

### P0 必须交付

- Web widget / SDK 接入
- 访客会话与消息
- AI 知识问答与来源引用
- 人工接管、转派、关闭
- 工单创建、跟进、关闭
- 知识库管理与索引
- 后台基础安全和审计

### P1 可以增强

- 会话摘要、客服建议、自动打标
- 更好的访客信息采集与页面上下文利用
- 更明确的协助态 UI
- 基础运营分析，例如 deflection、handoff rate、top unanswered questions

### P2 先不纳入 V1

- 多渠道
- 语音
- 多 Agent
- 完整远程协助工作台
- 平台化租户能力
- 深业务系统集成市场

## V1 不做的功能

以下能力可以保留架构预留，但不应成为第一版产品承诺：

- 多租户 SaaS 平台、套餐计费、租户自助开通
- WhatsApp、微信、Telegram、App、电话、SIP 等多渠道接入
- 语音机器人、呼叫中心、录音转写生产链路
- 完整 co-browsing、屏幕共享、远控工作台
- 多 Agent 自治工作流、复杂任务规划、跨系统自动执行
- CRM、订单、支付、物流等深业务系统的标准集成市场
- Provider marketplace 或面向开发者的插件平台

这些不是永远不做，而是必须排在 Web 独立站客服主链路之后。

## 远程协助在 V1 的位置

远程协助仍然可以保留为差异化方向，但在 V1 不应压过网站智能客服主链路：

- 可以表达为“会话内协助、引导、联合排查和持续接管”
- 可以保留 WebSocket / WebRTC runtime、连接状态和后续扩展入口
- 不应承诺已经交付完整远控或 co-browsing 产品

也就是说，V1 先把“Web 接入 -> AI 首答 -> 人工接管 -> 转派协作 -> 工单闭环”做成稳定产品，再把远程协助增强成独立工作台。

## V1 成功标准

第一版可以按以下标准判断是否收敛完成：

- 独立站能通过 Web widget 发起真实会话。
- AI 能基于知识库回答常见问题，并在无依据时明确降级。
- 坐席能在同一会话里接管、回复、转派和关闭。
- 会话能沉淀为工单，工单能继续跟进和关闭。
- 管理员能维护知识、坐席、基础配置、安全和运行状态。
- 生产配置不依赖隐式 mock / in-memory / legacy 能力伪装交付。
- README、文档站、官网和验收清单都围绕这条链路表达，不再把后续扩展写成当前承诺。

## 文档收敛规则

后续整理文档时按这个顺序判断：

1. 先看本文，确认能力是否属于 V1。
2. 属于 V1 的内容，进入 README、文档首页、部署、验收和 delivery priorities。
3. 不属于 V1 但有架构价值的内容，放入 architecture 或 implementation backlog。
4. 不属于 V1 且还没有真实实现证据的内容，只能作为 future direction，不能写成已交付能力。
