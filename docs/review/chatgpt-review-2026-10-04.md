> 来源：用户 2026-10-04 提供的 ChatGPT 审核分享（https://chatgpt.com/s/t_6ac1d31fad848191b16181a24c803262）。

## 追问（用户在该会话中的提问）

- 请根据当前Servify架构和产品方向，帮我规划未来几个版本（P1至P4）应优先实施的架构和功能优化重点，给出逐步实施方案及理由。
- 根据现有Routing模块设计，帮我提出具体的策略和技术实现建议，用以提升路由的智能度和效率。包括如何实现多维度评分和优先级策略。
- 请帮我详细解读为何Agent Workspace是当前产品层最大的机会，以及应该如何设计一个高效的Agent Workspace？请结合上文提及的功能模块与界面设计具体说明。

---

## 审核正文

我看了你现在的 `cuihairu/servify`，这次不是只看 README，而是把仓库首页、`ARCHITECTURE.md` 和当前架构说明一起看了。citeturn0view0turn2view0turn2view1

urlcuihairu/servifyhttps://github.com/cuihairu/servify

## 一句话评价

**Servify 现在已经从“远程客服/远程协助工具”比较明确地收敛成了“可私有部署的 AI Customer Service Platform（智能客服平台）”。**

而且目前的方向总体是对的：

> **Web 接入 → AI 首答 → 人工接管 → 路由/转接 → 工单 → 知识沉淀**

这个主链路已经比早期“什么都想做”的方向清晰很多。仓库自己也明确把 `tenant/workspace` 放在治理和隔离层，而不是产品主叙事。citeturn1view0

不过，我认为现在最大的问题不是技术架构“不够先进”，恰恰相反：

> **架构已经开始有点“过度平台化”，而真正应该继续打磨的是客服核心产品模型和 Agent Workspace。**

---

# 1. 现在 Servify 到底是什么

我建议你现在把 Servify 定义成：

> **Open-source, self-hosted AI customer service platform**
>
> 面向企业官网、SaaS、文档站、品牌独立站，提供 AI 客服、人工客服、会话路由、知识库和工单闭环。

而不是：

- Remote assistance platform
- Contact center
- Omnichannel communication platform
- AI Agent platform
- CRM
- Helpdesk
- Voice platform

这些都可以成为扩展，但**都不应该成为第一产品定义**。

你现在 README 的产品叙事其实已经相当接近这个方向。citeturn1view0

---

# 2. 我认为现在最正确的地方

## 2.1 Modular Monolith 是非常正确的

这一点我强烈赞成。

当前：

```text
HTTP / WebSocket
       │
       ▼
┌─────────────────────────┐
│      Modular Monolith   │
│                         │
│ conversation            │
│ routing                 │
│ ticket                  │
│ customer                │
│ agent                   │
│ knowledge               │
│ ai                      │
│ voice                   │
│ automation              │
│ analytics               │
└─────────────────────────┘
       │
       ├── PostgreSQL
       ├── Redis
       ├── LLM
       ├── Knowledge Provider
       └── Event Bus
```

这非常适合你现在这个项目。

`ARCHITECTURE.md` 也明确规定：

> 单体部署，只有规模或者组织需要时再拆微服务。

这个判断是成熟的。citeturn2view0

**不要现在为了“平台化”把 conversation / routing / ticket 拆成微服务。**

---

# 3. Domain-first 的模块边界也是对的

你现在已经不是：

```text
controllers/
services/
models/
utils/
```

这种典型 CRUD 项目。

而是：

```text
conversation
routing
ticket
customer
agent
knowledge
ai
voice
automation
analytics
```

再在模块内部：

```text
domain
application
infra
delivery
```

这对于 Servify 非常合适。

例如：

```text
conversation
├── domain
├── application
├── infra
└── delivery
```

这比传统三层 MVC 更容易继续演进。citeturn2view0turn2view1

---

# 4. Conversation 应该继续成为整个系统的“核心对象”

这是我认为 Servify 后面最应该强化的地方。

现在架构里已经明确：

> Conversation owns unified conversation record、participants、messages、channel bindings、conversation status。

而且 README 也明确把 conversation 作为多渠道交互中心。citeturn2view1

这个设计非常重要。

我甚至建议你进一步明确：

```text
Customer
   │
   ├── Conversation
   │      │
   │      ├── Message
   │      ├── Participant
   │      ├── AI interaction
   │      ├── Agent interaction
   │      ├── Routing
   │      ├── Transfer
   │      └── Ticket
   │
   └── Customer Profile
```

也就是说：

> **Conversation 是“服务过程”的中心，而 Customer 是“客户”的中心。**

不要让 Ticket 变成系统中心。

---

# 5. Ticket 不应该和 Conversation 平级竞争核心地位

目前：

```text
conversation
routing
ticket
```

三个都是一级模块。

技术上没问题，但产品模型上最好明确：

```text
Conversation
     │
     ├── AI
     │
     ├── Agent
     │
     ├── Routing
     │
     └── Ticket
```

因为真实客服场景通常是：

```text
用户进来
  ↓
开始 Conversation
  ↓
AI 尝试解决
  ↓
AI 不行
  ↓
Human Handoff
  ↓
Agent 接管
  ↓
需要后续处理
  ↓
创建 Ticket
  ↓
Ticket 完成
  ↓
Conversation 继续/关闭
```

**Ticket 是服务闭环中的一个工作对象，不应该取代 Conversation。**

这一点建议在领域模型文档里写死。

---

# 6. Routing 是一个很值得继续深挖的模块

现在已经有：

- queue
- assignment
- transfer
- skill-based routing
- escalation

这套设计是正确方向。citeturn2view1

但下一步不要只做：

```text
assign(agent_id)
```

而应该逐渐形成：

```text
RoutingPolicy
      │
      ├── skill
      ├── language
      ├── availability
      ├── workload
      ├── priority
      ├── customer tier
      ├── channel
      └── SLA
```

最终：

```text
Conversation
     ↓
Routing Engine
     ↓
Candidate Agents
     ↓
Scoring
     ↓
Assignment
```

这会成为 Servify 很有价值的核心能力。

---

# 7. Agent Workspace 是目前最值得投入的地方

这个是我认为你现在**产品层面最大的机会**。

你 README 里已经提到：

> 工作台密度、接待流程收敛、运营与扩展能力

这其实应该升级成当前第一优先级。citeturn1view0

一个真正好用的客服系统，Agent 登录以后应该不是看到一堆 CRUD 页面，而是：

```text
┌───────────────────────────────────────────────────────────┐
│ Inbox                         Search              Profile │
├───────────────┬─────────────────────────┬───────────────┤
│               │                         │               │
│ Conversations │ Conversation            │ Customer      │
│               │                         │               │
│ 🔴 12          │ User: xxx               │ Profile       │
│ 🟡 8           │                         │ Tags          │
│ 🟢 23          │ AI summary             │ History       │
│               │                         │ Tickets       │
│ Queue          │ Messages               │ Knowledge     │
│               │                         │               │
│ My tickets     │ Reply                  │ Actions       │
│               │                         │               │
└───────────────┴─────────────────────────┴───────────────┘
```

尤其应该让 Agent 在一个页面完成：

- 看客户
- 看历史
- 看 AI 摘要
- 看知识推荐
- 回复
- 接管
- 转接
- 创建 Ticket
- 修改 Ticket
- 查看 SLA
- 查看客户标签
- AI 改写回复
- AI 推荐回复

而不是在多个后台页面跳转。

---

# 8. AI 设计现在是合理的，但不要做成“AI 平台”

你的 AI 架构目前：

```text
AI Request
    ↓
Query Orchestrator
    ├── Prompt Builder
    ├── Retriever
    ├── Tool Registry
    └── LLM Provider
```

这个非常合理。citeturn1view0turn2view3

而且已经做了：

- OpenAI / Anthropic provider
- streaming
- conversation history
- knowledge retrieval
- source citation
- confidence
- handoff suggestion
- AgentCopilot

这些都很实用。citeturn2view3

但是这里有一个重要建议：

### 不要让 Servify 变成“AI Agent Framework”

不要继续无限增加：

```text
Agent
Memory
Planner
Workflow
Tools
MCP
Multi-agent
RAG
Vector DB
Model Router
...
```

那样会和很多 AI Agent 项目竞争，反而失去 Servify 的特色。

Servify 的 AI 应该永远服务于：

```text
客服
```

而不是：

```text
AI 本身
```

---

# 9. Knowledge 的 Provider 抽象非常正确

现在：

```text
KnowledgeProvider
    ├── pgvector
    ├── Dify
    ├── WeKnora
    └── future
```

这是很好的设计。citeturn1view0turn2view3

特别是你把：

```text
AI
 ↓
KnowledgeProvider
 ↓
具体知识库
```

隔离开了。

这样 Servify 可以做到：

### 默认

```text
PostgreSQL + pgvector
```

### 企业

```text
Dify
WeKnora
RAGFlow
Milvus
Elasticsearch
```

而不影响 AI 主流程。

这应该保留。

---

# 10. 但 Knowledge 应该进一步产品化

现在偏：

```text
Document
Embedding
Search
Provider
```

下一阶段建议增加：

```text
Knowledge Base
 ├── Sources
 │    ├── Markdown
 │    ├── Website
 │    ├── PDF
 │    ├── FAQ
 │    └── API
 │
 ├── Documents
 ├── Chunks
 ├── Index Jobs
 ├── Versions
 ├── Permissions
 └── Retrieval Analytics
```

尤其需要：

### “这条回答为什么是这样回答的？”

例如：

```text
AI Answer

根据退款政策……

Sources:
📄 refund-policy.md
   relevance: 0.91

📄 membership-faq.md
   relevance: 0.82
```

这对于企业客服非常重要。

---

# 11. 你现在最大的架构问题：Bounded Context 有一点过度

现在架构文档已经列了：

```text
Identity
Customer
Agent
Conversation
Routing
Ticket
Knowledge
AI
Voice
Analytics
Automation
SDK
```

数量已经不少。

而 README 又列出了：

```text
suggestion
gamification
voice
statistics
SLA
satisfaction
shift
workspace
macro
custom_field
app_integration
```

甚至迁移清单还在继续。citeturn1view0

我的建议是：

## 不要继续增加一级模块

特别是：

```text
suggestion
gamification
analytics
automation
SLA
satisfaction
shift
macro
```

这些很多更适合作为：

```text
Conversation
Agent
Ticket
Routing
```

的子能力。

否则最后很容易变成：

```text
100 个模块
每个模块都有
domain/application/infra/delivery
```

架构非常漂亮，但是产品越来越难改。

---

# 12. Analytics 应该尽量变成 Read Model，而不是业务中心

你的 `ARCHITECTURE.md` 其实已经意识到了这一点：

> Analytics owns dashboards / aggregated reporting / operational read models。

这个方向对。citeturn2view1

建议保持：

```text
业务模块
   │
   └── Domain Events
           ↓
      Analytics Read Model
           ↓
        Dashboard
```

而不是：

```text
Conversation → Analytics Service → 再决定业务
```

Analytics 应该是**观察者**。

---

# 13. Event Bus 现在也不要过度升级

你现在有：

```text
conversation.created
conversation.message_received
conversation.transfer_requested

ticket.created
ticket.assigned
ticket.resolved

call.started
call.ended

knowledge.document_indexed
```

这个方向很好。citeturn2view3

但是当前阶段我不建议直接：

```text
Kafka
```

然后把所有东西 Event Sourcing 化。

你自己架构文档也明确说：

> full event sourcing 是当前 non-goal。

继续保持这个判断。

更合适的是：

```text
Domain Event
     ↓
In-process Event Bus
     ↓
async handler
```

未来真的需要拆服务时，再把：

```text
In-process Event Bus
```

替换为：

```text
Redis Streams / NATS / Kafka
```

---

# 14. Voice / SIP：建议继续降级成 Extension

这一点你最近的收敛是对的。

目前 README 已经明确：

> Voice / SIP 不是 V1 中心。citeturn1view0

我建议继续坚持。

因为一旦进入：

```text
SIP
RTP
SRTP
WebRTC
PBX
IVR
recording
transcription
```

整个项目复杂度会暴涨。

Servify 第一阶段应该是：

```text
Web Chat
   ↓
AI
   ↓
Human
   ↓
Ticket
```

而不是：

```text
Web
Mobile
Telegram
WeCom
WhatsApp
SIP
PSTN
WebRTC
...
```

---

# 15. Remote Assist 也应该继续保持“高级能力”

你现在对远程协助的定义已经比以前合理：

> “解释”升级为“带着完成”。

而且明确不承诺已经完成完整 co-browsing 产品。citeturn1view0

这个非常重要。

我建议以后产品层次：

```text
L0 AI Answer
       ↓
L1 Human Handoff
       ↓
L2 Collaboration
       ↓
L3 Ticket
       ↓
L4 Remote Assist
```

而不是：

```text
Remote Assist = Servify
```

这样 Servify 会更容易形成产品闭环。

---

# 16. SDK 现在有点“设计超前”

你现在规划：

```text
sdk/
 ├── core
 ├── transport-http
 ├── transport-websocket
 ├── web-vanilla
 ├── web-react
 ├── web-vue
 ├── api-client
 └── app-core
```

架构本身没错。citeturn2view2

但我建议：

**不要为了架构完整性提前实现所有 SDK。**

当前真正需要的是：

```text
@servify/web
```

然后保证协议足够干净。

未来：

```text
@servify/react
@servify/vue
@servify/server
@servify/mobile
```

自然长出来即可。

否则很容易出现：

> SDK 架构做得比产品本身还复杂。

---

# 17. Tenant / Workspace：应该“存在，但不要抢戏”

你现在这个判断我赞成：

```text
tenant/workspace
```

属于：

> Governance / Isolation

而不是产品主叙事。citeturn1view0

不过需要把概念彻底定死：

```text
Tenant
  = 企业/组织

Workspace
  = 一个运营空间

Agent
  = 坐席

Customer
  = 客户

Conversation
  = 一次服务过程

Ticket
  = 后续工作项
```

例如：

```text
Tenant
 ├── Workspace: Support
 │     ├── Agents
 │     ├── Knowledge
 │     ├── Routing
 │     └── Conversations
 │
 └── Workspace: Sales
       ├── Agents
       ├── Knowledge
       └── Conversations
```

这样以后做企业部署就够用了。

---

# 18. Security 目前不错，但还需要补两个核心东西

现在已经有：

- AuthMiddleware
- Request Scope
- Permission
- Audit
- token_valid_after
- token_version
- revoke tokens

这些已经比很多开源客服项目认真很多。citeturn1view0

下一步重点应该是：

### ① Refresh Token Session

明确：

```text
User
 └── Sessions
      ├── browser
      ├── device
      ├── created_at
      ├── last_seen
      ├── revoked_at
      └── expires_at
```

支持：

```text
logout current session
logout all sessions
revoke session
```

### ② Customer Data Boundary

客服系统天然包含：

```text
姓名
邮箱
电话
聊天记录
附件
订单信息
```

因此应该把：

```text
PII
Audit
Retention
Deletion
Export
```

逐步考虑进去。

---

# 19. 我最建议增加一个“Service Timeline”

这是我觉得 Servify 后面会非常好用的东西。

对于 Customer / Conversation / Ticket：

```text
Customer Timeline

10:32  Conversation created
10:33  AI answered
10:35  Customer requested human
10:35  Routed to Alice
10:41  Alice replied
10:46  Ticket #1024 created
11:02  Ticket assigned to Bob
11:30  Ticket resolved
11:31  Customer notified
```

这比单纯：

```text
messages
tickets
logs
```

更符合客服产品。

它可以成为：

> **Customer 360 的核心视图。**

---

# 20. AI 和客服真正应该形成这个闭环

我建议最终产品模型变成：

```text
                ┌─────────────┐
                │   Customer  │
                └──────┬──────┘
                       │
                       ▼
                ┌─────────────┐
                │ Conversation│
                └──────┬──────┘
                       │
              ┌────────┴────────┐
              ▼                 ▼
          ┌───────┐         ┌────────┐
          │   AI  │         │ Agent  │
          └───┬───┘         └───┬────┘
              │                 │
              │ handoff         │
              └────────┬────────┘
                       ▼
                 ┌───────────┐
                 │  Routing  │
                 └─────┬─────┘
                       ▼
                 ┌───────────┐
                 │   Ticket  │
                 └─────┬─────┘
                       ▼
                 ┌───────────┐
                 │ Resolution│
                 └───────────┘
```

然后 Knowledge 是横向能力：

```text
              Knowledge
                  │
        ┌─────────┼─────────┐
        ▼         ▼         ▼
       AI       Agent     Ticket
```

而 Analytics：

```text
Conversation ─┐
Routing ──────┤
Ticket ───────┼──→ Events → Analytics
Agent ────────┤
AI ───────────┘
```

这会非常漂亮。

---

# 21. 当前代码/架构演进，我建议按这个顺序

不要继续大规模“架构重构”。

### P0：产品核心闭环

```text
Web SDK
 ↓
Conversation
 ↓
AI
 ↓
Handoff
 ↓
Agent Workspace
 ↓
Routing
 ↓
Ticket
```

必须真正顺滑。

---

### P1：Agent Workspace

重点：

- Inbox
- Queue
- Conversation
- Customer panel
- AI Copilot
- Knowledge suggestion
- Transfer
- Ticket
- Timeline

**这是我认为当前最高 ROI 的工作。**

---

### P2：Conversation Domain 完善

明确：

```text
Conversation
Participant
Message
ChannelBinding
ConversationEvent
ConversationState
```

以及：

```text
AI → Conversation
Agent → Conversation
Ticket → Conversation
Routing → Conversation
```

的关系。

---

### P3：Routing Engine

逐渐实现：

```text
skill
availability
capacity
priority
SLA
load
language
channel
```

---

### P4：Knowledge 产品化

重点：

```text
source
document
chunk
index
version
retrieval
citation
feedback
```

然后增加：

```text
Was this answer helpful?
```

让知识库能够通过客服反馈持续优化。

---

### P5：Security / Governance

继续补：

```text
session
refresh token
audit
PII
retention
data export
data deletion
workspace isolation
```

---

### P6：Analytics

重点不要做漂亮图表，而是做：

```text
First Response Time
Resolution Time
AI Resolution Rate
AI Handoff Rate
Agent Load
Queue Time
Ticket Resolution Rate
Customer Satisfaction
```

这些才是真正客服指标。

---

### P7：Extension

最后再做：

```text
Remote Assist
Voice
SIP
Telegram
WeCom
WhatsApp
Mobile
```

---

# 22. 我建议你删/压缩一些东西

尤其是这些：

```text
gamification
suggestion
voice
automation
analytics
```

不是删除代码，而是**降低架构存在感**。

例如：

```text
modules/
   conversation/
   customer/
   agent/
   routing/
   ticket/
   ai/
   knowledge/
```

是第一层。

然后：

```text
capabilities/
   analytics
   automation
   gamification
   suggestion
```

甚至 `analytics` 和 `automation` 更适合做横向能力。

这样项目会更容易理解。

---

# 23. 最终我会把 Servify 的架构收敛成

```text
                         Servify
                            │
             ┌──────────────┴──────────────┐
             │                             │
         Customer                       Agent
             │                             │
             └──────────┬──────────────────┘
                        ▼
                  Conversation
                        │
          ┌─────────────┼─────────────┐
          ▼             ▼             ▼
         AI           Routing       Ticket
          │             │             │
          └──────┬──────┴──────┬──────┘
                 │             │
                 ▼             ▼
             Knowledge      Workflow
                 │
                 ▼
            Provider Layer
```

横向：

```text
Identity / Auth
Tenant / Workspace
Realtime
Event Bus
Observability
Storage
SDK
```

扩展：

```text
Web
Mobile
Telegram
WeCom
Voice
SIP
Remote Assist
```

---

# 24. 最重要的定位建议

如果让我替你重新给 Servify 写一句定位，我会用：

> **Servify — Open-source AI customer service platform for self-hosted teams.**

副标题：

> **AI answers first. Humans take over when needed. Every conversation ends with resolution.**

中文：

> **Servify 是一个开源、可私有部署的 AI 智能客服系统：AI 首答，人工接管，智能路由，工单闭环。**

我认为这比“远程协助平台”“多渠道客服平台”“AI Agent 平台”都准确。

---

## 最后给 Servify 一个当前阶段的判断

| 维度 | 判断 |
|---|---|
| 产品定位 | ⭐⭐⭐⭐☆ |
| 模块化架构 | ⭐⭐⭐⭐⭐ |
| Domain 边界 | ⭐⭐⭐⭐☆ |
| AI 架构 | ⭐⭐⭐⭐⭐ |
| Knowledge 抽象 | ⭐⭐⭐⭐⭐ |
| SDK 设计 | ⭐⭐⭐⭐☆ |
| 安全基础 | ⭐⭐⭐⭐☆ |
| 实时能力 | ⭐⭐⭐⭐☆ |
| Agent 工作台 | ⭐⭐⭐☆☆ |
| 客服核心体验 | ⭐⭐⭐☆☆ |
| 当前最大风险 | **架构继续膨胀而产品体验没有同步增强** |

**所以我现在不建议你再给 Servify 做一次“大架构重构”。**

759 个 commit、模块迁移已经做了相当多工作，现在应该进入一个很不同的阶段：

> **从“把架构做正确”切换到“把客服产品做完整”。** citeturn0view0

尤其是接下来应该围绕 **Conversation + Agent Workspace + AI Handoff + Routing + Ticket** 做一轮产品级收敛，而不是继续增加更多模块。

如果你准备让其他 Code Agent 接手，我建议下一步直接给它们一份**“Servify V1.0 产品与架构收敛改造计划书”**，把**哪些保留、哪些降级、哪些删除、Domain 边界、数据模型、Agent Workspace、AI、Routing、Ticket、实施优先级和验收标准**全部写成可执行任务。

---

If you want, I can:

- 继续详细解读Agent Workspace设计
- 提出关于Routing模块的具体优化建议
- 协助规划Servify下一步架构优化重点