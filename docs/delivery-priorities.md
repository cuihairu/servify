# Servify Delivery Priorities

这份文档不负责罗列全部实现 backlog，而是回答一个更直接的问题：

当前仓库里，什么事情最值得先做，才能把“代码存在”推进到“可以交付”。

它和 [acceptance-checklist.md](./acceptance-checklist.md) 的关系是：

- `acceptance-checklist.md` 负责记录每条能力的验收证据与状态
- 本文负责定义当前阶段的执行顺序、取舍原则和恢复入口

## 当前判断

当前第一版产品目标已经收敛为 [Web 独立站智能客服](./v1-product-scope.md)：Web widget 接入、AI 基于知识库首答、人工接管、转接协作、工单闭环和必要后台运营。

`v1.0.0` 已于 2026-10-04 发布（[发布说明](./release-notes-v1.0.0.md)）。本文旧版列出的三类收口风险均已处理完毕：

1. 生产路径 `inmemory` / `mock` / `legacy` 兼容实现已收口（P0 批次，`services` 目录已整体移除）。
2. 主链路验收闭环已完成（P1 批次 + V1.0 收敛 B1–B4 过闸，见 [acceptance-checklist.md](./acceptance-checklist.md)）。
3. 文档/待办/实现漂移已通过 V1.0 收敛 B0 文档声明批与文档对账规则治理。

后续演进的取舍以 [v1.0.0 发布说明「演进方向」](./release-notes-v1.0.0.md) 为准：Routing 打分引擎、Ticket 关闭前拦截等，不插队扩张已冻结面（语音、多渠道）。

## 执行顺序

> 以下 P0/P1/P2/P3 顺序为 `v1.0.0` 发布前的历史执行序列，已全部执行完毕，仅作方法论留档；当前活跃 backlog 以 [todo.md](../todo.md) 为准。

### P0 先收运行时硬伤（已完成）

优先级最高的是会直接影响运行边界和部署可信度的事项：

1. 事件总线 durability 边界
2. Agent presence / load / assignment 的多实例边界
3. 配置、启动、健康检查、依赖装配的真实性
4. mock / disabled / compatibility 实现与 production 边界是否清晰

### P1 再补主链路验收闭环（已完成）

在 P0 没有继续扩大风险前，下一步是把主链路从“代码和测试基本在”推进到“有证据证明能交付”：

1. AI / Knowledge 主链路
2. Auth 自助 session 链路
3. 会话工作台主操作
4. 其它仍处于 `部分通过 / 未验 / 阻塞` 的高价值链路

### P2/P3 最后做增强项（已完成）

企业级增强、能力扩展、产品面继续铺开，必须建立在前两层已经稳定的前提上。

例如：

- 新 provider 扩展
- 更复杂的多实例治理
- 更完整的远程协助产品化工作台
- SDK / channel / voice 的能力面扩张
- 多 Agent 自治工作流或面向开发者的平台化扩展

## 当前优先任务

V1.0 收敛批次（B0–B4）已全部过闸，无进行中的优先任务。恢复工作时的入口：

1. 演进方向按 [v1.0.0 发布说明](./release-notes-v1.0.0.md)「演进方向」开列：Routing 打分引擎、Ticket 关闭前拦截规则、薄壳模块文档面收口剩余项。
2. 实施时按 [todo.md](../todo.md) 的批次纪律登记与过闸。

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
