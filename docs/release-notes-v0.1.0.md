# Servify v0.1.0 Release Notes

`Servify v0.1.0` 是首个公开预览版。

本版本的目标是：

- 可安装
- 可部署
- 可演示
- 核心客服主链路可跑通

本版本不代表企业生产稳定版，也不等同于 `v1.0`。

## 已具备能力

- 核心客服主链路：工作台、会话详情、消息收发、人工接管、转接、关闭
- 工单全流程：创建、列表、详情、更新、指派、评论、关闭、统计、导出
- AI 基础能力：AI 查询、状态、指标、provider 控制面、fallback 已有实现，但真实 provider 主路径验收仍未完全收口
- 知识链路基础能力：上传、同步、provider enable/disable、circuit breaker reset 已有接口与自动化覆盖，但当前仍不应视为“已完成发布验收”
- 认证与会话：登录、refresh 轮转、当前会话列表、退出当前会话、退出其它会话已有实现与自动化覆盖，真实发布证据仍需继续补齐
- 实时能力基础：WebSocket、WebRTC stats / connections、平台消息路由统计
- 后台基础运营面：客户、客服、审计、安全、配置治理、SLA 等已有基础入口，但完成度不一致，仍有多项仅达到“可运行/可测试”而未达到“已完成验收”

## 已验证入口

- `make build`
- `make release-check CONFIG=./config.yml`
- `GET /health`
- `GET /ready`
- `GET /metrics`
- `POST /api/v1/auth/login`
- `GET /api/omni/workspace`
- `POST /api/v1/ai/query`

补充说明：

- 默认根配置 `config.yml` 下的 `make release-check` 现已使用临时 SQLite 库完成迁移、启动与健康检查烟测，便于在空白开发机上复现最小发布自检
- 这不改变正式部署前置依赖要求；生产与 staging 仍应使用目标环境的 PostgreSQL、Redis 和真实外部依赖完成验收

## 已知限制

- 默认 event bus 仍为进程内实现，不提供持久化消息队列语义
- voice runtime 仍保留 mock / provider 边界，不等同于生产级真实 voice provider 接入
- agent runtime 仍存在部分内存态与 legacy compatibility 残留
- `/api/v1/ws` 仍为公开 realtime 入口，但现在要求显式提供 `session_id`；当前仍主要依赖限流与外围网络边界控制，不等同于完整鉴权接入
- WebRTC `GET /api/v1/webrtc/stats` 现支持无参汇总；传 `session_id` 时返回单会话连接详情
- Public knowledge `/public/kb/docs` 现只暴露显式标记为公开的文档；管理端新建文档默认保持内部可见
- AI / Knowledge 当前仅能确认“基础实现已存在、fallback 可工作、控制面可访问”；真实 provider 主路径、同步语义与生产级环境验收仍未完全收口
- 一部分管理面能力虽然已有 CRUD / 查询接口与自动化测试，但 handler 错误语义、异常路径和真实运行证据仍不均衡，不能等同于“后台运营面已全部完成”
- demo / mock / compatibility 资产仅用于开发、演示和验收辅助，不等同于正式生产能力

## 非目标

- 多实例高可用与跨实例一致性保证
- 持久化消息队列下的异步 durability 语义
- 完整 co-browsing 远程协助产品
- 企业级容量、压测、备份恢复基线
- 客户侧推荐问题 / 上下文联想问题

## 升级与部署说明

- 部署前先准备 PostgreSQL 与 Redis
- 生产环境应从 `config.production.secure.example.yml` 或等价部署配置出发
- 敏感配置必须通过环境变量或密钥管理系统注入
- 发布前至少执行：
  - `make security-check CONFIG=config.production.yml`
  - `make observability-check CONFIG=config.production.yml`
  - `make release-check CONFIG=config.production.yml`

## 推荐发布表达

可以说：

- Servify 已具备继续打磨预览版的基础
- 核心客服主链路、工单全流程和实时基础能力已可演示
- AI 基础能力与后台运营面已有基础实现，但 `0.1.0` 发布证据仍在补齐

不应说：

- `v0.1.0` 已准备好立即发布
- AI / Knowledge 已完成发布验收
- 后台基础运营面已全部完成
- 已完成企业级生产稳定交付
- 已提供完整生产级远程协助产品
