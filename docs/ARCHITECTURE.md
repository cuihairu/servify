# 架构总览

Servify 当前阶段保持为模块化单体，围绕统一 runtime、AI 编排、业务模块、voice 协议入口和 SDK surface 逐步硬化。

## 阅读入口

- V1 产品范围：[V1 产品收敛](./v1-product-scope.md)
- 根文档：仓库根目录 `ARCHITECTURE.md`
- 当前真实状态：[当前架构分析](./current-architecture.md)
- 下一轮计划：[架构重设计计划](./architecture-redesign-plan.md)
- 后续增强方向：[远程协助](./remote-assistance.md)
- 部署指南：[部署指南](./deployment.md)
- 实施 backlog：[./implementation/README.md](./implementation/README.md)
- 工程化硬化：[./implementation/05-engineering-hardening.md](./implementation/05-engineering-hardening.md)

## 当前文档站中的对应专题

- [V1 产品收敛](/v1-product-scope)
- [远程协助（后续增强方向）](/remote-assistance)
- [当前架构分析](/current-architecture)
- [架构重设计计划](/architecture-redesign-plan)
- [部署指南](/deployment)
- [实施计划](/implementation/)
- [版本发布策略](/release-versioning)
- [测试金字塔](/testing-pyramid)
- [外部知识库集成：Dify 优先，WeKnora 兼容](/WEKNORA_INTEGRATION)

## 站内约定

- 架构总览页只承担导航和阅读顺序，不复制根目录长文
- 详细设计仍以仓库根目录的 `ARCHITECTURE.md` 为主
- 当前实现状态以 `docs/current-architecture.md` 为主，避免把目标态误读成已全部落地
- 文档站页面负责把实施、工程化和专题文档组织成可部署站点
