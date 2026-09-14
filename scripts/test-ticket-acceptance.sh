#!/bin/bash

# 工单主闭环高频操作（P1-5）acceptance 测试。
#
# 覆盖：创建 -> 更新(优先级/标签/指派) -> 评论 -> 关闭(状态核验) -> 统计 -> CSV 导出，
# 另含三条拒绝路径（不存在工单 404 / 缺标题 400 / 未认证 401）。
#
# 前置：管理员账号（admin/admin123 之类，可先跑 scripts/seed-data.sh），
#       或直接提供 ADMIN_TOKEN。客户与客服用户由脚本经注册接口现场创建。
#
# 可选环境变量:
#   SERVIFY_URL            服务地址(默认 http://localhost:8080)
#   TICKET_ACCEPTANCE_MODE 证据模式标记(默认 real)
#   ADMIN_TOKEN            管理面 JWT,提供后跳过登录
#   ADMIN_USERNAME/ADMIN_PASSWORD  管理员凭证(默认 admin/admin123)
#
# 证据输出: scripts/test-results/ticket-acceptance/(manifest.json 之外
# 的文件默认不入库,.gitignore 已排除)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Ticket acceptance 测试开始..."

SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
TICKET_ACCEPTANCE_MODE=${TICKET_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/ticket-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"admin123"}

mkdir -p "$EVIDENCE_DIR"

ADMIN_AUTH_OK=false
AGENTS_READY=false
TICKET_CREATED=false
TICKET_UPDATED=false
COMMENT_ADDED=false
CLOSE_OK=false
CLOSED_STATE_VERIFIED=false
STATS_OK=false
EXPORT_OK=false
UNKNOWN_TICKET_REJECTED=false
CREATE_MISSING_TITLE_REJECTED=false
UNAUTHENTICATED_REJECTED=false
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
  MANIFEST_MODE="${TICKET_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_AGENTS_READY="${AGENTS_READY:-false}" \
  MANIFEST_TICKET_CREATED="${TICKET_CREATED:-false}" \
  MANIFEST_TICKET_UPDATED="${TICKET_UPDATED:-false}" \
  MANIFEST_COMMENT_ADDED="${COMMENT_ADDED:-false}" \
  MANIFEST_CLOSE_OK="${CLOSE_OK:-false}" \
  MANIFEST_CLOSED_STATE_VERIFIED="${CLOSED_STATE_VERIFIED:-false}" \
  MANIFEST_STATS_OK="${STATS_OK:-false}" \
  MANIFEST_EXPORT_OK="${EXPORT_OK:-false}" \
  MANIFEST_UNKNOWN_TICKET_REJECTED="${UNKNOWN_TICKET_REJECTED:-false}" \
  MANIFEST_CREATE_MISSING_TITLE_REJECTED="${CREATE_MISSING_TITLE_REJECTED:-false}" \
  MANIFEST_UNAUTHENTICATED_REJECTED="${UNAUTHENTICATED_REJECTED:-false}" \
  MANIFEST_TICKET_ID="${TICKET_ID:-0}" \
  MANIFEST_CUSTOMER_ID="${CUSTOMER_ID:-0}" \
  MANIFEST_AGENT_ID="${AGENT_ID:-0}" \
  MANIFEST_COMMENT_ID="${COMMENT_ID:-0}" \
  MANIFEST_STATS_TOTAL="${STATS_TOTAL:-0}" \
  MANIFEST_EXPORT_BYTES="${EXPORT_BYTES:-0}" \
  MANIFEST_CLOSED_AT="${CLOSED_AT:-}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
env = os.environ
payload = {
    "provider": "ticket",
    "mode": env.get("MANIFEST_MODE", "unknown"),
    "servify_url": env.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": env.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "admin_auth_ok": env.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "agents_ready": env.get("MANIFEST_AGENTS_READY", "false"),
        "ticket_created": env.get("MANIFEST_TICKET_CREATED", "false"),
        "ticket_updated": env.get("MANIFEST_TICKET_UPDATED", "false"),
        "comment_added": env.get("MANIFEST_COMMENT_ADDED", "false"),
        "close_ok": env.get("MANIFEST_CLOSE_OK", "false"),
        "closed_state_verified": env.get("MANIFEST_CLOSED_STATE_VERIFIED", "false"),
        "stats_ok": env.get("MANIFEST_STATS_OK", "false"),
        "export_ok": env.get("MANIFEST_EXPORT_OK", "false"),
        "unknown_ticket_rejected": env.get("MANIFEST_UNKNOWN_TICKET_REJECTED", "false"),
        "create_missing_title_rejected": env.get("MANIFEST_CREATE_MISSING_TITLE_REJECTED", "false"),
        "unauthenticated_rejected": env.get("MANIFEST_UNAUTHENTICATED_REJECTED", "false"),
    },
    "metrics": {
        "ticket_id": env.get("MANIFEST_TICKET_ID", "0"),
        "customer_id": env.get("MANIFEST_CUSTOMER_ID", "0"),
        "agent_id": env.get("MANIFEST_AGENT_ID", "0"),
        "comment_id": env.get("MANIFEST_COMMENT_ID", "0"),
        "stats_total": env.get("MANIFEST_STATS_TOTAL", "0"),
        "export_bytes": env.get("MANIFEST_EXPORT_BYTES", "0"),
        "closed_at": env.get("MANIFEST_CLOSED_AT", ""),
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

request_json() {
  local method=$1
  local url=$2
  local body=$3
  local auth_token=$4

  local response_file
  local status_file
  response_file=$(mktemp)
  status_file=$(mktemp)
  # 自清理:触发后立即摘除,避免命令替换子 shell 里 trap 泄漏到外层函数
  # (set -u 下外层函数返回时 $response_file 未定义会直接报错)
  trap 'rm -f "$response_file" "$status_file"; trap - RETURN' RETURN

  local -a curl_args
  curl_args=(-sS -X "$method" "$url" -H "Content-Type: application/json")
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

request_raw() {
  local method=$1
  local url=$2
  local auth_token=$3
  local output_file=$4

  local status_file
  status_file=$(mktemp)
  trap 'rm -f "$status_file"; trap - RETURN' RETURN

  local -a curl_args=(-sS -X "$method" "$url")
  if [ -n "$auth_token" ]; then
    curl_args+=(-H "Authorization: Bearer $auth_token")
  fi
  curl_args+=(-o "$output_file" -w "%{http_code}")
  curl "${curl_args[@]}" > "$status_file"
  RESPONSE_STATUS=$(cat "$status_file")
}

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

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Ticket acceptance summary
mode=$TICKET_ACCEPTANCE_MODE
servify_url=$SERVIFY_URL
EOF

echo "🗂️ 证据输出目录: $EVIDENCE_DIR"

echo "🔍 检查服务状态..."
wait_for "Servify Health" "$SERVIFY_URL/health" 30 2

TIMESTAMP=$(date +%s)
RANDOM_SUFFIX=${RANDOM:-$$}
TICKET_TITLE="ticket-acceptance-${TIMESTAMP}-${RANDOM_SUFFIX}"
COMMENT_CONTENT="验收评论 ticket-acceptance ${TIMESTAMP}"

# 1. 管理员凭证
echo "🔑 管理员认证..."
if [ -n "${ADMIN_TOKEN:-}" ]; then
  ADMIN_ACCESS_TOKEN="$ADMIN_TOKEN"
  save_response "admin-auth" '{"source":"ADMIN_TOKEN"}'
  append_summary "admin_auth_source=env"
else
  LOGIN_BODY=$(printf '{"username":"%s","password":"%s"}' "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
  request_json "POST" "$SERVIFY_URL/api/v1/auth/login" "$LOGIN_BODY" ""
  save_response "admin-auth" "$RESPONSE_BODY"
  append_summary "admin_login_status=$RESPONSE_STATUS"
  if [ "$RESPONSE_STATUS" != "200" ]; then
    echo "❌ 管理员登录失败(HTTP $RESPONSE_STATUS): 请先跑 scripts/seed-data.sh 创建 admin,或用 ADMIN_USERNAME/ADMIN_PASSWORD/ADMIN_TOKEN 提供凭证"
    exit 1
  fi
  if [ "$(json_get "$RESPONSE_BODY" '.two_factor_required // "false"')" = "true" ]; then
    echo "❌ 该管理员开启了两步验证,验收脚本无法自动通过挑战步;请提供未启用 2FA 的管理员或改用 ADMIN_TOKEN"
    exit 1
  fi
  ADMIN_ACCESS_TOKEN=$(json_get "$RESPONSE_BODY" '.token // ""')
  if [ -z "$ADMIN_ACCESS_TOKEN" ]; then
    echo "❌ 管理员登录响应缺少 token"
    exit 1
  fi
fi
ADMIN_AUTH_OK=true
append_summary "admin_auth_ok=$ADMIN_AUTH_OK"

# 2. 客户用户(工单创建前置)+ 客服用户与实体(指派目标)
echo "🧑‍💻 创建客户与客服实体..."
CUSTOMER_BODY=$(printf '{"username":"ticket-acceptance-c-%s-%s","email":"ticket-acceptance-c-%s-%s@example.com","password":"Acceptance!12345","name":"验收客户","role":"customer"}' "$TIMESTAMP" "$RANDOM_SUFFIX" "$TIMESTAMP" "$RANDOM_SUFFIX")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$CUSTOMER_BODY" ""
save_response "ticket-customer" "$RESPONSE_BODY"
append_summary "customer_register_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "customer_register"
CUSTOMER_ID=$(json_get "$RESPONSE_BODY" '.user.id // "0"')
if [ "$CUSTOMER_ID" = "0" ]; then
  echo "❌ 客户注册响应缺少 user.id"
  exit 1
fi

AGENT_BODY=$(printf '{"username":"ticket-acceptance-a-%s-%s","email":"ticket-acceptance-a-%s-%s@example.com","password":"Acceptance!12345","name":"验收坐席","role":"agent"}' "$TIMESTAMP" "$RANDOM_SUFFIX" "$TIMESTAMP" "$RANDOM_SUFFIX")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$AGENT_BODY" ""
save_response "ticket-agent" "$RESPONSE_BODY"
append_summary "agent_register_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "agent_register"
AGENT_ID=$(json_get "$RESPONSE_BODY" '.user.id // "0"')
if [ "$AGENT_ID" = "0" ]; then
  echo "❌ 客服注册响应缺少 user.id"
  exit 1
fi

AGENT_CREATE_BODY=$(printf '{"user_id":%s,"department":"acceptance","skills":"ticket","max_concurrent":5}' "$AGENT_ID")
request_json "POST" "$SERVIFY_URL/api/agents" "$AGENT_CREATE_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "agent-create" "$RESPONSE_BODY"
append_summary "agent_create_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "agent_create"
AGENTS_READY=true
append_summary "agents_ready=$AGENTS_READY"

# 3. 创建工单
echo "🎫 创建工单..."
CREATE_BODY=$(printf '{"title":"%s","description":"P1-5 验收工单","customer_id":%s,"category":"acceptance","priority":"normal","source":"web"}' "$TICKET_TITLE" "$CUSTOMER_ID")
request_json "POST" "$SERVIFY_URL/api/tickets" "$CREATE_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-created" "$RESPONSE_BODY"
append_summary "ticket_create_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "ticket_create"
TICKET_ID=$(json_get "$RESPONSE_BODY" '.id // "0"')
CREATED_TITLE=$(json_get "$RESPONSE_BODY" '.title // ""')
if [ "$TICKET_ID" = "0" ] || [ "$CREATED_TITLE" != "$TICKET_TITLE" ]; then
  echo "❌ 工单创建响应缺少 id 或 title 不一致"
  exit 1
fi
TICKET_CREATED=true
append_summary "ticket_created=$TICKET_CREATED"
append_summary "ticket_id=$TICKET_ID"

# 4. 更新工单(优先级/标签/指派)
echo "✏️ 更新工单..."
UPDATE_BODY=$(printf '{"priority":"high","tags":"acceptance,p1-5","agent_id":%s}' "$AGENT_ID")
request_json "PUT" "$SERVIFY_URL/api/tickets/$TICKET_ID" "$UPDATE_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-updated" "$RESPONSE_BODY"
append_summary "ticket_update_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_update"
UPDATED_PRIORITY=$(json_get "$RESPONSE_BODY" '.priority // ""')
UPDATED_AGENT_ID=$(json_get "$RESPONSE_BODY" '.agent_id // "0"')
UPDATED_TAGS=$(json_get "$RESPONSE_BODY" '.tags // ""')
if [ "$UPDATED_PRIORITY" != "high" ] || [ "$UPDATED_AGENT_ID" != "$AGENT_ID" ] || [ "$UPDATED_TAGS" != "acceptance,p1-5" ]; then
  echo "❌ 工单更新未生效: priority=$UPDATED_PRIORITY agent_id=$UPDATED_AGENT_ID tags=$UPDATED_TAGS"
  exit 1
fi
TICKET_UPDATED=true
append_summary "ticket_updated=$TICKET_UPDATED"

# 5. 详情核验(读路径确认更新落库)
request_json "GET" "$SERVIFY_URL/api/tickets/$TICKET_ID" "" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-detail-after-update" "$RESPONSE_BODY"
append_summary "ticket_detail_after_update_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_detail_after_update"
DETAIL_PRIORITY=$(json_get "$RESPONSE_BODY" '.priority // ""')
if [ "$DETAIL_PRIORITY" != "high" ]; then
  echo "❌ 详情读回 priority=$DETAIL_PRIORITY, want high"
  exit 1
fi

# 6. 评论
echo "💬 添加评论..."
COMMENT_BODY=$(printf '{"content":"%s"}' "$COMMENT_CONTENT")
request_json "POST" "$SERVIFY_URL/api/tickets/$TICKET_ID/comments" "$COMMENT_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-comment" "$RESPONSE_BODY"
append_summary "ticket_comment_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "ticket_comment"
COMMENT_ID=$(json_get "$RESPONSE_BODY" '.id // "0"')
if [ "$COMMENT_ID" = "0" ]; then
  echo "❌ 评论响应缺少 id"
  exit 1
fi
COMMENT_ADDED=true
append_summary "comment_added=$COMMENT_ADDED"
append_summary "comment_id=$COMMENT_ID"

# 7. 关闭
echo "🔒 关闭工单..."
CLOSE_BODY='{"reason":"P1-5 acceptance close"}'
request_json "POST" "$SERVIFY_URL/api/tickets/$TICKET_ID/close" "$CLOSE_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-close" "$RESPONSE_BODY"
append_summary "ticket_close_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_close"
CLOSE_OK=true
append_summary "close_ok=$CLOSE_OK"

# 8. 状态变化核验
request_json "GET" "$SERVIFY_URL/api/tickets/$TICKET_ID" "" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-detail-closed" "$RESPONSE_BODY"
append_summary "ticket_detail_closed_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_detail_closed"
CLOSED_STATUS=$(json_get "$RESPONSE_BODY" '.status // ""')
CLOSED_AT=$(json_get "$RESPONSE_BODY" '.closed_at // ""')
append_summary "closed_status=$CLOSED_STATUS"
append_summary "closed_at=$CLOSED_AT"
if [ "$CLOSED_STATUS" != "closed" ] || [ -z "$CLOSED_AT" ]; then
  echo "❌ 关闭后状态应为 closed 且 closed_at 非空, got status=$CLOSED_STATUS closed_at=$CLOSED_AT"
  exit 1
fi
CLOSED_STATE_VERIFIED=true
append_summary "closed_state_verified=$CLOSED_STATE_VERIFIED"

# 9. 工单统计
echo "📊 统计..."
request_json "GET" "$SERVIFY_URL/api/tickets/stats" "" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-stats" "$RESPONSE_BODY"
append_summary "ticket_stats_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_stats"
STATS_TOTAL=$(json_get "$RESPONSE_BODY" '.total // "0"')
append_summary "stats_total=$STATS_TOTAL"
if [ "${STATS_TOTAL:-0}" -lt 1 ]; then
  echo "❌ 工单统计 total=$STATS_TOTAL, 应至少包含本次创建的工单"
  exit 1
fi
STATS_OK=true
append_summary "stats_ok=$STATS_OK"

# 10. CSV 导出
echo "📤 CSV 导出..."
EXPORT_FILE="$EVIDENCE_DIR/ticket-export.csv"
request_raw "GET" "$SERVIFY_URL/api/tickets/export?limit=1000" "$ADMIN_ACCESS_TOKEN" "$EXPORT_FILE"
append_summary "ticket_export_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "ticket_export"
if ! grep -q "$TICKET_TITLE" "$EXPORT_FILE"; then
  echo "❌ CSV 导出未包含本次工单标题"
  exit 1
fi
EXPORT_BYTES=$(wc -c < "$EXPORT_FILE" | tr -d '[:space:]')
append_summary "export_bytes=$EXPORT_BYTES"
EXPORT_OK=true
append_summary "export_ok=$EXPORT_OK"

# 11. 拒绝路径: 不存在工单 404
echo "🚫 拒绝路径..."
request_json "GET" "$SERVIFY_URL/api/tickets/4294967295" "" "$ADMIN_ACCESS_TOKEN"
save_response "unknown-ticket" "$RESPONSE_BODY"
append_summary "unknown_ticket_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "404" ]; then
  echo "❌ 不存在工单应返回 404, got $RESPONSE_STATUS"
  exit 1
fi
UNKNOWN_TICKET_REJECTED=true
append_summary "unknown_ticket_rejected=$UNKNOWN_TICKET_REJECTED"

# 12. 拒绝路径: 缺标题 400
request_json "POST" "$SERVIFY_URL/api/tickets" "$(printf '{"description":"no title","customer_id":%s}' "$CUSTOMER_ID")" "$ADMIN_ACCESS_TOKEN"
save_response "ticket-create-missing-title" "$RESPONSE_BODY"
append_summary "create_missing_title_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "400" ]; then
  echo "❌ 缺标题创建应返回 400, got $RESPONSE_STATUS"
  exit 1
fi
CREATE_MISSING_TITLE_REJECTED=true
append_summary "create_missing_title_rejected=$CREATE_MISSING_TITLE_REJECTED"

# 13. 拒绝路径: 未认证 401
request_json "GET" "$SERVIFY_URL/api/tickets" "" ""
save_response "unauthenticated-tickets" "$RESPONSE_BODY"
append_summary "unauthenticated_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "401" ]; then
  echo "❌ 未认证访问应返回 401, got $RESPONSE_STATUS"
  exit 1
fi
UNAUTHENTICATED_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTHENTICATED_REJECTED"

OVERALL_STATUS=passed
append_summary "ticket_id=$TICKET_ID"
append_summary "customer_id=$CUSTOMER_ID"
append_summary "agent_id=$AGENT_ID"
append_summary "overall_status=$OVERALL_STATUS"

echo "✅ Ticket acceptance 测试通过"
