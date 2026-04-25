#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Auth session acceptance 测试开始..."

SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
AUTH_ACCEPTANCE_MODE=${AUTH_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/auth-session-acceptance"}

mkdir -p "$EVIDENCE_DIR"

REGISTER_OK=false
LOGIN_PRIMARY_OK=false
LOGIN_SECONDARY_OK=false
REFRESH_OK=false
OLD_REFRESH_REJECTED=false
SESSIONS_BEFORE_OK=false
LOGOUT_OTHERS_OK=false
SESSIONS_AFTER_LOGOUT_OTHERS_OK=false
LOGOUT_CURRENT_OK=false
POST_LOGOUT_CURRENT_REJECTED=false
OVERALL_STATUS=failed

save_response() {
  local name=$1
  local body=${2:-}
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

write_manifest() {
  MANIFEST_MODE="${AUTH_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_REGISTER_OK="${REGISTER_OK:-false}" \
  MANIFEST_LOGIN_PRIMARY_OK="${LOGIN_PRIMARY_OK:-false}" \
  MANIFEST_LOGIN_SECONDARY_OK="${LOGIN_SECONDARY_OK:-false}" \
  MANIFEST_REFRESH_OK="${REFRESH_OK:-false}" \
  MANIFEST_OLD_REFRESH_REJECTED="${OLD_REFRESH_REJECTED:-false}" \
  MANIFEST_SESSIONS_BEFORE_OK="${SESSIONS_BEFORE_OK:-false}" \
  MANIFEST_LOGOUT_OTHERS_OK="${LOGOUT_OTHERS_OK:-false}" \
  MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_OK="${SESSIONS_AFTER_LOGOUT_OTHERS_OK:-false}" \
  MANIFEST_LOGOUT_CURRENT_OK="${LOGOUT_CURRENT_OK:-false}" \
  MANIFEST_POST_LOGOUT_CURRENT_REJECTED="${POST_LOGOUT_CURRENT_REJECTED:-false}" \
  MANIFEST_SESSIONS_BEFORE_COUNT="${SESSIONS_BEFORE_COUNT:-0}" \
  MANIFEST_SESSIONS_BEFORE_ACTIVE_COUNT="${SESSIONS_BEFORE_ACTIVE_COUNT:-0}" \
  MANIFEST_SESSIONS_BEFORE_CURRENT_COUNT="${SESSIONS_BEFORE_CURRENT_COUNT:-0}" \
  MANIFEST_LOGOUT_OTHERS_COUNT="${LOGOUT_OTHERS_COUNT:-0}" \
  MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT="${SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT:-0}" \
  MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT="${SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT:-0}" \
  MANIFEST_LOGOUT_CURRENT_STATUS="${LOGOUT_CURRENT_STATUS:-unknown}" \
  MANIFEST_POST_LOGOUT_CURRENT_STATUS="${POST_LOGOUT_CURRENT_STATUS:-0}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "auth-session",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "register_ok": os.environ.get("MANIFEST_REGISTER_OK", "false"),
        "login_primary_ok": os.environ.get("MANIFEST_LOGIN_PRIMARY_OK", "false"),
        "login_secondary_ok": os.environ.get("MANIFEST_LOGIN_SECONDARY_OK", "false"),
        "refresh_ok": os.environ.get("MANIFEST_REFRESH_OK", "false"),
        "old_refresh_rejected": os.environ.get("MANIFEST_OLD_REFRESH_REJECTED", "false"),
        "sessions_before_ok": os.environ.get("MANIFEST_SESSIONS_BEFORE_OK", "false"),
        "logout_others_ok": os.environ.get("MANIFEST_LOGOUT_OTHERS_OK", "false"),
        "sessions_after_logout_others_ok": os.environ.get("MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_OK", "false"),
        "logout_current_ok": os.environ.get("MANIFEST_LOGOUT_CURRENT_OK", "false"),
        "post_logout_current_rejected": os.environ.get("MANIFEST_POST_LOGOUT_CURRENT_REJECTED", "false"),
    },
    "metrics": {
        "sessions_before_count": os.environ.get("MANIFEST_SESSIONS_BEFORE_COUNT", "0"),
        "sessions_before_active_count": os.environ.get("MANIFEST_SESSIONS_BEFORE_ACTIVE_COUNT", "0"),
        "sessions_before_current_count": os.environ.get("MANIFEST_SESSIONS_BEFORE_CURRENT_COUNT", "0"),
        "logout_others_count": os.environ.get("MANIFEST_LOGOUT_OTHERS_COUNT", "0"),
        "sessions_after_logout_others_active_count": os.environ.get("MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT", "0"),
        "sessions_after_logout_others_revoked_count": os.environ.get("MANIFEST_SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT", "0"),
        "logout_current_status": os.environ.get("MANIFEST_LOGOUT_CURRENT_STATUS", "unknown"),
        "post_logout_current_status": os.environ.get("MANIFEST_POST_LOGOUT_CURRENT_STATUS", "0"),
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

trap write_manifest EXIT

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
if "//" in expr:
    expr, fallback = expr.split("//", 1)
    expr = expr.strip()
    fallback = fallback.strip().strip('"')

result = None
if expr.startswith("."):
    result = query(data, expr.lstrip("."))

if result is None:
    print(fallback)
elif isinstance(result, bool):
    print("true" if result else "false")
else:
    print(result)
PY
}

json_count() {
  local json_input="${1:-}"
  local python_code="${2:-}"
  JSON_INPUT="$json_input" python3 - "$python_code" <<'PY'
import json
import os
import sys

payload = os.environ.get("JSON_INPUT", "")
code = sys.argv[1]
data = json.loads(payload)
safe_builtins = {
    "sum": sum,
    "len": len,
    "min": min,
    "max": max,
    "any": any,
    "all": all,
}
print(eval(code, {"__builtins__": safe_builtins}, {"data": data}))
PY
}

request_json() {
  local method=$1
  local url=$2
  local body=$3
  local auth_token=$4
  local device_id=$5
  local user_agent=$6
  local response_file
  local status_file

  response_file=$(mktemp)
  status_file=$(mktemp)
  trap 'rm -f "$response_file" "$status_file"' RETURN

  local -a curl_args
  curl_args=(-sS -X "$method" "$url" -H "Content-Type: application/json" -H "User-Agent: $user_agent")
  if [ -n "$device_id" ]; then
    curl_args+=(-H "X-Device-ID: $device_id")
  fi
  if [ -n "$auth_token" ]; then
    curl_args+=(-H "Authorization: Bearer $auth_token")
  fi
  if [ -n "$body" ]; then
    curl_args+=(--data "$body")
  fi
  curl_args+=(-o "$response_file" -w "%{http_code}")

  curl "${curl_args[@]}" > "$status_file"

  RESPONSE_STATUS=$(cat "$status_file")
  RESPONSE_BODY=$(cat "$response_file")
}

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Auth session acceptance summary
mode=$AUTH_ACCEPTANCE_MODE
servify_url=$SERVIFY_URL
EOF

echo "🗂️ 证据输出目录: $EVIDENCE_DIR"

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

assert_status() {
  local want=$1
  local actual=$2
  local message=$3
  if [ "$actual" != "$want" ]; then
    echo "❌ $message: expected HTTP $want got $actual"
    append_summary "${message}_status=$actual"
    exit 1
  fi
}

echo "🔍 检查服务状态..."
wait_for "Servify Health" "$SERVIFY_URL/health" 30 2

TIMESTAMP=$(date +%s)
RANDOM_SUFFIX=${RANDOM:-$$}
TEST_USERNAME="${AUTH_TEST_USERNAME:-auth-acceptance-${TIMESTAMP}-${RANDOM_SUFFIX}}"
TEST_EMAIL="${AUTH_TEST_EMAIL:-${TEST_USERNAME}@example.com}"
TEST_PASSWORD="${AUTH_TEST_PASSWORD:-AuthPass!12345}"

append_summary "test_username=$TEST_USERNAME"

REGISTER_BODY=$(printf '{"username":"%s","email":"%s","password":"%s","name":"Auth Acceptance User"}' "$TEST_USERNAME" "$TEST_EMAIL" "$TEST_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY" "" "auth-register" "servify-auth-acceptance/register"
save_response "auth-register" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "register"
REGISTER_OK=true

LOGIN_BODY=$(printf '{"username":"%s","password":"%s"}' "$TEST_USERNAME" "$TEST_PASSWORD")

request_json "POST" "$SERVIFY_URL/api/v1/auth/login" "$LOGIN_BODY" "" "auth-device-primary" "servify-auth-acceptance/login-primary"
save_response "auth-login-primary" "$RESPONSE_BODY"
append_summary "login_primary_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "login_primary"
LOGIN_PRIMARY_OK=true
PRIMARY_ACCESS_TOKEN=$(json_get "$RESPONSE_BODY" '.token // ""')
PRIMARY_REFRESH_TOKEN=$(json_get "$RESPONSE_BODY" '.refresh_token // ""')
if [ -z "$PRIMARY_ACCESS_TOKEN" ] || [ -z "$PRIMARY_REFRESH_TOKEN" ]; then
  echo "❌ login primary 响应缺少 token"
  exit 1
fi

request_json "POST" "$SERVIFY_URL/api/v1/auth/login" "$LOGIN_BODY" "" "auth-device-secondary" "servify-auth-acceptance/login-secondary"
save_response "auth-login-secondary" "$RESPONSE_BODY"
append_summary "login_secondary_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "login_secondary"
LOGIN_SECONDARY_OK=true

REFRESH_BODY=$(printf '{"refresh_token":"%s"}' "$PRIMARY_REFRESH_TOKEN")
request_json "POST" "$SERVIFY_URL/api/v1/auth/refresh" "$REFRESH_BODY" "" "auth-device-primary" "servify-auth-acceptance/refresh-primary"
save_response "auth-refresh" "$RESPONSE_BODY"
append_summary "refresh_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "refresh"
REFRESH_OK=true
REFRESHED_ACCESS_TOKEN=$(json_get "$RESPONSE_BODY" '.token // ""')
REFRESHED_REFRESH_TOKEN=$(json_get "$RESPONSE_BODY" '.refresh_token // ""')
if [ -z "$REFRESHED_ACCESS_TOKEN" ] || [ -z "$REFRESHED_REFRESH_TOKEN" ]; then
  echo "❌ refresh 响应缺少新 token"
  exit 1
fi

request_json "POST" "$SERVIFY_URL/api/v1/auth/refresh" "$REFRESH_BODY" "" "auth-device-primary" "servify-auth-acceptance/refresh-reuse-old"
save_response "auth-refresh-reuse-old" "$RESPONSE_BODY"
append_summary "refresh_reuse_old_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "401" ]; then
  echo "❌ 旧 refresh token 复用未被拒绝"
  exit 1
fi
OLD_REFRESH_REJECTED=true

request_json "GET" "$SERVIFY_URL/api/v1/auth/sessions" "" "$REFRESHED_ACCESS_TOKEN" "auth-device-primary" "servify-auth-acceptance/sessions-before"
save_response "auth-sessions-before" "$RESPONSE_BODY"
append_summary "sessions_before_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "sessions_before"
SESSIONS_BEFORE_OK=true
SESSIONS_BEFORE_COUNT=$(json_get "$RESPONSE_BODY" '.count // "0"')
SESSIONS_BEFORE_ACTIVE_COUNT=$(json_count "$RESPONSE_BODY" 'sum(1 for item in data.get("items", []) if item.get("status") == "active")')
SESSIONS_BEFORE_CURRENT_COUNT=$(json_count "$RESPONSE_BODY" 'sum(1 for item in data.get("items", []) if item.get("is_current") is True)')
append_summary "sessions_before_count=$SESSIONS_BEFORE_COUNT"
append_summary "sessions_before_active_count=$SESSIONS_BEFORE_ACTIVE_COUNT"
append_summary "sessions_before_current_count=$SESSIONS_BEFORE_CURRENT_COUNT"
if [ "${SESSIONS_BEFORE_COUNT:-0}" -lt 2 ] || [ "${SESSIONS_BEFORE_ACTIVE_COUNT:-0}" -lt 2 ] || [ "${SESSIONS_BEFORE_CURRENT_COUNT:-0}" -lt 1 ]; then
  echo "❌ refresh 后会话列表未体现双会话状态"
  exit 1
fi

request_json "POST" "$SERVIFY_URL/api/v1/auth/sessions/logout-others" "" "$REFRESHED_ACCESS_TOKEN" "auth-device-primary" "servify-auth-acceptance/logout-others"
save_response "auth-logout-others" "$RESPONSE_BODY"
append_summary "logout_others_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "logout_others"
LOGOUT_OTHERS_OK=true
LOGOUT_OTHERS_COUNT=$(json_get "$RESPONSE_BODY" '.count // "0"')
append_summary "logout_others_count=$LOGOUT_OTHERS_COUNT"
if [ "${LOGOUT_OTHERS_COUNT:-0}" -lt 1 ]; then
  echo "❌ logout-others 未注销其它会话"
  exit 1
fi

request_json "GET" "$SERVIFY_URL/api/v1/auth/sessions" "" "$REFRESHED_ACCESS_TOKEN" "auth-device-primary" "servify-auth-acceptance/sessions-after-logout-others"
save_response "auth-sessions-after-logout-others" "$RESPONSE_BODY"
append_summary "sessions_after_logout_others_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "sessions_after_logout_others"
SESSIONS_AFTER_LOGOUT_OTHERS_OK=true
SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT=$(json_count "$RESPONSE_BODY" 'sum(1 for item in data.get("items", []) if item.get("status") == "active")')
SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT=$(json_count "$RESPONSE_BODY" 'sum(1 for item in data.get("items", []) if item.get("status") == "revoked")')
append_summary "sessions_after_logout_others_active_count=$SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT"
append_summary "sessions_after_logout_others_revoked_count=$SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT"
if [ "${SESSIONS_AFTER_LOGOUT_OTHERS_ACTIVE_COUNT:-0}" -ne 1 ] || [ "${SESSIONS_AFTER_LOGOUT_OTHERS_REVOKED_COUNT:-0}" -lt 1 ]; then
  echo "❌ logout-others 后会话状态未按预期变化"
  exit 1
fi

request_json "POST" "$SERVIFY_URL/api/v1/auth/sessions/logout-current" "" "$REFRESHED_ACCESS_TOKEN" "auth-device-primary" "servify-auth-acceptance/logout-current"
save_response "auth-logout-current" "$RESPONSE_BODY"
append_summary "logout_current_status_code=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "logout_current"
LOGOUT_CURRENT_OK=true
LOGOUT_CURRENT_STATUS=$(json_get "$RESPONSE_BODY" '.status // ""')
append_summary "logout_current_status=$LOGOUT_CURRENT_STATUS"
if [ "$LOGOUT_CURRENT_STATUS" != "revoked" ]; then
  echo "❌ logout-current 未返回 revoked 状态"
  exit 1
fi

request_json "GET" "$SERVIFY_URL/api/v1/auth/sessions" "" "$REFRESHED_ACCESS_TOKEN" "auth-device-primary" "servify-auth-acceptance/sessions-after-logout-current"
save_response "auth-sessions-after-logout-current" "$RESPONSE_BODY"
POST_LOGOUT_CURRENT_STATUS=$RESPONSE_STATUS
append_summary "post_logout_current_status=$POST_LOGOUT_CURRENT_STATUS"
if [ "$POST_LOGOUT_CURRENT_STATUS" != "401" ]; then
  echo "❌ logout-current 后旧 access token 仍可访问 sessions"
  exit 1
fi
POST_LOGOUT_CURRENT_REJECTED=true

OVERALL_STATUS=passed
append_summary "register_ok=$REGISTER_OK"
append_summary "login_primary_ok=$LOGIN_PRIMARY_OK"
append_summary "login_secondary_ok=$LOGIN_SECONDARY_OK"
append_summary "refresh_ok=$REFRESH_OK"
append_summary "old_refresh_rejected=$OLD_REFRESH_REJECTED"
append_summary "sessions_before_ok=$SESSIONS_BEFORE_OK"
append_summary "logout_others_ok=$LOGOUT_OTHERS_OK"
append_summary "sessions_after_logout_others_ok=$SESSIONS_AFTER_LOGOUT_OTHERS_OK"
append_summary "logout_current_ok=$LOGOUT_CURRENT_OK"
append_summary "post_logout_current_rejected=$POST_LOGOUT_CURRENT_REJECTED"
append_summary "overall_status=$OVERALL_STATUS"

echo "✅ Auth session acceptance 测试通过"
