#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Scoped config 审批回滚链路 acceptance 测试开始（P2-5 第四刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/approval-rollback"}
OPERATOR_USERNAME=${OPERATOR_USERNAME:-"gov-operator"}
OPERATOR_PASSWORD=${OPERATOR_PASSWORD:-"gov-operator-123"}
REVIEWER_USERNAME=${REVIEWER_USERNAME:-"gov-reviewer"}
REVIEWER_PASSWORD=${REVIEWER_PASSWORD:-"gov-reviewer-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18097}
# 验收专用租户：scoped config 文档与审计行都落在该租户作用域下。
GOV_TENANT=${GOV_TENANT:-"gov-tenant"}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_WRITE_REJECTED=false
SECOND_ADMIN_REGISTRATION_DEGRADED=false
PUT_CHANGE_CONTROL_ENFORCED=false
UPDATE_CHANGE_CONTROL_RECORDED=false
ROLLBACK_REQUIRES_APPROVAL=false
SELF_APPROVAL_ROLLBACK_REJECTED=false
REVIEWER_APPROVAL_ROLLBACK_OK=false
ROLLBACK_SNAPSHOT_RESTORED=false
VERIFY_SAME_ACTOR_REJECTED=false
CROSS_REVIEWER_VERIFY_OK=false
HISTORY_RECONCILIATION_OK=false
AUDIT_SCOPED_CONFIG_RECONCILED=false
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

# json_extract <python表达式>：在最近一次响应 JSON 上求值，表达式里 `d` 为根对象。
json_extract() {
  printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
d = json.loads(m.group(0)) if m else {}
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
  MANIFEST_UNAUTH_WRITE_REJECTED="${UNAUTH_WRITE_REJECTED:-false}" \
  MANIFEST_SECOND_ADMIN_DEGRADED="${SECOND_ADMIN_REGISTRATION_DEGRADED:-false}" \
  MANIFEST_PUT_CHANGE_CONTROL_ENFORCED="${PUT_CHANGE_CONTROL_ENFORCED:-false}" \
  MANIFEST_UPDATE_CHANGE_CONTROL_RECORDED="${UPDATE_CHANGE_CONTROL_RECORDED:-false}" \
  MANIFEST_ROLLBACK_REQUIRES_APPROVAL="${ROLLBACK_REQUIRES_APPROVAL:-false}" \
  MANIFEST_SELF_APPROVAL_ROLLBACK_REJECTED="${SELF_APPROVAL_ROLLBACK_REJECTED:-false}" \
  MANIFEST_REVIEWER_APPROVAL_ROLLBACK_OK="${REVIEWER_APPROVAL_ROLLBACK_OK:-false}" \
  MANIFEST_ROLLBACK_SNAPSHOT_RESTORED="${ROLLBACK_SNAPSHOT_RESTORED:-false}" \
  MANIFEST_VERIFY_SAME_ACTOR_REJECTED="${VERIFY_SAME_ACTOR_REJECTED:-false}" \
  MANIFEST_CROSS_REVIEWER_VERIFY_OK="${CROSS_REVIEWER_VERIFY_OK:-false}" \
  MANIFEST_HISTORY_RECONCILIATION_OK="${HISTORY_RECONCILIATION_OK:-false}" \
  MANIFEST_AUDIT_SCOPED_CONFIG_RECONCILED="${AUDIT_SCOPED_CONFIG_RECONCILED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "approval-rollback",
    "mode": "runtime-governance-evidence",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_write_rejected_401": os.environ.get("MANIFEST_UNAUTH_WRITE_REJECTED", "false"),
        "second_admin_registration_degraded": os.environ.get("MANIFEST_SECOND_ADMIN_DEGRADED", "false"),
        "put_change_control_enforced_400": os.environ.get("MANIFEST_PUT_CHANGE_CONTROL_ENFORCED", "false"),
        "update_change_control_recorded": os.environ.get("MANIFEST_UPDATE_CHANGE_CONTROL_RECORDED", "false"),
        "rollback_requires_approval_400": os.environ.get("MANIFEST_ROLLBACK_REQUIRES_APPROVAL", "false"),
        "self_approval_rollback_rejected_403": os.environ.get("MANIFEST_SELF_APPROVAL_ROLLBACK_REJECTED", "false"),
        "reviewer_approval_rollback_ok": os.environ.get("MANIFEST_REVIEWER_APPROVAL_ROLLBACK_OK", "false"),
        "rollback_snapshot_restored": os.environ.get("MANIFEST_ROLLBACK_SNAPSHOT_RESTORED", "false"),
        "verify_same_actor_rejected_403": os.environ.get("MANIFEST_VERIFY_SAME_ACTOR_REJECTED", "false"),
        "cross_reviewer_verify_ok": os.environ.get("MANIFEST_CROSS_REVIEWER_VERIFY_OK", "false"),
        "history_reconciliation_ok": os.environ.get("MANIFEST_HISTORY_RECONCILIATION_OK", "false"),
        "audit_scoped_config_reconciled": os.environ.get("MANIFEST_AUDIT_SCOPED_CONFIG_RECONCILED", "false"),
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
Scoped config approval/rollback chain acceptance summary (P2-5 slice 4)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/approval-rollback-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/approval-rollback-XXXXXX.sqlite")"

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
SCOPED_BASE="$SERVIFY_URL/api/security/config/tenant"
if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"
append_summary "gov_tenant=$GOV_TENANT"

# scoped config 链路统一请求头：management 面 Bearer + 请求作用域声明。
# operator/reviewer 都是 admin principal，claims 无 tenant，由 X-Tenant-ID 声明。
scoped_req() {
  local evidence_name=$1 method=$2 token=$3
  shift 3
  request_capture "$evidence_name" -X "$method" \
    -H "Authorization: Bearer $token" \
    -H "X-Tenant-ID: $GOV_TENANT" \
    -H "Content-Type: application/json" \
    "$@"
}

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

# 3) 双管理员构造：operator 首个注册保留 admin；reviewer 第二个注册被降级
#    customer（注册降级逻辑本身即验收点），经种子式 DB 提权后重新登录——
#    等价生产环境"预置两个管理员互审"的种子流程。
append_summary "step=users_setup"
echo "👥 构造 operator / reviewer 双管理员..."

# N0：未认证写请求必须被 management 面 401 挡下。
request_capture "unauthenticated-write" -X PUT -H "Content-Type: application/json" \
  -d '{}' "$SCOPED_BASE"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-write.txt" && UNAUTH_WRITE_REJECTED=true
append_summary "unauthenticated_write_rejected=$UNAUTH_WRITE_REJECTED"

request_capture "register-operator" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$OPERATOR_USERNAME"'","email":"'"$OPERATOR_USERNAME"'@servify.io","password":"'"$OPERATOR_PASSWORD"'","name":"Gov Operator","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ operator 注册失败" >&2
  exit 1
fi
OPERATOR_TOKEN="$(body_json_field token)"
OPERATOR_ID="$(json_extract "d.get('user',{}).get('id','')")"

request_capture "register-reviewer" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$REVIEWER_USERNAME"'","email":"'"$REVIEWER_USERNAME"'@servify.io","password":"'"$REVIEWER_PASSWORD"'","name":"Gov Reviewer","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
REVIEWER_REGISTERED_ROLE="$(json_extract "d.get('user',{}).get('role','')")"
REVIEWER_ID="$(json_extract "d.get('user',{}).get('id','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ "$REVIEWER_REGISTERED_ROLE" = "customer" ]; then
  # 第二个请求 admin 的注册被降级 customer——注册侧角色收敛留档。
  SECOND_ADMIN_REGISTRATION_DEGRADED=true
fi
append_summary "second_admin_registration_degraded=$SECOND_ADMIN_REGISTRATION_DEGRADED"
append_summary "reviewer_registered_role=$REVIEWER_REGISTERED_ROLE"

# 种子式提权：直接更新 users.role（生产等价物是种子脚本/DBA 流程），
# 随后 reviewer 重新登录即持有 admin 角色 token。
python3 - "$DB_DSN" "$REVIEWER_USERNAME" > "$EVIDENCE_DIR/reviewer-promote.txt" 2>&1 <<'PY'
import sqlite3
import sys

db_path, username = sys.argv[1], sys.argv[2]
conn = sqlite3.connect(db_path, timeout=15)
try:
    cur = conn.execute(
        "UPDATE users SET role = 'admin' WHERE username = ? AND role <> 'admin'",
        (username,),
    )
    conn.commit()
    print("promoted_rows=%d" % cur.rowcount)
finally:
    conn.close()
PY
PROMOTED_ROWS="$(grep -o 'promoted_rows=[0-9]*' "$EVIDENCE_DIR/reviewer-promote.txt" | cut -d= -f2 || true)"
if [ "${PROMOTED_ROWS:-0}" != "1" ]; then
  echo "❌ reviewer 提权失败（promoted_rows=$PROMOTED_ROWS）" >&2
  exit 1
fi

request_capture "reviewer-login" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$REVIEWER_USERNAME"'","password":"'"$REVIEWER_PASSWORD"'"}' \
  "$SERVIFY_URL/api/v1/auth/login"
REVIEWER_TOKEN="$(body_json_field token)"
if [ "$RESPONSE_STATUS" != "200" ] || [ -z "$REVIEWER_TOKEN" ]; then
  echo "❌ reviewer 登录失败" >&2
  exit 1
fi
append_summary "operator_id=$OPERATOR_ID"
append_summary "reviewer_id=$REVIEWER_ID"

# 4) 负例守卫：change control 强制 + 未认证 401 已留档。
append_summary "step=negative_guards"

# 当前租户 scoped config 尚不存在（空文档）。
scoped_req "config-before" GET "$OPERATOR_TOKEN" "$SCOPED_BASE"
append_summary "config_before_status=$RESPONSE_STATUS"

# N1：PUT 缺 change_ref → 400 Change control required。
scoped_req "put-no-change-control" PUT "$OPERATOR_TOKEN" -d '{"session_risk":{"multi_public_ip_threshold":9}}' "$SCOPED_BASE"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q 'Change control required'; then
  PUT_CHANGE_CONTROL_ENFORCED=true
fi
append_summary "put_change_control_enforced=$PUT_CHANGE_CONTROL_ENFORCED"

# 5) 更新链：U1（threshold=9 + step_up）→ U2（threshold=99），均为 medium 无需审批。
append_summary "step=update_chain"
scoped_req "put-update-1" PUT "$OPERATOR_TOKEN" -d '{"session_risk":{"multi_public_ip_threshold":9,"login_enforcement":"step_up"},"change_ref":"CR-AR-U1","reason":"验收：收紧多公网阈值并开启登录增强"}' "$SCOPED_BASE"
U1_STATUS=$RESPONSE_STATUS
U1_CHANGE_REF="$(json_extract "d.get('change_control',{}).get('change_ref','')")"
scoped_req "put-update-2" PUT "$OPERATOR_TOKEN" -d '{"session_risk":{"multi_public_ip_threshold":99},"change_ref":"CR-AR-U2","reason":"验收：模拟一次错误的阈值上调"}' "$SCOPED_BASE"
U2_STATUS=$RESPONSE_STATUS
U2_CHANGE_REF="$(json_extract "d.get('change_control',{}).get('change_ref','')")"
if [ "$U1_STATUS" = "200" ] && [ "$U2_STATUS" = "200" ] \
  && [ "$U1_CHANGE_REF" = "CR-AR-U1" ] && [ "$U2_CHANGE_REF" = "CR-AR-U2" ]; then
  UPDATE_CHANGE_CONTROL_RECORDED=true
fi
append_summary "update_change_control_recorded=$UPDATE_CHANGE_CONTROL_RECORDED"

# history 列表：抽 U1/U2 的 audit id，确认 can_rollback/has_snapshot 元数据。
scoped_req "history-list" GET "$OPERATOR_TOKEN" "$SCOPED_BASE/history?page_size=50"
HISTORY_JSON_FILE="$EVIDENCE_DIR/history-list.json"
printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
json.dump(json.loads(m.group(0)) if m else {}, open(sys.argv[1], 'w'))
" "$HISTORY_JSON_FILE" || true
read -r ID_U1 ID_U2 CAN_ROLLBACK_COUNT < <(python3 - "$HISTORY_JSON_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    payload = json.load(fh)
items = payload.get("data") or payload.get("items") or []
by_ref = {}
can_rollback = 0
for item in items:
    audit = item.get("audit") or {}
    request_json = audit.get("request_json") or ""
    operation = item.get("operation") or ""
    if item.get("can_rollback"):
        can_rollback += 1
    for ref in ("CR-AR-U1", "CR-AR-U2"):
        if operation == "update" and ref in request_json:
            by_ref[ref] = audit.get("id")
print(by_ref.get("CR-AR-U1", ""), by_ref.get("CR-AR-U2", ""), can_rollback)
PY
) || true
append_summary "id_u1=$ID_U1 id_u2=$ID_U2 can_rollback_entries=$CAN_ROLLBACK_COUNT"
if [ -z "$ID_U1" ] || [ -z "$ID_U2" ]; then
  echo "❌ 未能在 history 中定位 U1/U2 audit id" >&2
  exit 1
fi

# U1 详情：抽取 verify 模板中 required checks（后续 verify 请求体动态覆盖）。
scoped_req "history-entry-update" GET "$OPERATOR_TOKEN" "$SCOPED_BASE/history/$ID_U1"
REQUIRED_CHECKS_JSON="$(json_extract "json.dumps([{'id': c['id'], 'status': 'passed'} for c in (d.get('verification_template') or {}).get('checks', []) if c.get('required')])")"
if [ -z "$REQUIRED_CHECKS_JSON" ] || [ "$REQUIRED_CHECKS_JSON" = "[]" ]; then
  echo "❌ U1 verify 模板无 required checks" >&2
  exit 1
fi
append_summary "u1_required_checks=$REQUIRED_CHECKS_JSON"

# 6) 审批回滚链：未审批拒绝 → 自审拒绝 → 双管理员互审放行 → 快照恢复。
append_summary "step=approval_rollback"

# N2：rollback 一律高风险，未携带审批 → 400。
scoped_req "rollback-no-approval" POST "$OPERATOR_TOKEN" \
  -d '{"change_ref":"CR-AR-RB","reason":"验收：回滚到 U1 基线"}' \
  "$SCOPED_BASE/rollback/$ID_U1?confirm=true"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q 'Approval'; then
  ROLLBACK_REQUIRES_APPROVAL=true
fi
append_summary "rollback_requires_approval=$ROLLBACK_REQUIRES_APPROVAL"

# N3：operator 给自己记录审批（approve 端点本身放行，留痕）。
# 自审走独立 change_ref（CR-AR-SELF），与正例 CR-AR-RB 的审批记录互不干扰。
scoped_req "approve-self" POST "$OPERATOR_TOKEN" \
  -d '{"change_ref":"CR-AR-SELF","reason":"验收：自审尝试","approval_ref":"APR-AR-SELF","notes":"operator 自己给自己批"}' \
  "$SCOPED_BASE/approve"
SELF_APPROVAL_STATUS=$RESPONSE_STATUS

# N3'：同一人审批 + 执行 → 403 职责分离。
scoped_req "rollback-self-approval" POST "$OPERATOR_TOKEN" \
  -d '{"change_ref":"CR-AR-SELF","reason":"验收：回滚到 U1 基线","approval_ref":"APR-AR-SELF"}' \
  "$SCOPED_BASE/rollback/$ID_U1?confirm=true"
if [ "$SELF_APPROVAL_STATUS" = "200" ] && [ "$RESPONSE_STATUS" = "403" ] \
  && printf '%s' "$RESPONSE_RAW" | grep -q 'Approval reviewer separation required'; then
  SELF_APPROVAL_ROLLBACK_REJECTED=true
fi
append_summary "self_approval_rollback_rejected=$SELF_APPROVAL_ROLLBACK_REJECTED"

# P1：reviewer（另一管理员）记录审批。
scoped_req "approve-reviewer" POST "$REVIEWER_TOKEN" \
  -d '{"change_ref":"CR-AR-RB","reason":"验收：回滚到 U1 基线","approval_ref":"APR-AR-RB","notes":"reviewer 复核 CR-AR-RB，同意回滚"}' \
  "$SCOPED_BASE/approve"
REVIEWER_APPROVAL_STATUS=$RESPONSE_STATUS

# P2：operator 携 reviewer 审批执行回滚（?confirm=true 显式确认）。
scoped_req "rollback-restore" POST "$OPERATOR_TOKEN" \
  -d '{"change_ref":"CR-AR-RB","reason":"验收：回滚到 U1 基线","approval_ref":"APR-AR-RB"}' \
  "$SCOPED_BASE/rollback/$ID_U1?confirm=true"
if [ "$REVIEWER_APPROVAL_STATUS" = "200" ] && [ "$RESPONSE_STATUS" = "200" ]; then
  REVIEWER_APPROVAL_ROLLBACK_OK=true
fi
append_summary "reviewer_approval_rollback_ok=$REVIEWER_APPROVAL_ROLLBACK_OK"

# P3：GET 确认恢复的是 U1 的完整快照（threshold 9 + step_up），而非 U2 的 99。
scoped_req "config-after-rollback" GET "$OPERATOR_TOKEN" "$SCOPED_BASE"
RESTORED_THRESHOLD="$(json_extract "d.get('config',{}).get('session_risk',{}).get('multi_public_ip_threshold','')")"
RESTORED_ENFORCEMENT="$(json_extract "d.get('config',{}).get('session_risk',{}).get('login_enforcement','')")"
if [ "$RESTORED_THRESHOLD" = "9" ] && [ "$RESTORED_ENFORCEMENT" = "step_up" ]; then
  ROLLBACK_SNAPSHOT_RESTORED=true
fi
append_summary "restored_threshold=$RESTORED_THRESHOLD restored_enforcement=$RESTORED_ENFORCEMENT"
append_summary "rollback_snapshot_restored=$ROLLBACK_SNAPSHOT_RESTORED"

# 7) 验证链：同操作人 verify 403 → 跨人 verify 200（update 与 rollback 各一轮）。
append_summary "step=verification"

build_verify_body() {
  # $1 = change_ref，输出合法 verify 请求体（status=passed 须带 evidence 与 required checks）。
  printf '{"status":"passed","notes":"验收：复核通过","evidence":["evidence/rollback-restore.txt"],"checks":%s,"change_ref":"%s","reason":"验收：提交复核"}' \
    "$REQUIRED_CHECKS_JSON" "$1"
}

# N4：operator 校验自己执行的 U1 → 403。
scoped_req "verify-update-same-actor" POST "$OPERATOR_TOKEN" \
  -d "$(build_verify_body "CR-AR-V1")" "$SCOPED_BASE/verify/$ID_U1"
VERIFY_U1_SAME_STATUS=$RESPONSE_STATUS

# P4：reviewer 校验 U1 → 200。
scoped_req "verify-update-cross" POST "$REVIEWER_TOKEN" \
  -d "$(build_verify_body "CR-AR-V1")" "$SCOPED_BASE/verify/$ID_U1"
VERIFY_U1_CROSS_STATUS=$RESPONSE_STATUS

# rollback 行的 audit id 与模板（rollback 模板含 rollback_baseline_restored）。
scoped_req "history-mid" GET "$OPERATOR_TOKEN" "$SCOPED_BASE/history?page_size=50"
HISTORY_MID_JSON="$EVIDENCE_DIR/history-mid.json"
printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
json.dump(json.loads(m.group(0)) if m else {}, open(sys.argv[1], 'w'))
" "$HISTORY_MID_JSON" || true
read -r ID_RB < <(python3 - "$HISTORY_MID_JSON" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    payload = json.load(fh)
items = payload.get("data") or payload.get("items") or []
for item in items:
    if item.get("operation") == "rollback":
        print((item.get("audit") or {}).get("id", ""))
        break
PY
) || true
if [ -z "$ID_RB" ]; then
  echo "❌ 未能在 history 中定位 rollback audit id" >&2
  exit 1
fi
scoped_req "history-entry-rollback" GET "$OPERATOR_TOKEN" "$SCOPED_BASE/history/$ID_RB"
ROLLBACK_REQUIRED_CHECKS_JSON="$(json_extract "json.dumps([{'id': c['id'], 'status': 'passed'} for c in (d.get('verification_template') or {}).get('checks', []) if c.get('required')])")"
if [ -n "$ROLLBACK_REQUIRED_CHECKS_JSON" ] && [ "$ROLLBACK_REQUIRED_CHECKS_JSON" != "[]" ]; then
  REQUIRED_CHECKS_JSON="$ROLLBACK_REQUIRED_CHECKS_JSON"
fi
append_summary "id_rb=$ID_RB rollback_required_checks=$REQUIRED_CHECKS_JSON"

# N5：operator 校验自己执行的 rollback → 403。
scoped_req "verify-rollback-same-actor" POST "$OPERATOR_TOKEN" \
  -d "$(build_verify_body "CR-AR-V2")" "$SCOPED_BASE/verify/$ID_RB"
VERIFY_RB_SAME_STATUS=$RESPONSE_STATUS

# P5：reviewer 校验 rollback → 200。
scoped_req "verify-rollback-cross" POST "$REVIEWER_TOKEN" \
  -d "$(build_verify_body "CR-AR-V2")" "$SCOPED_BASE/verify/$ID_RB"
VERIFY_RB_CROSS_STATUS=$RESPONSE_STATUS

if [ "$VERIFY_U1_SAME_STATUS" = "403" ] && [ "$VERIFY_RB_SAME_STATUS" = "403" ] \
  && printf '%s' "$(cat "$EVIDENCE_DIR/verify-update-same-actor.txt")" | grep -q 'Verification reviewer separation required' \
  && [ "$VERIFY_U1_CROSS_STATUS" = "200" ] && [ "$VERIFY_RB_CROSS_STATUS" = "200" ]; then
  VERIFY_SAME_ACTOR_REJECTED=true
  CROSS_REVIEWER_VERIFY_OK=true
fi
append_summary "verify_same_actor_rejected=$VERIFY_SAME_ACTOR_REJECTED"
append_summary "cross_reviewer_verify_ok=$CROSS_REVIEWER_VERIFY_OK"

# 8) history 对账：operation 集合、快照元数据、验证状态收口。
append_summary "step=history_reconciliation"
scoped_req "history-final" GET "$OPERATOR_TOKEN" "$SCOPED_BASE/history?page_size=50"
HISTORY_FINAL_JSON="$EVIDENCE_DIR/history-final.json"
printf '%s' "$RESPONSE_RAW" | python3 -c "
import json, sys, re
raw = sys.stdin.read()
m = re.search(r'\{.*\}', raw, re.S)
json.dump(json.loads(m.group(0)) if m else {}, open(sys.argv[1], 'w'))
" "$HISTORY_FINAL_JSON" || true
HISTORY_EVAL="$(python3 - "$HISTORY_FINAL_JSON" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as fh:
    payload = json.load(fh)
items = payload.get("data") or payload.get("items") or []
ops = [item.get("operation") for item in items]
update_items = [i for i in items if i.get("operation") == "update"]
rollback_items = [i for i in items if i.get("operation") == "rollback"]
update_verified = sum(1 for i in update_items if i.get("verification_status") == "passed")
rollback_verified = sum(1 for i in rollback_items if i.get("verification_status") == "passed")
# can_rollback 语义：该条目持有 AfterJSON 快照、可作为回滚目标
# （operation ∈ {update, rollback} 均可）。rollback 行自身有快照，应为可回滚。
rollback_can = sum(1 for i in rollback_items if i.get("can_rollback"))
checks = {
    "updates": len(update_items),
    "rollbacks": len(rollback_items),
    "update_verified": update_verified,
    "rollback_verified": rollback_verified,
    "rollback_can_rollback": rollback_can,
}
checks["ok"] = (
    checks["updates"] >= 2
    and checks["rollbacks"] == 1
    and checks["update_verified"] >= 1
    and checks["rollback_verified"] == 1
    and checks["rollback_can_rollback"] == 1
)
print(" ".join("%s=%s" % kv for kv in checks.items()))
PY
)"
append_summary "history_reconciliation=$HISTORY_EVAL"
eval "$HISTORY_EVAL"
if [ "${ok:-false}" = "True" ]; then
  HISTORY_RECONCILIATION_OK=true
fi
append_summary "history_reconciliation_ok=$HISTORY_RECONCILIATION_OK"

# 9) 审计对账：scoped_config.* 成功链全部留痕，操作人/审批人/验证人 actor 可区分。
append_summary "step=audit_reconciliation"
if [ -n "$OPERATOR_TOKEN" ]; then
  for entry in "update scoped_config.tenant.update audit-update" \
    "rollback scoped_config.tenant.rollback audit-rollback" \
    "approve scoped_config.tenant.approve audit-approve" \
    "verify scoped_config.tenant.verify audit-verify"; do
    set -- $entry
    scoped_req "$3" GET "$OPERATOR_TOKEN" \
      "$SERVIFY_URL/api/audit/logs?action=$2&page_size=50"
  done

  AUDIT_EVAL="$(python3 - "$EVIDENCE_DIR/audit-update.txt" "$EVIDENCE_DIR/audit-rollback.txt" \
    "$EVIDENCE_DIR/audit-approve.txt" "$EVIDENCE_DIR/audit-verify.txt" \
    "$OPERATOR_ID" "$REVIEWER_ID" <<'PY'
import json
import re
import sys

def rows(path):
    with open(path, encoding="utf-8") as fh:
        raw = fh.read()
    m = re.search(r"\{.*\}", raw, re.S)
    payload = json.loads(m.group(0)) if m else {}
    return [r for r in (payload.get("data") or payload.get("items") or [])
            if r.get("status_code") == 200 and r.get("success")]

update_rows, rollback_rows, approve_rows, verify_rows = (rows(p) for p in sys.argv[1:5])
operator_id, reviewer_id = sys.argv[5], sys.argv[6]

def actor_ids(rows):
    return {str(r.get("actor_user_id")) for r in rows}

resource_ok = all(
    r.get("resource_type") == "scoped_config"
    for rows in (update_rows, rollback_rows, approve_rows, verify_rows)
    for r in rows
)
checks = {
    "update_ok": len(update_rows),
    "rollback_ok": len(rollback_rows),
    "approve_ok": len(approve_rows),
    "verify_ok": len(verify_rows),
    "reviewer_approved": reviewer_id in actor_ids(approve_rows),
    "operator_executed_rollback": operator_id in actor_ids(rollback_rows),
    "reviewer_verified": actor_ids(verify_rows) == {reviewer_id},
    "resource_type_ok": resource_ok,
}
checks["ok"] = (
    checks["update_ok"] == 2
    and checks["rollback_ok"] == 1
    and checks["approve_ok"] == 2
    and checks["verify_ok"] == 2
    and checks["reviewer_approved"]
    and checks["operator_executed_rollback"]
    and checks["reviewer_verified"]
    and checks["resource_type_ok"]
)
print(" ".join("%s=%s" % kv for kv in checks.items()))
PY
)"
  append_summary "audit_reconciliation=$AUDIT_EVAL"
  eval "$AUDIT_EVAL"
  if [ "${ok:-false}" = "True" ]; then
    AUDIT_SCOPED_CONFIG_RECONCILED=true
  fi
fi
append_summary "audit_scoped_config_reconciled=$AUDIT_SCOPED_CONFIG_RECONCILED"

# 10) 对账
FAILED=false
for check in READY_OK UNAUTH_WRITE_REJECTED SECOND_ADMIN_REGISTRATION_DEGRADED \
  PUT_CHANGE_CONTROL_ENFORCED UPDATE_CHANGE_CONTROL_RECORDED \
  ROLLBACK_REQUIRES_APPROVAL SELF_APPROVAL_ROLLBACK_REJECTED \
  REVIEWER_APPROVAL_ROLLBACK_OK ROLLBACK_SNAPSHOT_RESTORED \
  VERIFY_SAME_ACTOR_REJECTED CROSS_REVIEWER_VERIFY_OK \
  HISTORY_RECONCILIATION_OK AUDIT_SCOPED_CONFIG_RECONCILED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Approval rollback acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Approval rollback acceptance 通过: change control / 职责分离审批 / 快照回滚 / 跨人验证 / history 与审计对账 全部真实留档"
