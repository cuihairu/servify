#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Automation/Gamification acceptance 测试开始（P2-6 第七刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/automation-gamification"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"st7-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"st7-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18104}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_AUTOMATIONS_REJECTED=false
AGENTS_PREPARED=false
AUTOMATION_BASELINE_EMPTY=false
AUTOMATION_CREATED=false
AUTOMATION_UNSUPPORTED_EVENT_REJECTED=false
AUTOMATION_EMPTY_NAME_REJECTED=false
AUTOMATION_DELAY_MISPLACED_REJECTED=false
AUTOMATION_LISTED=false
AUTOMATION_DRY_RUN_NO_SIDE_EFFECT=false
AUTOMATION_RUN_APPLIED=false
AUTOMATION_RUNS_LISTED=false
AUTOMATION_RUN_MISSING_TRIGGER_REJECTED=false
AUTOMATION_DELETED_AND_GONE=false
AUTOMATION_DELETE_MISSING_REJECTED=false
GAMIFICATION_UNAUTH_REJECTED=false
LEADERBOARD_RECONCILED=false
LEADERBOARD_DATE_RANGE_RECONCILED=false
LEADERBOARD_BAD_DATE_REJECTED=false
LEADERBOARD_DEPARTMENT_FILTERED=false
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

# require_2xx：样本链关键请求的硬校验。CI 负载下 curl --max-time 5 可能
# 超时（status 为空）——若静默传导，数据缺失会伪装成 LEADERBOARD_RECONCILED
# 失败，无法定位；这里在源头立即失败并留档。
require_2xx() {
  local evidence_name=$1
  case "$RESPONSE_STATUS" in
    2??) ;;
    *)
      append_summary "request_failed=$evidence_name status=${RESPONSE_STATUS:-none}"
      echo "❌ 请求失败: $evidence_name (status=${RESPONSE_STATUS:-none})，evidence: $EVIDENCE_DIR/$evidence_name.txt" >&2
      exit 1
      ;;
  esac
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
  MANIFEST_UNAUTH="${UNAUTH_AUTOMATIONS_REJECTED:-false}" \
  MANIFEST_AGENTS="${AGENTS_PREPARED:-false}" \
  MANIFEST_BASELINE="${AUTOMATION_BASELINE_EMPTY:-false}" \
  MANIFEST_CREATED="${AUTOMATION_CREATED:-false}" \
  MANIFEST_UNSUPPORTED="${AUTOMATION_UNSUPPORTED_EVENT_REJECTED:-false}" \
  MANIFEST_EMPTY_NAME="${AUTOMATION_EMPTY_NAME_REJECTED:-false}" \
  MANIFEST_DELAY_MISPLACED="${AUTOMATION_DELAY_MISPLACED_REJECTED:-false}" \
  MANIFEST_LISTED="${AUTOMATION_LISTED:-false}" \
  MANIFEST_DRY_RUN="${AUTOMATION_DRY_RUN_NO_SIDE_EFFECT:-false}" \
  MANIFEST_RUN_APPLIED="${AUTOMATION_RUN_APPLIED:-false}" \
  MANIFEST_RUNS_LISTED="${AUTOMATION_RUNS_LISTED:-false}" \
  MANIFEST_RUN_MISSING="${AUTOMATION_RUN_MISSING_TRIGGER_REJECTED:-false}" \
  MANIFEST_DELETED="${AUTOMATION_DELETED_AND_GONE:-false}" \
  MANIFEST_DELETE_MISSING="${AUTOMATION_DELETE_MISSING_REJECTED:-false}" \
  MANIFEST_GAM_UNAUTH="${GAMIFICATION_UNAUTH_REJECTED:-false}" \
  MANIFEST_LB_RECONCILED="${LEADERBOARD_RECONCILED:-false}" \
  MANIFEST_LB_DATES="${LEADERBOARD_DATE_RANGE_RECONCILED:-false}" \
  MANIFEST_LB_BAD_DATE="${LEADERBOARD_BAD_DATE_REJECTED:-false}" \
  MANIFEST_LB_DEPT="${LEADERBOARD_DEPARTMENT_FILTERED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "automation-gamification",
    "mode": "runtime-automation-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_automations_rejected_401": os.environ.get("MANIFEST_UNAUTH", "false"),
        "agents_and_customers_prepared": os.environ.get("MANIFEST_AGENTS", "false"),
        "automation_baseline_empty": os.environ.get("MANIFEST_BASELINE", "false"),
        "automation_created_with_normalized_event": os.environ.get("MANIFEST_CREATED", "false"),
        "automation_unsupported_event_rejected_400": os.environ.get("MANIFEST_UNSUPPORTED", "false"),
        "automation_empty_name_rejected_400": os.environ.get("MANIFEST_EMPTY_NAME", "false"),
        "automation_delay_misplaced_rejected_400": os.environ.get("MANIFEST_DELAY_MISPLACED", "false"),
        "automation_listed": os.environ.get("MANIFEST_LISTED", "false"),
        "automation_dry_run_matched_without_side_effect": os.environ.get("MANIFEST_DRY_RUN", "false"),
        "automation_run_applied_to_ticket": os.environ.get("MANIFEST_RUN_APPLIED", "false"),
        "automation_runs_listed": os.environ.get("MANIFEST_RUNS_LISTED", "false"),
        "automation_run_missing_trigger_rejected_400": os.environ.get("MANIFEST_RUN_MISSING", "false"),
        "automation_deleted_and_gone": os.environ.get("MANIFEST_DELETED", "false"),
        "automation_delete_missing_rejected_404": os.environ.get("MANIFEST_DELETE_MISSING", "false"),
        "gamification_unauthenticated_rejected_401": os.environ.get("MANIFEST_GAM_UNAUTH", "false"),
        "leaderboard_reconciled_with_scores": os.environ.get("MANIFEST_LB_RECONCILED", "false"),
        "leaderboard_date_range_reconciled": os.environ.get("MANIFEST_LB_DATES", "false"),
        "leaderboard_bad_date_rejected_400": os.environ.get("MANIFEST_LB_BAD_DATE", "false"),
        "leaderboard_department_filtered": os.environ.get("MANIFEST_LB_DEPT", "false"),
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
Automation/Gamification acceptance summary (P2-6 slice 7)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/automation-gamification-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/automation-gamification-XXXXXX.sqlite")"

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
AUTOMATION_BASE="$SERVIFY_URL/api/automations"
LB_BASE="$SERVIFY_URL/api/gamification/leaderboard"
AGENTS_BASE="$SERVIFY_URL/api/agents"
TICKETS_BASE="$SERVIFY_URL/api/tickets"
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

# 3) 样本数据：admin、两个客服（激励排行输入）。
append_summary "step=setup"

# N0：未认证读自动化列表 → 401。
request_capture "unauthenticated-automations" "$AUTOMATION_BASE"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-automations.txt" && UNAUTH_AUTOMATIONS_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_AUTOMATIONS_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"ST7 Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)

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

AGENT_A_USER="st7-agent-a-${TIMESTAMP}"
AGENT_B_USER="st7-agent-b-${TIMESTAMP}"
AGENT_A_ID="$(register_agent_user "$AGENT_A_USER")"
AGENT_B_ID="$(register_agent_user "$AGENT_B_USER")"

request_capture "agent-create-a1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_A_ID"',"department":"st7-dept","skills":"transfer,billing","max_concurrent":5}' \
  "$AGENTS_BASE"
AGENT_A_PROFILE_ID="$(json_extract "d.get('id')")"
request_capture "agent-create-a2" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_B_ID"',"department":"st7-dept","skills":"transfer","max_concurrent":5}' \
  "$AGENTS_BASE"
AGENT_B_PROFILE_ID="$(json_extract "d.get('id')")"

# 工单 assign 的可用性校验要求 agent 处于 online/busy（默认 offline 不可派单）。
# /api/agents/:id/online 的 :id 是 user_id（handler 直接透传 service GoOnline(userID)）。
request_capture "agent-a1-online" -X POST -H "$AUTH" "$AGENTS_BASE/$AGENT_A_ID/online"
request_capture "agent-a2-online" -X POST -H "$AUTH" "$AGENTS_BASE/$AGENT_B_ID/online"

create_customer() {
  local name=$1
  request_capture "customer-create-$name" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"username":"st7-cust-'"$name"'-'"$TIMESTAMP"'","email":"st7-cust-'"$name"'-'"$TIMESTAMP"'@servify.io","name":"ST7 客户 '"$name"'","source":"web"}' \
    "$SERVIFY_URL/api/customers"
  json_extract "d.get('id')"
}

CUSTOMER_OP_ID="$(create_customer op)"
CUSTOMER_A_ID="$(create_customer a)"
CUSTOMER_B_ID="$(create_customer b)"

# 注意：create_customer 在 $(...) 子 shell 里跑，父 shell 的 RESPONSE_STATUS
# 仍是 last online 调用的 200 —— 这里直接校验证据文件，不依赖子 shell 副作用。
if grep -q 'HTTP/1.1 201' "$EVIDENCE_DIR/customer-create-b.txt" \
  && [ -n "$CUSTOMER_B_ID" ] && [ "$CUSTOMER_B_ID" != "None" ] \
  && [ -n "$AGENT_A_ID" ] && [ "$AGENT_A_ID" != "None" ] && [ -n "$AGENT_B_ID" ] && [ "$AGENT_B_ID" != "None" ]; then
  AGENTS_PREPARED=true
fi
append_summary "agent_a=$AGENT_A_ID agent_b=$AGENT_B_ID customer_a=$CUSTOMER_A_ID customer_b=$CUSTOMER_B_ID"
append_summary "agents_prepared=$AGENTS_PREPARED"

# 4) 自动化链路（§11 前三行）：基线 → 创建 → 三负例 → 列表。
append_summary "step=automation_chain"

request_capture "automation-baseline" -H "$AUTH" "$AUTOMATION_BASE"
BASELINE_COUNT="$(json_extract "len(d) if isinstance(d, list) else -1")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${BASELINE_COUNT:--1}" = "0" ]; then
  AUTOMATION_BASELINE_EMPTY=true
fi
append_summary "automation_baseline_count=$BASELINE_COUNT"
append_summary "automation_baseline_empty=$AUTOMATION_BASELINE_EMPTY"

TRIGGER_NAME="st7-high-tag-${TIMESTAMP}"
request_capture "automation-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"'"$TRIGGER_NAME"'","event":"ticket_updated","conditions":[{"field":"ticket.priority","op":"eq","value":"high"}],"actions":[{"type":"add_tag","params":{"tag":"st7-auto-hit"}}],"active":true}' \
  "$AUTOMATION_BASE"
TRIGGER_ID="$(json_extract "d.get('id')")"
TRIGGER_EVENT="$(json_extract "d.get('event','')")"
TRIGGER_ACTIVE="$(json_extract "d.get('active', False)")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$TRIGGER_ID" ] && [ "$TRIGGER_ID" != "None" ] \
  && [ "$TRIGGER_EVENT" = "ticket.updated" ] && [ "$TRIGGER_ACTIVE" = "True" ]; then
  AUTOMATION_CREATED=true
fi
append_summary "trigger_id=$TRIGGER_ID trigger_event=$TRIGGER_EVENT"
append_summary "automation_created_with_normalized_event=$AUTOMATION_CREATED"

# 负例三连：非法事件 / 空名 / delay 不在末位。
request_capture "automation-unsupported-event" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"st7-bad-event","event":"ticket.exploded","actions":[]}' "$AUTOMATION_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "unsupported event"; then
  AUTOMATION_UNSUPPORTED_EVENT_REJECTED=true
fi
append_summary "automation_unsupported_event_rejected_400=$AUTOMATION_UNSUPPORTED_EVENT_REJECTED"

request_capture "automation-empty-name" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"","event":"ticket.created","actions":[]}' "$AUTOMATION_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "name required"; then
  AUTOMATION_EMPTY_NAME_REJECTED=true
fi
append_summary "automation_empty_name_rejected_400=$AUTOMATION_EMPTY_NAME_REJECTED"

request_capture "automation-delay-misplaced" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"name":"st7-delay-misplaced","event":"ticket.created","actions":[{"type":"delay","params":{"minutes":1,"actions":[{"type":"add_tag","params":{"tag":"x"}}]}},{"type":"add_tag","params":{"tag":"y"}}]}' \
  "$AUTOMATION_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "delay must be the last action"; then
  AUTOMATION_DELAY_MISPLACED_REJECTED=true
fi
append_summary "automation_delay_misplaced_rejected_400=$AUTOMATION_DELAY_MISPLACED_REJECTED"

request_capture "automation-list" -H "$AUTH" "$AUTOMATION_BASE"
LIST_HAS_TRIGGER="$(json_extract "any(t.get('id') == $TRIGGER_ID for t in d) if isinstance(d, list) else False")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$LIST_HAS_TRIGGER" = "True" ]; then
  AUTOMATION_LISTED=true
fi
append_summary "automation_listed=$AUTOMATION_LISTED"

# 5) 批量运行链路：工单样本 → dry-run 匹配不落副作用 → 真实运行落标签。
append_summary "step=batch_run_chain"

request_capture "ticket-op-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"st7-op-ticket","description":"automation target","category":"technical","priority":"high","customer_id":'"$CUSTOMER_OP_ID"'}' \
  "$TICKETS_BASE"
TICKET_OP_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" != "201" ] || [ -z "$TICKET_OP_ID" ] || [ "$TICKET_OP_ID" = "None" ]; then
  echo "❌ automation 样本工单创建失败" >&2
  exit 1
fi
append_summary "ticket_op_id=$TICKET_OP_ID"

request_capture "automation-dry-run" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"event":"ticket_updated","ticket_ids":['"$TICKET_OP_ID"'],"dry_run":true}' \
  "$AUTOMATION_BASE/run"
DRY_MATCHES="$(json_extract "d.get('matches', -1)")"
DRY_PROCESSED="$(json_extract "d.get('tickets_processed', -1)")"
DRY_DRYRUN="$(json_extract "d.get('dry_run', False)")"

request_capture "ticket-op-after-dry-run" -H "$AUTH" "$TICKETS_BASE/$TICKET_OP_ID"
DRY_TAGS="$(json_extract "d.get('tags','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${DRY_MATCHES:--1}" = "1" ] && [ "${DRY_PROCESSED:--1}" = "1" ] \
  && [ "$DRY_DRYRUN" = "True" ] && [ -z "$DRY_TAGS" ]; then
  AUTOMATION_DRY_RUN_NO_SIDE_EFFECT=true
fi
append_summary "dry_run_matches=$DRY_MATCHES tags_after_dry_run='$DRY_TAGS'"
append_summary "automation_dry_run_no_side_effect=$AUTOMATION_DRY_RUN_NO_SIDE_EFFECT"

request_capture "automation-run-real" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"event":"ticket_updated","ticket_ids":['"$TICKET_OP_ID"'],"dry_run":false}' \
  "$AUTOMATION_BASE/run"
REAL_MATCHES="$(json_extract "d.get('matches', -1)")"

request_capture "ticket-op-after-run" -H "$AUTH" "$TICKETS_BASE/$TICKET_OP_ID"
REAL_TAGS="$(json_extract "d.get('tags','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${REAL_MATCHES:--1}" = "1" ] && printf '%s' "$REAL_TAGS" | grep -q "st7-auto-hit"; then
  AUTOMATION_RUN_APPLIED=true
fi
append_summary "real_matches=$REAL_MATCHES tags_after_run='$REAL_TAGS'"
append_summary "automation_run_applied_to_ticket=$AUTOMATION_RUN_APPLIED"

request_capture "automation-runs-list" -H "$AUTH" "$AUTOMATION_BASE/runs?page=1&page_size=20"
RUNS_TOTAL="$(json_extract "d.get('total', -1)")"
RUNS_HAS_SUCCESS="$(json_extract "any(r.get('ticket_id') == $TICKET_OP_ID and r.get('status') == 'success' for r in d.get('data', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${RUNS_TOTAL:--1}" -ge 1 ] 2>/dev/null && [ "$RUNS_HAS_SUCCESS" = "True" ]; then
  AUTOMATION_RUNS_LISTED=true
fi
append_summary "runs_total=$RUNS_TOTAL runs_has_success=$RUNS_HAS_SUCCESS"
append_summary "automation_runs_listed=$AUTOMATION_RUNS_LISTED"

# 负例：手动运行不存在的触发器 → 400 trigger not found。
request_capture "automation-run-missing-trigger" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"trigger_id":999999,"ticket_ids":['"$TICKET_OP_ID"'],"dry_run":true}' \
  "$AUTOMATION_BASE/run"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "trigger not found"; then
  AUTOMATION_RUN_MISSING_TRIGGER_REJECTED=true
fi
append_summary "automation_run_missing_trigger_rejected_400=$AUTOMATION_RUN_MISSING_TRIGGER_REJECTED"

request_capture "automation-delete" -X DELETE -H "$AUTH" "$AUTOMATION_BASE/$TRIGGER_ID"
request_capture "automation-list-after-delete" -H "$AUTH" "$AUTOMATION_BASE"
AFTER_COUNT="$(json_extract "len(d) if isinstance(d, list) else -1")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${AFTER_COUNT:--1}" = "0" ]; then
  AUTOMATION_DELETED_AND_GONE=true
fi
append_summary "automation_count_after_delete=$AFTER_COUNT"
append_summary "automation_deleted_and_gone=$AUTOMATION_DELETED_AND_GONE"

request_capture "automation-delete-missing" -X DELETE -H "$AUTH" "$AUTOMATION_BASE/999999"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "trigger not found"; then
  AUTOMATION_DELETE_MISSING_REJECTED=true
fi
append_summary "automation_delete_missing_rejected_404=$AUTOMATION_DELETE_MISSING_REJECTED"

# 6) 激励排行链路（§11）：未认证 → 样本工单 resolve + CSAT → 排行对账。
append_summary "step=gamification_chain"

request_capture "gamification-unauthenticated" "$LB_BASE?days=7"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/gamification-unauthenticated.txt" && GAMIFICATION_UNAUTH_REJECTED=true
append_summary "gamification_unauthenticated_rejected_401=$GAMIFICATION_UNAUTH_REJECTED"

resolve_ticket_and_csat() {
  local ticket_name=$1 agent_id=$2 customer_id=$3 rating=$4
  request_capture "ticket-$ticket_name-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"title":"st7-lb-'"$ticket_name"'","description":"leaderboard sample","category":"technical","priority":"normal","customer_id":'"$customer_id"'}' \
    "$TICKETS_BASE"
  require_2xx "ticket-$ticket_name-create"
  local tid
  tid="$(json_extract "d.get('id')")"
  request_capture "ticket-$ticket_name-assign" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"agent_id":'"$agent_id"'}' "$TICKETS_BASE/$tid/assign"
  require_2xx "ticket-$ticket_name-assign"
  request_capture "ticket-$ticket_name-resolve" -X PUT -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"status":"resolved"}' "$TICKETS_BASE/$tid"
  require_2xx "ticket-$ticket_name-resolve"
  request_capture "satisfaction-$ticket_name-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d '{"ticket_id":'"$tid"',"customer_id":'"$customer_id"',"agent_id":'"$agent_id"',"rating":'"$rating"',"comment":"st7","category":"service_quality"}' \
    "$SAT_BASE"
  require_2xx "satisfaction-$ticket_name-create"
  echo "$tid"
}

LB_A1_TICKETS=""
for seq_no in 1 2 3; do
  tid="$(resolve_ticket_and_csat "a$seq_no" "$AGENT_A_ID" "$CUSTOMER_A_ID" 5)"
  LB_A1_TICKETS="$LB_A1_TICKETS $tid"
done
LB_B1_TICKET="$(resolve_ticket_and_csat "b1" "$AGENT_B_ID" "$CUSTOMER_B_ID" 4)"
append_summary "lb_ticket_a1:$LB_A1_TICKETS lb_ticket_b1:$LB_B1_TICKET"

# leaderboard days=7 的窗口上界是秒级截断的 server now：与查询同秒落库的
# resolved_at/created_at 会因小数部分被边界比较排除（真实边界行为）。
# 验收先错开 2 秒再查询；CI 负载下若首次不对账，按 1s 间隔重查（最多 10 次），
# 吸收查询超时与落库可见性延迟。判定经 LB_NOW_OK 传递（函数返回值会被 set -e 处理）。
leaderboard_reconciled_now() {
  request_capture "leaderboard-days" -H "$AUTH" --get "$LB_BASE" \
    --data-urlencode "days=7" --data-urlencode "limit=10"
  LB_COUNT="$(json_extract "len(d.get('entries', []))")"
  LB_A1_RANK="$(json_extract "next((e.get('rank') for e in d.get('entries', []) if e.get('agent_id') == $AGENT_A_ID), -1)")"
  LB_A1_RESOLVED="$(json_extract "next((e.get('resolved_tickets') for e in d.get('entries', []) if e.get('agent_id') == $AGENT_A_ID), -1)")"
  LB_A1_SCORE="$(json_extract "next((e.get('score') for e in d.get('entries', []) if e.get('agent_id') == $AGENT_A_ID), -1)")"
  LB_A1_CSAT_COUNT="$(json_extract "next((e.get('csat_count') for e in d.get('entries', []) if e.get('agent_id') == $AGENT_A_ID), -1)")"
  LB_A1_BADGES="$(json_extract "len(next((e.get('badges') or [] for e in d.get('entries', []) if e.get('agent_id') == $AGENT_A_ID), []))")"
  LB_B1_SCORE="$(json_extract "next((e.get('score') for e in d.get('entries', []) if e.get('agent_id') == $AGENT_B_ID), -1)")"
  LB_RANKS_ASC="$(json_extract "[e.get('rank') for e in d.get('entries', [])] == sorted(e.get('rank') for e in d.get('entries', []))")"
  # 排行对账：A1 = 3 resolved + CSAT 5 分×3（count>=3 权重 20）→ 3*10+5*20=130；
  # B1 = 1 resolved + CSAT 4 分×1（count<3 权重 10）→ 1*10+4*10=50。
  LB_NOW_OK=false
  if [ "$RESPONSE_STATUS" = "200" ] && [ "${LB_COUNT:--1}" = "2" ] && [ "${LB_A1_RANK:--1}" = "1" ] \
    && [ "${LB_A1_RESOLVED:--1}" = "3" ] && [ "${LB_A1_CSAT_COUNT:--1}" = "3" ] \
    && [ "${LB_A1_SCORE:--1}" = "130" ] && [ "${LB_B1_SCORE:--1}" = "50" ] \
    && [ "${LB_A1_BADGES:--1}" -ge 1 ] 2>/dev/null && [ "$LB_RANKS_ASC" = "True" ]; then
    LB_NOW_OK=true
  fi
  return 0
}

sleep 2
LB_ATTEMPTS=0
leaderboard_reconciled_now
until [ "$LB_NOW_OK" = "true" ]; do
  LB_ATTEMPTS=$((LB_ATTEMPTS+1))
  if [ "$LB_ATTEMPTS" -ge 10 ]; then break; fi
  sleep 1
  leaderboard_reconciled_now
done
if [ "$LB_NOW_OK" = "true" ]; then
  LEADERBOARD_RECONCILED=true
fi
append_summary "lb_attempts=$LB_ATTEMPTS lb_count=$LB_COUNT a1_rank=$LB_A1_RANK a1_resolved=$LB_A1_RESOLVED a1_score=$LB_A1_SCORE b1_score=$LB_B1_SCORE a1_badges=$LB_A1_BADGES"
append_summary "leaderboard_reconciled_with_scores=$LEADERBOARD_RECONCILED"

LB_START="$(date -d '1 day ago' +%Y-%m-%d)"
LB_END="$(date +%Y-%m-%d)"
request_capture "leaderboard-date-range" -H "$AUTH" --get "$LB_BASE" \
  --data-urlencode "start_date=$LB_START" --data-urlencode "end_date=$LB_END"
DR_START="$(json_extract "d.get('start_date','')")"
DR_END="$(json_extract "d.get('end_date','')")"
DR_COUNT="$(json_extract "len(d.get('entries', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${DR_COUNT:--1}" -ge 2 ] 2>/dev/null \
  && printf '%s' "$DR_START" | grep -q "$LB_START" && printf '%s' "$DR_END" | grep -q "$LB_END"; then
  LEADERBOARD_DATE_RANGE_RECONCILED=true
fi
append_summary "lb_date_range=$DR_START..$DR_END count=$DR_COUNT"
append_summary "leaderboard_date_range_reconciled=$LEADERBOARD_DATE_RANGE_RECONCILED"

# 负例：非法日期格式 → 400。
request_capture "leaderboard-bad-date" -H "$AUTH" --get "$LB_BASE" \
  --data-urlencode "start_date=2026/09/18" --data-urlencode "end_date=$LB_END"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "Invalid start_date format"; then
  LEADERBOARD_BAD_DATE_REJECTED=true
fi
append_summary "leaderboard_bad_date_rejected_400=$LEADERBOARD_BAD_DATE_REJECTED"

request_capture "leaderboard-dept-filter" -H "$AUTH" --get "$LB_BASE" \
  --data-urlencode "department=st7-dept" --data-urlencode "days=7"
DEPT_COUNT="$(json_extract "len(d.get('entries', []))")"
request_capture "leaderboard-dept-nomatch" -H "$AUTH" --get "$LB_BASE" \
  --data-urlencode "department=st7-no-such" --data-urlencode "days=7"
NOMATCH_COUNT="$(json_extract "len(d.get('entries', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${DEPT_COUNT:--1}" = "2" ] && [ "${NOMATCH_COUNT:--1}" = "0" ]; then
  LEADERBOARD_DEPARTMENT_FILTERED=true
fi
append_summary "lb_dept_count=$DEPT_COUNT nomatch_count=$NOMATCH_COUNT"
append_summary "leaderboard_department_filtered=$LEADERBOARD_DEPARTMENT_FILTERED"

# 7) 对账
FAILED=false
for check in READY_OK UNAUTH_AUTOMATIONS_REJECTED AGENTS_PREPARED \
  AUTOMATION_BASELINE_EMPTY AUTOMATION_CREATED AUTOMATION_UNSUPPORTED_EVENT_REJECTED \
  AUTOMATION_EMPTY_NAME_REJECTED AUTOMATION_DELAY_MISPLACED_REJECTED AUTOMATION_LISTED \
  AUTOMATION_DRY_RUN_NO_SIDE_EFFECT AUTOMATION_RUN_APPLIED AUTOMATION_RUNS_LISTED \
  AUTOMATION_RUN_MISSING_TRIGGER_REJECTED AUTOMATION_DELETED_AND_GONE AUTOMATION_DELETE_MISSING_REJECTED \
  GAMIFICATION_UNAUTH_REJECTED LEADERBOARD_RECONCILED LEADERBOARD_DATE_RANGE_RECONCILED \
  LEADERBOARD_BAD_DATE_REJECTED LEADERBOARD_DEPARTMENT_FILTERED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "----- evidence summary（诊断用）-----" >&2
  cat "$EVIDENCE_DIR/summary.txt" >&2
  echo "❌ Automation/Gamification acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Automation/Gamification acceptance 通过: 触发器创建(事件名归一化)/三负例/dry-run 匹配无副作用/真实运行落标签/runs 审计对账/删除与 404 + 排行榜分数精确对账(130/50)/日期窗/非法日期 400/部门过滤 全部真实留档"
