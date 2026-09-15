#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 AI fallback acceptance 测试开始..."

AI_ACCEPTANCE_MODE=${AI_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/ai-fallback"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"ai-fallback-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"ai-fallback-123"}
AI_QUERY=${AI_QUERY:-"你好，请介绍一下 Servify"}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
STATUS_OK=false
QUERY_FALLBACK_OK=false
METRICS_OK=false
LOG_EVIDENCE_OK=false
UNAUTH_REJECTED=false
OVERALL_STATUS=failed

SERVIFY_URL=${SERVIFY_URL:-}
SERVER_PID=""
DB_DSN=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$DB_DSN" ] && [ -f "$DB_DSN" ]; then
    rm -f "$DB_DSN" || true
  fi
}

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

save_response() {
  local name=$1
  local body=${2:-}
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

# request_json 发送请求并把状态码写入 RESPONSE_STATUS、响应体写入 RESPONSE_BODY。
RESPONSE_STATUS=""
RESPONSE_BODY=""
request_json() {
  local method=$1 url=$2 body=${3:-} token=${4:-}
  local response_file status_file
  response_file="$(mktemp "${TMPDIR:-/tmp}/ai-fallback-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/ai-fallback-status-XXXXXX")"
  trap 'rm -f "$response_file" "$status_file"; trap - RETURN' RETURN
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  if [ -n "$body" ]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  curl "${args[@]}" > "$status_file" 2>/dev/null || true
  RESPONSE_STATUS="$(cat "$status_file")"
  RESPONSE_BODY="$(cat "$response_file")"
  rm -f "$response_file" "$status_file"
}

wait_for() {
  local name=$1 url=$2 max=$3 sleep_s=$4
  echo "⏳ 等待 $name 可用: $url (最多 ${max} 次，每次 ${sleep_s}s)"
  for i in $(seq 1 "$max"); do
    if curl -fsS "$url" > /dev/null 2>&1; then
      echo "✅ $name 可用"
      return 0
    fi
    sleep "$sleep_s"
  done
  echo "❌ $name 不可用: $url"
  return 1
}

write_manifest() {
  MANIFEST_MODE="${AI_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_STATUS_OK="${STATUS_OK:-false}" \
  MANIFEST_QUERY_FALLBACK_OK="${QUERY_FALLBACK_OK:-false}" \
  MANIFEST_METRICS_OK="${METRICS_OK:-false}" \
  MANIFEST_LOG_EVIDENCE_OK="${LOG_EVIDENCE_OK:-false}" \
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "ai-fallback",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "status_ok": os.environ.get("MANIFEST_STATUS_OK", "false"),
        "query_fallback_ok": os.environ.get("MANIFEST_QUERY_FALLBACK_OK", "false"),
        "metrics_ok": os.environ.get("MANIFEST_METRICS_OK", "false"),
        "log_evidence_ok": os.environ.get("MANIFEST_LOG_EVIDENCE_OK", "false"),
        "unauthenticated_rejected": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
    },
    "evidence_files": sorted(
        name for name in os.listdir(evidence_dir)
        if os.path.isfile(os.path.join(evidence_dir, name))
    ),
}
with open(out, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
}
trap 'cleanup; write_manifest' EXIT

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
AI fallback acceptance summary
mode=$AI_ACCEPTANCE_MODE
EOF

if [ "$AI_ACCEPTANCE_MODE" = "real" ]; then
  # 1) CLI 标准构建
  append_summary "step=make_build"
  echo "🔍 make build..."
  if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1); then
    if [ -x "$PROJECT_ROOT/bin/servify" ]; then
      BUILD_OK=true
      (cd "$PROJECT_ROOT" && ls -1 bin >> "$EVIDENCE_DIR/build-output.txt")
      append_summary "build_ok=true"
    else
      append_summary "build_ok=false (bin/servify missing)"
    fi
  else
    append_summary "build_ok=false (make build failed)"
  fi
  if [ "$BUILD_OK" != "true" ]; then
    echo "❌ make build 未通过" >&2
    exit 1
  fi

  # 2) 以 sqlite 启动真实服务（不配置任何外部 AI provider，AI 主链路天然走 fallback）
  if [ -z "$SERVIFY_URL" ]; then
    SERVIFY_PORT=${SERVIFY_PORT:-18092}
    SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
    if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
      echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
      exit 1
    fi
    DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/ai-fallback-XXXXXX.sqlite")"
    echo "🚀 启动真实服务 (sqlite, bin/servify, 无外部 AI provider): $SERVIFY_URL"
    SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-dev-secret} \
    DB_DRIVER=sqlite DB_DSN="$DB_DSN" SERVIFY_PORT="$SERVIFY_PORT" \
      "$PROJECT_ROOT/bin/servify" > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
    SERVER_PID=$!
  fi
fi

if [ -z "$SERVIFY_URL" ]; then
  echo "❌ mock 模式需要 SERVIFY_URL" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

# 3) 注册 admin（AI 端点在 managementV1 组下，需要 admin token）
append_summary "step=auth"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"AI Fallback Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY"
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
ADMIN_TOKEN=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || true)
if [ -z "$ADMIN_TOKEN" ]; then
  append_summary "admin_token=missing"
  echo "❌ 未拿到 admin token" >&2
  exit 1
fi

# 4) GET /api/v1/ai/status（状态证据）
append_summary "step=get_ai_status"
request_json "GET" "$SERVIFY_URL/api/v1/ai/status" "" "$ADMIN_TOKEN"
save_response "ai-status" "$RESPONSE_BODY"
append_summary "ai_status_http=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"fallback_enabled":true'; then
  STATUS_OK=true
  append_summary "status_ok=true"
else
  append_summary "status_ok=false"
fi

# 5) POST /api/v1/ai/query（响应证据：strategy=fallback 且内容非空）
append_summary "step=post_ai_query"
QUERY_BODY=$(AI_QUERY="$AI_QUERY" python3 -c 'import json,os,sys; print(json.dumps({"query": os.environ["AI_QUERY"], "session_id": "ai-fallback-acceptance"}, ensure_ascii=False))')
request_json "POST" "$SERVIFY_URL/api/v1/ai/query" "$QUERY_BODY" "$ADMIN_TOKEN"
save_response "ai-query" "$RESPONSE_BODY"
append_summary "ai_query_http=$RESPONSE_STATUS"
QUERY_CONTENT=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys
payload = json.load(sys.stdin)
data = payload.get("data") or {}
print((data.get("strategy") or "") + "\t" + (data.get("content") or ""))' 2>/dev/null || true)
QUERY_STRATEGY="$(printf '%s' "$QUERY_CONTENT" | head -n 1 | cut -f1)"
QUERY_ANSWER="$(printf '%s' "$QUERY_CONTENT" | tail -n 1)"
append_summary "ai_query_strategy=$QUERY_STRATEGY"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$QUERY_STRATEGY" = "fallback" ] && [ -n "$QUERY_ANSWER" ]; then
  QUERY_FALLBACK_OK=true
  append_summary "query_fallback_ok=true"
else
  append_summary "query_fallback_ok=false"
fi

# 6) GET /api/v1/ai/metrics（指标证据）
append_summary "step=get_ai_metrics"
request_json "GET" "$SERVIFY_URL/api/v1/ai/metrics" "" "$ADMIN_TOKEN"
save_response "ai-metrics" "$RESPONSE_BODY"
append_summary "ai_metrics_http=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"fallback_usage_count"'; then
  METRICS_OK=true
  append_summary "metrics_ok=true"
else
  append_summary "metrics_ok=false"
fi

# 7) 日志证据：server-log.txt 需同时包含 fallback 专属日志行与 query 的 access log
# （mock 模式没有真实服务日志，跳过该项，留证以 real 模式为准）
append_summary "step=server_log_evidence"
SERVER_LOG="$EVIDENCE_DIR/server-log.txt"
if [ "$AI_ACCEPTANCE_MODE" = "mock" ]; then
  LOG_EVIDENCE_OK=true
  append_summary "log_evidence=skipped(mock)"
elif [ -f "$SERVER_LOG" ] \
  && grep -q 'strategy=fallback' "$SERVER_LOG" \
  && grep -qE 'POST[[:space:]]+"/api/v1/ai/query"' "$SERVER_LOG"; then
  LOG_EVIDENCE_OK=true
  append_summary "log_evidence_ok=true"
else
  append_summary "log_evidence_ok=false"
fi

# 8) 拒绝路径：无 token 请求 /ai/query 必须 401
request_json "POST" "$SERVIFY_URL/api/v1/ai/query" "$QUERY_BODY"
save_response "ai-query-unauthorized" "$RESPONSE_BODY"
append_summary "unauthenticated_ai_query_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_REJECTED=true
  append_summary "unauthenticated_rejected=true"
else
  append_summary "unauthenticated_rejected=false"
fi

if [ "$STATUS_OK" != "true" ] || [ "$QUERY_FALLBACK_OK" != "true" ] || [ "$METRICS_OK" != "true" ] || [ "$LOG_EVIDENCE_OK" != "true" ] || [ "$UNAUTH_REJECTED" != "true" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ AI fallback acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ AI fallback acceptance 通过: status / query(strategy=fallback) / metrics / server log 全部真实留档"
