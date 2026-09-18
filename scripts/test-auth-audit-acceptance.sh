#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Auth audit & login risk enforcement acceptance 测试开始（P2-5 第二刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/auth-audit"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"auth-audit-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"auth-audit-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18094}
STUB_PORT=${STUB_PORT:-18095}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
LOGIN_BLOCKED_BY_RISK=false
LOGIN_BLOCKED_AUDITED=false
BAD_CREDENTIALS_REJECTED=false
BAD_CREDENTIALS_AUDITED=false
CLEAN_LOGIN_OK=false
CLEAN_LOGIN_AUDITED=false
CREDENTIALS_REDACTED_IN_AUDIT=false
REGISTER_AUDITED=false
OVERALL_STATUS=failed

SERVER_PID=""
STUB_PID=""
DB_DSN=""
WORK_DIR=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$STUB_PID" ] && kill -0 "$STUB_PID" 2>/dev/null; then
    kill "$STUB_PID" 2>/dev/null || true
    wait "$STUB_PID" 2>/dev/null || true
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
  local config_file=$1
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
  MANIFEST_LOGIN_BLOCKED_BY_RISK="${LOGIN_BLOCKED_BY_RISK:-false}" \
  MANIFEST_LOGIN_BLOCKED_AUDITED="${LOGIN_BLOCKED_AUDITED:-false}" \
  MANIFEST_BAD_CREDENTIALS_REJECTED="${BAD_CREDENTIALS_REJECTED:-false}" \
  MANIFEST_BAD_CREDENTIALS_AUDITED="${BAD_CREDENTIALS_AUDITED:-false}" \
  MANIFEST_CLEAN_LOGIN_OK="${CLEAN_LOGIN_OK:-false}" \
  MANIFEST_CLEAN_LOGIN_AUDITED="${CLEAN_LOGIN_AUDITED:-false}" \
  MANIFEST_CREDENTIALS_REDACTED_IN_AUDIT="${CREDENTIALS_REDACTED_IN_AUDIT:-false}" \
  MANIFEST_REGISTER_AUDITED="${REGISTER_AUDITED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "auth-audit",
    "mode": "runtime-risk-enforcement",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "risky_login_blocked_403": os.environ.get("MANIFEST_LOGIN_BLOCKED_BY_RISK", "false"),
        "risky_login_audited": os.environ.get("MANIFEST_LOGIN_BLOCKED_AUDITED", "false"),
        "bad_credentials_rejected_401": os.environ.get("MANIFEST_BAD_CREDENTIALS_REJECTED", "false"),
        "bad_credentials_audited": os.environ.get("MANIFEST_BAD_CREDENTIALS_AUDITED", "false"),
        "clean_login_ok": os.environ.get("MANIFEST_CLEAN_LOGIN_OK", "false"),
        "clean_login_audited": os.environ.get("MANIFEST_CLEAN_LOGIN_AUDITED", "false"),
        "credentials_redacted_in_audit": os.environ.get("MANIFEST_CREDENTIALS_REDACTED_IN_AUDIT", "false"),
        "register_audited": os.environ.get("MANIFEST_REGISTER_AUDITED", "false"),
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
Auth audit & login risk enforcement acceptance summary (P2-5 slice 2)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/auth-audit-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/auth-audit-XXXXXX.sqlite")"

# 2) stub IP 情报源：任何 IP 都标注为 hosting（数据中心网络）——构成
#    高风险判定信号，驱动 block 档位的真实拦截。
python3 - "$STUB_PORT" > /dev/null 2>&1 <<'PY' &
import http.server
import json
import sys

port = int(sys.argv[1])

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = json.dumps({"network_label": "hosting", "location_label": "stub-datacenter"}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass

http.server.HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
STUB_PID=$!
STUB_URL="http://127.0.0.1:${STUB_PORT}"
if ! wait_for "stub IP intelligence" "$STUB_URL/lookup/127.0.0.1" 15 1; then
  echo "❌ stub IP 情报源未就绪" >&2
  exit 1
fi
append_summary "stub_url=$STUB_URL"

make_config() {
  local dst=$1 enforcement=$2
  python3 - "$PROJECT_ROOT/config.yml" "$dst" "$STUB_URL/lookup/{ip}" "$enforcement" <<'PY'
import sys
import yaml

src, dst, stub_base, enforcement = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["session_risk"]["login_enforcement"] = enforcement
cfg["security"]["session_ip_intelligence"] = {
    "enabled": True,
    "base_url": stub_base,
    "api_key": "",
    "auth_header": "Authorization",
    "timeout_ms": 1500,
}
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

# 3) 阶段一：block 档位——高风险来源登录一律 403。
append_summary "step=phase_block"
echo "🚀 阶段一：login_enforcement=block（stub 情报源标注 hosting）"
make_config "$WORK_DIR/config.yml" "block"
if curl -fsS "http://127.0.0.1:${SERVIFY_PORT}/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
append_summary "servify_url=$SERVIFY_URL"
start_server "$WORK_DIR/config.yml"

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

# 注册 admin（审计应记 auth.register；register 不走登录风险执行）。
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Auth Audit Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_capture "register" -X POST -H "Content-Type: application/json" -d "$REGISTER_BODY" "$SERVIFY_URL/api/v1/auth/register"
append_summary "register_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi

# block：正确凭据也因高风险来源被拒。
request_capture "login-blocked" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}" "$SERVIFY_URL/api/v1/auth/login"
grep -q 'HTTP/1.1 403' "$EVIDENCE_DIR/login-blocked.txt" &&
  grep -q '风险策略拦截' "$EVIDENCE_DIR/login-blocked.txt" &&
  LOGIN_BLOCKED_BY_RISK=true
append_summary "login_blocked_by_risk=$LOGIN_BLOCKED_BY_RISK"

# 错误凭据 401（与风险拦截区分开的普通失败）。
request_capture "login-bad-credentials" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","password":"definitely-wrong"}' "$SERVIFY_URL/api/v1/auth/login"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/login-bad-credentials.txt" &&
  BAD_CREDENTIALS_REJECTED=true
append_summary "bad_credentials_rejected=$BAD_CREDENTIALS_REJECTED"

# 4) 阶段二：off 档位——同一 DB 重启，正常登录放行（DB 保留阶段一审计行）。
append_summary "step=phase_off"
echo "🔄 阶段二：login_enforcement=off（重启服务，同库保留审计）"
stop_server
make_config "$WORK_DIR/config.yml" "off"
start_server "$WORK_DIR/config.yml"
if ! wait_for "Servify Health (phase 2)" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 阶段二服务未就绪" >&2
  exit 1
fi

request_capture "login-clean" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}" "$SERVIFY_URL/api/v1/auth/login"
grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/login-clean.txt" &&
  grep -q '"token"' "$EVIDENCE_DIR/login-clean.txt" &&
  CLEAN_LOGIN_OK=true
append_summary "clean_login_ok=$CLEAN_LOGIN_OK"
ADMIN_TOKEN="$(body_json_field token)"

# 5) 审计对账：admin token 查 auth.login / auth.register 审计行。
append_summary "step=audit_reconciliation"
if [ -n "$ADMIN_TOKEN" ]; then
  # 审计查询挂在管理面 /api 组（非 /api/v1）。
  request_capture "audit-logins" -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$SERVIFY_URL/api/audit/logs?action=auth.login&page_size=50"
  AUDIT_RAW="$(cat "$EVIDENCE_DIR/audit-logins.txt")"

  if [ "$(grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/audit-logins.txt" && echo yes)" = "yes" ]; then
    EVAL=$(printf '%s' "$AUDIT_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
payload = json.loads(m.group(0)) if m else {}
items = payload.get('data') or payload.get('items') or []
blocked = [i for i in items if i.get('status_code') == 403 and not i.get('success')]
bad = [i for i in items if i.get('status_code') == 401 and not i.get('success')]
clean = [i for i in items if i.get('status_code') == 200 and i.get('success')]
redacted = any('[REDACTED]' in (i.get('request_json') or '') for i in items)
leaked = any('definitely-wrong' in (i.get('request_json') or '') or '$ADMIN_PASSWORD' in (i.get('request_json') or '') for i in items)
print('blocked=%d bad=%d clean=%d redacted=%s leaked=%s' % (len(blocked), len(bad), len(clean), redacted, leaked))
")
    append_summary "audit_login_reconciliation=$EVAL"
    eval "$EVAL"
    [ "${blocked:-0}" -ge 1 ] && LOGIN_BLOCKED_AUDITED=true
    [ "${bad:-0}" -ge 1 ] && BAD_CREDENTIALS_AUDITED=true
    [ "${clean:-0}" -ge 1 ] && CLEAN_LOGIN_AUDITED=true
    { [ "$redacted" = "True" ] && [ "$leaked" = "False" ]; } && CREDENTIALS_REDACTED_IN_AUDIT=true
  fi

  request_capture "audit-registers" -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$SERVIFY_URL/api/audit/logs?action=auth.register&page_size=10"
  if grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/audit-registers.txt" &&
     grep -q '"total": *[1-9]' "$EVIDENCE_DIR/audit-registers.txt"; then
    REGISTER_AUDITED=true
  fi
fi
append_summary "login_blocked_audited=$LOGIN_BLOCKED_AUDITED"
append_summary "bad_credentials_audited=$BAD_CREDENTIALS_AUDITED"
append_summary "clean_login_audited=$CLEAN_LOGIN_AUDITED"
append_summary "credentials_redacted_in_audit=$CREDENTIALS_REDACTED_IN_AUDIT"
append_summary "register_audited=$REGISTER_AUDITED"

# 6) 对账
FAILED=false
for check in READY_OK LOGIN_BLOCKED_BY_RISK LOGIN_BLOCKED_AUDITED BAD_CREDENTIALS_REJECTED \
  BAD_CREDENTIALS_AUDITED CLEAN_LOGIN_OK CLEAN_LOGIN_AUDITED CREDENTIALS_REDACTED_IN_AUDIT \
  REGISTER_AUDITED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Auth audit acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Auth audit acceptance 通过: block 拦截/失败审计/凭据脱敏/正常登录 全部真实留档"
