#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Runtime baseline acceptance 测试开始..."

RUNTIME_ACCEPTANCE_MODE=${RUNTIME_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/runtime-baseline"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"runtime-baseline-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"runtime-baseline-123"}
METRICS_PATH=${METRICS_PATH:-"/metrics"}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
METRICS_OK=false
PLATFORMS_OK=false
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
trap cleanup EXIT

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
  response_file="$(mktemp "${TMPDIR:-/tmp}/runtime-baseline-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/runtime-baseline-status-XXXXXX")"
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
  MANIFEST_MODE="${RUNTIME_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_METRICS_OK="${METRICS_OK:-false}" \
  MANIFEST_PLATFORMS_OK="${PLATFORMS_OK:-false}" \
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "runtime-baseline",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "metrics_ok": os.environ.get("MANIFEST_METRICS_OK", "false"),
        "platforms_ok": os.environ.get("MANIFEST_PLATFORMS_OK", "false"),
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
Runtime baseline acceptance summary
mode=$RUNTIME_ACCEPTANCE_MODE
EOF

if [ "$RUNTIME_ACCEPTANCE_MODE" = "real" ]; then
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

  # 2) 以 sqlite 启动真实服务（直接用第 1 步构建出的 bin/servify，无 go run 中间层）
  if [ -z "$SERVIFY_URL" ]; then
    SERVIFY_PORT=${SERVIFY_PORT:-18091}
    SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
    if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
      echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
      exit 1
    fi
    DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/runtime-baseline-XXXXXX.sqlite")"
    echo "🚀 启动真实服务 (sqlite, bin/servify): $SERVIFY_URL"
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

# 3) GET /ready
append_summary "step=get_ready"
request_json "GET" "$SERVIFY_URL/ready"
save_response "ready" "$RESPONSE_BODY"
append_summary "ready_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"ready":true'; then
  READY_OK=true
  append_summary "ready_ok=true"
else
  append_summary "ready_ok=false"
fi

# 4) GET <metricsPath>
append_summary "step=get_metrics"
request_json "GET" "$SERVIFY_URL$METRICS_PATH"
printf '%s\n' "$RESPONSE_BODY" > "$EVIDENCE_DIR/metrics.txt"
append_summary "metrics_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && grep -qE '^(# (HELP|TYPE)|[a-zA-Z_]+)' "$EVIDENCE_DIR/metrics.txt"; then
  METRICS_OK=true
  append_summary "metrics_ok=true"
else
  append_summary "metrics_ok=false"
fi

# 5) 注册 admin 并请求 /api/v1/messages/platforms
append_summary "step=auth_and_platforms"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Runtime Baseline Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY"
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
ADMIN_TOKEN=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || true)

if [ -n "$ADMIN_TOKEN" ]; then
  request_json "GET" "$SERVIFY_URL/api/v1/messages/platforms" "" "$ADMIN_TOKEN"
  save_response "platforms" "$RESPONSE_BODY"
  append_summary "platforms_status=$RESPONSE_STATUS"
  if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"total_platforms"'; then
    PLATFORMS_OK=true
    append_summary "platforms_ok=true"
  else
    append_summary "platforms_ok=false"
  fi
else
  save_response "platforms" '{}'
  append_summary "platforms_ok=false (no admin token)"
fi

# 6) 拒绝路径：无 token 请求 platforms 必须 401
request_json "GET" "$SERVIFY_URL/api/v1/messages/platforms"
save_response "platforms-unauthorized" "$RESPONSE_BODY"
append_summary "unauthenticated_platforms_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_REJECTED=true
  append_summary "unauthenticated_rejected=true"
else
  append_summary "unauthenticated_rejected=false"
fi

if [ "$READY_OK" != "true" ] || [ "$METRICS_OK" != "true" ] || [ "$PLATFORMS_OK" != "true" ] || [ "$UNAUTH_REJECTED" != "true" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ Runtime baseline acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Runtime baseline acceptance 通过: build / ready / metrics / platforms 全部真实留档"
