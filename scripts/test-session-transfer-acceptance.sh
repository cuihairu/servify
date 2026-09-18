#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Session transfer acceptance 测试开始（P2-6 第一刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/session-transfer"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"st-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"st-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18098}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
AGENTS_READY=false
AGENT_ONLINE_TOGGLED=false
VISITOR_SESSION_CREATED=false
TRANSFER_TO_HUMAN_DIRECT_OK=false
TRANSFER_HISTORY_RETRIEVABLE=false
TRANSFER_TO_WAITING_QUEUED=false
WAITING_QUEUE_LISTED=false
DUPLICATE_TRANSFER_IDEMPOTENT=false
PROCESS_QUEUE_DISPATCHED=false
TRANSFER_TO_AGENT_OK=false
CANCEL_WAITING_OK=false
CHECK_AUTO_OK=false
RECENT_HISTORY_LISTED=false
UNKNOWN_SESSION_REJECTED=false
OFFLINE_TARGET_REJECTED=false
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
    m = re.search(r'[\\{\\[].*[\\}\\]]', raw, re.S)
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
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  MANIFEST_AGENTS_READY="${AGENTS_READY:-false}" \
  MANIFEST_AGENT_ONLINE_TOGGLED="${AGENT_ONLINE_TOGGLED:-false}" \
  MANIFEST_VISITOR_SESSION_CREATED="${VISITOR_SESSION_CREATED:-false}" \
  MANIFEST_TO_HUMAN_DIRECT="${TRANSFER_TO_HUMAN_DIRECT_OK:-false}" \
  MANIFEST_HISTORY_RETRIEVABLE="${TRANSFER_HISTORY_RETRIEVABLE:-false}" \
  MANIFEST_TO_WAITING_QUEUED="${TRANSFER_TO_WAITING_QUEUED:-false}" \
  MANIFEST_WAITING_LISTED="${WAITING_QUEUE_LISTED:-false}" \
  MANIFEST_DUPLICATE_IDEMPOTENT="${DUPLICATE_TRANSFER_IDEMPOTENT:-false}" \
  MANIFEST_QUEUE_DISPATCHED="${PROCESS_QUEUE_DISPATCHED:-false}" \
  MANIFEST_TO_AGENT_OK="${TRANSFER_TO_AGENT_OK:-false}" \
  MANIFEST_CANCEL_WAITING_OK="${CANCEL_WAITING_OK:-false}" \
  MANIFEST_CHECK_AUTO_OK="${CHECK_AUTO_OK:-false}" \
  MANIFEST_RECENT_HISTORY="${RECENT_HISTORY_LISTED:-false}" \
  MANIFEST_UNKNOWN_SESSION_REJECTED="${UNKNOWN_SESSION_REJECTED:-false}" \
  MANIFEST_OFFLINE_TARGET_REJECTED="${OFFLINE_TARGET_REJECTED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "session-transfer",
    "mode": "runtime-transfer-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "agents_ready": os.environ.get("MANIFEST_AGENTS_READY", "false"),
        "agent_online_toggled": os.environ.get("MANIFEST_AGENT_ONLINE_TOGGLED", "false"),
        "visitor_session_created": os.environ.get("MANIFEST_VISITOR_SESSION_CREATED", "false"),
        "transfer_to_human_direct_ok": os.environ.get("MANIFEST_TO_HUMAN_DIRECT", "false"),
        "transfer_history_retrievable": os.environ.get("MANIFEST_HISTORY_RETRIEVABLE", "false"),
        "transfer_to_waiting_queued": os.environ.get("MANIFEST_TO_WAITING_QUEUED", "false"),
        "waiting_queue_listed": os.environ.get("MANIFEST_WAITING_LISTED", "false"),
        "duplicate_transfer_idempotent": os.environ.get("MANIFEST_DUPLICATE_IDEMPOTENT", "false"),
        "process_queue_dispatched": os.environ.get("MANIFEST_QUEUE_DISPATCHED", "false"),
        "transfer_to_agent_ok": os.environ.get("MANIFEST_TO_AGENT_OK", "false"),
        "cancel_waiting_ok": os.environ.get("MANIFEST_CANCEL_WAITING_OK", "false"),
        "check_auto_ok": os.environ.get("MANIFEST_CHECK_AUTO_OK", "false"),
        "recent_history_listed": os.environ.get("MANIFEST_RECENT_HISTORY", "false"),
        "unknown_session_rejected": os.environ.get("MANIFEST_UNKNOWN_SESSION_REJECTED", "false"),
        "offline_target_rejected": os.environ.get("MANIFEST_OFFLINE_TARGET_REJECTED", "false"),
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
Session transfer acceptance summary (P2-6 slice 1)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/session-transfer-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/session-transfer-XXXXXX.sqlite")"

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
TRANSFER_BASE="$SERVIFY_URL/api/session-transfer"
if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"

# visitor_ws_message URL PAYLOAD —— 用 python3 标准库实现最小 WS 客户端,
# 走真实访客入口(/api/v1/ws)建会话,不依赖任何第三方库。
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

    expected = base64.b64encode(
        hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()
    ).decode()
    accept = ""
    for line in head.decode("latin-1").split("\r\n")[1:]:
        if ":" in line:
            k, v = line.split(":", 1)
            if k.strip().lower() == "sec-websocket-accept":
                accept = v.strip()
    if accept != expected:
        sys.stderr.write("ws accept key mismatch\n")
        sys.exit(1)

    mask = os.urandom(4)
    header = bytearray([0x81])
    n = len(payload)
    if n < 126:
        header.append(0x80 | n)
    elif n < 65536:
        header.append(0x80 | 126)
        header += struct.pack(">H", n)
    else:
        header.append(0x80 | 127)
        header += struct.pack(">Q", n)
    header += mask
    masked = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
    sock.sendall(bytes(header) + masked)

    # 等服务端处理完成:对端关闭即返回,长期保持连接则超时后继续
    sock.settimeout(5)
    try:
        while True:
            if not sock.recv(4096):
                break
    except (socket.timeout, OSError):
        pass
finally:
    sock.close()
PY
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

# 3) 用户与客服实体（先建好但保持离线：WS 建会话时无在线客服才不会触发 auto-assign，
#    未分配会话正是 to-human 直转/排队链路的输入）。
append_summary "step=users_setup"

# N0：未认证读等待队列 → 401。
request_capture "unauthenticated-waiting" "$TRANSFER_BASE/waiting"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-waiting.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"ST Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"

register_agent_user() {
  # $1 = 用户名，回显 user id（转接链路的 target_agent_id 即 user_id）。
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

TIMESTAMP=$(date +%s)
AGENT_A_USER="st-agent-a-${TIMESTAMP}"
AGENT_B_USER="st-agent-b-${TIMESTAMP}"
AGENT_A_ID="$(register_agent_user "$AGENT_A_USER")"
AGENT_B_ID="$(register_agent_user "$AGENT_B_USER")"

for agent_id in "$AGENT_A_ID" "$AGENT_B_ID"; do
  request_capture "agent-create-$agent_id" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"user_id":'"$agent_id"',"department":"acceptance","skills":"transfer","max_concurrent":5}' \
    "$SERVIFY_URL/api/agents"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 客服实体 $agent_id 创建失败" >&2
    exit 1
  fi
done
AGENTS_READY=true
append_summary "agents_ready=$AGENTS_READY agent_a=$AGENT_A_ID agent_b=$AGENT_B_ID"

set_agent_state() {
  # $1 = agent user id，$2 = online|offline，$3 = 证据名前缀
  local agent_id=$1 action=$2 prefix=$3
  request_capture "$prefix-$agent_id" -X POST \
    -H "Authorization: Bearer $ADMIN_TOKEN" \
    "$SERVIFY_URL/api/agents/$agent_id/$action"
  if [ "$RESPONSE_STATUS" != "200" ]; then
    echo "❌ 客服 $agent_id $action 失败" >&2
    exit 1
  fi
}

S1="st-acc-s1-${TIMESTAMP}"
S2="st-acc-s2-${TIMESTAMP}"
S3="st-acc-s3-${TIMESTAMP}"
S4="st-acc-s4-${TIMESTAMP}"

# 4) 转人工直转：会话建立于无在线客服时（未分配）→ 客服上线 → to-human 直转成功。
append_summary "step=transfer_to_human"
visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${S1}" \
  "$(printf '{"type":"text-message","data":{"content":"st-acceptance-msg-s1 %s"}}' "$S1")"
VISITOR_SESSION_CREATED=true
append_summary "visitor_session_created=$VISITOR_SESSION_CREATED"

set_agent_state "$AGENT_A_ID" "online" "agent-online"
set_agent_state "$AGENT_B_ID" "online" "agent-online"
request_capture "agents-online-list" -H "Authorization: Bearer $ADMIN_TOKEN" "$SERVIFY_URL/api/agents/online"
ONLINE_COUNT="$(json_extract "isinstance(d, list) and len(d) or 0")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${ONLINE_COUNT:-0}" -ge 2 ]; then
  AGENT_ONLINE_TOGGLED=true
fi
append_summary "online_count=$ONLINE_COUNT agent_online_toggled=$AGENT_ONLINE_TOGGLED"

request_capture "to-human-direct" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S1"'","reason":"验收：转人工"}' "$TRANSFER_BASE/to-human"
S1_SUCCESS="$(json_extract "d.get('success')")"
S1_AGENT="$(json_extract "d.get('new_agent_id') or 0")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$S1_SUCCESS" = "True" ] && [ "${S1_AGENT:-0}" -gt 0 ]; then
  TRANSFER_TO_HUMAN_DIRECT_OK=true
fi
append_summary "s1_success=$S1_SUCCESS s1_new_agent=$S1_AGENT"
append_summary "transfer_to_human_direct_ok=$TRANSFER_TO_HUMAN_DIRECT_OK"

request_capture "history-s1" -H "Authorization: Bearer $ADMIN_TOKEN" "$TRANSFER_BASE/history/$S1"
S1_HISTORY_COUNT="$(json_extract "isinstance(d, list) and len(d) or 0")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${S1_HISTORY_COUNT:-0}" -ge 1 ]; then
  TRANSFER_HISTORY_RETRIEVABLE=true
fi
append_summary "s1_history_count=$S1_HISTORY_COUNT"
append_summary "transfer_history_retrievable=$TRANSFER_HISTORY_RETRIEVABLE"

# 5) 等待队列链路：无可用客服 → 排队 → 幂等重试 → 派发。
append_summary "step=waiting_queue"
set_agent_state "$AGENT_A_ID" "offline" "agent-offline"
set_agent_state "$AGENT_B_ID" "offline" "agent-offline"

visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${S2}" \
  "$(printf '{"type":"text-message","data":{"content":"st-acceptance-msg-s2 %s"}}' "$S2")"

request_capture "to-human-queued" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S2"'","reason":"验收：无可用客服排队"}' "$TRANSFER_BASE/to-human"
S2_WAITING="$(json_extract "d.get('is_waiting')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$S2_WAITING" = "True" ]; then
  TRANSFER_TO_WAITING_QUEUED=true
fi
append_summary "s2_is_waiting=$S2_WAITING"
append_summary "transfer_to_waiting_queued=$TRANSFER_TO_WAITING_QUEUED"

request_capture "waiting-list" -H "Authorization: Bearer $ADMIN_TOKEN" "$TRANSFER_BASE/waiting?status=waiting"
WAITING_COUNT="$(json_extract "d.get('count', 0)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${WAITING_COUNT:-0}" -ge 1 ]; then
  WAITING_QUEUE_LISTED=true
fi
append_summary "waiting_count=$WAITING_COUNT waiting_queue_listed=$WAITING_QUEUE_LISTED"

# 同会话重复转人工应幂等命中既有等待记录。
request_capture "to-human-duplicate" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S2"'","reason":"验收：重复转人工"}' "$TRANSFER_BASE/to-human"
DUP_WAITING="$(json_extract "d.get('is_waiting')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$DUP_WAITING" = "True" ]; then
  DUPLICATE_TRANSFER_IDEMPOTENT=true
fi
append_summary "duplicate_transfer_idempotent=$DUPLICATE_TRANSFER_IDEMPOTENT"

# 客服回到线 → 手动处理队列 → S2 派发成功且队列清空。
set_agent_state "$AGENT_A_ID" "online" "agent-online"
set_agent_state "$AGENT_B_ID" "online" "agent-online"
request_capture "process-queue" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$TRANSFER_BASE/process-queue"
PROCESSED="$(json_extract "d.get('processed', 0)")"
request_capture "waiting-list-after" -H "Authorization: Bearer $ADMIN_TOKEN" "$TRANSFER_BASE/waiting?status=waiting"
WAITING_AFTER="$(json_extract "d.get('count', 99)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${PROCESSED:-0}" -ge 1 ] && [ "${WAITING_AFTER:-99}" = "0" ]; then
  PROCESS_QUEUE_DISPATCHED=true
fi
append_summary "processed=$PROCESSED waiting_after=$WAITING_AFTER"
append_summary "process_queue_dispatched=$PROCESS_QUEUE_DISPATCHED"

# 6) 指定客服转接：B 下线构造离线目标负例，A 保持在线承接正例。
append_summary "step=transfer_to_agent"
set_agent_state "$AGENT_B_ID" "offline" "agent-offline"
visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${S3}" \
  "$(printf '{"type":"text-message","data":{"content":"st-acceptance-msg-s3 %s"}}' "$S3")"

# 负例：目标客服不在线 → 拒绝。
request_capture "to-agent-offline-rejected" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S3"'","target_agent_id":'"$AGENT_B_ID"',"reason":"验收：离线客服应拒绝"}' "$TRANSFER_BASE/to-agent"
OFFLINE_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "500" ] && printf '%s' "$OFFLINE_MSG" | grep -q "not online"; then
  OFFLINE_TARGET_REJECTED=true
fi
append_summary "offline_target_rejected=$OFFLINE_TARGET_REJECTED"

request_capture "to-agent-direct" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S3"'","target_agent_id":'"$AGENT_A_ID"',"reason":"验收：指定客服"}' "$TRANSFER_BASE/to-agent"
S3_AGENT="$(json_extract "d.get('new_agent_id') or 0")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$S3_AGENT" = "$AGENT_A_ID" ]; then
  TRANSFER_TO_AGENT_OK=true
fi
append_summary "s3_new_agent=$S3_AGENT transfer_to_agent_ok=$TRANSFER_TO_AGENT_OK"

# 7) 取消等待（含幂等重放）：全部客服下线，S4 只能入队。
append_summary "step=cancel_waiting"
set_agent_state "$AGENT_A_ID" "offline" "agent-offline"
visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${S4}" \
  "$(printf '{"type":"text-message","data":{"content":"st-acceptance-msg-s4 %s"}}' "$S4")"
request_capture "to-human-s4" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S4"'","reason":"验收：进队后取消"}' "$TRANSFER_BASE/to-human"

request_capture "cancel-waiting" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S4"'","reason":"验收：用户主动取消"}' "$TRANSFER_BASE/cancel"
CANCEL_STATUS=$RESPONSE_STATUS
# 幂等：再取消一次仍应 200。
request_capture "cancel-waiting-again" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S4"'","reason":"验收：幂等重放"}' "$TRANSFER_BASE/cancel"
CANCEL_AGAIN_STATUS=$RESPONSE_STATUS
request_capture "waiting-list-cancelled" -H "Authorization: Bearer $ADMIN_TOKEN" "$TRANSFER_BASE/waiting?status=waiting"
WAITING_CANCELLED="$(json_extract "d.get('count', 99)")"
if [ "$CANCEL_STATUS" = "200" ] && [ "$CANCEL_AGAIN_STATUS" = "200" ] && [ "${WAITING_CANCELLED:-99}" = "0" ]; then
  CANCEL_WAITING_OK=true
fi
append_summary "cancel_status=$CANCEL_STATUS cancel_again_status=$CANCEL_AGAIN_STATUS waiting_cancelled=$WAITING_CANCELLED"
append_summary "cancel_waiting_ok=$CANCEL_WAITING_OK"

# 8) 自动转接检查 + 近期历史对账 + 负例。
append_summary "step=check_and_history"
request_capture "check-auto" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"'"$S1"'","messages":[{"content":"我要投诉，转人工","sender":"user"}]}' \
  "$TRANSFER_BASE/check-auto"
SHOULD_TRANSFER_RAW="$(json_extract "str(d.get('should_transfer')).lower()")"
if [ "$RESPONSE_STATUS" = "200" ] && { [ "$SHOULD_TRANSFER_RAW" = "true" ] || [ "$SHOULD_TRANSFER_RAW" = "false" ]; }; then
  CHECK_AUTO_OK=true
fi
append_summary "should_transfer=$SHOULD_TRANSFER_RAW check_auto_ok=$CHECK_AUTO_OK"

request_capture "history-recent" -H "Authorization: Bearer $ADMIN_TOKEN" "$TRANSFER_BASE/history?limit=50"
RECENT_COUNT="$(json_extract "d.get('count', 0)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${RECENT_COUNT:-0}" -ge 2 ]; then
  RECENT_HISTORY_LISTED=true
fi
append_summary "recent_count=$RECENT_COUNT recent_history_listed=$RECENT_HISTORY_LISTED"

# 负例：不存在的会话转人工 → 拒绝（service 错误经 handler 统一 500）。
request_capture "to-human-unknown-session" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"session_id":"st-acc-not-exist-000","reason":"验收：不存在会话"}' "$TRANSFER_BASE/to-human"
UNKNOWN_MSG="$(json_extract "d.get('message','')")"
if [ "$RESPONSE_STATUS" = "500" ] && printf '%s' "$UNKNOWN_MSG" | grep -q "session not found"; then
  UNKNOWN_SESSION_REJECTED=true
fi
append_summary "unknown_session_rejected=$UNKNOWN_SESSION_REJECTED"

# 9) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED AGENTS_READY AGENT_ONLINE_TOGGLED \
  VISITOR_SESSION_CREATED TRANSFER_TO_HUMAN_DIRECT_OK TRANSFER_HISTORY_RETRIEVABLE \
  TRANSFER_TO_WAITING_QUEUED WAITING_QUEUE_LISTED DUPLICATE_TRANSFER_IDEMPOTENT \
  PROCESS_QUEUE_DISPATCHED TRANSFER_TO_AGENT_OK CANCEL_WAITING_OK CHECK_AUTO_OK \
  RECENT_HISTORY_LISTED UNKNOWN_SESSION_REJECTED OFFLINE_TARGET_REJECTED; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ Session transfer acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Session transfer acceptance 通过: 转人工直转 / 等待队列(排队·幂等·派发·取消) / 指定客服转接 / 历史对账 / 负例拒绝 全部真实留档"
