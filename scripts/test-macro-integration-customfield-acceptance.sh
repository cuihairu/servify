#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Macro/Integration/CustomField acceptance 测试开始（P2-6 第五刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/macro-integration-customfield"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"st5-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"st5-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18102}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
MACRO_BASELINE_EMPTY=false
MACRO_CREATED=false
MACRO_LISTED=false
MACRO_UPDATED=false
MACRO_APPLIED=false
MACRO_MISSING_TICKET_REJECTED=false
MACRO_INACTIVE_APPLY_REJECTED=false
MACRO_DELETED_AND_GONE=false
INTEGRATION_CREATED=false
INTEGRATION_LISTED=false
INTEGRATION_DUPLICATE_REJECTED=false
INTEGRATION_UPDATED_ENABLED=false
INTEGRATION_SEARCH_RECONCILED=false
INTEGRATION_DELETED_AND_GONE=false
CUSTOM_FIELD_CREATED=false
CUSTOM_FIELD_LISTED=false
CUSTOM_FIELD_INVALID_KEY_REJECTED=false
CUSTOM_FIELD_INVALID_TYPE_REJECTED=false
CUSTOM_FIELD_UNSUPPORTED_RESOURCE_REJECTED=false
CUSTOM_FIELD_UPDATED=false
CUSTOM_FIELD_DELETED_GONE_404=false
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

# json_extract <python表达式>：在最近一次响应 JSON 上求值，表达式里 `d` 为根对象。
json_extract() {
  printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
body = raw.split('\r\n\r\n', 1)[-1]
try:
    d = json.loads(body)
except Exception:
    m = re.search(r'[\{\[].*[\}\]]', raw, re.S)
    try:
        d = json.loads(m.group(0)) if m else {}
    except Exception:
        d = {}
try:
    print(eval(sys.argv[1], {'d': d, 'json': json}))
except Exception:
    print('')
" "$1" 2>/dev/null || true
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

write_manifest() {
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  MANIFEST_MACRO_BASELINE="${MACRO_BASELINE_EMPTY:-false}" \
  MANIFEST_MACRO_CREATED="${MACRO_CREATED:-false}" \
  MANIFEST_MACRO_LISTED="${MACRO_LISTED:-false}" \
  MANIFEST_MACRO_UPDATED="${MACRO_UPDATED:-false}" \
  MANIFEST_MACRO_APPLIED="${MACRO_APPLIED:-false}" \
  MANIFEST_MACRO_MISSING_TICKET="${MACRO_MISSING_TICKET_REJECTED:-false}" \
  MANIFEST_MACRO_INACTIVE_APPLY="${MACRO_INACTIVE_APPLY_REJECTED:-false}" \
  MANIFEST_MACRO_DELETED="${MACRO_DELETED_AND_GONE:-false}" \
  MANIFEST_INTEGRATION_CREATED="${INTEGRATION_CREATED:-false}" \
  MANIFEST_INTEGRATION_LISTED="${INTEGRATION_LISTED:-false}" \
  MANIFEST_INTEGRATION_DUPLICATE="${INTEGRATION_DUPLICATE_REJECTED:-false}" \
  MANIFEST_INTEGRATION_UPDATED="${INTEGRATION_UPDATED_ENABLED:-false}" \
  MANIFEST_INTEGRATION_SEARCH="${INTEGRATION_SEARCH_RECONCILED:-false}" \
  MANIFEST_INTEGRATION_DELETED="${INTEGRATION_DELETED_AND_GONE:-false}" \
  MANIFEST_CF_CREATED="${CUSTOM_FIELD_CREATED:-false}" \
  MANIFEST_CF_LISTED="${CUSTOM_FIELD_LISTED:-false}" \
  MANIFEST_CF_INVALID_KEY="${CUSTOM_FIELD_INVALID_KEY_REJECTED:-false}" \
  MANIFEST_CF_INVALID_TYPE="${CUSTOM_FIELD_INVALID_TYPE_REJECTED:-false}" \
  MANIFEST_CF_UNSUPPORTED_RESOURCE="${CUSTOM_FIELD_UNSUPPORTED_RESOURCE_REJECTED:-false}" \
  MANIFEST_CF_UPDATED="${CUSTOM_FIELD_UPDATED:-false}" \
  MANIFEST_CF_DELETED_GONE="${CUSTOM_FIELD_DELETED_GONE_404:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "macro-integration-customfield",
    "mode": "runtime-ops-tooling-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "macro_baseline_empty": os.environ.get("MANIFEST_MACRO_BASELINE", "false"),
        "macro_created": os.environ.get("MANIFEST_MACRO_CREATED", "false"),
        "macro_listed": os.environ.get("MANIFEST_MACRO_LISTED", "false"),
        "macro_updated": os.environ.get("MANIFEST_MACRO_UPDATED", "false"),
        "macro_applied_to_ticket": os.environ.get("MANIFEST_MACRO_APPLIED", "false"),
        "macro_apply_missing_ticket_rejected_404": os.environ.get("MANIFEST_MACRO_MISSING_TICKET", "false"),
        "macro_inactive_apply_rejected_400": os.environ.get("MANIFEST_MACRO_INACTIVE_APPLY", "false"),
        "macro_deleted_and_gone": os.environ.get("MANIFEST_MACRO_DELETED", "false"),
        "integration_created": os.environ.get("MANIFEST_INTEGRATION_CREATED", "false"),
        "integration_listed": os.environ.get("MANIFEST_INTEGRATION_LISTED", "false"),
        "integration_duplicate_slug_rejected_409": os.environ.get("MANIFEST_INTEGRATION_DUPLICATE", "false"),
        "integration_updated_enabled": os.environ.get("MANIFEST_INTEGRATION_UPDATED", "false"),
        "integration_search_reconciled": os.environ.get("MANIFEST_INTEGRATION_SEARCH", "false"),
        "integration_deleted_and_gone": os.environ.get("MANIFEST_INTEGRATION_DELETED", "false"),
        "custom_field_created": os.environ.get("MANIFEST_CF_CREATED", "false"),
        "custom_field_listed": os.environ.get("MANIFEST_CF_LISTED", "false"),
        "custom_field_invalid_key_rejected_400": os.environ.get("MANIFEST_CF_INVALID_KEY", "false"),
        "custom_field_invalid_type_rejected_400": os.environ.get("MANIFEST_CF_INVALID_TYPE", "false"),
        "custom_field_unsupported_resource_rejected_400": os.environ.get("MANIFEST_CF_UNSUPPORTED_RESOURCE", "false"),
        "custom_field_updated": os.environ.get("MANIFEST_CF_UPDATED", "false"),
        "custom_field_deleted_gone_404": os.environ.get("MANIFEST_CF_DELETED_GONE", "false"),
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
Macro/Integration/CustomField acceptance summary (P2-6 slice 5)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/macro-integration-customfield-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/macro-integration-customfield-XXXXXX.sqlite")"

make_config() {
  local dst=$1
  python3 - "$PROJECT_ROOT/config.yml" "$dst" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["paths"] = [
    {
        "enabled": True,
        "prefix": "/api/",
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
MACROS_BASE="$SERVIFY_URL/api/macros"
INTEGRATIONS_BASE="$SERVIFY_URL/api/apps/integrations"
CUSTOM_FIELDS_BASE="$SERVIFY_URL/api/custom-fields"
if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"

# 2) 起服与就绪。
append_summary "step=ready"
echo "🚀 启动 Servify (sqlite, 端口 $SERVIFY_PORT)..."
make_config "$WORK_DIR/config.yml"
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

# 3) 样本数据：admin、宏应用目标客户与工单。
append_summary "step=setup"

# N0：未认证读宏列表 → 401。
request_capture "unauthenticated-macros" "$MACROS_BASE"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-macros.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"ST5 Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)

request_capture "customer-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"st5-cust-'"$TIMESTAMP"'","email":"st5-cust-'"$TIMESTAMP"'@servify.io","name":"运营工具客户","source":"web"}' \
  "$SERVIFY_URL/api/customers"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 客户创建失败" >&2
  exit 1
fi
CUSTOMER_ID="$(json_extract "d.get('id')")"

request_capture "ticket-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"宏应用目标工单","description":"宏 apply 验收","category":"technical","priority":"high","customer_id":'"$CUSTOMER_ID"'}' \
  "$SERVIFY_URL/api/tickets"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 工单创建失败" >&2
  exit 1
fi
TICKET_ID="$(json_extract "d.get('id')")"
append_summary "customer_id=$CUSTOMER_ID ticket_id=$TICKET_ID"

# 4) 宏链路（§9）：基线 → 创建 → 列表 → 更新 → 应用 → 负例 → 停用 → 删除。
append_summary "step=macro_chain"

request_capture "macro-baseline" -H "$AUTH" "$MACROS_BASE"
BASELINE_LEN="$(json_extract "len(d) if isinstance(d, list) else -1")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${BASELINE_LEN:--1}" = "0" ]; then
  MACRO_BASELINE_EMPTY=true
fi
append_summary "macro_baseline_len=$BASELINE_LEN"
append_summary "macro_baseline_empty=$MACRO_BASELINE_EMPTY"

MACRO_NAME="macro-greeting-${TIMESTAMP}"
MACRO_CONTENT_UPDATED="您好，工单已收到，预计24小时内回复"
request_capture "macro-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"'"$MACRO_NAME"'","content":"您好，您的工单已收到","language":"zh"}' \
  "$MACROS_BASE"
MACRO_ID="$(json_extract "d.get('id')")"
MACRO_ACTIVE="$(json_extract "d.get('active')")"
MACRO_LANG="$(json_extract "d.get('language','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$MACRO_ID" ] && [ "$MACRO_ID" != "None" ] \
  && [ "$MACRO_ACTIVE" = "True" ] && [ "$MACRO_LANG" = "zh" ]; then
  MACRO_CREATED=true
fi
append_summary "macro_id=$MACRO_ID macro_active=$MACRO_ACTIVE macro_language=$MACRO_LANG"
append_summary "macro_created=$MACRO_CREATED"

request_capture "macro-list" -H "$AUTH" "$MACROS_BASE"
LIST_LEN="$(json_extract "len(d) if isinstance(d, list) else -1")"
LIST_HAS_MACRO="$(json_extract "any(m.get('name') == '"$MACRO_NAME"' for m in (d if isinstance(d, list) else []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${LIST_LEN:--1}" = "1" ] && [ "$LIST_HAS_MACRO" = "True" ]; then
  MACRO_LISTED=true
fi
append_summary "macro_list_len=$LIST_LEN"
append_summary "macro_listed=$MACRO_LISTED"

request_capture "macro-update" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"content":"'"$MACRO_CONTENT_UPDATED"'"}' "$MACROS_BASE/$MACRO_ID"
UPDATED_CONTENT="$(json_extract "d.get('content','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$UPDATED_CONTENT" = "$MACRO_CONTENT_UPDATED" ]; then
  MACRO_UPDATED=true
fi
append_summary "macro_updated_content=$UPDATED_CONTENT"
append_summary "macro_updated=$MACRO_UPDATED"

request_capture "macro-apply" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":'"$TICKET_ID"',"user_id":1}' "$MACROS_BASE/$MACRO_ID/apply"
APPLY_TYPE="$(json_extract "d.get('type','')")"
APPLY_TICKET="$(json_extract "d.get('ticket_id', -1)")"
APPLY_CONTENT="$(json_extract "d.get('content','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$APPLY_TYPE" = "system" ] && [ "${APPLY_TICKET:--1}" = "$TICKET_ID" ] \
  && [ "$APPLY_CONTENT" = "$MACRO_CONTENT_UPDATED" ]; then
  MACRO_APPLIED=true
fi
append_summary "macro_apply_type=$APPLY_TYPE macro_apply_ticket=$APPLY_TICKET"
append_summary "macro_applied_to_ticket=$MACRO_APPLIED"

# 负例：应用到不存在的工单 → 404 ticket not found（handler isNotFoundError 匹配）。
request_capture "macro-apply-missing-ticket" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":999999}' "$MACROS_BASE/$MACRO_ID/apply"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "ticket not found"; then
  MACRO_MISSING_TICKET_REJECTED=true
fi
append_summary "macro_apply_missing_ticket_rejected_404=$MACRO_MISSING_TICKET_REJECTED"

# 停用后 apply → 400 macro inactive。
request_capture "macro-deactivate" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"active":false}' "$MACROS_BASE/$MACRO_ID"
DEACTIVATED="$(json_extract "d.get('active')")"
request_capture "macro-apply-inactive" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":'"$TICKET_ID"'}' "$MACROS_BASE/$MACRO_ID/apply"
if [ "$RESPONSE_STATUS" = "400" ] && [ "$DEACTIVATED" = "False" ] \
  && printf '%s' "$RESPONSE_RAW" | grep -q "macro inactive"; then
  MACRO_INACTIVE_APPLY_REJECTED=true
fi
append_summary "macro_inactive_apply_rejected_400=$MACRO_INACTIVE_APPLY_REJECTED"

# 恢复启用，保证删除路径作用在正常状态实体上。
request_capture "macro-reactivate" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"active":true}' "$MACROS_BASE/$MACRO_ID"
if [ "$RESPONSE_STATUS" != "200" ]; then
  echo "❌ 宏恢复启用失败" >&2
  exit 1
fi

request_capture "macro-delete" -X DELETE -H "$AUTH" "$MACROS_BASE/$MACRO_ID"
request_capture "macro-list-after-delete" -H "$AUTH" "$MACROS_BASE"
AFTER_LEN="$(json_extract "len(d) if isinstance(d, list) else -1")"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$(cat "$EVIDENCE_DIR/macro-delete.txt")" | grep -q '"deleted"' \
  && [ "${AFTER_LEN:--1}" = "0" ]; then
  MACRO_DELETED_AND_GONE=true
fi
append_summary "macro_list_len_after_delete=$AFTER_LEN"
append_summary "macro_deleted_and_gone=$MACRO_DELETED_AND_GONE"

# 5) 应用集成链路（§9）：slug 规范化创建 → 列表 → 重复 slug 负例 → 启用 → search → 删除。
append_summary "step=integration_chain"

request_capture "integration-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"Zapier Bridge","iframe_url":"https://hooks.zapier.com/bridge","capabilities":["sync"],"category":"automation"}' \
  "$INTEGRATIONS_BASE"
INTEGRATION_ID="$(json_extract "d.get('id')")"
INTEGRATION_SLUG="$(json_extract "d.get('slug','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$INTEGRATION_ID" ] && [ "$INTEGRATION_ID" != "None" ] \
  && [ "$INTEGRATION_SLUG" = "zapier-bridge" ]; then
  INTEGRATION_CREATED=true
fi
append_summary "integration_id=$INTEGRATION_ID integration_slug=$INTEGRATION_SLUG"
append_summary "integration_created=$INTEGRATION_CREATED"

request_capture "integration-list" -H "$AUTH" "$INTEGRATIONS_BASE"
INT_TOTAL="$(json_extract "d.get('total', -1)")"
INT_HAS_SLUG="$(json_extract "any(i.get('slug') == 'zapier-bridge' for i in d.get('data', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${INT_TOTAL:--1}" = "1" ] && [ "$INT_HAS_SLUG" = "True" ]; then
  INTEGRATION_LISTED=true
fi
append_summary "integration_list_total=$INT_TOTAL"
append_summary "integration_listed=$INTEGRATION_LISTED"

# 负例：重复 slug → 409 integration slug already exists（CreateIntegration 冲突语义）。
request_capture "integration-duplicate" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"Zapier Bridge Copy","slug":"zapier-bridge","iframe_url":"https://hooks.zapier.com/copy"}' \
  "$INTEGRATIONS_BASE"
if [ "$RESPONSE_STATUS" = "409" ] && printf '%s' "$RESPONSE_RAW" | grep -q "integration slug already exists"; then
  INTEGRATION_DUPLICATE_REJECTED=true
fi
append_summary "integration_duplicate_slug_rejected_409=$INTEGRATION_DUPLICATE_REJECTED"

request_capture "integration-update" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"enabled":true}' "$INTEGRATIONS_BASE/$INTEGRATION_ID"
INT_ENABLED="$(json_extract "d.get('enabled')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$INT_ENABLED" = "True" ]; then
  INTEGRATION_UPDATED_ENABLED=true
fi
append_summary "integration_enabled=$INT_ENABLED"
append_summary "integration_updated_enabled=$INTEGRATION_UPDATED_ENABLED"

# search 筛选（跨方言 LOWER LIKE 链路）：命中 1 条 + 无关词空对照。
request_capture "integration-search" -H "$AUTH" --get "$INTEGRATIONS_BASE" \
  --data-urlencode "search=zapier"
SEARCH_TOTAL="$(json_extract "d.get('total', -1)")"
request_capture "integration-search-nomatch" -H "$AUTH" --get "$INTEGRATIONS_BASE" \
  --data-urlencode "search=nomatchxyz"
NOMATCH_TOTAL="$(json_extract "d.get('total', -1)")"
if [ "${SEARCH_TOTAL:--1}" = "1" ] && [ "${NOMATCH_TOTAL:--1}" = "0" ]; then
  INTEGRATION_SEARCH_RECONCILED=true
fi
append_summary "integration_search_total=$SEARCH_TOTAL integration_search_nomatch_total=$NOMATCH_TOTAL"
append_summary "integration_search_reconciled=$INTEGRATION_SEARCH_RECONCILED"

request_capture "integration-delete" -X DELETE -H "$AUTH" "$INTEGRATIONS_BASE/$INTEGRATION_ID"
request_capture "integration-list-after-delete" -H "$AUTH" "$INTEGRATIONS_BASE"
INT_AFTER_TOTAL="$(json_extract "d.get('total', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$(cat "$EVIDENCE_DIR/integration-delete.txt")" | grep -q "integration deleted" \
  && [ "${INT_AFTER_TOTAL:--1}" = "0" ]; then
  INTEGRATION_DELETED_AND_GONE=true
fi
append_summary "integration_total_after_delete=$INT_AFTER_TOTAL"
append_summary "integration_deleted_and_gone=$INTEGRATION_DELETED_AND_GONE"

# 6) 自定义字段链路（§9）：创建 → 详情 → 列表 → 三类负例 → 更新 → 删除后 404。
append_summary "step=custom_field_chain"

CF_KEY="escalation_reason"
request_capture "customfield-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"key":"'"$CF_KEY"'","name":"升级原因","type":"string","required":true,"options":["容量不足","系统故障"]}' \
  "$CUSTOM_FIELDS_BASE"
CF_ID="$(json_extract "d.get('id')")"
CF_RESOURCE="$(json_extract "d.get('resource','')")"
CF_ACTIVE="$(json_extract "d.get('active')")"
CF_KEY_READBACK="$(json_extract "d.get('key','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$CF_ID" ] && [ "$CF_ID" != "None" ] \
  && [ "$CF_RESOURCE" = "ticket" ] && [ "$CF_ACTIVE" = "True" ] && [ "$CF_KEY_READBACK" = "$CF_KEY" ]; then
  CUSTOM_FIELD_CREATED=true
fi
append_summary "custom_field_id=$CF_ID custom_field_resource=$CF_RESOURCE"
append_summary "custom_field_created=$CUSTOM_FIELD_CREATED"

request_capture "customfield-get" -H "$AUTH" "$CUSTOM_FIELDS_BASE/$CF_ID"
CF_GET_NAME="$(json_extract "d.get('name','')")"
CF_GET_TYPE="$(json_extract "d.get('type','')")"
CF_GET_REQUIRED="$(json_extract "d.get('required')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$CF_GET_NAME" = "升级原因" ] && [ "$CF_GET_TYPE" = "string" ] \
  && [ "$CF_GET_REQUIRED" = "True" ]; then
  append_summary "custom_field_get_reconciled=true"
else
  append_summary "custom_field_get_reconciled=false"
fi

request_capture "customfield-list" -H "$AUTH" --get "$CUSTOM_FIELDS_BASE" \
  --data-urlencode "resource=ticket"
CF_LIST_LEN="$(json_extract "len(d) if isinstance(d, list) else -1")"
CF_LIST_HAS_KEY="$(json_extract "any(f.get('key') == '"$CF_KEY"' for f in (d if isinstance(d, list) else []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${CF_LIST_LEN:--1}" -ge 1 ] 2>/dev/null && [ "$CF_LIST_HAS_KEY" = "True" ]; then
  CUSTOM_FIELD_LISTED=true
fi
append_summary "custom_field_list_len=$CF_LIST_LEN"
append_summary "custom_field_listed=$CUSTOM_FIELD_LISTED"

# 负例 1：key 不满足 ^[a-z][a-z0-9_]*$ → 400 invalid key。
request_capture "customfield-negative-key" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"key":"9invalid","name":"非法键","type":"string"}' "$CUSTOM_FIELDS_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "invalid key"; then
  CUSTOM_FIELD_INVALID_KEY_REJECTED=true
fi
append_summary "custom_field_invalid_key_rejected_400=$CUSTOM_FIELD_INVALID_KEY_REJECTED"

# 负例 2：type 不在白名单 → 400 invalid type。
request_capture "customfield-negative-type" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"key":"bad_type","name":"非法类型","type":"unknown"}' "$CUSTOM_FIELDS_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "invalid type"; then
  CUSTOM_FIELD_INVALID_TYPE_REJECTED=true
fi
append_summary "custom_field_invalid_type_rejected_400=$CUSTOM_FIELD_INVALID_TYPE_REJECTED"

# 负例 3：resource 仅支持 ticket → 400 unsupported resource。
request_capture "customfield-negative-resource" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"key":"bad_resource","name":"非法资源","type":"string","resource":"customer"}' "$CUSTOM_FIELDS_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "unsupported resource"; then
  CUSTOM_FIELD_UNSUPPORTED_RESOURCE_REJECTED=true
fi
append_summary "custom_field_unsupported_resource_rejected_400=$CUSTOM_FIELD_UNSUPPORTED_RESOURCE_REJECTED"

request_capture "customfield-update" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"required":false}' "$CUSTOM_FIELDS_BASE/$CF_ID"
CF_REQUIRED_AFTER="$(json_extract "d.get('required')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$CF_REQUIRED_AFTER" = "False" ]; then
  CUSTOM_FIELD_UPDATED=true
fi
append_summary "custom_field_required_after=$CF_REQUIRED_AFTER"
append_summary "custom_field_updated=$CUSTOM_FIELD_UPDATED"

request_capture "customfield-delete" -X DELETE -H "$AUTH" "$CUSTOM_FIELDS_BASE/$CF_ID"
request_capture "customfield-get-after-delete" -H "$AUTH" "$CUSTOM_FIELDS_BASE/$CF_ID"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "Custom field not found"; then
  CUSTOM_FIELD_DELETED_GONE_404=true
fi
append_summary "custom_field_deleted_gone_404=$CUSTOM_FIELD_DELETED_GONE_404"

# 7) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED MACRO_BASELINE_EMPTY MACRO_CREATED MACRO_LISTED \
  MACRO_UPDATED MACRO_APPLIED MACRO_MISSING_TICKET_REJECTED MACRO_INACTIVE_APPLY_REJECTED \
  MACRO_DELETED_AND_GONE INTEGRATION_CREATED INTEGRATION_LISTED INTEGRATION_DUPLICATE_REJECTED \
  INTEGRATION_UPDATED_ENABLED INTEGRATION_SEARCH_RECONCILED INTEGRATION_DELETED_AND_GONE \
  CUSTOM_FIELD_CREATED CUSTOM_FIELD_LISTED CUSTOM_FIELD_INVALID_KEY_REJECTED \
  CUSTOM_FIELD_INVALID_TYPE_REJECTED CUSTOM_FIELD_UNSUPPORTED_RESOURCE_REJECTED \
  CUSTOM_FIELD_UPDATED CUSTOM_FIELD_DELETED_GONE_404; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Macro/Integration/CustomField acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Macro/Integration/CustomField acceptance 通过: 宏 CRUD 与 apply 对账(缺工单 404/停用 400 负例) / 集成 slug 规范化与重复 409 与 search 对账 / 自定义字段创建与三类负例与删除后 404 全部真实留档"
