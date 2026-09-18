#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Statistics/Shift acceptance 测试开始（P2-6 第四刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/statistics"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"st4-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"st4-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18101}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
TICKET_CATEGORY_STATS=false
TICKET_PRIORITY_STATS=false
AGENT_PERFORMANCE_STATS=false
AGENT_PERFORMANCE_MISSING_DATES_REJECTED=false
CUSTOMER_SOURCE_STATS=false
DAILY_STATS_UPDATED=false
DAILY_STATS_IDEMPOTENT=false
SHIFT_CREATED=false
SHIFT_MISSING_AGENT_REJECTED=false
SHIFT_INVALID_TIME_REJECTED=false
SHIFT_LISTED=false
SHIFT_UPDATED_ACTIVE=false
SHIFT_STATS_RECONCILED=false
SHIFT_DELETED_AND_GONE=false
SHIFT_MISSING_UPDATE_REJECTED=false
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
  MANIFEST_TICKET_CATEGORY="${TICKET_CATEGORY_STATS:-false}" \
  MANIFEST_TICKET_PRIORITY="${TICKET_PRIORITY_STATS:-false}" \
  MANIFEST_AGENT_PERFORMANCE="${AGENT_PERFORMANCE_STATS:-false}" \
  MANIFEST_AGENT_PERF_DATES="${AGENT_PERFORMANCE_MISSING_DATES_REJECTED:-false}" \
  MANIFEST_CUSTOMER_SOURCE="${CUSTOMER_SOURCE_STATS:-false}" \
  MANIFEST_DAILY_UPDATED="${DAILY_STATS_UPDATED:-false}" \
  MANIFEST_DAILY_IDEMPOTENT="${DAILY_STATS_IDEMPOTENT:-false}" \
  MANIFEST_SHIFT_CREATED="${SHIFT_CREATED:-false}" \
  MANIFEST_SHIFT_MISSING_AGENT="${SHIFT_MISSING_AGENT_REJECTED:-false}" \
  MANIFEST_SHIFT_INVALID_TIME="${SHIFT_INVALID_TIME_REJECTED:-false}" \
  MANIFEST_SHIFT_LISTED="${SHIFT_LISTED:-false}" \
  MANIFEST_SHIFT_UPDATED="${SHIFT_UPDATED_ACTIVE:-false}" \
  MANIFEST_SHIFT_STATS="${SHIFT_STATS_RECONCILED:-false}" \
  MANIFEST_SHIFT_DELETED="${SHIFT_DELETED_AND_GONE:-false}" \
  MANIFEST_SHIFT_MISSING_UPDATE="${SHIFT_MISSING_UPDATE_REJECTED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "statistics",
    "mode": "runtime-statistics-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "ticket_category_stats_reconciled": os.environ.get("MANIFEST_TICKET_CATEGORY", "false"),
        "ticket_priority_stats_reconciled": os.environ.get("MANIFEST_TICKET_PRIORITY", "false"),
        "agent_performance_stats_reconciled": os.environ.get("MANIFEST_AGENT_PERFORMANCE", "false"),
        "agent_performance_missing_dates_rejected_400": os.environ.get("MANIFEST_AGENT_PERF_DATES", "false"),
        "customer_source_stats_reconciled": os.environ.get("MANIFEST_CUSTOMER_SOURCE", "false"),
        "daily_stats_updated": os.environ.get("MANIFEST_DAILY_UPDATED", "false"),
        "daily_stats_idempotent": os.environ.get("MANIFEST_DAILY_IDEMPOTENT", "false"),
        "shift_created": os.environ.get("MANIFEST_SHIFT_CREATED", "false"),
        "shift_missing_agent_rejected_400": os.environ.get("MANIFEST_SHIFT_MISSING_AGENT", "false"),
        "shift_invalid_time_rejected_400": os.environ.get("MANIFEST_SHIFT_INVALID_TIME", "false"),
        "shift_listed": os.environ.get("MANIFEST_SHIFT_LISTED", "false"),
        "shift_updated_active": os.environ.get("MANIFEST_SHIFT_UPDATED", "false"),
        "shift_stats_reconciled": os.environ.get("MANIFEST_SHIFT_STATS", "false"),
        "shift_deleted_and_gone": os.environ.get("MANIFEST_SHIFT_DELETED", "false"),
        "shift_missing_update_rejected_404": os.environ.get("MANIFEST_SHIFT_MISSING_UPDATE", "false"),
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
Statistics/Shift acceptance summary (P2-6 slice 4)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/statistics-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/statistics-XXXXXX.sqlite")"

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
STATS_BASE="$SERVIFY_URL/api/statistics"
SHIFTS_BASE="$SERVIFY_URL/api/shifts"
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

# 3) 样本数据：admin、来源差化客户、分类/优先级差化工单、客服与工单分配。
append_summary "step=setup"

# N0：未认证读仪表盘 → 401。
request_capture "unauthenticated-dashboard" "$STATS_BASE/dashboard"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-dashboard.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"ST4 Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)
TODAY_UTC="$(date -u +%F)"
YESTERDAY_UTC="$(date -u -d yesterday +%F)"
TOMORROW_UTC="$(date -u -d tomorrow +%F)"
SHIFT_START="$(date -u +%Y-%m-%dT01:00:00Z)"
SHIFT_END="$(date -u +%Y-%m-%dT04:00:00Z)"

create_customer() {
  # $1 = 证据名后缀，$2 = 用户名，$3 = source
  request_capture "customer-create-$1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"username":"'"$2"'","email":"'"$2"'@servify.io","name":"统计客户'"$1"'","source":"'"$3"'"}' \
    "$SERVIFY_URL/api/customers"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 客户 $2 创建失败" >&2
    exit 1
  fi
  json_extract "d.get('id')"
}

CUST_WEB1="$(create_customer "web1" "st4-cust-web1-${TIMESTAMP}" "web")"
CUST_WEB2="$(create_customer "web2" "st4-cust-web2-${TIMESTAMP}" "web")"
CUST_REF1="$(create_customer "ref1" "st4-cust-ref1-${TIMESTAMP}" "referral")"
append_summary "cust_web1=$CUST_WEB1 cust_web2=$CUST_WEB2 cust_ref1=$CUST_REF1"

create_ticket() {
  # $1 = 证据名后缀，$2 = category，$3 = priority，$4 = customer_id
  request_capture "ticket-create-$1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"title":"统计工单'"$1"'","description":"统计聚合验收","category":"'"$2"'","priority":"'"$3"'","customer_id":'"$4"'}' \
    "$SERVIFY_URL/api/tickets"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 工单 $1 创建失败" >&2
    exit 1
  fi
  json_extract "d.get('id')"
}

TICKET_T1="$(create_ticket "t1" "technical" "high" "$CUST_WEB1")"
TICKET_T2="$(create_ticket "t2" "technical" "normal" "$CUST_WEB1")"
TICKET_T3="$(create_ticket "t3" "billing" "high" "$CUST_WEB2")"
append_summary "ticket_t1=$TICKET_T1 ticket_t2=$TICKET_T2 ticket_t3=$TICKET_T3"

register_agent_user() {
  local username=$1
  request_capture "agent-register-$username" -X POST -H "Content-Type: application/json" \
    -d '{"username":"'"$username"'","email":"'"$username"'@servify.io","password":"Acceptance!12345","name":"'"$username"'","role":"agent"}' \
    "$SERVIFY_URL/api/v1/auth/register"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ agent 用户 $username 注册失败" >&2
    exit 1
  fi
  json_extract "d.get('user',{}).get('id','')"
}

AGENT_A_USER="st4-agent-a-${TIMESTAMP}"
AGENT_B_USER="st4-agent-b-${TIMESTAMP}"
AGENT_A_ID="$(register_agent_user "$AGENT_A_USER")"
AGENT_B_ID="$(register_agent_user "$AGENT_B_USER")"

for agent_id in "$AGENT_A_ID" "$AGENT_B_ID"; do
  request_capture "agent-create-$agent_id" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"user_id":'"$agent_id"',"department":"acceptance","skills":"statistics","max_concurrent":5}' \
    "$SERVIFY_URL/api/agents"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 客服实体 $agent_id 创建失败" >&2
    exit 1
  fi
done
append_summary "agent_a=$AGENT_A_ID agent_b=$AGENT_B_ID"

# 分配工单要求客服可接单（status IN online/busy 且未满载），先上线。
set_agent_state() {
  local agent_id=$1 action=$2 prefix=$3
  request_capture "$prefix-$agent_id" -X POST -H "$AUTH" "$SERVIFY_URL/api/agents/$agent_id/$action"
  if [ "$RESPONSE_STATUS" != "200" ]; then
    echo "❌ 客服 $agent_id $action 失败" >&2
    exit 1
  fi
}
set_agent_state "$AGENT_A_ID" "online" "agent-online"
set_agent_state "$AGENT_B_ID" "online" "agent-online"
append_summary "agents_online=2"

assign_ticket() {
  # $1 = ticket id，$2 = agent user id，$3 = 证据名后缀
  request_capture "ticket-assign-$3" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"agent_id":'"$2"'}' "$SERVIFY_URL/api/tickets/$1/assign"
  if [ "$RESPONSE_STATUS" != "200" ]; then
    echo "❌ 工单 $1 分配失败" >&2
    exit 1
  fi
}

assign_ticket "$TICKET_T1" "$AGENT_A_ID" "t1-to-a"
assign_ticket "$TICKET_T2" "$AGENT_A_ID" "t2-to-a"
assign_ticket "$TICKET_T3" "$AGENT_B_ID" "t3-to-b"
append_summary "tickets_assigned=3 (A:2 B:1)"

# 4) 统计聚合（§10）：分类/优先级/绩效/来源对账。
append_summary "step=stats_aggregation"

request_capture "stats-category" -H "$AUTH" "$STATS_BASE/ticket-category"
CAT_TECH="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'technical']), {}).get('count', -1)")"
CAT_BILL="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'billing']), {}).get('count', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${CAT_TECH:--1}" = "2" ] && [ "${CAT_BILL:--1}" = "1" ]; then
  TICKET_CATEGORY_STATS=true
fi
append_summary "category_technical=$CAT_TECH category_billing=$CAT_BILL"
append_summary "ticket_category_stats_reconciled=$TICKET_CATEGORY_STATS"

request_capture "stats-priority" -H "$AUTH" "$STATS_BASE/ticket-priority"
PRIO_HIGH="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'high']), {}).get('count', -1)")"
PRIO_NORMAL="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'normal']), {}).get('count', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${PRIO_HIGH:--1}" = "2" ] && [ "${PRIO_NORMAL:--1}" = "1" ]; then
  TICKET_PRIORITY_STATS=true
fi
append_summary "priority_high=$PRIO_HIGH priority_normal=$PRIO_NORMAL"
append_summary "ticket_priority_stats_reconciled=$TICKET_PRIORITY_STATS"

request_capture "stats-agent-perf" -H "$AUTH" --get "$STATS_BASE/agent-performance" \
  --data-urlencode "start_date=$YESTERDAY_UTC" --data-urlencode "end_date=$TOMORROW_UTC"
PERF_A_TOTAL="$(json_extract "next(iter([a for a in (d if isinstance(d, list) else []) if a.get('agent_id') == $AGENT_A_ID]), {}).get('total_tickets', -1)")"
PERF_B_TOTAL="$(json_extract "next(iter([a for a in (d if isinstance(d, list) else []) if a.get('agent_id') == $AGENT_B_ID]), {}).get('total_tickets', -1)")"
PERF_A_INDEX="$(json_extract "next((i for i, a in enumerate(d) if a.get('agent_id') == $AGENT_A_ID), -1)")"
PERF_B_INDEX="$(json_extract "next((i for i, a in enumerate(d) if a.get('agent_id') == $AGENT_B_ID), -1)")"
# total_tickets DESC 排序：A（2 单）必须排在 B（1 单）之前。
if [ "$RESPONSE_STATUS" = "200" ] && [ "${PERF_A_TOTAL:--1}" = "2" ] && [ "${PERF_B_TOTAL:--1}" = "1" ] \
  && [ "${PERF_A_INDEX:--1}" -ge 0 ] 2>/dev/null && [ "${PERF_A_INDEX:--1}" -lt "${PERF_B_INDEX:--1}" ]; then
  AGENT_PERFORMANCE_STATS=true
fi
append_summary "perf_a_total=$PERF_A_TOTAL perf_b_total=$PERF_B_TOTAL perf_a_index=$PERF_A_INDEX perf_b_index=$PERF_B_INDEX"
append_summary "agent_performance_stats_reconciled=$AGENT_PERFORMANCE_STATS"

# 负例：缺必填日期 → 400。
request_capture "stats-agent-perf-missing-dates" -H "$AUTH" "$STATS_BASE/agent-performance"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "start_date and end_date are required"; then
  AGENT_PERFORMANCE_MISSING_DATES_REJECTED=true
fi
append_summary "agent_performance_missing_dates_rejected_400=$AGENT_PERFORMANCE_MISSING_DATES_REJECTED"

request_capture "stats-source" -H "$AUTH" "$STATS_BASE/customer-source"
SRC_WEB="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'web']), {}).get('count', -1)")"
SRC_REF="$(json_extract "next(iter([c for c in (d if isinstance(d, list) else []) if c.get('category') == 'referral']), {}).get('count', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${SRC_WEB:--1}" = "2" ] && [ "${SRC_REF:--1}" = "1" ]; then
  CUSTOMER_SOURCE_STATS=true
fi
append_summary "source_web=$SRC_WEB source_referral=$SRC_REF"
append_summary "customer_source_stats_reconciled=$CUSTOMER_SOURCE_STATS"

# 5) 每日统计更新（§10）：指定日期更新 + 默认日期幂等。
append_summary "step=daily_update"
request_capture "daily-update" -X POST -H "$AUTH" --get "$STATS_BASE/update-daily" \
  --data-urlencode "date=$TODAY_UTC"
DAILY_MSG="$(json_extract "d.get('message','')")"
DAILY_DATE="$(json_extract "d.get('date','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$DAILY_MSG" = "Daily statistics updated successfully" ] && [ "$DAILY_DATE" = "$TODAY_UTC" ]; then
  DAILY_STATS_UPDATED=true
fi
append_summary "daily_msg=$DAILY_MSG daily_date=$DAILY_DATE"
append_summary "daily_stats_updated=$DAILY_STATS_UPDATED"

request_capture "daily-update-default" -X POST -H "$AUTH" "$STATS_BASE/update-daily"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$(json_extract "d.get('message','')")" = "Daily statistics updated successfully" ]; then
  DAILY_STATS_IDEMPOTENT=true
fi
append_summary "daily_stats_idempotent=$DAILY_STATS_IDEMPOTENT"

# 6) 排班 CRUD 与统计（§10）：创建 → 列表 → 更新 → 统计 → 删除对账。
append_summary "step=shift_crud"

request_capture "shift-stats-base" -H "$AUTH" "$SHIFTS_BASE/stats"
SHIFT_BASE_TOTAL="$(json_extract "d.get('total', -1)")"
if [ "$RESPONSE_STATUS" != "200" ] || [ "${SHIFT_BASE_TOTAL:--1}" != "0" ]; then
  echo "❌ 排班基线非空（total=$SHIFT_BASE_TOTAL），无法精确对账" >&2
  exit 1
fi

request_capture "shift-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"agent_id":'"$AGENT_A_ID"',"shift_type":"morning","start_time":"'"$SHIFT_START"'","end_time":"'"$SHIFT_END"'"}' \
  "$SHIFTS_BASE"
SHIFT_ID="$(json_extract "d.get('id')")"
SHIFT_STATUS="$(json_extract "d.get('status','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$SHIFT_ID" ] && [ "$SHIFT_ID" != "None" ] && [ "$SHIFT_STATUS" = "scheduled" ]; then
  SHIFT_CREATED=true
fi
append_summary "shift_id=$SHIFT_ID shift_status=$SHIFT_STATUS"
append_summary "shift_created=$SHIFT_CREATED"

# 负例：客服不存在 → 400（CreateShift 错误统一 400）。
request_capture "shift-create-missing-agent" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"agent_id":999999,"shift_type":"morning","start_time":"'"$SHIFT_START"'","end_time":"'"$SHIFT_END"'"}' \
  "$SHIFTS_BASE"
MISSING_AGENT_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$MISSING_AGENT_MSG" | grep -q "agent not found"; then
  SHIFT_MISSING_AGENT_REJECTED=true
fi
append_summary "shift_missing_agent_rejected_400=$SHIFT_MISSING_AGENT_REJECTED"

# 负例：结束时间不晚于开始时间 → 400。
request_capture "shift-create-invalid-time" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"agent_id":'"$AGENT_A_ID"',"shift_type":"morning","start_time":"'"$SHIFT_END"'","end_time":"'"$SHIFT_START"'"}' \
  "$SHIFTS_BASE"
INVALID_TIME_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$INVALID_TIME_MSG" | grep -q "end_time must be after start_time"; then
  SHIFT_INVALID_TIME_REJECTED=true
fi
append_summary "shift_invalid_time_rejected_400=$SHIFT_INVALID_TIME_REJECTED"

request_capture "shift-list" -H "$AUTH" --get "$SHIFTS_BASE" --data-urlencode "agent_id=$AGENT_A_ID" \
  --data-urlencode "page=1" --data-urlencode "page_size=10"
LIST_TOTAL="$(json_extract "d.get('total', 0)")"
LIST_HAS_SHIFT="$(json_extract "any(s.get('id') == $SHIFT_ID for s in d.get('data', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${LIST_TOTAL:-0}" = "1" ] && [ "$LIST_HAS_SHIFT" = "True" ]; then
  SHIFT_LISTED=true
fi
append_summary "shift_list_total=$LIST_TOTAL"
append_summary "shift_listed=$SHIFT_LISTED"

request_capture "shift-update" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"status":"active"}' "$SHIFTS_BASE/$SHIFT_ID"
UPDATED_STATUS="$(json_extract "d.get('status','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$UPDATED_STATUS" = "active" ]; then
  SHIFT_UPDATED_ACTIVE=true
fi
append_summary "shift_updated_active=$SHIFT_UPDATED_ACTIVE"

# 负例：更新不存在的班次 → 404。
request_capture "shift-update-missing" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"status":"active"}' "$SHIFTS_BASE/999999"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "shift not found"; then
  SHIFT_MISSING_UPDATE_REJECTED=true
fi
append_summary "shift_missing_update_rejected_404=$SHIFT_MISSING_UPDATE_REJECTED"

request_capture "shift-stats-active" -H "$AUTH" "$SHIFTS_BASE/stats"
STATS_TOTAL="$(json_extract "d.get('total', -1)")"
STATS_MORNING="$(json_extract "d.get('by_type', {}).get('morning', -1)")"
STATS_ACTIVE="$(json_extract "d.get('by_status', {}).get('active', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${STATS_TOTAL:--1}" = "1" ] \
  && [ "${STATS_MORNING:--1}" = "1" ] && [ "${STATS_ACTIVE:--1}" = "1" ]; then
  SHIFT_STATS_RECONCILED=true
fi
append_summary "shift_stats_total=$STATS_TOTAL by_type_morning=$STATS_MORNING by_status_active=$STATS_ACTIVE"
append_summary "shift_stats_reconciled=$SHIFT_STATS_RECONCILED"

request_capture "shift-delete" -X DELETE -H "$AUTH" "$SHIFTS_BASE/$SHIFT_ID"
request_capture "shift-stats-after-delete" -H "$AUTH" "$SHIFTS_BASE/stats"
AFTER_TOTAL="$(json_extract "d.get('total', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$(cat "$EVIDENCE_DIR/shift-delete.txt")" | grep -q "班次删除成功" \
  && [ "${AFTER_TOTAL:--1}" = "0" ]; then
  SHIFT_DELETED_AND_GONE=true
fi
append_summary "shift_total_after_delete=$AFTER_TOTAL"
append_summary "shift_deleted_and_gone=$SHIFT_DELETED_AND_GONE"

# 7) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED TICKET_CATEGORY_STATS TICKET_PRIORITY_STATS \
  AGENT_PERFORMANCE_STATS AGENT_PERFORMANCE_MISSING_DATES_REJECTED \
  CUSTOMER_SOURCE_STATS DAILY_STATS_UPDATED DAILY_STATS_IDEMPOTENT \
  SHIFT_CREATED SHIFT_MISSING_AGENT_REJECTED SHIFT_INVALID_TIME_REJECTED \
  SHIFT_LISTED SHIFT_UPDATED_ACTIVE SHIFT_STATS_RECONCILED \
  SHIFT_DELETED_AND_GONE SHIFT_MISSING_UPDATE_REJECTED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Statistics/Shift acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Statistics/Shift acceptance 通过: 分类/优先级/绩效/来源聚合对账 / 缺日期负例 / 每日更新与幂等 / 排班 CRUD 与统计全对账 / 不存在客服与非法时间与不存在班次负例 全部真实留档"
