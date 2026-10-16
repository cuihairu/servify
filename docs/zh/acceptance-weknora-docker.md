# WeKnora Docker 验收报告

**日期**: 2026-04-19
**环境**: Docker Compose (Servify + PostgreSQL + Redis + WeKnora Mock)

> 注（2026-10-08 文档对账）：本文 §1/§2 的 compose 挂载技术细节已被 **2026-09-29 挂载修正取代**——挂载点现为 `/root/{logs,uploads,config.yml}`（见 `infra/compose/docker-compose.yml:71-76`，有 runtime 测试与 checklist 双守卫）；文中 `expandViperEnvVars()` 现名 `expandEnvVarsInConfig`（`bootstrap/config.go:164`）。验收结论保留为历史存档。

## 验收结果

### 核心功能通过

| 功能项 | 状态 | 说明 |
|--------|------|------|
| 配置加载 | ✅ 通过 | 环境变量 `${WEKNORA_ENABLED}` 正确展开 |
| 服务启动 | ✅ 通过 | 服务正常启动，health check 通过 |
| Knowledge Provider 启用 | ✅ 通过 | `knowledge_provider_enabled: true` |
| WeKnora 连接 | ✅ 通过 | `knowledge_provider: "weknora"` |
| Health Check | ✅ 通过 | `knowledge_provider_healthy: true` |
| 控制面 API | ✅ 通过 | enable/disable 正常工作 |
| 查询降级 | ✅ 通过 | fallback 机制正常 |

### 部分通过

| 功能项 | 状态 | 说明 |
|--------|------|------|
| 知识上传 | ⚠️ 部分 | 配置正确，需真实 WeKnora 服务完成端到端测试 |
| 知识同步 | ⚠️ 部分 | 配置正确，需真实 WeKnora 服务完成端到端测试 |

## 修复内容

### 1. 配置文件挂载

**文件**: `infra/compose/docker-compose.yml`

```yaml
volumes:
  - ../../logs:/app/logs
  - ../../uploads:/app/uploads
  - ../../config.yml:/app/config.yml:ro  # 新增
```

### 2. 环境变量展开

**文件**: `apps/server/internal/app/bootstrap/config.go`

新增 `expandViperEnvVars()` 函数，在 Viper 解析配置前展开 `${VAR}` 环境变量占位符。

### 3. 环境变量对齐

**文件**: `infra/compose/docker-compose.yml`

```yaml
environment:
  - JWT_SECRET=${JWT_SECRET:-default-secret-key}
  - SERVIFY_JWT_SECRET=${JWT_SECRET:-default-secret-key}  # 新增
```

### 4. 配置文件简化

**文件**: `config.yml`

```yaml
weknora:
  enabled: ${WEKNORA_ENABLED}  # 简化，移除 bash 风格默认值
  base_url: "${WEKNORA_BASE_URL}"
  api_key: "${WEKNORA_API_KEY}"
  tenant_id: "${WEKNORA_TENANT_ID}"
  knowledge_base_id: "${WEKNORA_KB_ID}"
```

## 运行证据

```bash
# 服务状态
$ curl -s http://localhost:8080/health | jq '.services.ai.details.status'
{
  "knowledge_provider": "weknora",
  "knowledge_provider_enabled": true,
  "knowledge_provider_healthy": true
}

# AI Status API
$ curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/ai/status | jq '.data'
{
  "knowledge_provider": "weknora",
  "knowledge_provider_enabled": true,
  "knowledge_provider_healthy": true,
  "type": "orchestrated_enhanced"
}
```

## 后续工作

1. 使用真实 WeKnora 服务完成 upload/sync 端到端测试
2. 更新 CI/CD 配置以包含 WeKnora Docker 环境
3. 补充 E2E 测试用例覆盖完整链路
