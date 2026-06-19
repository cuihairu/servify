# Servify Delivery Priorities

这份文档不负责罗列全部实现 backlog，而是回答一个更直接的问题：

当前仓库里，什么事情最值得先做，才能把“代码存在”推进到“可以交付”。

它和 [acceptance-checklist.md](./acceptance-checklist.md) 的关系是：

- `acceptance-checklist.md` 负责记录每条能力的验收证据与状态
- 本文负责定义当前阶段的执行顺序、取舍原则和恢复入口

## 当前判断

当前第一版产品目标已经收敛为 [Web 独立站智能客服](./v1-product-scope.md)：Web widget 接入、AI 基于知识库首答、人工接管、转接协作、工单闭环和必要后台运营。

现阶段最需要优先处理的，不是继续扩展功能面，而是收口以下三类风险：

1. 生产路径里仍保留 `inmemory` / `mock` / `legacy` 兼容实现，导致本地可演示不等于生产可交付。
2. 多条主链路仍停留在 `部分通过` 或缺少真实运行证据，验收闭环没有完成。
3. 文档、待办、实现状态曾出现漂移，容易让后续执行建立在错误进度判断上。

## 执行顺序

### P0 先收运行时硬伤

优先级最高的是会直接影响运行边界和部署可信度的事项：

1. 事件总线 durability 边界
2. Agent presence / load / assignment 的多实例边界
3. 配置、启动、健康检查、依赖装配的真实性
4. mock / disabled / compatibility 实现与 production 边界是否清晰

当前对应入口以 [todo.md](../todo.md) 中的 `P0-代码审查问题` 为准。

### P1 再补主链路验收闭环

在 P0 没有继续扩大风险前，下一步是把主链路从“代码和测试基本在”推进到“有证据证明能交付”：

1. AI / Knowledge 主链路
2. Auth 自助 session 链路
3. 会话工作台主操作
4. 其它仍处于 `部分通过 / 未验 / 阻塞` 的高价值链路

当前验收事实以 [acceptance-checklist.md](./acceptance-checklist.md) 为准。

### P2/P3 最后做增强项

企业级增强、能力扩展、产品面继续铺开，必须建立在前两层已经稳定的前提上。

例如：

- 新 provider 扩展
- 更复杂的多实例治理
- 更完整的远程协助产品化工作台
- SDK / channel / voice 的能力面扩张
- 多 Agent 自治工作流或面向开发者的平台化扩展

这些内容应以 [implementation/README.md](./implementation/README.md) 为索引继续拆解，而不是插队覆盖 P0/P1。

## 当前优先任务

截至当前仓库状态，建议严格按下面顺序恢复：

1. `P0-1` 事件总线从“已接上线”推进到“真实运行验证”
2. `P0-3` Agent Redis registry / 多实例边界补集成级验证
3. `P1-1` AI / Knowledge 主链路验收闭环
4. `P1-2` Auth 自助 session 真实验收
5. `P1-3` 会话工作台收口到“通过”

如果中断恢复，以 [todo.md](../todo.md) 中最近一个 `[-]` 项为主，不要按记忆跳转。

## 取舍原则

出现下面几种冲突时，统一按此原则决策：

1. 优先修“交付边界错误”，而不是继续补“功能入口更多”。
2. 优先补真实运行证据，而不是只补单元测试。
3. 优先让文档与实现对齐，而不是维持乐观状态。
4. 能明确声明限制时，不要伪装成已支持企业能力。

## 配套文档

1. [todo.md](../todo.md)
2. [acceptance-checklist.md](./acceptance-checklist.md)
3. [implementation/README.md](./implementation/README.md)
4. [demo-and-mock-boundaries.md](./demo-and-mock-boundaries.md)
5. [operator-runbook.md](./operator-runbook.md)
