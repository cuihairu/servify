# WeKnora Docker Acceptance Report

**Date**: 2026-04-19
**Environment**: Docker Compose (Servify + PostgreSQL + Redis + WeKnora Mock)

> Note (2026-10-08 doc reconciliation): the compose mount technical details in §1/§2 below have been **superseded by the 2026-09-29 mount fix** — the mount points are now `/root/{logs,uploads,config.yml}` (see `infra/compose/docker-compose.yml:71-76`, guarded by both a runtime test and the checklist); `expandViperEnvVars()` in this document is now named `expandEnvVarsInConfig` (`bootstrap/config.go:164`). The acceptance conclusions below are retained as history.

## Acceptance Results

### Core functions passed

| Item | Status | Notes |
|--------|------|------|
| Config loading | ✅ passed | env var `${WEKNORA_ENABLED}` expands correctly |
| Service startup | ✅ passed | service starts normally, health check passes |
| Knowledge Provider enabled | ✅ passed | `knowledge_provider_enabled: true` |
| WeKnora connection | ✅ passed | `knowledge_provider: "weknora"` |
| Health Check | ✅ passed | `knowledge_provider_healthy: true` |
| Control-plane API | ✅ passed | enable/disable work normally |
| Query fallback | ✅ passed | fallback mechanism works |

### Partially passed

| Item | Status | Notes |
|--------|------|------|
| Knowledge upload | ⚠️ partial | config correct; needs a real WeKnora service to complete end-to-end testing |
| Knowledge sync | ⚠️ partial | config correct; needs a real WeKnora service to complete end-to-end testing |

## Fixes Made

### 1. Config file mount

**File**: `infra/compose/docker-compose.yml`

```yaml
volumes:
  - ../../logs:/app/logs
  - ../../uploads:/app/uploads
  - ../../config.yml:/app/config.yml:ro  # 新增
```

### 2. Environment variable expansion

**File**: `apps/server/internal/app/bootstrap/config.go`

Added the `expandViperEnvVars()` function, which expands `${VAR}` env var placeholders before Viper parses the config.

### 3. Environment variable alignment

**File**: `infra/compose/docker-compose.yml`

```yaml
environment:
  - JWT_SECRET=${JWT_SECRET:-default-secret-key}
  - SERVIFY_JWT_SECRET=${JWT_SECRET:-default-secret-key}  # 新增
```

### 4. Config file simplification

**File**: `config.yml`

```yaml
weknora:
  enabled: ${WEKNORA_ENABLED}  # 简化，移除 bash 风格默认值
  base_url: "${WEKNORA_BASE_URL}"
  api_key: "${WEKNORA_API_KEY}"
  tenant_id: "${WEKNORA_TENANT_ID}"
  knowledge_base_id: "${WEKNORA_KB_ID}"
```

## Runtime Evidence

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

## Follow-up Work

1. Complete upload/sync end-to-end testing with a real WeKnora service
2. Update CI/CD configuration to include the WeKnora Docker environment
3. Add E2E test cases to cover the full chain
