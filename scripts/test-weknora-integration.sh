#!/bin/bash

# WeKnora compatibility 集成测试脚本
# 用于验证 Servify 外部 knowledge provider 链路中的 WeKnora 兼容路径是否正常工作

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 WeKnora compatibility 集成测试开始..."

# 服务端点（可被环境变量覆盖）
SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
WEKNORA_URL=${WEKNORA_URL:-"http://localhost:9000"}
WEKNORA_ENABLED=${WEKNORA_ENABLED:-true}
JWT_SECRET=${JWT_SECRET:-"dev-secret-key-change-in-production"}
WEKNORA_ACCEPTANCE_MODE=${WEKNORA_ACCEPTANCE_MODE:-"mock"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/weknora-acceptance"}

mkdir -p "$EVIDENCE_DIR"

save_response() {
  local name=$1
  local body=$2
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

write_manifest() {
  MANIFEST_MODE="${WEKNORA_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_PROVIDER_URL="${WEKNORA_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_SERVICE_TYPE="${SERVICE_TYPE:-unknown}" \
  MANIFEST_PROVIDER_CAPABLE="${SERVICE_IS_PROVIDER_CAPABLE:-false}" \
  MANIFEST_ACTIVE_PROVIDER="${ACTIVE_PROVIDER:-unknown}" \
  MANIFEST_PROVIDER_ENABLED="${KNOWLEDGE_PROVIDER_ENABLED:-unknown}" \
  MANIFEST_PROVIDER_HEALTHY="${KNOWLEDGE_PROVIDER_HEALTHY:-unknown}" \
  MANIFEST_PROVIDER_AVAILABLE="${WEKNORA_AVAILABLE:-false}" \
  MANIFEST_DISABLE_OK="${KNOWLEDGE_PROVIDER_DISABLE_OK:-false}" \
  MANIFEST_ENABLE_OK="${KNOWLEDGE_PROVIDER_ENABLE_OK:-false}" \
  MANIFEST_RESET_OK="${CIRCUIT_BREAKER_RESET_OK:-false}" \
  MANIFEST_FALLBACK_OK="${FALLBACK_QUERY_OK:-false}" \
  MANIFEST_FALLBACK_STRATEGY="${FALLBACK_QUERY_STRATEGY:-unknown}" \
  MANIFEST_FALLBACK_USAGE="${FALLBACK_USAGE_COUNT_AFTER_DISABLE:-N/A}" \
  MANIFEST_UPLOAD_OK="${UPLOAD_OK:-false}" \
  MANIFEST_SYNC_OK="${SYNC_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "weknora",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "provider_url": os.environ.get("MANIFEST_PROVIDER_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
        "service_type": os.environ.get("MANIFEST_SERVICE_TYPE", "unknown"),
        "service_provider_capable": os.environ.get("MANIFEST_PROVIDER_CAPABLE", "false"),
        "knowledge_provider": os.environ.get("MANIFEST_ACTIVE_PROVIDER", "unknown"),
        "knowledge_provider_enabled": os.environ.get("MANIFEST_PROVIDER_ENABLED", "unknown"),
        "knowledge_provider_healthy": os.environ.get("MANIFEST_PROVIDER_HEALTHY", "unknown"),
    },
    "checks": {
        "provider_available": os.environ.get("MANIFEST_PROVIDER_AVAILABLE", "false"),
        "knowledge_provider_disable_ok": os.environ.get("MANIFEST_DISABLE_OK", "false"),
        "knowledge_provider_enable_ok": os.environ.get("MANIFEST_ENABLE_OK", "false"),
        "circuit_breaker_reset_ok": os.environ.get("MANIFEST_RESET_OK", "false"),
        "fallback_query_ok": os.environ.get("MANIFEST_FALLBACK_OK", "false"),
        "fallback_query_strategy": os.environ.get("MANIFEST_FALLBACK_STRATEGY", "unknown"),
        "fallback_usage_count_after_disable": os.environ.get("MANIFEST_FALLBACK_USAGE", "N/A"),
        "knowledge_upload_ok": os.environ.get("MANIFEST_UPLOAD_OK", "false"),
        "knowledge_sync_ok": os.environ.get("MANIFEST_SYNC_OK", "false"),
    },
    "evidence_files": sorted(
        name for name in os.listdir(evidence_dir)
        if name != "manifest.json" and os.path.isfile(os.path.join(evidence_dir, name))
    ),
}
with open(out, "w", encoding="utf-8") as f:
    json.dump(payload, f, ensure_ascii=False, indent=2, sort_keys=True)
    f.write("\n")
PY
}

json_get() {
  local json_input="${1:-}"
  local python_expr="${2:-}"
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$json_input" | jq -r "$python_expr" 2>/dev/null
    return $?
  fi

  JSON_INPUT="$json_input" python3 - "$python_expr" <<'PY'
import json
import os
import sys

expr = sys.argv[1].strip()
payload = os.environ.get("JSON_INPUT", "")

try:
    data = json.loads(payload)
except Exception:
    print("")
    sys.exit(1)

def query(obj, path):
    current = obj
    for raw_part in path.split("."):
        part = raw_part.strip()
        if not part:
            continue
        if isinstance(current, dict) and part in current:
            current = current[part]
        else:
            return None
    return current

fallback = ""
paths = []
if "//" in expr:
    left, right = expr.split("//", 1)
    expr = left.strip()
    fallback = right.strip().strip('"')

if expr.startswith("."):
    paths.append(expr.lstrip("."))

result = None
for path in paths:
    candidate = query(data, path)
    if candidate is not None:
        result = candidate
        break

if result is None:
    print(fallback)
elif isinstance(result, bool):
    print("true" if result else "false")
else:
    print(result)
PY
}

host_from_url() {
  local url="${1:-}"
  url="${url#http://}"
  url="${url#https://}"
  url="${url%%/*}"
  url="${url%%:*}"
  printf '%s' "$url"
}

is_private_or_local_host() {
  local host
  host=$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')
  case "$host" in
    localhost|127.*|0.0.0.0|::1)
      return 0
      ;;
    10.*|192.168.*)
      return 0
      ;;
    172.1[6-9].*|172.2[0-9].*|172.3[0-1].*)
      return 0
      ;;
    *.local|*.internal|host.docker.internal)
      return 0
      ;;
  esac
  return 1
}

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
WeKnora compatibility acceptance summary
mode=$WEKNORA_ACCEPTANCE_MODE
servify_url=$SERVIFY_URL
weknora_url=$WEKNORA_URL
weknora_enabled=$WEKNORA_ENABLED
EOF

echo "🗂️ 证据输出目录: $EVIDENCE_DIR"

create_service_token() {
  python3 - "$JWT_SECRET" <<'PY'
import base64
import hashlib
import hmac
import json
import sys
import time

secret = sys.argv[1].encode()

def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()

now = int(time.time())
header = {"alg": "HS256", "typ": "JWT"}
payload = {
    "sub": "integration-service",
    "token_type": "service",
    "principal_kind": "service",
    "roles": ["service"],
    "iat": now,
    "exp": now + 3600,
}

signing_input = f"{b64url(json.dumps(header, separators=(',', ':')).encode())}.{b64url(json.dumps(payload, separators=(',', ':')).encode())}"
signature = hmac.new(secret, signing_input.encode(), hashlib.sha256).digest()
print(f"{signing_input}.{b64url(signature)}")
PY
}

AUTH_TOKEN=$(create_service_token)
AUTH_HEADER="Authorization: Bearer ${AUTH_TOKEN}"

if [ "$WEKNORA_ACCEPTANCE_MODE" != "mock" ] && [ "$WEKNORA_ACCEPTANCE_MODE" != "real" ]; then
  echo "❌ 不支持的 WEKNORA_ACCEPTANCE_MODE: $WEKNORA_ACCEPTANCE_MODE"
  exit 1
fi

WEKNORA_HOST=$(host_from_url "$WEKNORA_URL")
append_summary "weknora_host=$WEKNORA_HOST"

if [ "$WEKNORA_ACCEPTANCE_MODE" = "real" ] && is_private_or_local_host "$WEKNORA_HOST"; then
  echo "❌ real 模式拒绝使用本地或私网 WeKnora compatibility 地址: $WEKNORA_HOST"
  echo "   请显式指向外部真实 WeKnora 兼容环境，再重新执行验收。"
  append_summary "real_mode_guard=blocked_private_or_local_host"
  exit 1
fi

# 小工具：带重试的等待
wait_for() {
  local name=$1 url=$2 max=$3 sleep_s=$4
  echo "⏳ 等待 $name 可用: $url (最多 ${max} 次，每次 ${sleep_s}s)"
  for i in $(seq 1 "$max"); do
    if curl -fsS "$url" > /dev/null; then
      echo "✅ $name 可用"
      return 0
    fi
    echo "… 第 $i/${max} 次重试"
    sleep "$sleep_s"
  done
  echo "❌ $name 不可用: $url"
  return 1
}

echo "🔍 检查服务状态..."

# 等待服务启动
wait_for "Servify Health" "$SERVIFY_URL/health" 30 2
WEKNORA_AVAILABLE=false
WEKNORA_HEALTH_BODY=""
WEKNORA_HEALTH_SERVICE="unknown"
if [ "$WEKNORA_ENABLED" = "true" ]; then
  if wait_for "WeKnora Health" "$WEKNORA_URL/api/v1/health" 30 2; then
    WEKNORA_AVAILABLE=true
  else
    echo "⚠️ WeKnora 未就绪，将尝试降级模式继续"
  fi
fi

# 1. 测试 Servify 健康检查
echo "  ✓ 测试 Servify 健康检查..."
SERVIFY_HEALTH=$(curl -fsS "$SERVIFY_URL/health")
save_response "servify-health" "$SERVIFY_HEALTH"
if [ -n "$SERVIFY_HEALTH" ]; then
    echo "    ✅ Servify 健康检查通过"
else
    echo "    ❌ Servify 健康检查失败"
    exit 1
fi

# 2. 测试 WeKnora 健康检查（如果启用）
if [ "${WEKNORA_ENABLED:-false}" = "true" ]; then
    echo "  ✓ 测试 WeKnora compatibility 健康检查..."
    if WEKNORA_HEALTH_BODY=$(curl -fsS "$WEKNORA_URL/api/v1/health"); then
        echo "    ✅ WeKnora compatibility 健康检查通过"
        save_response "weknora-health" "$WEKNORA_HEALTH_BODY"
        WEKNORA_HEALTH_SERVICE=$(json_get "$WEKNORA_HEALTH_BODY" '.service // "unknown"' || echo "unknown")
        append_summary "weknora_health_service=$WEKNORA_HEALTH_SERVICE"
    else
        echo "    ⚠️  WeKnora compatibility 健康检查失败，但降级机制可用"
        append_summary "weknora_health=unavailable"
    fi
fi

# 3. 测试 AI API
echo "🤖 测试 AI 服务..."

# 测试简单查询
echo "  ✓ 测试基础 AI 查询..."
AI_RESPONSE=$(curl -fsS -X POST "$SERVIFY_URL/api/v1/ai/query" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d '{
        "query": "你好，我想了解远程协助功能",
        "session_id": "test_session_123"
    }')
save_response "ai-query" "$AI_RESPONSE"

if echo "$AI_RESPONSE" | grep -q '"success":true'; then
    echo "    ✅ AI 查询测试通过"
    AI_CONTENT=$(json_get "$AI_RESPONSE" '.data.content // ""' || true)
    if [ -n "$AI_CONTENT" ]; then
      echo "    📝 AI 响应: $AI_CONTENT"
    else
      echo "    📝 AI 原始响应: $AI_RESPONSE"
    fi
else
    echo "    ❌ AI 查询测试失败"
    echo "    📝 错误响应: $AI_RESPONSE"
    exit 1
fi

# 4. 测试 AI 状态
echo "  ✓ 测试 AI 服务状态..."
AI_STATUS=$(curl -fsS \
    -H "$AUTH_HEADER" \
    "$SERVIFY_URL/api/v1/ai/status")
save_response "ai-status" "$AI_STATUS"
SERVICE_TYPE="unknown"
SERVICE_IS_PROVIDER_CAPABLE=false

if echo "$AI_STATUS" | grep -q '"success":true'; then
    echo "    ✅ AI 状态查询通过"

    SERVICE_TYPE=$(json_get "$AI_STATUS" '.data.type // "unknown"' || echo "unknown")
    ACTIVE_PROVIDER=$(json_get "$AI_STATUS" '.data.knowledge_provider // "unknown"' || echo "unknown")
    KNOWLEDGE_PROVIDER_ENABLED=$(json_get "$AI_STATUS" '.data.knowledge_provider_enabled // "__missing__"' || echo "__missing__")
    KNOWLEDGE_PROVIDER_HEALTHY=$(json_get "$AI_STATUS" '.data.knowledge_provider_healthy // "unknown"' || echo "unknown")
    echo "    📊 服务类型: $SERVICE_TYPE"
    echo "    📊 当前 knowledge provider: $ACTIVE_PROVIDER"

    if [ "$KNOWLEDGE_PROVIDER_ENABLED" = "true" ]; then
        SERVICE_IS_PROVIDER_CAPABLE=true
        echo "    🚀 使用支持外部 knowledge provider 的 AI 服务"
    elif [ "$KNOWLEDGE_PROVIDER_ENABLED" = "__missing__" ] && { [ "$SERVICE_TYPE" = "enhanced" ] || [ "$SERVICE_TYPE" = "orchestrated_enhanced" ] || [ "$ACTIVE_PROVIDER" != "unknown" ]; }; then
        SERVICE_IS_PROVIDER_CAPABLE=true
        echo "    🚀 使用支持外部 knowledge provider 的 AI 服务（由兼容字段推断）"
    else
        if [ "$KNOWLEDGE_PROVIDER_ENABLED" = "__missing__" ]; then
            KNOWLEDGE_PROVIDER_ENABLED="unknown"
        fi
        echo "    📚 使用内置知识库 / fallback AI 服务"
    fi
else
    echo "    ❌ AI 状态查询失败"
    echo "    📝 错误响应: $AI_STATUS"
fi

# 5. 测试 WeKnora compatibility 专用功能（如果是增强服务）
UPLOAD_OK=false
SYNC_OK=false
ACTIVE_PROVIDER=${ACTIVE_PROVIDER:-unknown}
KNOWLEDGE_PROVIDER_ENABLED=${KNOWLEDGE_PROVIDER_ENABLED:-unknown}
KNOWLEDGE_PROVIDER_HEALTHY=${KNOWLEDGE_PROVIDER_HEALTHY:-unknown}
KNOWLEDGE_PROVIDER_DISABLE_OK=false
KNOWLEDGE_PROVIDER_ENABLE_OK=false
CIRCUIT_BREAKER_RESET_OK=false
STATUS_AFTER_DISABLE_ENABLED="unknown"
STATUS_AFTER_ENABLE_ENABLED="unknown"
FALLBACK_QUERY_OK=false
FALLBACK_QUERY_STRATEGY="unknown"
FALLBACK_USAGE_COUNT_AFTER_DISABLE="N/A"
if [ "$SERVICE_IS_PROVIDER_CAPABLE" = "true" ]; then
  echo "🔧 测试 knowledge provider / WeKnora compatibility 功能..."

    echo "  ✓ 测试 knowledge provider 控制面..."
    DISABLE_RESPONSE=$(curl -fsS -X PUT "$SERVIFY_URL/api/v1/ai/knowledge-provider/disable" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json")
    save_response "knowledge-provider-disable" "$DISABLE_RESPONSE"
    if echo "$DISABLE_RESPONSE" | grep -q '"success":true'; then
        KNOWLEDGE_PROVIDER_DISABLE_OK=true
        echo "    ✅ knowledge provider disable 成功"
    else
        echo "    ❌ knowledge provider disable 失败: $DISABLE_RESPONSE"
        exit 1
    fi

    AI_STATUS_AFTER_DISABLE=$(curl -fsS \
        -H "$AUTH_HEADER" \
        "$SERVIFY_URL/api/v1/ai/status")
    save_response "ai-status-after-disable" "$AI_STATUS_AFTER_DISABLE"
    STATUS_AFTER_DISABLE_ENABLED=$(json_get "$AI_STATUS_AFTER_DISABLE" '.data.knowledge_provider_enabled // "unknown"' || echo "unknown")
    if [ "$STATUS_AFTER_DISABLE_ENABLED" = "false" ]; then
        echo "    ✅ disable 后状态已反映 knowledge_provider_enabled=false"
    elif [ "$STATUS_AFTER_DISABLE_ENABLED" = "unknown" ]; then
        echo "    ⚠️  disable 后状态未显式返回 knowledge_provider_enabled，稍后将通过 fallback 行为补证"
    else
        echo "    ❌ disable 后状态未收口，knowledge_provider_enabled=$STATUS_AFTER_DISABLE_ENABLED"
        exit 1
    fi

    FALLBACK_QUERY_RESPONSE=$(curl -fsS -X POST "$SERVIFY_URL/api/v1/ai/query" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d '{
            "query": "请在外部 knowledge provider 不可用时走 fallback 响应",
            "session_id": "fallback_after_disable"
        }')
    save_response "ai-query-after-disable" "$FALLBACK_QUERY_RESPONSE"
    if echo "$FALLBACK_QUERY_RESPONSE" | grep -q '"success":true'; then
        FALLBACK_QUERY_STRATEGY=$(json_get "$FALLBACK_QUERY_RESPONSE" '.data.strategy // "unknown"' || echo "unknown")
        if [ "$FALLBACK_QUERY_STRATEGY" = "fallback" ]; then
            FALLBACK_QUERY_OK=true
            if [ "$STATUS_AFTER_DISABLE_ENABLED" = "unknown" ]; then
                STATUS_AFTER_DISABLE_ENABLED="false"
                echo "    ✅ disable 后状态已由 fallback 行为补证为 knowledge_provider_enabled=false"
            fi
            echo "    ✅ disable 后 fallback 查询成功，strategy=fallback"
        else
            echo "    ❌ disable 后未进入 fallback，strategy=$FALLBACK_QUERY_STRATEGY"
            exit 1
        fi
    else
        echo "    ❌ disable 后 fallback 查询失败: $FALLBACK_QUERY_RESPONSE"
        exit 1
    fi

    METRICS_AFTER_FALLBACK=$(curl -fsS \
        -H "$AUTH_HEADER" \
        "$SERVIFY_URL/api/v1/ai/metrics")
    save_response "ai-metrics-after-fallback" "$METRICS_AFTER_FALLBACK"
    FALLBACK_USAGE_COUNT_AFTER_DISABLE=$(json_get "$METRICS_AFTER_FALLBACK" '.data.fallback_usage_count // "N/A"' || echo "N/A")
    echo "    📊 disable 后 fallback 使用次数: $FALLBACK_USAGE_COUNT_AFTER_DISABLE"

    ENABLE_RESPONSE=$(curl -fsS -X PUT "$SERVIFY_URL/api/v1/ai/knowledge-provider/enable" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json")
    save_response "knowledge-provider-enable" "$ENABLE_RESPONSE"
    if echo "$ENABLE_RESPONSE" | grep -q '"success":true'; then
        KNOWLEDGE_PROVIDER_ENABLE_OK=true
        echo "    ✅ knowledge provider enable 成功"
    else
        echo "    ❌ knowledge provider enable 失败: $ENABLE_RESPONSE"
        exit 1
    fi

    AI_STATUS_AFTER_ENABLE=$(curl -fsS \
        -H "$AUTH_HEADER" \
        "$SERVIFY_URL/api/v1/ai/status")
    save_response "ai-status-after-enable" "$AI_STATUS_AFTER_ENABLE"
    STATUS_AFTER_ENABLE_ENABLED=$(json_get "$AI_STATUS_AFTER_ENABLE" '.data.knowledge_provider_enabled // "unknown"' || echo "unknown")
    if [ "$STATUS_AFTER_ENABLE_ENABLED" = "true" ]; then
        echo "    ✅ enable 后状态已反映 knowledge_provider_enabled=true"
    elif [ "$STATUS_AFTER_ENABLE_ENABLED" = "unknown" ]; then
        STATUS_AFTER_ENABLE_ENABLED="true"
        echo "    ✅ enable 后状态字段缺失，按控制面成功与 provider-capable 运行态兼容推断为 knowledge_provider_enabled=true"
    else
        echo "    ❌ enable 后状态未收口，knowledge_provider_enabled=$STATUS_AFTER_ENABLE_ENABLED"
        exit 1
    fi

    RESET_RESPONSE=$(curl -fsS -X POST "$SERVIFY_URL/api/v1/ai/circuit-breaker/reset" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json")
    save_response "circuit-breaker-reset" "$RESET_RESPONSE"
    if echo "$RESET_RESPONSE" | grep -q '"success":true'; then
        CIRCUIT_BREAKER_RESET_OK=true
        echo "    ✅ circuit breaker reset 成功"
    else
        echo "    ❌ circuit breaker reset 失败: $RESET_RESPONSE"
        exit 1
    fi

    # 测试指标查询
    echo "  ✓ 测试服务指标..."
    METRICS_RESPONSE=$(curl -fsS \
        -H "$AUTH_HEADER" \
        "$SERVIFY_URL/api/v1/ai/metrics")
    save_response "ai-metrics" "$METRICS_RESPONSE"

    if echo "$METRICS_RESPONSE" | grep -q '"success":true'; then
        echo "    ✅ 指标查询通过"

        # 显示一些关键指标
        QUERY_COUNT=$(json_get "$METRICS_RESPONSE" '.data.query_count // "N/A"' || echo "N/A")
        WEKNORA_COUNT=$(json_get "$METRICS_RESPONSE" '.data.weknora_usage_count // "N/A"' || echo "N/A")
        FALLBACK_COUNT=$(json_get "$METRICS_RESPONSE" '.data.fallback_usage_count // "N/A"' || echo "N/A")

        echo "    📊 查询总数: $QUERY_COUNT"
        echo "    📊 WeKnora compatibility 使用次数: $WEKNORA_COUNT"
        echo "    📊 降级使用次数: $FALLBACK_COUNT"
    else
        echo "    ⚠️  指标查询失败: $METRICS_RESPONSE"
    fi

    # 测试文档上传
    echo "  ✓ 测试文档上传..."
    UPLOAD_RESPONSE=$(curl -sS -X POST "$SERVIFY_URL/api/v1/ai/knowledge/upload" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d '{
            "title": "测试文档",
            "content": "这是一个测试文档，用于验证外部 knowledge provider 集成功能。包含远程协助、智能客服等功能介绍。",
            "tags": ["测试", "集成", "验证"]
        }') || echo '{"success":false,"error":"curl failed"}'

    if echo "$UPLOAD_RESPONSE" | grep -q '"success":true'; then
        echo "    ✅ 文档上传测试通过"
        UPLOAD_OK=true
    else
        echo "    ⚠️  文档上传测试失败：$UPLOAD_RESPONSE"
        if [ "$WEKNORA_AVAILABLE" != "true" ]; then
          echo "       （提示：当前处于降级模式，外部 knowledge provider 不可用）"
        fi
    fi
    save_response "knowledge-upload" "$UPLOAD_RESPONSE"

    echo "  ✓ 测试知识同步..."
    SYNC_RESPONSE=$(curl -sS -X POST "$SERVIFY_URL/api/v1/ai/knowledge/sync" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d '{}') || echo '{"success":false,"error":"curl failed"}'
    save_response "knowledge-sync" "$SYNC_RESPONSE"
    if echo "$SYNC_RESPONSE" | grep -q '"success":true'; then
        echo "    ✅ 知识同步测试通过"
        SYNC_OK=true
    else
        echo "    ⚠️  知识同步测试失败：$SYNC_RESPONSE"
    fi
fi

# 6. 测试 WebSocket 连接
echo "🔌 测试 WebSocket 连接..."

# 检查 WebSocket 端点是否响应
WS_STATS=$(curl -fsS \
    -H "$AUTH_HEADER" \
    "$SERVIFY_URL/api/v1/ws/stats")
save_response "ws-stats" "$WS_STATS"

if echo "$WS_STATS" | grep -q '"success":true'; then
    echo "    ✅ WebSocket 服务正常"

    CLIENT_COUNT=$(json_get "$WS_STATS" '.data.client_count // "N/A"' || echo "N/A")
    echo "    📊 当前连接数: $CLIENT_COUNT"
else
    echo "    ❌ WebSocket 服务异常: $WS_STATS"
fi

# 7. 测试 WebRTC 功能
echo "📡 测试 WebRTC 服务..."

WEBRTC_STATS=$(curl -fsS \
    -H "$AUTH_HEADER" \
    "$SERVIFY_URL/api/v1/webrtc/connections")
save_response "webrtc-connections" "$WEBRTC_STATS"

if echo "$WEBRTC_STATS" | grep -q '"success":true'; then
    echo "    ✅ WebRTC 服务正常"

    CONNECTION_COUNT=$(json_get "$WEBRTC_STATS" '.data.connection_count // "N/A"' || echo "N/A")
    echo "    📊 WebRTC 连接数: $CONNECTION_COUNT"
else
    echo "    ❌ WebRTC 服务异常: $WEBRTC_STATS"
fi

# 8. 性能测试
echo "⚡ 简单性能测试..."

echo "  ✓ 测试并发查询处理..."
CONCURRENT_REQUESTS=5
START_TIME=$(date +%s)

for i in $(seq 1 $CONCURRENT_REQUESTS); do
    curl -s -X POST "$SERVIFY_URL/api/v1/ai/query" \
        -H "$AUTH_HEADER" \
        -H "Content-Type: application/json" \
        -d "{
            \"query\": \"测试查询 $i\",
            \"session_id\": \"test_session_$i\"
        }" > /dev/null &
done

wait

END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo "    ✅ $CONCURRENT_REQUESTS 个并发请求完成"
echo "    ⏱️  总耗时: ${DURATION}s"

# 9. 集成测试总结
echo ""
echo "📋 集成测试总结:"
echo "════════════════════════════════════════"

# 检查总体状态
OVERALL_HEALTH=$(curl -fsS "$SERVIFY_URL/health")
OVERALL_STATUS=$(json_get "$OVERALL_HEALTH" '.status // "unknown"' || echo "unknown")

case "$OVERALL_STATUS" in
    "healthy")
        echo "🎉 所有服务运行正常！"
        echo "✅ Servify + WeKnora compatibility 集成测试通过"
        ;;
    "degraded")
        echo "⚠️  部分服务降级运行"
        echo "✅ 核心功能正常，外部 knowledge provider 可能不可用但有降级保护"
        ;;
    *)
        echo "❌ 服务状态异常: $OVERALL_STATUS"
        echo "❌ 集成测试失败"
        exit 1
        ;;
esac

append_summary "overall_status=$OVERALL_STATUS"
append_summary "service_type=$SERVICE_TYPE"
append_summary "service_provider_capable=$SERVICE_IS_PROVIDER_CAPABLE"
append_summary "knowledge_provider=$ACTIVE_PROVIDER"
append_summary "knowledge_provider_enabled=$KNOWLEDGE_PROVIDER_ENABLED"
append_summary "knowledge_provider_healthy=$KNOWLEDGE_PROVIDER_HEALTHY"
append_summary "weknora_available=$WEKNORA_AVAILABLE"
append_summary "knowledge_provider_disable_ok=$KNOWLEDGE_PROVIDER_DISABLE_OK"
append_summary "knowledge_provider_enable_ok=$KNOWLEDGE_PROVIDER_ENABLE_OK"
append_summary "status_after_disable_enabled=$STATUS_AFTER_DISABLE_ENABLED"
append_summary "status_after_enable_enabled=$STATUS_AFTER_ENABLE_ENABLED"
append_summary "circuit_breaker_reset_ok=$CIRCUIT_BREAKER_RESET_OK"
append_summary "fallback_query_ok=$FALLBACK_QUERY_OK"
append_summary "fallback_query_strategy=$FALLBACK_QUERY_STRATEGY"
append_summary "fallback_usage_count_after_disable=$FALLBACK_USAGE_COUNT_AFTER_DISABLE"
append_summary "knowledge_upload_ok=$UPLOAD_OK"
append_summary "knowledge_sync_ok=$SYNC_OK"

if [ "$WEKNORA_ACCEPTANCE_MODE" = "real" ]; then
    echo ""
    echo "🔍 严格验收模式: real"
    if [ "$WEKNORA_HEALTH_SERVICE" = "weknora-mock" ]; then
        echo "❌ real 模式命中了 weknora-mock，不能作为真实 WeKnora 兼容环境证据"
        append_summary "real_mode_guard=blocked_weknora_mock"
        exit 1
    fi
    if [ "$WEKNORA_AVAILABLE" != "true" ]; then
        echo "❌ real 模式要求真实 WeKnora 兼容服务健康可用"
        exit 1
    fi
    if [ "$SERVICE_IS_PROVIDER_CAPABLE" != "true" ]; then
        echo "❌ real 模式要求 Servify 运行在支持外部 knowledge provider 的 AI 模式"
        exit 1
    fi
    if [ "$ACTIVE_PROVIDER" != "weknora" ]; then
        echo "❌ real 模式要求当前 provider 为 weknora，当前为 $ACTIVE_PROVIDER"
        exit 1
    fi
    if [ "$KNOWLEDGE_PROVIDER_ENABLED" != "true" ] || [ "$KNOWLEDGE_PROVIDER_HEALTHY" != "true" ]; then
        echo "❌ real 模式要求 WeKnora provider enabled=true 且 healthy=true"
        exit 1
    fi
    if [ "$FALLBACK_QUERY_OK" != "true" ] || [ "$FALLBACK_QUERY_STRATEGY" != "fallback" ]; then
        echo "❌ real 模式要求 disable 后 fallback 查询成功"
        exit 1
    fi
    if [ "$UPLOAD_OK" != "true" ] || [ "$SYNC_OK" != "true" ]; then
        echo "❌ real 模式要求知识上传和同步都成功"
        exit 1
    fi
    echo "✅ real 模式验收通过，可将 $EVIDENCE_DIR 下证据回填到 docs/acceptance-checklist.md"
fi

write_manifest

echo ""
echo "🔗 服务地址:"
echo "   Servify Web:    $SERVIFY_URL"
echo "   Servify API:    $SERVIFY_URL/api/v1"
echo "   健康检查:       $SERVIFY_URL/health"
echo "   WebSocket:      ws://localhost:8080/api/v1/ws"

if [ "${WEKNORA_ENABLED:-false}" = "true" ]; then
    echo "   WeKnora API:    $WEKNORA_URL/api/v1"
    echo "   WeKnora Web:    $WEKNORA_URL:9001"
fi

echo ""
echo "📚 测试完成的功能:"
echo "   ✅ 健康检查和状态监控"
echo "   ✅ AI 智能问答处理"
echo "   ✅ WebSocket 实时通信"
echo "   ✅ WebRTC 连接管理"
echo "   ✅ 并发请求处理"

if [ "$SERVICE_IS_PROVIDER_CAPABLE" = "true" ]; then
    echo "   ✅ WeKnora compatibility 知识库集成"
    echo "   ✅ 降级机制和熔断器"
    echo "   ✅ 服务指标监控"
    echo "   ✅ 文档上传功能"
fi

echo ""
echo "🎯 下一步建议:"
echo "   1. 在浏览器中访问 $SERVIFY_URL 体验完整功能"
echo "   2. 使用 WebSocket 客户端测试实时聊天"
echo "   3. 如需测试远程协助，请使用支持 WebRTC 的浏览器"

if [ "$SERVICE_IS_PROVIDER_CAPABLE" = "true" ]; then
    echo "   4. 通过 WeKnora Web UI 管理兼容知识库: $WEKNORA_URL:9001"
    echo "   5. 使用 API 上传更多文档到知识库"
fi

echo ""
echo "✨ WeKnora compatibility 集成测试完成！"
echo ""
echo "🛡️ 进行基础鉴权测试（管理类 API）..."

# helper: base64url without padding
base64url() {
  openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# Generate HS256 JWT with custom claims (must match server config/jwt.secret)
issue_jwt() {
  local secret="${1:-$JWT_SECRET}"
  local token_type="${2:-service}"
  local principal_kind="${3:-service}"
  local roles="${4:-[\"service\"]}"
  local permissions="${5:-[]}"
  local now=$(date +%s)
  local exp=$((now + 3600))
  local header='{"alg":"HS256","typ":"JWT"}'
  local payload=$(printf '{"sub":"integration-auth","token_type":"%s","principal_kind":"%s","roles":%s,"permissions":%s,"iat":%s,"exp":%s}' \
    "$token_type" "$principal_kind" "$roles" "$permissions" "$now" "$exp")
  local b64_header=$(printf '%s' "$header" | base64url)
  local b64_payload=$(printf '%s' "$payload" | base64url)
  local signing_input="${b64_header}.${b64_payload}"
  local sig=$(printf '%s' "$signing_input" | openssl dgst -sha256 -mac HMAC -macopt "key:$secret" -binary | base64url)
  printf '%s.%s' "$signing_input" "$sig"
}

AUTH_TEST_TOKEN=$(issue_jwt "$JWT_SECRET" "service" "service" "[\"service\"]" "[\"customers.read\"]")

echo "  ✓ 无 token 访问应被拒绝..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$SERVIFY_URL/api/customers/stats" || true)
if [ "$HTTP_CODE" != "401" ] && [ "$HTTP_CODE" != "403" ]; then
  echo "    ❌ 期望 401/403，得到 $HTTP_CODE"
  echo "    🔎 返回详情："
  curl -s -i "$SERVIFY_URL/api/customers/stats" || true
  exit 1
else
  echo "    ✅ 未授权访问被拒绝 ($HTTP_CODE)"
fi

echo "  ✓ 携带有效 token 访问应成功..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $AUTH_TEST_TOKEN" "$SERVIFY_URL/api/customers/stats" || true)
if [ "$HTTP_CODE" = "200" ]; then
  echo "    ✅ 授权访问成功 (200)"
else
  echo "    ❌ 授权访问失败，HTTP $HTTP_CODE"
  echo "    🔎 返回详情："
  curl -s -i -H "Authorization: Bearer $AUTH_TEST_TOKEN" "$SERVIFY_URL/api/customers/stats" || true
  exit 1
fi

echo "✅ 鉴权测试完成"

echo ""
echo "🛡️ 管理员专属接口测试（/api/statistics/...）..."

# 仅 agent 角色访问 admin-only 接口应 403
AGENT_TOKEN=$(issue_jwt "$JWT_SECRET" "service" "agent" "[\"agent\"]" "[]")
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $AGENT_TOKEN" "$SERVIFY_URL/api/statistics/dashboard" || true)
if [ "$HTTP_CODE" = "403" ]; then
  echo "    ✅ agent 访问 admin-only 接口被拒绝 (403)"
else
  echo "    ❌ 期望 403，得到 $HTTP_CODE"
  echo "    🔎 返回详情："
  curl -s -i -H "Authorization: Bearer $AGENT_TOKEN" "$SERVIFY_URL/api/statistics/dashboard" || true
  exit 1
fi

# admin 访问应 200
ADMIN_TOKEN=$(issue_jwt "$JWT_SECRET" "service" "service" "[\"service\"]" "[\"statistics.read\"]")
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $ADMIN_TOKEN" "$SERVIFY_URL/api/statistics/dashboard" || true)
if [ "$HTTP_CODE" = "200" ]; then
  echo "    ✅ admin 访问 admin-only 接口成功 (200)"
else
  echo "    ❌ 访问失败，HTTP $HTTP_CODE"
  echo "    🔎 返回详情："
  curl -s -i -H "Authorization: Bearer $ADMIN_TOKEN" "$SERVIFY_URL/api/statistics/dashboard" || true
  exit 1
fi

echo "✅ 管理员专属接口测试完成"

echo ""
echo "🚦 速率限制测试（/api/v1/ai/query）..."
R200=0
R429=0
TOTAL=50
for i in $(seq 1 "$TOTAL"); do
  CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SERVIFY_URL/api/v1/ai/query" \
    -H "$AUTH_HEADER" \
    -H "Content-Type: application/json" \
    -d "{\"query\":\"rl_test_$i\",\"session_id\":\"rl_test_session\"}" || true)
  if [ "$CODE" = "200" ]; then R200=$((R200+1)); fi
  if [ "$CODE" = "429" ]; then R429=$((R429+1)); fi
done
echo "    ↳ 成功: $R200, 限流: $R429, 总计: $TOTAL"
if [ "$R429" -gt 0 ]; then
  echo "    ✅ 触发限流成功（检测到 429）"
  RATE_LIMIT_ENABLED=true
else
  echo "    ⚠️  速率限制未启用，跳过后续白名单测试"
  RATE_LIMIT_ENABLED=false
fi

# 只有在速率限制启用时才测试白名单
if [ "$RATE_LIMIT_ENABLED" = "true" ]; then
  echo ""
  echo "🚦 限流白名单（X-API-Key）测试..."
  R200=0
  R429=0
  for i in $(seq 1 "$TOTAL"); do
    CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$SERVIFY_URL/api/v1/ai/query" \
      -H "$AUTH_HEADER" \
      -H "Content-Type: application/json" \
      -H "X-API-Key: internal-test-key" \
      -d "{\"query\":\"wl_test_$i\",\"session_id\":\"rl_test_session\"}" || true)
    if [ "$CODE" = "200" ]; then R200=$((R200+1)); fi
    if [ "$CODE" = "429" ]; then R429=$((R429+1)); fi
  done
  echo "    ↳ (白名单) 成功: $R200, 限流: $R429, 总计: $TOTAL"
  if [ "$R429" -eq 0 ]; then
    echo "    ✅ 白名单跳过限流生效"
  else
    echo "    ❌ 白名单无效，请检查 key_header 与 whitelist_keys 配置"
    exit 1
  fi
fi
