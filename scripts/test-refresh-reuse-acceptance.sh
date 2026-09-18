#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Refresh token reuse family revocation acceptance 测试开始（P2-5 第三刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/refresh-reuse"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"reuse-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"reuse-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18096}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
OFF_REUSE_REJECTED=false
OFF_SESSION_STILL_ALIVE=false
REVOKE_REUSE_REJECTED=false
REVOKE_FAMILY_LATEST_DEAD=false
REVOKE_RELOGIN_OK=false
AUDIT_REFRESH_REJECTIONS_AUDITED=false
AUDIT_REFRESH_SUCCESS_AUDITED=false
OVERALL_STATUS=failed

SERVER_PID=""
DB_DSN=""
WORK_DIR=""

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

request_capture() {
  local evidence_name=$1
  shift
  curl -sS -i --max-time 5 "$@" > "$EVIDENCE_DIR/$evidence_name.txt" 2>/dev/null || true
  RESPONSE_STATUS="$(head -1 "$EVIDENCE_DIR/$evidence_name.txt" | awk '{print $2}')"
  RESPONSE_RAW="$(cat "$EVIDENCE_DIR/$evidence_name.txt")"
}

body_json_field() {
  # 从最近一次 request_capture 的响应里抽 JSON 字段（简易 grep）。
  printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
print(json.loads(m.group(0)).get('$1','') if m else '')
" 2>/dev/null || true
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

start_server() {
  bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    >> "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
}

stop_server() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
    SERVER_PID=""
  fi
}

write_manifest() {
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_OFF_REUSE_REJECTED="${OFF_REUSE_REJECTED:-false}" \
  MANIFEST_OFF_SESSION_STILL_ALIVE="${OFF_SESSION_STILL_ALIVE:-false}" \
  MANIFEST_REVOKE_REUSE_REJECTED="${REVOKE_REUSE_REJECTED:-false}" \
  MANIFEST_REVOKE_FAMILY_LATEST_DEAD="${REVOKE_FAMILY_LATEST_DEAD:-false}" \
  MANIFEST_REVOKE_RELOGIN_OK="${REVOKE_RELOGIN_OK:-false}" \
  MANIFEST_AUDIT_REFRESH_REJECTIONS="${AUDIT_REFRESH_REJECTIONS_AUDITED:-false}" \
  MANIFEST_AUDIT_REFRESH_SUCCESS="${AUDIT_REFRESH_SUCCESS_AUDITED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "refresh-reuse",
    "mode": "runtime-family-revocation",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "off_reuse_rejected_401": os.environ.get("MANIFEST_OFF_REUSE_REJECTED", "false"),
        "off_session_still_alive": os.environ.get("MANIFEST_OFF_SESSION_STILL_ALIVE", "false"),
        "revoke_reuse_rejected_401": os.environ.get("MANIFEST_REVOKE_REUSE_REJECTED", "false"),
        "revoke_family_latest_token_dead": os.environ.get("MANIFEST_REVOKE_FAMILY_LATEST_DEAD", "false"),
        "revoke_relogin_ok": os.environ.get("MANIFEST_REVOKE_RELOGIN_OK", "false"),
        "audit_refresh_rejections_audited": os.environ.get("MANIFEST_AUDIT_REFRESH_REJECTIONS", "false"),
        "audit_refresh_success_audited": os.environ.get("MANIFEST_AUDIT_REFRESH_SUCCESS", "false"),
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

# 0) 清场。
rm -f "$EVIDENCE_DIR"/*.txt "$EVIDENCE_DIR"/*.json 2>/dev/null || true

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Refresh token reuse family revocation acceptance summary (P2-5 slice 3)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/refresh-reuse-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/refresh-reuse-XXXXXX.sqlite")"

make_config() {
  local dst=$1 policy=$2
  python3 - "$PROJECT_ROOT/config.yml" "$dst" "$policy" <<'PY'
import sys
import yaml

src, dst, policy = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["session_risk"]["refresh_reuse_policy"] = policy
cfg["security"]["rate_limiting"]["paths"] = [
    {
        "enabled": True,
        "prefix": "/api/v1/auth/",
        "requests_per_minute": 120,
        "burst": 60,
    },
]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = "./uploads"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
}

SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"

do_register() {
  local body
  body=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Reuse Admin","role":"admin"}' \
    "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
  request_capture "register" -X POST -H "Content-Type: application/json" -d "$body" "$SERVIFY_URL/api/v1/auth/register"
}

do_login() {
  # $1 = 证据文件名
  request_capture "$1" -X POST -H "Content-Type: application/json" \
    -d '{"username":"'"$ADMIN_USERNAME"'","password":"'"$ADMIN_PASSWORD"'"}' "$SERVIFY_URL/api/v1/auth/login"
  LOGIN_TOKEN="$(body_json_field token)"
  LOGIN_REFRESH="$(body_json_field refresh_token)"
}

do_refresh() {
  # $1 = 证据文件名，$2 = refresh token。仅在 200 成功时更新全局
  # REFRESHED_* 变量——401 拒绝（重放/家族死亡）不能冲掉合法客户端
  # 手里仍持有的 token。
  local evidence_name=$1 token=$2
  request_capture "$evidence_name" -X POST -H "Content-Type: application/json" \
    -d '{"refresh_token":"'"$token"'"}' "$SERVIFY_URL/api/v1/auth/refresh"
  if [ "$RESPONSE_STATUS" = "200" ]; then
    REFRESHED_TOKEN="$(body_json_field token)"
    REFRESHED_REFRESH="$(body_json_field refresh_token)"
  fi
}

# 2) 阶段一：off 档位——旧 token 重放被拒但会话存活（既有行为）。
append_summary "step=phase_off"
echo "🚀 阶段一：refresh_reuse_policy=off"
make_config "$WORK_DIR/config.yml" "off"
start_server
if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

request_capture "ready" "$SERVIFY_URL/ready"
append_summary "ready_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_RAW" | grep -q '"ready":true'; then
  READY_OK=true
fi
append_summary "ready_ok=$READY_OK"

do_register
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi

do_login "off-login"
do_refresh "off-refresh-1" "$LOGIN_REFRESH"
# 重放旧 refresh token（已轮换）。
do_refresh "off-refresh-reuse" "$LOGIN_REFRESH"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/off-refresh-reuse.txt" && OFF_REUSE_REJECTED=true
append_summary "off_reuse_rejected=$OFF_REUSE_REJECTED"
# 会话应仍存活：最新 token 继续刷新成功。
do_refresh "off-refresh-alive" "$REFRESHED_REFRESH"
grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/off-refresh-alive.txt" && OFF_SESSION_STILL_ALIVE=true
append_summary "off_session_still_alive=$OFF_SESSION_STILL_ALIVE"

# 3) 阶段二：revoke_family 档位——重放触发家族吊销，最新 token 一并死亡。
append_summary "step=phase_revoke"
echo "🔄 阶段二：refresh_reuse_policy=revoke_family（重启服务，同库保留审计）"
stop_server
make_config "$WORK_DIR/config.yml" "revoke_family"
start_server
if ! wait_for "Servify Health (phase 2)" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 阶段二服务未就绪" >&2
  exit 1
fi

do_login "revoke-login"
do_refresh "revoke-refresh-1" "$LOGIN_REFRESH"
# 重放旧 refresh token（已轮换）→ 触发家族吊销。
do_refresh "revoke-refresh-reuse" "$LOGIN_REFRESH"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/revoke-refresh-reuse.txt" && REVOKE_REUSE_REJECTED=true
append_summary "revoke_reuse_rejected=$REVOKE_REUSE_REJECTED"
# 家族最新的 token 也应已死亡。
do_refresh "revoke-refresh-latest" "$REFRESHED_REFRESH"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/revoke-refresh-latest.txt" && REVOKE_FAMILY_LATEST_DEAD=true
append_summary "revoke_family_latest_token_dead=$REVOKE_FAMILY_LATEST_DEAD"
# 重新登录应正常（新会话可建）。
do_login "revoke-relogin"
grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/revoke-relogin.txt" && REVOKE_RELOGIN_OK=true
append_summary "revoke_relogin_ok=$REVOKE_RELOGIN_OK"
ADMIN_TOKEN="$LOGIN_TOKEN"

# 4) 审计对账：auth.refresh 的 401 重放行与 200 成功行均留痕。
append_summary "step=audit_reconciliation"
if [ -n "$ADMIN_TOKEN" ]; then
  request_capture "audit-refresh" -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$SERVIFY_URL/api/audit/logs?action=auth.refresh&page_size=50"
  if grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/audit-refresh.txt"; then
    EVAL=$(cat "$EVIDENCE_DIR/audit-refresh.txt" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
payload = json.loads(m.group(0)) if m else {}
items = payload.get('data') or payload.get('items') or []
rejected = [i for i in items if i.get('status_code') == 401 and not i.get('success')]
ok = [i for i in items if i.get('status_code') == 200 and i.get('success')]
print('rejected=%d ok=%d' % (len(rejected), len(ok)))
")
    append_summary "audit_refresh_reconciliation=$EVAL"
    eval "$EVAL"
    [ "${rejected:-0}" -ge 3 ] && AUDIT_REFRESH_REJECTIONS_AUDITED=true
    [ "${ok:-0}" -ge 1 ] && AUDIT_REFRESH_SUCCESS_AUDITED=true
  fi
fi
append_summary "audit_refresh_rejections_audited=$AUDIT_REFRESH_REJECTIONS_AUDITED"
append_summary "audit_refresh_success_audited=$AUDIT_REFRESH_SUCCESS_AUDITED"

# 5) 对账
FAILED=false
for check in READY_OK OFF_REUSE_REJECTED OFF_SESSION_STILL_ALIVE REVOKE_REUSE_REJECTED \
  REVOKE_FAMILY_LATEST_DEAD REVOKE_RELOGIN_OK AUDIT_REFRESH_REJECTIONS_AUDITED \
  AUDIT_REFRESH_SUCCESS_AUDITED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Refresh reuse acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Refresh reuse acceptance 通过: off 会话存活 / revoke_family 家族吊销 / 审计留痕 全部真实留档"
