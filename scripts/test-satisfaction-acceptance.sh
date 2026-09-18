#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Satisfaction acceptance 测试开始（P2-6 第二刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/satisfaction"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"sat-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"sat-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18099}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
CUSTOMER_CREATED=false
TICKET_CREATED=false
TICKET_CLOSED_SURVEY_SCHEDULED=false
SATISFACTION_CREATED=false
DUPLICATE_REJECTED_409=false
NON_OWNER_REJECTED_403=false
SATISFACTION_LISTED=false
STATS_RECONCILED=false
SURVEYS_LISTED=false
SURVEY_RESENT=false
DETAIL_RETRIEVABLE=false
COMMENT_UPDATED=false
BY_TICKET_RETRIEVABLE=false
DELETED_AND_GONE=false
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
  MANIFEST_CUSTOMER_CREATED="${CUSTOMER_CREATED:-false}" \
  MANIFEST_TICKET_CREATED="${TICKET_CREATED:-false}" \
  MANIFEST_TICKET_CLOSED="${TICKET_CLOSED_SURVEY_SCHEDULED:-false}" \
  MANIFEST_SATISFACTION_CREATED="${SATISFACTION_CREATED:-false}" \
  MANIFEST_DUPLICATE_REJECTED="${DUPLICATE_REJECTED_409:-false}" \
  MANIFEST_NON_OWNER_REJECTED="${NON_OWNER_REJECTED_403:-false}" \
  MANIFEST_LISTED="${SATISFACTION_LISTED:-false}" \
  MANIFEST_STATS_OK="${STATS_RECONCILED:-false}" \
  MANIFEST_SURVEYS_LISTED="${SURVEYS_LISTED:-false}" \
  MANIFEST_SURVEY_RESENT="${SURVEY_RESENT:-false}" \
  MANIFEST_DETAIL_OK="${DETAIL_RETRIEVABLE:-false}" \
  MANIFEST_COMMENT_UPDATED="${COMMENT_UPDATED:-false}" \
  MANIFEST_BY_TICKET_OK="${BY_TICKET_RETRIEVABLE:-false}" \
  MANIFEST_DELETED_AND_GONE="${DELETED_AND_GONE:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "satisfaction",
    "mode": "runtime-satisfaction-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "customer_created": os.environ.get("MANIFEST_CUSTOMER_CREATED", "false"),
        "ticket_created": os.environ.get("MANIFEST_TICKET_CREATED", "false"),
        "ticket_closed_survey_scheduled": os.environ.get("MANIFEST_TICKET_CLOSED", "false"),
        "satisfaction_created": os.environ.get("MANIFEST_SATISFACTION_CREATED", "false"),
        "duplicate_satisfaction_rejected_409": os.environ.get("MANIFEST_DUPLICATE_REJECTED", "false"),
        "non_owner_satisfaction_rejected_403": os.environ.get("MANIFEST_NON_OWNER_REJECTED", "false"),
        "satisfaction_listed": os.environ.get("MANIFEST_LISTED", "false"),
        "satisfaction_stats_reconciled": os.environ.get("MANIFEST_STATS_OK", "false"),
        "surveys_listed": os.environ.get("MANIFEST_SURVEYS_LISTED", "false"),
        "survey_resent": os.environ.get("MANIFEST_SURVEY_RESENT", "false"),
        "satisfaction_detail_retrievable": os.environ.get("MANIFEST_DETAIL_OK", "false"),
        "satisfaction_comment_updated": os.environ.get("MANIFEST_COMMENT_UPDATED", "false"),
        "satisfaction_by_ticket_retrievable": os.environ.get("MANIFEST_BY_TICKET_OK", "false"),
        "satisfaction_deleted_and_gone": os.environ.get("MANIFEST_DELETED_AND_GONE", "false"),
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
Satisfaction acceptance summary (P2-6 slice 2)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/satisfaction-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/satisfaction-XXXXXX.sqlite")"

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
SAT_BASE="$SERVIFY_URL/api/satisfactions"
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

# 3) 用户与客户。
append_summary "step=setup"

# N0：未认证读满意度列表 → 401。
request_capture "unauthenticated-list" "$SAT_BASE"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-list.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"SAT Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)
# POST /api/customers 一次创建 user+customer（注册接口不会自动建 customers 行）。
request_capture "customer-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"sat-cust-'"$TIMESTAMP"'","email":"sat-cust-'"$TIMESTAMP"'@servify.io","name":"验收客户"}' \
  "$SERVIFY_URL/api/customers"
CUSTOMER_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$CUSTOMER_ID" ] && [ "$CUSTOMER_ID" != "None" ]; then
  CUSTOMER_CREATED=true
fi
append_summary "customer_id=$CUSTOMER_ID customer_created=$CUSTOMER_CREATED"

# 非所有者负例用的第二个客户。
request_capture "customer-create-other" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"sat-other-'"$TIMESTAMP"'","email":"sat-other-'"$TIMESTAMP"'@servify.io","name":"非所有者客户"}' \
  "$SERVIFY_URL/api/customers"
OTHER_CUSTOMER_ID="$(json_extract "d.get('id')")"
append_summary "other_customer_id=$OTHER_CUSTOMER_ID"

# 4) 工单与调查调度。
append_summary "step=ticket_and_survey"
request_capture "ticket-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"满意度验收工单","description":"用于满意度链路验收","customer_id":'"$CUSTOMER_ID"'}' \
  "$SERVIFY_URL/api/tickets"
TICKET_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$TICKET_ID" ] && [ "$TICKET_ID" != "None" ]; then
  TICKET_CREATED=true
fi
append_summary "ticket_id=$TICKET_ID ticket_created=$TICKET_CREATED"

# 关闭工单 → 编排器自动调度 CSAT 调查（调度成功在第 6 步 surveys 列表确认）。
request_capture "ticket-close" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"reason":"验收：关闭触发 CSAT 调查"}' "$SERVIFY_URL/api/tickets/$TICKET_ID/close"
CLOSE_STATUS=$RESPONSE_STATUS
append_summary "ticket_close_status=$CLOSE_STATUS"

# 5) 满意度评价主链路。
append_summary "step=satisfaction_crud"
request_capture "satisfaction-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":'"$TICKET_ID"',"customer_id":'"$CUSTOMER_ID"',"rating":5,"comment":"服务很好","category":"service_quality"}' \
  "$SAT_BASE"
SAT_ID="$(json_extract "d.get('id')")"
SAT_RATING="$(json_extract "d.get('rating')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$SAT_ID" ] && [ "$SAT_ID" != "None" ] && [ "$SAT_RATING" = "5" ]; then
  SATISFACTION_CREATED=true
fi
append_summary "satisfaction_id=$SAT_ID satisfaction_created=$SATISFACTION_CREATED"

# 负例：同工单重复评价 → 409。
request_capture "satisfaction-duplicate" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":'"$TICKET_ID"',"customer_id":'"$CUSTOMER_ID"',"rating":4,"comment":"重复评价"}' \
  "$SAT_BASE"
if [ "$RESPONSE_STATUS" = "409" ] && printf '%s' "$RESPONSE_RAW" | grep -q "already exists"; then
  DUPLICATE_REJECTED_409=true
fi
append_summary "duplicate_satisfaction_rejected_409=$DUPLICATE_REJECTED_409"

# 负例：非工单所有者的客户评价 → 403。
request_capture "satisfaction-non-owner" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"ticket_id":'"$TICKET_ID"',"customer_id":'"$OTHER_CUSTOMER_ID"',"rating":3,"comment":"非所有者"}' \
  "$SAT_BASE"
if [ "$RESPONSE_STATUS" = "403" ] && printf '%s' "$RESPONSE_RAW" | grep -q "not the owner"; then
  NON_OWNER_REJECTED_403=true
fi
append_summary "non_owner_satisfaction_rejected_403=$NON_OWNER_REJECTED_403"

request_capture "satisfaction-list" -H "$AUTH" "$SAT_BASE?page=1&page_size=10"
LIST_TOTAL="$(json_extract "d.get('total', 0)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${LIST_TOTAL:-0}" -ge 1 ]; then
  SATISFACTION_LISTED=true
fi
append_summary "list_total=$LIST_TOTAL satisfaction_listed=$SATISFACTION_LISTED"

request_capture "satisfaction-detail" -H "$AUTH" "$SAT_BASE/$SAT_ID"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$(json_extract "d.get('id')")" = "$SAT_ID" ]; then
  DETAIL_RETRIEVABLE=true
fi
append_summary "satisfaction_detail_retrievable=$DETAIL_RETRIEVABLE"

request_capture "satisfaction-update" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"comment":"更新后的评价内容"}' "$SAT_BASE/$SAT_ID"
UPDATED_COMMENT="$(json_extract "d.get('comment','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$UPDATED_COMMENT" = "更新后的评价内容" ]; then
  COMMENT_UPDATED=true
fi
append_summary "satisfaction_comment_updated=$COMMENT_UPDATED"

request_capture "satisfaction-by-ticket" -H "$AUTH" "$SERVIFY_URL/api/tickets/$TICKET_ID/satisfaction"
BY_TICKET_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$BY_TICKET_ID" = "$SAT_ID" ]; then
  BY_TICKET_RETRIEVABLE=true
fi
append_summary "satisfaction_by_ticket_retrievable=$BY_TICKET_RETRIEVABLE"

# 6) 统计与调查。
append_summary "step=stats_and_surveys"
request_capture "satisfaction-stats" -H "$AUTH" "$SAT_BASE/stats"
STATS_TOTAL="$(json_extract "d.get('total_ratings', 0)")"
STATS_AVG="$(json_extract "d.get('average_rating', 0)")"
STATS_DIST5="$(json_extract "d.get('rating_distribution', {}).get('5', 0)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${STATS_TOTAL:-0}" -ge 1 ] && python3 -c "exit(0 if float('${STATS_AVG:-0}') > 0 else 1)" 2>/dev/null; then
  STATS_RECONCILED=true
fi
append_summary "stats_total=$STATS_TOTAL stats_avg=$STATS_AVG stats_rating5=$STATS_DIST5"
append_summary "satisfaction_stats_reconciled=$STATS_RECONCILED"

request_capture "surveys-list" -H "$AUTH" "$SAT_BASE/surveys?page=1&page_size=10"
SURVEY_TOTAL="$(json_extract "d.get('total', 0)")"
SURVEY_ID="$(json_extract "next(iter([s for s in d.get('data', []) if s.get('ticket_id') == $TICKET_ID]), {}).get('id', '')")"
SURVEY_STATUS="$(json_extract "next(iter([s for s in d.get('data', []) if s.get('ticket_id') == $TICKET_ID]), {}).get('status', '')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${SURVEY_TOTAL:-0}" -ge 1 ] && [ -n "$SURVEY_ID" ]; then
  SURVEYS_LISTED=true
fi
# 关闭工单的副作用:该工单的 CSAT 调查已调度(mailer 未注入时直接置 sent)。
if [ "$CLOSE_STATUS" = "200" ] && [ "$SURVEY_STATUS" = "sent" ]; then
  TICKET_CLOSED_SURVEY_SCHEDULED=true
fi
append_summary "survey_total=$SURVEY_TOTAL survey_id=$SURVEY_ID survey_status=$SURVEY_STATUS"
append_summary "surveys_listed=$SURVEYS_LISTED ticket_closed_survey_scheduled=$TICKET_CLOSED_SURVEY_SCHEDULED"

request_capture "survey-resend" -X POST -H "$AUTH" "$SAT_BASE/surveys/$SURVEY_ID/resend"
RESENT_TOKEN="$(json_extract "d.get('survey_token','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ -n "$RESENT_TOKEN" ] && [ "$RESENT_TOKEN" != "None" ]; then
  SURVEY_RESENT=true
fi
append_summary "survey_resent=$SURVEY_RESENT"

# 7) 删除与数据证据。
append_summary "step=delete_and_verify"
request_capture "satisfaction-delete" -X DELETE -H "$AUTH" "$SAT_BASE/$SAT_ID"
DELETE_STATUS=$RESPONSE_STATUS
request_capture "satisfaction-by-ticket-after-delete" -H "$AUTH" "$SERVIFY_URL/api/tickets/$TICKET_ID/satisfaction"
BY_TICKET_AFTER=$RESPONSE_STATUS
request_capture "satisfaction-detail-after-delete" -H "$AUTH" "$SAT_BASE/$SAT_ID"
DETAIL_AFTER=$RESPONSE_STATUS
if [ "$DELETE_STATUS" = "204" ] && [ "$BY_TICKET_AFTER" = "204" ] && [ "$DETAIL_AFTER" = "404" ]; then
  DELETED_AND_GONE=true
fi
append_summary "delete_status=$DELETE_STATUS by_ticket_after_delete=$BY_TICKET_AFTER detail_after_delete=$DETAIL_AFTER"
append_summary "satisfaction_deleted_and_gone=$DELETED_AND_GONE"

# 8) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED CUSTOMER_CREATED TICKET_CREATED \
  TICKET_CLOSED_SURVEY_SCHEDULED SATISFACTION_CREATED DUPLICATE_REJECTED_409 \
  NON_OWNER_REJECTED_403 SATISFACTION_LISTED STATS_RECONCILED SURVEYS_LISTED \
  SURVEY_RESENT DETAIL_RETRIEVABLE COMMENT_UPDATED BY_TICKET_RETRIEVABLE \
  DELETED_AND_GONE; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Satisfaction acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Satisfaction acceptance 通过: 建客户/工单/关闭调度调查 / 评价 CRUD / 重复与非所有者负例 / 统计对账 / 调查重发 / 删除后数据证据 全部真实留档"
