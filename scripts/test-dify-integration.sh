#!/bin/bash

# Dify 主路径集成测试脚本
# 用于验证 Servify 外部 knowledge provider 链路中的 Dify 主路径是否正常工作

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Dify primary integration 测试开始..."

SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
DIFY_URL=${DIFY_URL:-"http://localhost:5001/v1"}
DIFY_DATASET_ID=${DIFY_DATASET_ID:-"dataset-1"}
JWT_SECRET=${JWT_SECRET:-"dev-secret-key-change-in-production"}
DIFY_ACCEPTANCE_MODE=${DIFY_ACCEPTANCE_MODE:-"mock"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/dify-acceptance"}

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
  MANIFEST_MODE="${DIFY_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_PROVIDER_URL="${DIFY_URL:-}" \
  MANIFEST_DATASET_ID="${DIFY_DATASET_ID:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_SERVICE_TYPE="${SERVICE_TYPE:-unknown}" \
  MANIFEST_ACTIVE_PROVIDER="${ACTIVE_PROVIDER:-unknown}" \
  MANIFEST_PROVIDER_ENABLED="${KNOWLEDGE_PROVIDER_ENABLED:-unknown}" \
  MANIFEST_PROVIDER_HEALTHY="${KNOWLEDGE_PROVIDER_HEALTHY:-unknown}" \
  MANIFEST_PROVIDER_AVAILABLE="${DIFY_AVAILABLE:-false}" \
  MANIFEST_QUERY_OK="${QUERY_OK:-false}" \
  MANIFEST_QUERY_STRATEGY="${QUERY_STRATEGY:-unknown}" \
  MANIFEST_UPLOAD_OK="${UPLOAD_OK:-false}" \
  MANIFEST_SYNC_OK="${SYNC_OK:-false}" \
  MANIFEST_USAGE_COUNT="${DIFY_USAGE_COUNT:-N/A}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "dify",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "provider_url": os.environ.get("MANIFEST_PROVIDER_URL", ""),
    "dataset_id": os.environ.get("MANIFEST_DATASET_ID", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
        "service_type": os.environ.get("MANIFEST_SERVICE_TYPE", "unknown"),
        "knowledge_provider": os.environ.get("MANIFEST_ACTIVE_PROVIDER", "unknown"),
        "knowledge_provider_enabled": os.environ.get("MANIFEST_PROVIDER_ENABLED", "unknown"),
        "knowledge_provider_healthy": os.environ.get("MANIFEST_PROVIDER_HEALTHY", "unknown"),
    },
    "checks": {
        "provider_available": os.environ.get("MANIFEST_PROVIDER_AVAILABLE", "false"),
        "query_ok": os.environ.get("MANIFEST_QUERY_OK", "false"),
        "query_strategy": os.environ.get("MANIFEST_QUERY_STRATEGY", "unknown"),
        "knowledge_upload_ok": os.environ.get("MANIFEST_UPLOAD_OK", "false"),
        "knowledge_sync_ok": os.environ.get("MANIFEST_SYNC_OK", "false"),
        "dify_usage_count": os.environ.get("MANIFEST_USAGE_COUNT", "N/A"),
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
Dify primary acceptance summary
mode=$DIFY_ACCEPTANCE_MODE
servify_url=$SERVIFY_URL
dify_url=$DIFY_URL
dify_dataset_id=$DIFY_DATASET_ID
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

if [ "$DIFY_ACCEPTANCE_MODE" != "mock" ] && [ "$DIFY_ACCEPTANCE_MODE" != "real" ]; then
  echo "❌ 不支持的 DIFY_ACCEPTANCE_MODE: $DIFY_ACCEPTANCE_MODE"
  exit 1
fi

DIFY_HOST=$(host_from_url "$DIFY_URL")
append_summary "dify_host=$DIFY_HOST"

if [ "$DIFY_ACCEPTANCE_MODE" = "real" ] && is_private_or_local_host "$DIFY_HOST"; then
  echo "❌ real 模式拒绝使用本地或私网 Dify 地址: $DIFY_HOST"
  echo "   请显式指向外部真实 Dify 环境，再重新执行验收。"
  append_summary "real_mode_guard=blocked_private_or_local_host"
  exit 1
fi

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

wait_for "Servify Health" "$SERVIFY_URL/health" 30 2
DIFY_AVAILABLE=false
DIFY_DATASET_BODY=""
if wait_for "Dify Dataset" "$DIFY_URL/datasets/$DIFY_DATASET_ID" 30 2; then
  DIFY_AVAILABLE=true
else
  echo "⚠️ Dify 数据集未就绪"
fi

SERVIFY_HEALTH=$(curl -fsS "$SERVIFY_URL/health")
save_response "servify-health" "$SERVIFY_HEALTH"

if [ "$DIFY_AVAILABLE" = "true" ]; then
  DIFY_DATASET_BODY=$(curl -fsS "$DIFY_URL/datasets/$DIFY_DATASET_ID")
  save_response "dify-dataset" "$DIFY_DATASET_BODY"
fi

AI_STATUS=$(curl -fsS -H "$AUTH_HEADER" "$SERVIFY_URL/api/v1/ai/status")
save_response "ai-status" "$AI_STATUS"
SERVICE_TYPE=$(json_get "$AI_STATUS" '.data.type // "unknown"' || echo "unknown")
ACTIVE_PROVIDER=$(json_get "$AI_STATUS" '.data.knowledge_provider // "unknown"' || echo "unknown")
KNOWLEDGE_PROVIDER_ENABLED=$(json_get "$AI_STATUS" '.data.knowledge_provider_enabled // false' || echo "false")
KNOWLEDGE_PROVIDER_HEALTHY=$(json_get "$AI_STATUS" '.data.knowledge_provider_healthy // "unknown"' || echo "unknown")

if [ "$ACTIVE_PROVIDER" != "dify" ]; then
  echo "❌ 当前激活 provider 不是 dify: $ACTIVE_PROVIDER"
  exit 1
fi

if [ "$DIFY_ACCEPTANCE_MODE" = "real" ]; then
  if [ "$KNOWLEDGE_PROVIDER_ENABLED" != "true" ]; then
    echo "❌ real 模式要求 Dify provider 已启用，当前 knowledge_provider_enabled=$KNOWLEDGE_PROVIDER_ENABLED"
    exit 1
  fi
  if [ "$KNOWLEDGE_PROVIDER_HEALTHY" != "true" ]; then
    echo "❌ real 模式要求 Dify provider 健康，当前 knowledge_provider_healthy=$KNOWLEDGE_PROVIDER_HEALTHY"
    exit 1
  fi
fi

AI_QUERY=$(curl -fsS -X POST "$SERVIFY_URL/api/v1/ai/query" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "请总结 Dify 主路径知识检索状态",
    "session_id": "dify_primary_query"
  }')
save_response "ai-query" "$AI_QUERY"
QUERY_STRATEGY=$(json_get "$AI_QUERY" '.data.strategy // "unknown"' || echo "unknown")
QUERY_OK=false
if echo "$AI_QUERY" | grep -q '"success":true'; then
  QUERY_OK=true
fi

if [ "$DIFY_ACCEPTANCE_MODE" = "real" ]; then
  if [ "$QUERY_OK" != "true" ]; then
    echo "❌ real 模式要求 AI 查询成功"
    exit 1
  fi
  if [ "$QUERY_STRATEGY" = "fallback" ]; then
    echo "❌ real 模式要求查询命中 Dify 主路径，不能只落入 fallback"
    exit 1
  fi
fi

UPLOAD_RESPONSE=$(curl -sS -X POST "$SERVIFY_URL/api/v1/ai/knowledge/upload" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Dify Primary Acceptance Document",
    "content": "This document validates the Dify primary knowledge provider path.",
    "tags": ["dify", "acceptance"]
  }' || echo '{"success":false,"error":"curl failed"}')
save_response "knowledge-upload" "$UPLOAD_RESPONSE"
UPLOAD_OK=false
if echo "$UPLOAD_RESPONSE" | grep -q '"success":true'; then
  UPLOAD_OK=true
fi

SYNC_RESPONSE=$(curl -sS -X POST "$SERVIFY_URL/api/v1/ai/knowledge/sync" \
  -H "$AUTH_HEADER" \
  -H "Content-Type: application/json" \
  -d '{}' || echo '{"success":false,"error":"curl failed"}')
save_response "knowledge-sync" "$SYNC_RESPONSE"
SYNC_OK=false
if echo "$SYNC_RESPONSE" | grep -q '"success":true'; then
  SYNC_OK=true
fi

METRICS_RESPONSE=$(curl -fsS -H "$AUTH_HEADER" "$SERVIFY_URL/api/v1/ai/metrics")
save_response "ai-metrics" "$METRICS_RESPONSE"
DIFY_USAGE_COUNT=$(json_get "$METRICS_RESPONSE" '.data.dify_usage_count // "N/A"' || echo "N/A")
OVERALL_STATUS=$(json_get "$SERVIFY_HEALTH" '.status // "unknown"' || echo "unknown")

append_summary "overall_status=$OVERALL_STATUS"
append_summary "service_type=$SERVICE_TYPE"
append_summary "knowledge_provider_enabled=$KNOWLEDGE_PROVIDER_ENABLED"
append_summary "knowledge_provider=$ACTIVE_PROVIDER"
append_summary "knowledge_provider_healthy=$KNOWLEDGE_PROVIDER_HEALTHY"
append_summary "query_ok=$QUERY_OK"
append_summary "query_strategy=$QUERY_STRATEGY"
append_summary "dify_available=$DIFY_AVAILABLE"
append_summary "dify_usage_count=$DIFY_USAGE_COUNT"
append_summary "knowledge_upload_ok=$UPLOAD_OK"
append_summary "knowledge_sync_ok=$SYNC_OK"

if [ "$DIFY_ACCEPTANCE_MODE" = "real" ]; then
  if [ "$DIFY_AVAILABLE" != "true" ]; then
    echo "❌ real 模式要求 Dify 数据集健康可用"
    exit 1
  fi
  if [ "$ACTIVE_PROVIDER" != "dify" ]; then
    echo "❌ real 模式要求 Servify 当前 provider 为 dify"
    exit 1
  fi
  if [ "$KNOWLEDGE_PROVIDER_ENABLED" != "true" ] || [ "$KNOWLEDGE_PROVIDER_HEALTHY" != "true" ]; then
    echo "❌ real 模式要求 Dify provider enabled=true 且 healthy=true"
    exit 1
  fi
  if [ "$QUERY_OK" != "true" ] || [ "$QUERY_STRATEGY" = "fallback" ]; then
    echo "❌ real 模式要求 AI 查询走 Dify 主路径而非 fallback"
    exit 1
  fi
  if [ "$UPLOAD_OK" != "true" ] || [ "$SYNC_OK" != "true" ]; then
    echo "❌ real 模式要求 Dify 主路径 upload/sync 都成功"
    exit 1
  fi
fi

write_manifest
echo "✅ Dify primary integration 测试完成"
