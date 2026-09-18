#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Public surface acceptance 测试开始（P2-5 第一刀：运行时安全基线）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/public-surface"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"public-surface-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"public-surface-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18093}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
SECURITY_HEADERS_OK=false
CORS_ECHO_OK=false
CORS_REJECT_OK=false
CORS_PREFLIGHT_OK=false
BODY_LIMIT_413_OK=false
RATE_LIMIT_429_OK=false
UPLOADS_FILE_OK=false
UPLOADS_DIR_404_OK=false
WS_ORIGIN_REJECTED_OK=false
WS_ORIGIN_ADMITTED_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
DB_DSN=""
WORK_DIR=""
ALLOWED_ORIGIN="https://admin.example.com"
SECOND_ORIGIN="https://console.example.com"
EVIL_ORIGIN="https://evil.example.net"

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$DB_DSN" ] && [ -f "$DB_DSN" ]; then
    rm -f "$DB_DSN" || true
  fi
  if [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR" || true
  fi
}

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

# request sends a request and captures status into RESPONSE_STATUS, full
# response (headers + body) into RESPONSE_RAW.
RESPONSE_STATUS=""
RESPONSE_RAW=""
request() {
  local out_file status_file
  shift
  out_file="$(mktemp "${TMPDIR:-/tmp}/public-surface-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/public-surface-status-XXXXXX")"
  trap 'rm -f "$out_file" "$status_file"; trap - RETURN' RETURN
  curl -sS -o "$out_file" -w '%{http_code}' "$@" > "$status_file" 2>/dev/null || true
  RESPONSE_STATUS="$(cat "$status_file")"
  RESPONSE_RAW="$(cat "$out_file")"
  rm -f "$out_file" "$status_file"
}

# request_capture 通过 -i 拿完整响应头+体，落盘到证据文件。
request_capture() {
  local evidence_name=$1
  shift
  curl -sS -i --max-time 5 "$@" > "$EVIDENCE_DIR/$evidence_name.txt" 2>/dev/null || true
  RESPONSE_STATUS="$(head -1 "$EVIDENCE_DIR/$evidence_name.txt" | awk '{print $2}')"
  RESPONSE_RAW="$(cat "$EVIDENCE_DIR/$evidence_name.txt")"
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
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_SECURITY_HEADERS_OK="${SECURITY_HEADERS_OK:-false}" \
  MANIFEST_CORS_ECHO_OK="${CORS_ECHO_OK:-false}" \
  MANIFEST_CORS_REJECT_OK="${CORS_REJECT_OK:-false}" \
  MANIFEST_CORS_PREFLIGHT_OK="${CORS_PREFLIGHT_OK:-false}" \
  MANIFEST_BODY_LIMIT_413_OK="${BODY_LIMIT_413_OK:-false}" \
  MANIFEST_RATE_LIMIT_429_OK="${RATE_LIMIT_429_OK:-false}" \
  MANIFEST_UPLOADS_FILE_OK="${UPLOADS_FILE_OK:-false}" \
  MANIFEST_UPLOADS_DIR_404_OK="${UPLOADS_DIR_404_OK:-false}" \
  MANIFEST_WS_ORIGIN_REJECTED_OK="${WS_ORIGIN_REJECTED_OK:-false}" \
  MANIFEST_WS_ORIGIN_ADMITTED_OK="${WS_ORIGIN_ADMITTED_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "public-surface",
    "mode": "runtime-security-baseline",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "security_headers_ok": os.environ.get("MANIFEST_SECURITY_HEADERS_OK", "false"),
        "cors_allowlisted_origin_echoed": os.environ.get("MANIFEST_CORS_ECHO_OK", "false"),
        "cors_foreign_origin_rejected": os.environ.get("MANIFEST_CORS_REJECT_OK", "false"),
        "cors_preflight_vary_origin": os.environ.get("MANIFEST_CORS_PREFLIGHT_OK", "false"),
        "oversized_body_rejected_413": os.environ.get("MANIFEST_BODY_LIMIT_413_OK", "false"),
        "rate_limit_enforced_429": os.environ.get("MANIFEST_RATE_LIMIT_429_OK", "false"),
        "uploads_file_served": os.environ.get("MANIFEST_UPLOADS_FILE_OK", "false"),
        "uploads_directory_404": os.environ.get("MANIFEST_UPLOADS_DIR_404_OK", "false"),
        "websocket_foreign_origin_rejected": os.environ.get("MANIFEST_WS_ORIGIN_REJECTED_OK", "false"),
        "websocket_allowlisted_origin_admitted": os.environ.get("MANIFEST_WS_ORIGIN_ADMITTED_OK", "false"),
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

# 0) 清场：旧留档不得混入本轮证据。
rm -f "$EVIDENCE_DIR"/*.txt "$EVIDENCE_DIR"/*.json 2>/dev/null || true

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Public surface acceptance summary (P2-5 slice 1)
EOF

# 1) CLI 标准构建
append_summary "step=make_build"
echo "🔍 make build..."
if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1); then
  if [ -x "$PROJECT_ROOT/bin/servify" ]; then
    BUILD_OK=true
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

# 2) 生成定制配置（在临时工作目录，server 按 cwd 搜索 config.yml）。
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/public-surface-work-XXXXXX")"
UPLOADS_DIR="$WORK_DIR/uploads"
mkdir -p "$UPLOADS_DIR"
python3 - "$PROJECT_ROOT/config.yml" "$WORK_DIR/config.yml" "$UPLOADS_DIR" <<'PY'
import sys
import yaml

src, dst, uploads_dir = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["cors"]["enabled"] = True
cfg["security"]["cors"]["allowed_origins"] = [
    "https://admin.example.com",
    "https://console.example.com",
]
cfg["security"]["headers"] = {
    "enabled": True,
    "hsts_enabled": False,
    "hsts_max_age_seconds": 31536000,
    "frame_options": "DENY",
    "referrer_policy": "strict-origin-when-cross-origin",
    "content_security_policy": "default-src 'self'; frame-ancestors 'none'",
}
cfg["security"]["max_body_bytes"] = 4096
cfg["security"]["websocket_allowed_origins"] = ["https://admin.example.com"]
cfg["security"]["rate_limiting"]["enabled"] = True
cfg["security"]["rate_limiting"]["whitelist_ips"] = []
cfg["security"]["rate_limiting"]["whitelist_keys"] = []
cfg["security"]["rate_limiting"]["paths"] = [
    {
        "enabled": True,
        "prefix": "/api/v1/auth/",
        "requests_per_minute": 3,
        "burst": 3,
    },
]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = uploads_dir

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY

# 本地上传探测文件（公开面应能读到文件本身，而非目录列表）。
printf 'public-surface-probe' > "$UPLOADS_DIR/probe.txt"
mkdir -p "$UPLOADS_DIR/nested"
printf 'nested-probe' > "$UPLOADS_DIR/nested/inner.txt"

# 3) 以临时工作目录为 cwd 启动真实服务（sqlite）。
if curl -fsS "http://127.0.0.1:${SERVIFY_PORT}/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
  exit 1
fi
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/public-surface-XXXXXX.sqlite")"
SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
echo "🚀 启动真实服务 (sqlite, 定制 security 配置): $SERVIFY_URL"
append_summary "servify_url=$SERVIFY_URL"
# 通过 bash -c 'cd … && exec …' 启动：exec 让 $! 直接是 servify 进程本身，
# cleanup 的 kill 才能真正终止服务（子 shell 包裹会泄漏进程、占住端口）。
bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
  _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
  > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
SERVER_PID=$!

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

# 4) /ready 留档
append_summary "step=ready"
request_capture "ready" "$SERVIFY_URL/ready"
append_summary "ready_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_RAW" | grep -q '"ready":true'; then
  READY_OK=true
  append_summary "ready_ok=true"
else
  append_summary "ready_ok=false"
fi

# 5) 安全响应头：任何响应（含 /health）都注入；同时验证未启用 HSTS
#    （TLS 由反代终结的部署形态）。
append_summary "step=security_headers"
request_capture "headers-health" "$SERVIFY_URL/health"
HEADERS_FILE="$EVIDENCE_DIR/headers-health.txt"
grep -qi '^X-Content-Type-Options: nosniff' "$HEADERS_FILE" &&
  grep -qi '^X-Frame-Options: DENY' "$HEADERS_FILE" &&
  grep -qi '^Referrer-Policy: strict-origin-when-cross-origin' "$HEADERS_FILE" &&
  grep -qi "^Content-Security-Policy: default-src 'self'" "$HEADERS_FILE" &&
  ! grep -qi '^Strict-Transport-Security:' "$HEADERS_FILE" &&
  SECURITY_HEADERS_OK=true
append_summary "security_headers_ok=$SECURITY_HEADERS_OK"

# 6) CORS 多 origin：白名单内按请求回显 Origin + Vary: Origin；白名单外
#    不带 ACAO；预检（OPTIONS 无 Origin）204 且不带 ACAO 但带 Vary。
append_summary "step=cors"
request_capture "cors-echo" -H "Origin: $ALLOWED_ORIGIN" "$SERVIFY_URL/health"
grep -qi "^Access-Control-Allow-Origin: $ALLOWED_ORIGIN" "$EVIDENCE_DIR/cors-echo.txt" &&
  grep -qi '^Vary: Origin' "$EVIDENCE_DIR/cors-echo.txt" &&
  CORS_ECHO_OK=true
append_summary "cors_echo_ok=$CORS_ECHO_OK"

request_capture "cors-reject" -H "Origin: $EVIL_ORIGIN" "$SERVIFY_URL/health"
if ! grep -qi '^Access-Control-Allow-Origin:' "$EVIDENCE_DIR/cors-reject.txt"; then
  CORS_REJECT_OK=true
fi
append_summary "cors_reject_ok=$CORS_REJECT_OK"

request_capture "cors-preflight" -X OPTIONS "$SERVIFY_URL/health"
grep -q 'HTTP/1.1 204' "$EVIDENCE_DIR/cors-preflight.txt" &&
  grep -qi '^Vary: Origin' "$EVIDENCE_DIR/cors-preflight.txt" &&
  ! grep -qi '^Access-Control-Allow-Origin:' "$EVIDENCE_DIR/cors-preflight.txt" &&
  CORS_PREFLIGHT_OK=true
append_summary "cors_preflight_ok=$CORS_PREFLIGHT_OK"

# 7) 全局 body 上限：Content-Length 超限的公开请求 413，且 413 响应也带
#    安全头（验证安全头挂载在 body 上限之前）。
append_summary "step=body_limit"
BIG_BODY="$(python3 -c 'print("x"*8192)')"
request_capture "body-413" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"$BIG_BODY\"}" "$SERVIFY_URL/api/v1/auth/login"
grep -q 'HTTP/1.1 413' "$EVIDENCE_DIR/body-413.txt" &&
  grep -qi '^X-Frame-Options: DENY' "$EVIDENCE_DIR/body-413.txt" &&
  BODY_LIMIT_413_OK=true
append_summary "body_limit_413_ok=$BODY_LIMIT_413_OK"

# 8) 限流：auth 前缀 3rpm/burst=3，连续打满后必须出现 429，且 429 带安全头。
append_summary "step=rate_limit"
for i in 1 2 3 4 5 6 7 8; do
  request_capture "rate-limit-$i" -X POST -H "Content-Type: application/json" \
    -d '{"username":"nobody","password":"wrong"}' "$SERVIFY_URL/api/v1/auth/refresh" || true
done
grep -q 'HTTP/1.1 429' "$EVIDENCE_DIR/rate-limit-8.txt" &&
  grep -qi '^X-Frame-Options: DENY' "$EVIDENCE_DIR/rate-limit-8.txt" &&
  RATE_LIMIT_429_OK=true
append_summary "rate_limit_429_ok=$RATE_LIMIT_429_OK"

# 9) 本地 uploads：具体文件可读，目录与缺失 404（不暴露存储拓扑）。
append_summary "step=uploads"
request_capture "uploads-file" "$SERVIFY_URL/uploads/probe.txt"
grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/uploads-file.txt" &&
  grep -q 'public-surface-probe' "$EVIDENCE_DIR/uploads-file.txt" &&
  UPLOADS_FILE_OK=true
append_summary "uploads_file_ok=$UPLOADS_FILE_OK"

request_capture "uploads-dir" "$SERVIFY_URL/uploads/"
grep -q 'HTTP/1.1 404' "$EVIDENCE_DIR/uploads-dir.txt" &&
  UPLOADS_DIR_404_OK=true
append_summary "uploads_dir_404_ok=$UPLOADS_DIR_404_OK"

# 10) WS Origin 白名单：白名单外握手被拒（非 101）；白名单内完成升级（101）。
append_summary "step=ws_origin"
WS_URL="$SERVIFY_URL/api/v1/ws?session_id=public-surface-probe"
curl -sS -i --max-time 2 \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Origin: $EVIL_ORIGIN" "$WS_URL" > "$EVIDENCE_DIR/ws-reject.txt" 2>/dev/null || true
if grep -q 'HTTP/1.1 403' "$EVIDENCE_DIR/ws-reject.txt"; then
  WS_ORIGIN_REJECTED_OK=true
fi
append_summary "ws_origin_rejected_ok=$WS_ORIGIN_REJECTED_OK"

curl -sS -i --max-time 2 \
  -H "Connection: Upgrade" -H "Upgrade: websocket" \
  -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==" \
  -H "Origin: $ALLOWED_ORIGIN" "$WS_URL" > "$EVIDENCE_DIR/ws-admit.txt" 2>/dev/null || true
if grep -q 'HTTP/1.1 101' "$EVIDENCE_DIR/ws-admit.txt"; then
  WS_ORIGIN_ADMITTED_OK=true
fi
append_summary "ws_origin_admitted_ok=$WS_ORIGIN_ADMITTED_OK"

# 11) 对账
FAILED=false
for check in READY_OK SECURITY_HEADERS_OK CORS_ECHO_OK CORS_REJECT_OK CORS_PREFLIGHT_OK \
  BODY_LIMIT_413_OK RATE_LIMIT_429_OK UPLOADS_FILE_OK UPLOADS_DIR_404_OK \
  WS_ORIGIN_REJECTED_OK WS_ORIGIN_ADMITTED_OK; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Public surface acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Public surface acceptance 通过: 安全头/CORS/body 上限/限流/uploads/WS 白名单 全部真实留档"
