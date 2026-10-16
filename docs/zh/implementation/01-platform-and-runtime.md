# 01 Platform And Runtime

范围：

- bootstrap
- app wiring
- router assembly
- auth
- event bus
- realtime abstraction
- migration entry

## P1 bootstrap skeleton

- [x] 创建 `apps/server/internal/app/bootstrap`
- [x] 创建 `apps/server/internal/app/server`
- [x] 创建 `apps/server/internal/app/worker`
- [x] 新增 `bootstrap/app.go`
- [x] 定义 `App` 结构
- [x] 收口 `Config`
- [x] 收口 `Logger`
- [x] 收口 `DB`
- [x] 收口 `Router`
- [x] 收口 `Workers`
- [x] 收口 `ShutdownHooks`
- [x] 新增 `BuildApp(cfg)` 入口
- [x] 新增 `bootstrap/app_test.go`

验收：

- `BuildApp` 存在
- 不改现有功能路径

中断点：

- 目录和 `App` 已存在，但入口尚未接入

## P2 config logging observability

- [x] 新增 `bootstrap/config.go`
- [x] 新增 `bootstrap/logging.go`
- [x] 新增 `bootstrap/observability.go`
- [x] server 入口切到 bootstrap config/logging
- [x] cli 入口切到 bootstrap config/logging
- [x] tracing shutdown 统一挂到 `ShutdownHooks`

验收：

- 入口中不再重复初始化 config/logger/tracing

## P3 database and migration

- [x] 新增 `bootstrap/database.go`
- [x] 抽 DSN builder
- [x] 抽 GORM builder
- [x] 抽 GORM tracing plugin 注入
- [x] 新增 `bootstrap/migrate.go`
- [x] 收口模型迁移列表
- [x] 保留 server 自动迁移兼容开关

验收：

- `AutoMigrate` 不再写死在多个入口里

## P4 router assembly

- [x] 新增 `app/server/router.go`
- [x] 新增 `app/server/middleware.go`
- [x] 新增 `app/server/health.go`
- [x] 新增 `app/server/static.go`
- [x] 提取 Gin init
- [x] 提取 middleware registration
- [x] 提取 static root detection

验收：

- `cmd/server/main.go` 不再直接创建和装配 router

## P5 workers

- [x] 新增 `app/worker/jobs.go`
- [x] 定义 `Worker` 接口
- [x] 包装 SLA monitor
- [x] 包装 statistics worker
- [x] 注册 worker 到 `App`
- [x] 增加 worker 启停测试占位

验收：

- 后台任务不再散落在入口文件里

## P6 auth extraction

- [x] 创建 `internal/platform/auth`
- [x] 新增 `claims.go`
- [x] 新增 `validator.go`
- [x] 新增 `permissions.go`
- [x] 新增 `resolver.go`
- [x] 新增 `gin_middleware.go`
- [x] 从 middleware 中迁出 JWT 解析
- [x] 从 middleware 中迁出 perms 展开

验收：

- Gin middleware 只做 HTTP 适配

## P7 event bus

- [x] 创建 `internal/platform/eventbus`
- [x] 定义 `Event`
- [x] 定义 `Handler`
- [x] 定义 `Bus`
- [x] 实现 `inmemory_bus.go`
- [x] 增加 contract test
- [x] 将 bus 注入 `App`

验收：

- 模块可以基于 event bus 通信

## P8 realtime abstraction

- [x] 创建 `internal/platform/realtime`
- [x] 定义 `RealtimeGateway`
- [x] 定义 `RTCGateway`
- [x] 包装现有 `WebSocketHub`
- [x] 包装现有 `WebRTCService`

验收：

- 业务层不直接依赖旧 services 里的 realtime struct
