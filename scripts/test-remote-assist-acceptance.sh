#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 RemoteAssist/Suggest acceptance 测试开始（P2-6 第六刀）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/remote-assist"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"st6-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"st6-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18103}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
VISITOR_SESSION_CREATED=false
ASSIST_MISSING_CONVERSATION_REJECTED=false
ASSIST_STARTED=false
ASSIST_LISTED=false
ASSIST_GET_RECONCILED=false
ANNOTATION_ADDED=false
ANNOTATION_LISTED_ASCENDING=false
ANNOTATION_SHAPE_INVALID_REJECTED=false
ANNOTATION_DELETED_AND_GONE=false
ANNOTATION_MISSING_DELETE_REJECTED=false
ASSIST_ENDED_WITH_RECORDING=false
ASSIST_REEND_CONFLICT_REJECTED=false
ASSIST_END_MISSING_REJECTED=false
SUGGEST_UNAUTH_REJECTED=false
SUGGEST_GET_TICKET_RECONCILED=false
SUGGEST_POST_TICKET_RECONCILED=false
SUGGEST_INTENT_PRESENT=false
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

write_manifest() {
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  MANIFEST_VISITOR_SESSION="${VISITOR_SESSION_CREATED:-false}" \
  MANIFEST_ASSIST_MISSING_CONV="${ASSIST_MISSING_CONVERSATION_REJECTED:-false}" \
  MANIFEST_ASSIST_STARTED="${ASSIST_STARTED:-false}" \
  MANIFEST_ASSIST_LISTED="${ASSIST_LISTED:-false}" \
  MANIFEST_ASSIST_GET="${ASSIST_GET_RECONCILED:-false}" \
  MANIFEST_ANNOTATION_ADDED="${ANNOTATION_ADDED:-false}" \
  MANIFEST_ANNOTATION_LISTED="${ANNOTATION_LISTED_ASCENDING:-false}" \
  MANIFEST_ANNOTATION_SHAPE_INVALID="${ANNOTATION_SHAPE_INVALID_REJECTED:-false}" \
  MANIFEST_ANNOTATION_DELETED="${ANNOTATION_DELETED_AND_GONE:-false}" \
  MANIFEST_ANNOTATION_MISSING_DELETE="${ANNOTATION_MISSING_DELETE_REJECTED:-false}" \
  MANIFEST_ASSIST_ENDED="${ASSIST_ENDED_WITH_RECORDING:-false}" \
  MANIFEST_ASSIST_REEND_CONFLICT="${ASSIST_REEND_CONFLICT_REJECTED:-false}" \
  MANIFEST_ASSIST_END_MISSING="${ASSIST_END_MISSING_REJECTED:-false}" \
  MANIFEST_SUGGEST_UNAUTH="${SUGGEST_UNAUTH_REJECTED:-false}" \
  MANIFEST_SUGGEST_GET="${SUGGEST_GET_TICKET_RECONCILED:-false}" \
  MANIFEST_SUGGEST_POST="${SUGGEST_POST_TICKET_RECONCILED:-false}" \
  MANIFEST_SUGGEST_INTENT="${SUGGEST_INTENT_PRESENT:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "remote-assist",
    "mode": "runtime-assist-chain",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_rejected_401": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "visitor_session_created": os.environ.get("MANIFEST_VISITOR_SESSION", "false"),
        "assist_start_missing_conversation_rejected_404": os.environ.get("MANIFEST_ASSIST_MISSING_CONV", "false"),
        "assist_started": os.environ.get("MANIFEST_ASSIST_STARTED", "false"),
        "assist_listed": os.environ.get("MANIFEST_ASSIST_LISTED", "false"),
        "assist_get_reconciled": os.environ.get("MANIFEST_ASSIST_GET", "false"),
        "annotation_added": os.environ.get("MANIFEST_ANNOTATION_ADDED", "false"),
        "annotation_listed_ascending": os.environ.get("MANIFEST_ANNOTATION_LISTED", "false"),
        "annotation_shape_invalid_rejected_400": os.environ.get("MANIFEST_ANNOTATION_SHAPE_INVALID", "false"),
        "annotation_deleted_and_gone": os.environ.get("MANIFEST_ANNOTATION_DELETED", "false"),
        "annotation_missing_delete_rejected_404": os.environ.get("MANIFEST_ANNOTATION_MISSING_DELETE", "false"),
        "assist_ended_with_recording": os.environ.get("MANIFEST_ASSIST_ENDED", "false"),
        "assist_reend_conflict_rejected_409": os.environ.get("MANIFEST_ASSIST_REEND_CONFLICT", "false"),
        "assist_end_missing_rejected_404": os.environ.get("MANIFEST_ASSIST_END_MISSING", "false"),
        "suggest_unauthenticated_rejected_401": os.environ.get("MANIFEST_SUGGEST_UNAUTH", "false"),
        "suggest_get_ticket_reconciled": os.environ.get("MANIFEST_SUGGEST_GET", "false"),
        "suggest_post_ticket_reconciled": os.environ.get("MANIFEST_SUGGEST_POST", "false"),
        "suggest_intent_present": os.environ.get("MANIFEST_SUGGEST_INTENT", "false"),
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
RemoteAssist/Suggest acceptance summary (P2-6 slice 6)
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

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/remote-assist-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/remote-assist-XXXXXX.sqlite")"

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
ASSIST_BASE="$SERVIFY_URL/api/remote-assist"
SUGGEST_BASE="$SERVIFY_URL/api/assist/suggest"
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

# 3) 样本数据：admin 与真实访客会话。
append_summary "step=setup"

# N0：未认证读远程协助列表 → 401。
request_capture "unauthenticated-assist" "$ASSIST_BASE/sessions"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/unauthenticated-assist.txt" && UNAUTH_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTH_REJECTED"

request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"ST6 Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"
ADMIN_USER_ID="$(json_extract "d.get('user',{}).get('id','')")"
AUTH="Authorization: Bearer $ADMIN_TOKEN"

TIMESTAMP=$(date +%s)

# WS 真实访客入口建会话（消息内容避开"人工/客服"等转人工关键词）。
CONV_SESSION_ID="ra-acc-conv-${TIMESTAMP}"
visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${CONV_SESSION_ID}" \
  "$(printf '{"type":"text-message","data":{"content":"ra-acceptance-msg %s"}}' "$CONV_SESSION_ID")"
VISITOR_SESSION_CREATED=true
append_summary "conv_session_id=$CONV_SESSION_ID"
append_summary "visitor_session_created=$VISITOR_SESSION_CREATED"

# 4) 远程协助会话链路（§远程协助）：负例 → 发起 → 列表 → 详情。
append_summary "step=assist_chain"

# 负例：会话不存在 → 404 remote assist session not found。
request_capture "assist-start-missing-conversation" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"conversation_session_id":"ra-acc-no-such-000"}' "$ASSIST_BASE/sessions"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "remote assist session not found"; then
  ASSIST_MISSING_CONVERSATION_REJECTED=true
fi
append_summary "assist_start_missing_conversation_rejected_404=$ASSIST_MISSING_CONVERSATION_REJECTED"

request_capture "assist-start" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"conversation_session_id":"'"$CONV_SESSION_ID"'","agent_user_id":'"$ADMIN_USER_ID"'}' \
  "$ASSIST_BASE/sessions"
ASSIST_ID="$(json_extract "d.get('id')")"
ASSIST_STATUS="$(json_extract "d.get('status','')")"
ASSIST_CONV="$(json_extract "d.get('conversation_session_id','')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$ASSIST_ID" ] && [ "$ASSIST_ID" != "None" ] \
  && [ "$ASSIST_STATUS" = "active" ] && [ "$ASSIST_CONV" = "$CONV_SESSION_ID" ]; then
  ASSIST_STARTED=true
fi
append_summary "assist_id=$ASSIST_ID assist_status=$ASSIST_STATUS"
append_summary "assist_started=$ASSIST_STARTED"

request_capture "assist-list" -H "$AUTH" --get "$ASSIST_BASE/sessions" \
  --data-urlencode "conversation_session_id=$CONV_SESSION_ID"
LIST_TOTAL="$(json_extract "d.get('total', -1)")"
LIST_HAS_ASSIST="$(json_extract "any(s.get('id') == $ASSIST_ID for s in d.get('items', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${LIST_TOTAL:--1}" = "1" ] && [ "$LIST_HAS_ASSIST" = "True" ]; then
  ASSIST_LISTED=true
fi
append_summary "assist_list_total=$LIST_TOTAL"
append_summary "assist_listed=$ASSIST_LISTED"

request_capture "assist-get" -H "$AUTH" "$ASSIST_BASE/sessions/$ASSIST_ID"
GET_STATUS="$(json_extract "d.get('status','')")"
GET_AGENT="$(json_extract "d.get('agent_user_id', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$GET_STATUS" = "active" ] && [ "${GET_AGENT:--1}" = "$ADMIN_USER_ID" ]; then
  ASSIST_GET_RECONCILED=true
fi
append_summary "assist_get_status=$GET_STATUS assist_get_agent=$GET_AGENT"
append_summary "assist_get_reconciled=$ASSIST_GET_RECONCILED"

# 5) 标注链路：两条标注（乱序时间戳）→ 升序对账 → 非法 shape → 删除 → 不存在删除。
append_summary "step=annotation_chain"

request_capture "annotation-add-1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"timestamp_ms":12000,"shape":"rect","payload":{"x":0.1,"y":0.2,"color":"#f5222d"}}' \
  "$ASSIST_BASE/sessions/$ASSIST_ID/annotations"
ANN1_ID="$(json_extract "d.get('id')")"
ANN1_SHAPE="$(json_extract "d.get('shape','')")"

request_capture "annotation-add-2" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"timestamp_ms":5000,"shape":"freehand","payload":{"points":[0.3,0.4]}}' \
  "$ASSIST_BASE/sessions/$ASSIST_ID/annotations"
ANN2_ID="$(json_extract "d.get('id')")"
if [ "$RESPONSE_STATUS" = "201" ] && [ -n "$ANN1_ID" ] && [ "$ANN1_ID" != "None" ] \
  && [ -n "$ANN2_ID" ] && [ "$ANN2_ID" != "None" ] && [ "$ANN1_SHAPE" = "rect" ]; then
  ANNOTATION_ADDED=true
fi
append_summary "annotation_ids=$ANN1_ID,$ANN2_ID"
append_summary "annotation_added=$ANNOTATION_ADDED"

request_capture "annotation-list" -H "$AUTH" "$ASSIST_BASE/sessions/$ASSIST_ID/annotations"
ANN_TOTAL="$(json_extract "d.get('total', -1)")"
ANN_ASC="$(json_extract "[a.get('timestamp_ms') for a in d.get('items', [])] == sorted(a.get('timestamp_ms') for a in d.get('items', [])) and len(d.get('items', [])) == 2")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${ANN_TOTAL:--1}" = "2" ] && [ "$ANN_ASC" = "True" ]; then
  ANNOTATION_LISTED_ASCENDING=true
fi
append_summary "annotation_list_total=$ANN_TOTAL ascending=$ANN_ASC"
append_summary "annotation_listed_ascending=$ANNOTATION_LISTED_ASCENDING"

# 负例：shape 不在白名单 → 400。
request_capture "annotation-shape-invalid" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"timestamp_ms":1000,"shape":"circle","payload":{}}' \
  "$ASSIST_BASE/sessions/$ASSIST_ID/annotations"
if [ "$RESPONSE_STATUS" = "400" ] && printf '%s' "$RESPONSE_RAW" | grep -q "annotation shape must be one of"; then
  ANNOTATION_SHAPE_INVALID_REJECTED=true
fi
append_summary "annotation_shape_invalid_rejected_400=$ANNOTATION_SHAPE_INVALID_REJECTED"

request_capture "annotation-delete" -X DELETE -H "$AUTH" "$ASSIST_BASE/annotations/$ANN2_ID"
request_capture "annotation-list-after-delete" -H "$AUTH" "$ASSIST_BASE/sessions/$ASSIST_ID/annotations"
ANN_AFTER_TOTAL="$(json_extract "d.get('total', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "${ANN_AFTER_TOTAL:--1}" = "1" ]; then
  ANNOTATION_DELETED_AND_GONE=true
fi
append_summary "annotation_total_after_delete=$ANN_AFTER_TOTAL"
append_summary "annotation_deleted_and_gone=$ANNOTATION_DELETED_AND_GONE"

# 负例：删除不存在的标注 → 404（repo 返回 ErrAssistAnnotationNotFound，
# assistErrorStatus 映射 404，REST 语义正确）。
request_capture "annotation-delete-missing" -X DELETE -H "$AUTH" "$ASSIST_BASE/annotations/999999"
if [ "$RESPONSE_STATUS" = "404" ] && printf '%s' "$RESPONSE_RAW" | grep -q "remote assist annotation not found"; then
  ANNOTATION_MISSING_DELETE_REJECTED=true
fi
append_summary "annotation_missing_delete_rejected_404=$ANNOTATION_MISSING_DELETE_REJECTED"

# 6) 结束链路：带录制元数据结束 → 重复结束 409 → 不存在 404。
append_summary "step=assist_end_chain"

request_capture "assist-end" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"outcome":"ended","recording_key":"ra/acc/rec-1.webm","recording_mime":"video/webm","recording_duration_ms":65000,"recording_size":1048576}' \
  "$ASSIST_BASE/sessions/$ASSIST_ID/end"
END_STATUS="$(json_extract "d.get('status','')")"
END_KEY="$(json_extract "d.get('recording_key','')")"
END_DURATION="$(json_extract "d.get('recording_duration_ms', -1)")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$END_STATUS" = "ended" ] \
  && [ "$END_KEY" = "ra/acc/rec-1.webm" ] && [ "${END_DURATION:--1}" = "65000" ]; then
  ASSIST_ENDED_WITH_RECORDING=true
fi
append_summary "assist_end_status=$END_STATUS recording_key=$END_KEY"
append_summary "assist_ended_with_recording=$ASSIST_ENDED_WITH_RECORDING"

# 负例：重复结束 → 409 remote assist session already ended。
request_capture "assist-reend-conflict" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"outcome":"ended"}' "$ASSIST_BASE/sessions/$ASSIST_ID/end"
if [ "$RESPONSE_STATUS" = "409" ] && printf '%s' "$RESPONSE_RAW" | grep -q "already ended"; then
  ASSIST_REEND_CONFLICT_REJECTED=true
fi
append_summary "assist_reend_conflict_rejected_409=$ASSIST_REEND_CONFLICT_REJECTED"

# 负例：结束不存在的协助会话 → 404。
request_capture "assist-end-missing" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"outcome":"ended"}' "$ASSIST_BASE/sessions/999999/end"
if [ "$RESPONSE_STATUS" = "404" ]; then
  ASSIST_END_MISSING_REJECTED=true
fi
append_summary "assist_end_missing_rejected_404=$ASSIST_END_MISSING_REJECTED"

# 7) 辅助建议链路（§11）：未认证 → 工单样本 → GET/POST 相似工单对账。
append_summary "step=suggest_chain"

request_capture "suggest-unauthenticated" "$SUGGEST_BASE?query=printer"
grep -q 'HTTP/1.1 401' "$EVIDENCE_DIR/suggest-unauthenticated.txt" && SUGGEST_UNAUTH_REJECTED=true
append_summary "suggest_unauthenticated_rejected_401=$SUGGEST_UNAUTH_REJECTED"

request_capture "customer-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"username":"st6-cust-'"$TIMESTAMP"'","email":"st6-cust-'"$TIMESTAMP"'@servify.io","name":"辅助建议客户","source":"web"}' \
  "$SERVIFY_URL/api/customers"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 客户创建失败" >&2
  exit 1
fi
CUSTOMER_ID="$(json_extract "d.get('id')")"

SUGGEST_TICKET_TITLE="assist-acceptance-printer-issue"
request_capture "ticket-create" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"title":"'"$SUGGEST_TICKET_TITLE"'","description":"printer cannot connect","category":"technical","priority":"high","customer_id":'"$CUSTOMER_ID"'}' \
  "$SERVIFY_URL/api/tickets"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 工单创建失败" >&2
  exit 1
fi
TICKET_ID="$(json_extract "d.get('id')")"
append_summary "customer_id=$CUSTOMER_ID suggest_ticket_id=$TICKET_ID"

request_capture "suggest-get" -H "$AUTH" --get "$SUGGEST_BASE" \
  --data-urlencode "query=printer" --data-urlencode "limit=5"
GET_HIT="$(json_extract "any(t.get('id') == $TICKET_ID for t in d.get('data',{}).get('similar_tickets', []))")"
GET_INTENT="$(json_extract "d.get('data',{}).get('intent',{}).get('label','')")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$GET_HIT" = "True" ]; then
  SUGGEST_GET_TICKET_RECONCILED=true
fi
if [ "$RESPONSE_STATUS" = "200" ] && [ -n "$GET_INTENT" ] && [ "$GET_INTENT" != "" ]; then
  SUGGEST_INTENT_PRESENT=true
fi
append_summary "suggest_get_hit=$GET_HIT intent=$GET_INTENT"
append_summary "suggest_get_ticket_reconciled=$SUGGEST_GET_TICKET_RECONCILED"
append_summary "suggest_intent_present=$SUGGEST_INTENT_PRESENT"

request_capture "suggest-post" -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"query":"printer","ticket_limit":5,"knowledge_doc_limit":5}' "$SUGGEST_BASE"
POST_HIT="$(json_extract "any(t.get('id') == $TICKET_ID for t in d.get('data',{}).get('similar_tickets', []))")"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$POST_HIT" = "True" ]; then
  SUGGEST_POST_TICKET_RECONCILED=true
fi
append_summary "suggest_post_hit=$POST_HIT"
append_summary "suggest_post_ticket_reconciled=$SUGGEST_POST_TICKET_RECONCILED"

# 8) 对账
FAILED=false
for check in READY_OK UNAUTH_REJECTED VISITOR_SESSION_CREATED \
  ASSIST_MISSING_CONVERSATION_REJECTED ASSIST_STARTED ASSIST_LISTED ASSIST_GET_RECONCILED \
  ANNOTATION_ADDED ANNOTATION_LISTED_ASCENDING ANNOTATION_SHAPE_INVALID_REJECTED \
  ANNOTATION_DELETED_AND_GONE ANNOTATION_MISSING_DELETE_REJECTED \
  ASSIST_ENDED_WITH_RECORDING ASSIST_REEND_CONFLICT_REJECTED ASSIST_END_MISSING_REJECTED \
  SUGGEST_UNAUTH_REJECTED SUGGEST_GET_TICKET_RECONCILED SUGGEST_POST_TICKET_RECONCILED \
  SUGGEST_INTENT_PRESENT; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 检查未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  append_summary "overall_status=failed"
  echo "❌ RemoteAssist/Suggest acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ RemoteAssist/Suggest acceptance 通过: WS 真实访客会话 / 协助发起(缺会话 404)/列表/详情 / 标注增删查与升序对账(非法 shape 400) / 带录制元数据结束(重复 409/不存在 404) / 辅助建议未认证 401 与 GET/POST 相似工单对账 全部真实留档"
