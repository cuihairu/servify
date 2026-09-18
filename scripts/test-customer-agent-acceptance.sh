#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Customer/Agent acceptance 测试开始（P2-6 第三刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/customer-agent"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"ca-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"ca-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18100}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
CUSTOMER_CREATED=false
CUSTOMER_LISTED=false
CUSTOMER_ACTIVITY_RECONCILED=false
AGENT_CREATED=false
DUPLICATE_AGENT_REJECTED_409=false
FIND_AVAILABLE_EMPTY_REJECTED_404=false
FIND_AVAILABLE_SELECTED=false
SESSION_ASSIGNED_LOAD_INCREASED=false
ASSIGN_OVERCAPACITY_REJECTED=false
SESSION_RELEASED_LOAD_DECREASED=false
RELEASE_UNASSIGNED_REJECTED_404=false
ASSIGN_MISSING_SESSION_REJECTED_404=false
ASSIGN_MISSING_AGENT_REJECTED_404=false
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
  MANIFEST_CUSTOMER_LISTED="${CUSTOMER_LISTED:-false}" \
  MANIFEST_CUSTOMER_ACTIVITY="${CUSTOMER_ACTIVITY_RECONCILED:-false}" \
  MANIFEST_AGENT_CREATED="${AGENT_CREATED:-false}" \
  MANIFEST_DUPLICATE_AGENT_REJECTED="${DUPLICATE_AGENT_REJECTED_409:-false}" \
  MANIFEST_FIND_AVAILABLE_EMPTY="${FIND_AVAILABLE_EMPTY_REJECTED_404:-false}" \
  MANIFEST_FIND_AVAILABLE_SELECTED="${FIND_AVAILABLE_SELECTED:-false}" \
  MANIFEST_ASSIGN_LOAD_INCREASED="${SESSION_ASSIGNED_LOAD_INCREASED:-false}" \
  MANIFEST_ASSIGN_OVERCAPACITY="${ASSIGN_OVERCAPACITY_REJECTED:-false}" \
  MANIFEST_RELEASE_LOAD_DECREASED="${SESSION_RELEASED_LOAD_DECREASED:-false}" \
  MANIFEST_RELEASE_UNASSIGNED_REJECTED="${RELEASE_UNASSIGNED_REJECTED_404:-false}" \
  MANIFEST_ASSIGN_MISSING_SESSION_REJECTED="${ASSIGN_MISSING_SESSION_REJECTED_404:-false}" \
  MANIFEST_ASSIGN_MISSING_AGENT_REJECTED="${ASSIGN_MISSING_AGENT_REJECTED_404:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "customer-agent",
    "mode": "runtime-customer-agent-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "customer_created": os.environ.get("MANIFEST_CUSTOMER_CREATED", "false"),
        "customer_listed_with_filter": os.environ.get("MANIFEST_CUSTOMER_LISTED", "false"),
        "customer_activity_reconciled": os.environ.get("MANIFEST_CUSTOMER_ACTIVITY", "false"),
        "agent_created": os.environ.get("MANIFEST_AGENT_CREATED", "false"),
        "duplicate_agent_rejected_409": os.environ.get("MANIFEST_DUPLICATE_AGENT_REJECTED", "false"),
        "find_available_empty_rejected_404": os.environ.get("MANIFEST_FIND_AVAILABLE_EMPTY", "false"),
        "find_available_selected_with_skills": os.environ.get("MANIFEST_FIND_AVAILABLE_SELECTED", "false"),
        "assigned_session_load_increased": os.environ.get("MANIFEST_ASSIGN_LOAD_INCREASED", "false"),
        "assign_overcapacity_rejected": os.environ.get("MANIFEST_ASSIGN_OVERCAPACITY", "false"),
        "released_session_load_decreased": os.environ.get("MANIFEST_RELEASE_LOAD_DECREASED", "false"),
        "release_unassigned_rejected_404": os.environ.get("MANIFEST_RELEASE_UNASSIGNED_REJECTED", "false"),
        "assign_missing_session_rejected_404": os.environ.get("MANIFEST_ASSIGN_MISSING_SESSION_REJECTED", "false"),
        "assign_missing_agent_rejected_404": os.environ.get("MANIFEST_ASSIGN_MISSING_AGENT_REJECTED", "false"),
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
Customer/Agent acceptance summary (P2-6 slice 3)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/customer-agent-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/customer-agent-XXXXXX.sqlite")"

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
CUSTOMERS_BASE="$SERVIFY_URL/api/customers"
AGENTS_BASE="$SERVIFY_URL/api/agents"
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

# N0：未认证读客户列表 → 401。
request_capture "unauthenticated-list" "$CUSTOMERS_BASE"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-list.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"CA Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)
# POST /api/customers 一次创建 user+customer（注册接口不会自动建 customers 行）。
request_capture "customer-create-c1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"ca-cust-'"$TIMESTAMP"'","email":"ca-cust-'"$TIMESTAMP"'@servify.io","name":"轨迹客户甲'"$TIMESTAMP"'","industry":"manufacturing"}' \
  "$CUSTOMERS_BASE"
CUSTOMER_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$CUSTOMER_ID" ] && [ "$CUSTOMER_ID" != "None" ]; then
  CUSTOMER_CREATED=true
fi
append_summary "customer_id=$CUSTOMER_ID customer_created=$CUSTOMER_CREATED"

# 空活动对照客户 + 筛选对照。
request_capture "customer-create-c2" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"ca-other-'"$TIMESTAMP"'","email":"ca-other-'"$TIMESTAMP"'@servify.io","name":"轨迹客户乙'"$TIMESTAMP"'","industry":"retail"}' \
  "$CUSTOMERS_BASE"
OTHER_CUSTOMER_ID="$(json_extract "d.get('id')")"
append_summary "other_customer_id=$OTHER_CUSTOMER_ID"

# 4) 客户列表（§4）：全量分页 + search 筛选。
append_summary "step=customer_list"
request_capture "customer-list" -H "$AUTH" "$CUSTOMERS_BASE?page=1&page_size=50"
LIST_TOTAL="$(json_extract "d.get('total', 0)")"
LIST_HAS_BOTH="$(json_extract "any(c.get('id') == $CUSTOMER_ID for c in d.get('data', [])) and any(c.get('id') == $OTHER_CUSTOMER_ID for c in d.get('data', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${LIST_TOTAL:-0}" -ge 2 ] && [ "$LIST_HAS_BOTH" = "True" ]; then
  CUSTOMER_LISTED=true
fi
append_summary "list_total=$LIST_TOTAL list_has_both=$LIST_HAS_BOTH"

request_capture "customer-list-search" -H "$AUTH" --get "$CUSTOMERS_BASE" --data-urlencode "search=轨迹客户甲$TIMESTAMP" --data-urlencode "page=1" --data-urlencode "page_size=10"
SEARCH_TOTAL="$(json_extract "d.get('total', 0)")"
SEARCH_FIRST_ID="$(json_extract "next(iter(d.get('data', [])), {}).get('id', '')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${SEARCH_TOTAL:-0}" = "1" ] && [ "$SEARCH_FIRST_ID" = "$CUSTOMER_ID" ]; then
  append_summary "customer_listed=true (total=$LIST_TOTAL, search_total=$SEARCH_TOTAL)"
else
  CUSTOMER_LISTED=false
  append_summary "customer_listed=false (total=$LIST_TOTAL, search_total=$SEARCH_TOTAL, search_first=$SEARCH_FIRST_ID)"
fi
append_summary "customer_listed=$CUSTOMER_LISTED"

# 5) 活动轨迹（§4）：两张工单入 recent_tickets 且倒序；无活动客户为空对照。
append_summary "step=customer_activity"
request_capture "ticket-create-t1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"轨迹工单一","description":"客户活动轨迹验收","customer_id":'"$CUSTOMER_ID"'}' \
  "$SERVIFY_URL/api/tickets"
TICKET_T1="$(json_extract "d.get('id')")"
request_capture "ticket-create-t2" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"轨迹工单二","description":"客户活动轨迹验收","customer_id":'"$CUSTOMER_ID"'}' \
  "$SERVIFY_URL/api/tickets"
TICKET_T2="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" != "201" ] || [ -z "$TICKET_T1" ] || [ -z "$TICKET_T2" ] || [ "$TICKET_T1" = "None" ] || [ "$TICKET_T2" = "None" ]; then
  echo "❌ 工单创建失败" >&2
  exit 1
fi
append_summary "ticket_t1=$TICKET_T1 ticket_t2=$TICKET_T2"

request_capture "customer-activity-c1" -H "$AUTH" "$CUSTOMERS_BASE/$CUSTOMER_ID/activity?limit=10"
ACT_CUSTOMER="$(json_extract "d.get('customer_id')")"
ACT_TICKET_COUNT="$(json_extract "len(d.get('recent_tickets', []))")"
ACT_LATEST_ID="$(json_extract "next(iter(d.get('recent_tickets', [])), {}).get('id', '')")"
ACT_BOTH="$(json_extract "any(t.get('id') == $TICKET_T1 for t in d.get('recent_tickets', [])) and any(t.get('id') == $TICKET_T2 for t in d.get('recent_tickets', []))")"
request_capture "customer-activity-c2" -H "$AUTH" "$CUSTOMERS_BASE/$OTHER_CUSTOMER_ID/activity?limit=10"
ACT_OTHER_TICKET_COUNT="$(json_extract "len(d.get('recent_tickets', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$ACT_CUSTOMER" = "$CUSTOMER_ID" ] \
  && [ "${ACT_TICKET_COUNT:-0}" = "2" ] && [ "$ACT_BOTH" = "True" ] \
  && [ "$ACT_LATEST_ID" = "$TICKET_T2" ] && [ "${ACT_OTHER_TICKET_COUNT:-0}" = "0" ]; then
  CUSTOMER_ACTIVITY_RECONCILED=true
fi
append_summary "activity_customer=$ACT_CUSTOMER tickets=$ACT_TICKET_COUNT latest=$ACT_LATEST_ID other_tickets=$ACT_OTHER_TICKET_COUNT"
append_summary "customer_activity_reconciled=$CUSTOMER_ACTIVITY_RECONCILED"

# 6) 客服实体（§5 创建客服）：两个客服 user+实体，重复创建 409。
append_summary "step=agent_setup"

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

AGENT_A_USER="ca-agent-a-${TIMESTAMP}"
AGENT_B_USER="ca-agent-b-${TIMESTAMP}"
AGENT_A_ID="$(register_agent_user "$AGENT_A_USER")"
AGENT_B_ID="$(register_agent_user "$AGENT_B_USER")"
append_summary "agent_a=$AGENT_A_ID agent_b=$AGENT_B_ID"

# A1 技能含 billing 且 max_concurrent=1（满载负例输入）；A2 通用容量 5。
request_capture "agent-create-a1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_A_ID"',"department":"acceptance","skills":"transfer,billing","max_concurrent":1}' \
  "$AGENTS_BASE"
A1_ID="$(json_extract "d.get('id') or d.get('user_id')")"
request_capture "agent-create-a2" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_B_ID"',"department":"acceptance","skills":"transfer","max_concurrent":5}' \
  "$AGENTS_BASE"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$A1_ID" ] && [ "$A1_ID" != "None" ]; then
  AGENT_CREATED=true
fi
append_summary "agent_a1_entity=$A1_ID agent_created=$AGENT_CREATED"

# 负例：同一用户重复成为客服 → 409。
request_capture "agent-create-duplicate" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_A_ID"',"department":"acceptance","skills":"transfer","max_concurrent":5}' \
  "$AGENTS_BASE"
if [ "$RESPONSE_STATUS" = "409" ] && printf '%s' "$RESPONSE_RAW" | grep -q "already an agent"; then
  DUPLICATE_AGENT_REJECTED_409=true
fi
append_summary "duplicate_agent_rejected_409=$DUPLICATE_AGENT_REJECTED_409"

# 7) 查找可用客服（§5）：全离线 404 负例 → 上线 → 命中 + 技能偏好。
append_summary "step=find_available"
request_capture "find-available-empty" -H "$AUTH" "$AGENTS_BASE/find-available"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "No available agent found"; then
  FIND_AVAILABLE_EMPTY_REJECTED_404=true
fi
append_summary "find_available_empty_rejected_404=$FIND_AVAILABLE_EMPTY_REJECTED_404"

set_agent_state() {
  local agent_id=$1 action=$2 prefix=$3
  request_capture "$prefix-$agent_id" -X POST -H "$AUTH" "$AGENTS_BASE/$agent_id/$action"
  if [ "$RESPONSE_STATUS" != "200" ]; then
    echo "❌ 客服 $agent_id $action 失败" >&2
    exit 1
  fi
}

set_agent_state "$AGENT_A_ID" "online" "agent-online"
set_agent_state "$AGENT_B_ID" "online" "agent-online"

request_capture "find-available-any" -H "$AUTH" "$AGENTS_BASE/find-available"
ANY_USER_ID="$(json_extract "d.get('user_id')")"
ANY_STATUS="$(json_extract "d.get('status')")"
request_capture "find-available-skills" -H "$AUTH" "$AGENTS_BASE/find-available?skills=billing"
SKILLS_USER_ID="$(json_extract "d.get('user_id')")"
# 打分：billing 匹配比 ×2 使 A1（5 分）确定压过 A2（3 分）。
if [ "$RESPONSE_STATUS" = "200" ] && [ "$SKILLS_USER_ID" = "$AGENT_A_ID" ] \
  && [ "$ANY_STATUS" = "online" ] && { [ "$ANY_USER_ID" = "$AGENT_A_ID" ] || [ "$ANY_USER_ID" = "$AGENT_B_ID" ]; }; then
  FIND_AVAILABLE_SELECTED=true
fi
append_summary "find_any=$ANY_USER_ID($ANY_STATUS) find_skills=$SKILLS_USER_ID"
append_summary "find_available_selected=$FIND_AVAILABLE_SELECTED"

# 8) 会话分配/释放（§5）：WS 建三个干净会话（客服离线 + 中性消息，避开
#    自动转人工），客服上线后手动 assign/release，负载经 /agents/online 对账。
append_summary "step=assign_release"
set_agent_state "$AGENT_A_ID" "offline" "agent-offline"
set_agent_state "$AGENT_B_ID" "offline" "agent-offline"

# visitor_ws_message URL PAYLOAD —— python3 标准库最小 WS 客户端（同第一刀）。
visitor_ws_message() {
  local url=$1
  local payload=$2
  WS_URL="$url" WS_PAYLOAD="$payload" python3 - <<'PY'
import base64
import hashlib
import os
import socket
import ssl
import struct
import sys
import urllib.parse

url = os.environ["WS_URL"]
payload = os.environ["WS_PAYLOAD"].encode("utf-8")

u = urllib.parse.urlparse(url)
secure = u.scheme in ("wss", "https")
host = u.hostname or "127.0.0.1"
port = u.port or (443 if secure else 80)
path = u.path or "/"
if u.query:
    path += "?" + u.query

sock = socket.create_connection((host, port), timeout=10)
try:
    if secure:
        ctx = ssl.create_default_context()
        ctx.check_hostname = False
        ctx.verify_mode = ssl.CERT_NONE
        sock = ctx.wrap_socket(sock, server_hostname=host)

    key = base64.b64encode(os.urandom(16)).decode()
    request = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}:{port}\r\n"
        "Upgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\n"
        "Sec-WebSocket-Version: 13\r\n"
        "\r\n"
    )
    sock.sendall(request.encode())

    buf = b""
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            sys.stderr.write("ws handshake: connection closed\n")
            sys.exit(1)
        buf += chunk
    head, _, _rest = buf.partition(b"\r\n\r\n")
    status_line = head.split(b"\r\n")[0].decode("latin-1")
    if " 101 " not in status_line:
        sys.stderr.write("ws handshake failed: %s\n" % status_line)
        sys.exit(1)

    expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest())
    accept = head.split(b"Sec-WebSocket-Accept:", 1)[1].split(b"\r\n", 1)[0].strip() if b"Sec-WebSocket-Accept:" in head else b""
    if accept != expected:
        sys.stderr.write("ws accept mismatch\n")
        sys.exit(1)

    mask_key = os.urandom(4)
    length = len(payload)
    if length < 126:
        header = struct.pack("!BB", 0x81, 0x80 | length)
    elif length < 65536:
        header = struct.pack("!BBH", 0x81, 0x80 | 126, length)
    else:
        header = struct.pack("!BBQ", 0x81, 0x80 | 127, length)
    masked = bytes(b ^ mask_key[i % 4] for i, b in enumerate(payload))
    sock.sendall(header + mask_key + masked)

    def recv_exact(n):
        data = b""
        while len(data) < n:
            chunk = sock.recv(n - len(data))
            if not chunk:
                raise EOFError("ws closed")
            data += chunk
        return data

    head = recv_exact(2)
    opcode = head[0] & 0x0F
    ln = head[1] & 0x7F
    if ln == 126:
        ln = struct.unpack("!H", recv_exact(2))[0]
    elif ln == 127:
        ln = struct.unpack("!Q", recv_exact(8))[0]
    body = recv_exact(ln) if ln else b""
    if opcode == 0x8:
        sys.stderr.write("ws closed by server\n")
        sys.exit(1)
    print("ws-message-sent opcode=%s bytes=%s" % (opcode, len(body)))
finally:
    sock.close()
PY
}

S1="ca-acc-s1-${TIMESTAMP}"
S2="ca-acc-s2-${TIMESTAMP}"
S3="ca-acc-s3-${TIMESTAMP}"
for sid in "$S1" "$S2" "$S3"; do
  visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${sid}" \
    "$(printf '{"type":"text-message","data":{"content":"ca-acceptance-msg %s"}}' "$sid")" \
    > "$EVIDENCE_DIR/ws-session-${sid}.txt" 2>&1
done
append_summary "ws_sessions_created=3"

set_agent_state "$AGENT_A_ID" "online" "agent-online"
set_agent_state "$AGENT_B_ID" "online" "agent-online"

agent_chat_load() {
  # $1 = agent user id；从 /agents/online（顶层数组，legacy AgentInfo）读 current_load。
  request_capture "$2" -H "$AUTH" "$AGENTS_BASE/online"
  json_extract "next(iter([a for a in (d if isinstance(d, list) else []) if a.get('user_id') == $1]), {}).get('current_load', -1)"
}

LOAD_BASELINE="$(agent_chat_load "$AGENT_A_ID" "online-baseline")"
append_summary "a1_load_baseline=$LOAD_BASELINE"

request_capture "assign-s1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S1"'"}' "$AGENTS_BASE/$AGENT_A_ID/assign-session"
ASSIGN_MSG="$(json_extract "d.get('message','')")"
LOAD_AFTER_ASSIGN="$(agent_chat_load "$AGENT_A_ID" "online-after-assign")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$ASSIGN_MSG" = "Session assigned successfully" ] \
  && [ "${LOAD_AFTER_ASSIGN:--1}" = "1" ] && [ "${LOAD_BASELINE:--1}" = "0" ]; then
  SESSION_ASSIGNED_LOAD_INCREASED=true
fi
append_summary "assign_s1_msg=$ASSIGN_MSG a1_load_after_assign=$LOAD_AFTER_ASSIGN"
append_summary "assigned_session_load_increased=$SESSION_ASSIGNED_LOAD_INCREASED"

# 负例：A1 容量 1 已满 → 500 at maximum capacity。
request_capture "assign-overcapacity" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S2"'"}' "$AGENTS_BASE/$AGENT_A_ID/assign-session"
OVERCAPACITY_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "500" ] && printf '%s' "$OVERCAPACITY_MSG" | grep -q "at maximum capacity"; then
  ASSIGN_OVERCAPACITY_REJECTED=true
fi
append_summary "assign_overcapacity_rejected=$ASSIGN_OVERCAPACITY_REJECTED"

request_capture "assign-s3" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S3"'"}' "$AGENTS_BASE/$AGENT_B_ID/assign-session"
if [ "$RESPONSE_STATUS" != "200" ]; then
  echo "❌ S3 分配给 B 失败" >&2
  exit 1
fi
append_summary "assign_s3_status=200"

request_capture "release-s1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S1"'"}' "$AGENTS_BASE/$AGENT_A_ID/release-session"
RELEASE_MSG="$(json_extract "d.get('message','')")"
LOAD_AFTER_RELEASE="$(agent_chat_load "$AGENT_A_ID" "online-after-release")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$RELEASE_MSG" = "Session released successfully" ] \
  && [ "${LOAD_AFTER_RELEASE:--1}" = "0" ]; then
  SESSION_RELEASED_LOAD_DECREASED=true
fi
append_summary "release_s1_msg=$RELEASE_MSG a1_load_after_release=$LOAD_AFTER_RELEASE"
append_summary "released_session_load_decreased=$SESSION_RELEASED_LOAD_DECREASED"

# 负例：释放未分配给该客服的会话 → 404。
request_capture "release-unassigned" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S2"'"}' "$AGENTS_BASE/$AGENT_A_ID/release-session"
RELEASE_UNASSIGNED_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RELEASE_UNASSIGNED_MSG" | grep -q "not found or not assigned"; then
  RELEASE_UNASSIGNED_REJECTED_404=true
fi
append_summary "release_unassigned_rejected_404=$RELEASE_UNASSIGNED_REJECTED_404"

# 负例：分配不存在的会话 → 404 session not found。
request_capture "assign-missing-session" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"ca-no-such-session-'"$TIMESTAMP"'"}' "$AGENTS_BASE/$AGENT_A_ID/assign-session"
MISSING_SESSION_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$MISSING_SESSION_MSG" | grep -q "session not found"; then
  ASSIGN_MISSING_SESSION_REJECTED_404=true
fi
append_summary "assign_missing_session_rejected_404=$ASSIGN_MISSING_SESSION_REJECTED_404"

# 负例：分配给不存在的客服 → 404 agent not found。
request_capture "assign-missing-agent" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S2"'"}' "$AGENTS_BASE/999999/assign-session"
MISSING_AGENT_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$MISSING_AGENT_MSG" | grep -q "agent not found"; then
  ASSIGN_MISSING_AGENT_REJECTED_404=true
fi
append_summary "assign_missing_agent_rejected_404=$ASSIGN_MISSING_AGENT_REJECTED_404"

# 9) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED CUSTOMER_CREATED CUSTOMER_LISTED \
  CUSTOMER_ACTIVITY_RECONCILED AGENT_CREATED DUPLICATE_AGENT_REJECTED_409 \
  FIND_AVAILABLE_EMPTY_REJECTED_404 FIND_AVAILABLE_SELECTED \
  SESSION_ASSIGNED_LOAD_INCREASED ASSIGN_OVERCAPACITY_REJECTED \
  SESSION_RELEASED_LOAD_DECREASED RELEASE_UNASSIGNED_REJECTED_404 \
  ASSIGN_MISSING_SESSION_REJECTED_404 ASSIGN_MISSING_AGENT_REJECTED_404; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Customer/Agent acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Customer/Agent acceptance 通过: 客户列表筛选 / 活动轨迹对账 / 建客服与重复负例 / find-available 空池与技能偏好 / 手动分配释放负载对账 / 满载与未分配与不存在资源负例 全部真实留档"
