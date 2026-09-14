#!/bin/bash

# 会话工作台（P1-3）acceptance 测试。
#
# 覆盖：访客 WS 建会话 -> 会话详情/消息列表/坐席回复/接管/转接/关闭 + 工作台概览，
# 另含三条拒绝路径（不存在会话 404 / 空消息 400 / 未认证 401）。
#
# 前置：管理员账号（admin/admin123 之类，可先跑 scripts/seed-data.sh），
#       或直接提供 ADMIN_TOKEN。
#
# 可选环境变量:
#   SERVIFY_URL               服务地址(默认 http://localhost:8080)
#   WORKSPACE_ACCEPTANCE_MODE 证据模式标记(默认 real)
#   ADMIN_TOKEN               管理面 JWT,提供后跳过登录
#   ADMIN_USERNAME/ADMIN_PASSWORD  管理员凭证(默认 admin/admin123)
#
# 证据输出: scripts/test-results/workspace-acceptance/(manifest.json 之外
# 的文件默认不入库,.gitignore 已排除)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Workspace acceptance 测试开始..."

SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
WORKSPACE_ACCEPTANCE_MODE=${WORKSPACE_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/workspace-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"admin123"}

mkdir -p "$EVIDENCE_DIR"

ADMIN_AUTH_OK=false
AGENTS_READY=false
VISITOR_WS_INGRESS_OK=false
SESSION_CREATED=false
SESSION_DETAIL_OK=false
MESSAGE_LIST_OK=false
AGENT_MESSAGE_OK=false
AGENT_MESSAGE_PERSISTED=false
ASSIGN_OK=false
TRANSFER_OK=false
CLOSE_OK=false
WORKSPACE_OVERVIEW_OK=false
UNKNOWN_SESSION_REJECTED=false
EMPTY_MESSAGE_REJECTED=false
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
  MANIFEST_MODE="${WORKSPACE_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_AGENTS_READY="${AGENTS_READY:-false}" \
  MANIFEST_VISITOR_WS_INGRESS_OK="${VISITOR_WS_INGRESS_OK:-false}" \
  MANIFEST_SESSION_CREATED="${SESSION_CREATED:-false}" \
  MANIFEST_SESSION_DETAIL_OK="${SESSION_DETAIL_OK:-false}" \
  MANIFEST_MESSAGE_LIST_OK="${MESSAGE_LIST_OK:-false}" \
  MANIFEST_AGENT_MESSAGE_OK="${AGENT_MESSAGE_OK:-false}" \
  MANIFEST_AGENT_MESSAGE_PERSISTED="${AGENT_MESSAGE_PERSISTED:-false}" \
  MANIFEST_ASSIGN_OK="${ASSIGN_OK:-false}" \
  MANIFEST_TRANSFER_OK="${TRANSFER_OK:-false}" \
  MANIFEST_CLOSE_OK="${CLOSE_OK:-false}" \
  MANIFEST_WORKSPACE_OVERVIEW_OK="${WORKSPACE_OVERVIEW_OK:-false}" \
  MANIFEST_UNKNOWN_SESSION_REJECTED="${UNKNOWN_SESSION_REJECTED:-false}" \
  MANIFEST_EMPTY_MESSAGE_REJECTED="${EMPTY_MESSAGE_REJECTED:-false}" \
  MANIFEST_UNAUTHENTICATED_REJECTED="${UNAUTHENTICATED_REJECTED:-false}" \
  MANIFEST_SESSION_ID="${SESSION_ID:-}" \
  MANIFEST_AGENT_PRIMARY_ID="${AGENT_PRIMARY_ID:-0}" \
  MANIFEST_AGENT_SECONDARY_ID="${AGENT_SECONDARY_ID:-0}" \
  MANIFEST_VISITOR_MESSAGES="${VISITOR_MESSAGES:-0}" \
  MANIFEST_AGENT_MESSAGES="${AGENT_MESSAGES:-0}" \
  MANIFEST_STATUS_AFTER_TRANSFER="${STATUS_AFTER_TRANSFER:-unknown}" \
  MANIFEST_STATUS_AFTER_CLOSE="${STATUS_AFTER_CLOSE:-unknown}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
env = os.environ
payload = {
    "provider": "workspace",
    "mode": env.get("MANIFEST_MODE", "unknown"),
    "servify_url": env.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": env.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "admin_auth_ok": env.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "agents_ready": env.get("MANIFEST_AGENTS_READY", "false"),
        "visitor_ws_ingress_ok": env.get("MANIFEST_VISITOR_WS_INGRESS_OK", "false"),
        "session_created": env.get("MANIFEST_SESSION_CREATED", "false"),
        "session_detail_ok": env.get("MANIFEST_SESSION_DETAIL_OK", "false"),
        "message_list_ok": env.get("MANIFEST_MESSAGE_LIST_OK", "false"),
        "agent_message_ok": env.get("MANIFEST_AGENT_MESSAGE_OK", "false"),
        "agent_message_persisted": env.get("MANIFEST_AGENT_MESSAGE_PERSISTED", "false"),
        "assign_ok": env.get("MANIFEST_ASSIGN_OK", "false"),
        "transfer_ok": env.get("MANIFEST_TRANSFER_OK", "false"),
        "close_ok": env.get("MANIFEST_CLOSE_OK", "false"),
        "workspace_overview_ok": env.get("MANIFEST_WORKSPACE_OVERVIEW_OK", "false"),
        "unknown_session_rejected": env.get("MANIFEST_UNKNOWN_SESSION_REJECTED", "false"),
        "empty_message_rejected": env.get("MANIFEST_EMPTY_MESSAGE_REJECTED", "false"),
        "unauthenticated_rejected": env.get("MANIFEST_UNAUTHENTICATED_REJECTED", "false"),
    },
    "metrics": {
        "session_id": env.get("MANIFEST_SESSION_ID", ""),
        "agent_primary_id": env.get("MANIFEST_AGENT_PRIMARY_ID", "0"),
        "agent_secondary_id": env.get("MANIFEST_AGENT_SECONDARY_ID", "0"),
        "visitor_messages": env.get("MANIFEST_VISITOR_MESSAGES", "0"),
        "agent_messages": env.get("MANIFEST_AGENT_MESSAGES", "0"),
        "status_after_transfer": env.get("MANIFEST_STATUS_AFTER_TRANSFER", "unknown"),
        "status_after_close": env.get("MANIFEST_STATUS_AFTER_CLOSE", "unknown"),
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
    "int": int,
    "str": str,
}
print(eval(code, {"__builtins__": safe_builtins}, {"data": data}))
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

    try:
        sock.sendall(bytes(bytearray([0x88, 0x80]) + os.urandom(4)))
    except OSError:
        pass
finally:
    try:
        sock.close()
    except OSError:
        pass
PY
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
Workspace acceptance summary
mode=$WORKSPACE_ACCEPTANCE_MODE
servify_url=$SERVIFY_URL
EOF

echo "🗂️ 证据输出目录: $EVIDENCE_DIR"

echo "🔍 检查服务状态..."
wait_for "Servify Health" "$SERVIFY_URL/health" 30 2

TIMESTAMP=$(date +%s)
RANDOM_SUFFIX=${RANDOM:-$$}
SESSION_ID="ws-acceptance-${TIMESTAMP}-${RANDOM_SUFFIX}"
VISITOR_CONTENT="访客验收消息 workspace-acceptance ${SESSION_ID}"
AGENT_CONTENT="坐席回复 workspace-acceptance ${SESSION_ID}"

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

# 2. 两个客服用户 + 客服实体(接管/转接目标)
echo "🧑‍💻 创建客服实体..."
register_agent_user() {
  local username=$1
  local body
  body=$(printf '{"username":"%s","email":"%s@example.com","password":"Acceptance!12345","name":"%s","role":"agent"}' \
    "$username" "$username" "$username")
  request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$body" ""
  save_response "agent-register-$username" "$RESPONSE_BODY"
  assert_status "201" "$RESPONSE_STATUS" "agent_register_$username"
  local user_id
  user_id=$(json_get "$RESPONSE_BODY" '.user.id // "0"')
  if [ "$user_id" = "0" ]; then
    echo "❌ 客服注册响应缺少 user.id"
    exit 1
  fi
  printf '%s' "$user_id"
}

AGENT_PRIMARY_ID=$(register_agent_user "ws-acceptance-a-${TIMESTAMP}-${RANDOM_SUFFIX}")
AGENT_BODY=$(printf '{"user_id":%s,"department":"acceptance","skills":"workspace","max_concurrent":5}' "$AGENT_PRIMARY_ID")
request_json "POST" "$SERVIFY_URL/api/agents" "$AGENT_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "agent-create-primary" "$RESPONSE_BODY"
append_summary "agent_create_primary_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "agent_create_primary"

AGENT_SECONDARY_ID=$(register_agent_user "ws-acceptance-b-${TIMESTAMP}-${RANDOM_SUFFIX}")
AGENT_BODY=$(printf '{"user_id":%s,"department":"acceptance","skills":"workspace","max_concurrent":5}' "$AGENT_SECONDARY_ID")
request_json "POST" "$SERVIFY_URL/api/agents" "$AGENT_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "agent-create-secondary" "$RESPONSE_BODY"
append_summary "agent_create_secondary_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "agent_create_secondary"
AGENTS_READY=true
append_summary "agents_ready=$AGENTS_READY"

# 3. 访客经真实 WS 入口建会话并留言
# 契约:内容必须放在 data.content(WebSocketMessage.Data),顶层 content 会被服务端静默丢弃
echo "💬 访客 WS 建会话..."
visitor_ws_message "${SERVIFY_URL}/api/v1/ws?session_id=${SESSION_ID}" \
  "$(printf '{"type":"text-message","data":{"content":"%s"}}' "$VISITOR_CONTENT")"
VISITOR_WS_INGRESS_OK=true
append_summary "visitor_ws_ingress_ok=$VISITOR_WS_INGRESS_OK"

# 4. 会话详情:WS 首条消息应已自动建会话
echo "📋 会话详情..."
request_json "GET" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID" "" "$ADMIN_ACCESS_TOKEN"
save_response "session-detail" "$RESPONSE_BODY"
append_summary "session_detail_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_detail"
SESSION_CREATED=true
DETAIL_STATUS=$(json_get "$RESPONSE_BODY" '.data.status // ""')
append_summary "session_detail_status_field=$DETAIL_STATUS"
if [ "$DETAIL_STATUS" != "active" ]; then
  echo "❌ WS 建会话后状态应为 active, got $DETAIL_STATUS"
  exit 1
fi
SESSION_DETAIL_OK=true
append_summary "session_created=$SESSION_CREATED"
append_summary "session_detail_ok=$SESSION_DETAIL_OK"

# 5. 消息列表:含访客首条消息
request_json "GET" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/messages" "" "$ADMIN_ACCESS_TOKEN"
save_response "session-messages-visitor" "$RESPONSE_BODY"
append_summary "session_messages_visitor_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_messages_visitor"
VISITOR_MESSAGES=$(json_count "$RESPONSE_BODY" "sum(1 for m in data.get('data', []) if m.get('sender') == 'customer' and '${SESSION_ID}' in m.get('content', ''))")
append_summary "visitor_messages=$VISITOR_MESSAGES"
if [ "${VISITOR_MESSAGES:-0}" -lt 1 ]; then
  echo "❌ 消息列表未读到访客 WS 消息"
  exit 1
fi
MESSAGE_LIST_OK=true
append_summary "message_list_ok=$MESSAGE_LIST_OK"

# 6. 坐席回复
echo "💬 坐席回复..."
SEND_BODY=$(printf '{"content":"%s"}' "$AGENT_CONTENT")
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/messages" "$SEND_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "agent-message" "$RESPONSE_BODY"
append_summary "agent_message_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "agent_message"
AGENT_MESSAGE_OK=true
append_summary "agent_message_ok=$AGENT_MESSAGE_OK"

request_json "GET" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/messages" "" "$ADMIN_ACCESS_TOKEN"
save_response "session-messages-after-agent" "$RESPONSE_BODY"
append_summary "session_messages_after_agent_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_messages_after_agent"
AGENT_MESSAGES=$(json_count "$RESPONSE_BODY" "sum(1 for m in data.get('data', []) if m.get('sender') == 'agent' and '${SESSION_ID}' in m.get('content', ''))")
append_summary "agent_messages=$AGENT_MESSAGES"
if [ "${AGENT_MESSAGES:-0}" -lt 1 ]; then
  echo "❌ 坐席回复未持久化"
  exit 1
fi
AGENT_MESSAGE_PERSISTED=true
append_summary "agent_message_persisted=$AGENT_MESSAGE_PERSISTED"

# 7. 接管
echo "🙋 接管..."
ASSIGN_BODY=$(printf '{"agent_id":%s}' "$AGENT_PRIMARY_ID")
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/assign" "$ASSIGN_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "session-assigned" "$RESPONSE_BODY"
append_summary "session_assign_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_assign"
ASSIGNED_AGENT_HITS=$(json_count "$RESPONSE_BODY" "sum(1 for p in (data.get('data', {}).get('participants') or []) if (p.get('role') or p.get('Role')) == 'agent' and int(p.get('user_id') or p.get('UserID') or 0) == int('${AGENT_PRIMARY_ID}'))")
append_summary "assigned_agent_hits=$ASSIGNED_AGENT_HITS"
if [ "${ASSIGNED_AGENT_HITS:-0}" -lt 1 ]; then
  echo "❌ 接管后参与者中未见坐席 #$AGENT_PRIMARY_ID"
  exit 1
fi
ASSIGN_OK=true
append_summary "assign_ok=$ASSIGN_OK"

# 8. 转接
echo "🔀 转接..."
TRANSFER_BODY=$(printf '{"to_agent_id":%s}' "$AGENT_SECONDARY_ID")
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/transfer" "$TRANSFER_BODY" "$ADMIN_ACCESS_TOKEN"
save_response "session-transferred" "$RESPONSE_BODY"
append_summary "session_transfer_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_transfer"
STATUS_AFTER_TRANSFER=$(json_get "$RESPONSE_BODY" '.data.status // ""')
append_summary "status_after_transfer=$STATUS_AFTER_TRANSFER"
if [ "$STATUS_AFTER_TRANSFER" != "transferred" ]; then
  echo "❌ 转接后状态应为 transferred, got $STATUS_AFTER_TRANSFER"
  exit 1
fi
TRANSFER_OK=true
append_summary "transfer_ok=$TRANSFER_OK"

# 9. 关闭
echo "🔒 关闭..."
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/close" "" "$ADMIN_ACCESS_TOKEN"
save_response "session-closed" "$RESPONSE_BODY"
append_summary "session_close_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "session_close"
STATUS_AFTER_CLOSE=$(json_get "$RESPONSE_BODY" '.data.status // ""')
CLOSED_AT=$(json_get "$RESPONSE_BODY" '.data.ended_at // ""')
append_summary "status_after_close=$STATUS_AFTER_CLOSE"
if [ "$STATUS_AFTER_CLOSE" != "closed" ] || [ -z "$CLOSED_AT" ]; then
  echo "❌ 关闭后状态应为 closed 且 ended_at 非空, got status=$STATUS_AFTER_CLOSE ended_at=$CLOSED_AT"
  exit 1
fi
CLOSE_OK=true
append_summary "close_ok=$CLOSE_OK"

# 10. 工作台概览
request_json "GET" "$SERVIFY_URL/api/omni/workspace" "" "$ADMIN_ACCESS_TOKEN"
save_response "workspace-overview" "$RESPONSE_BODY"
append_summary "workspace_overview_status=$RESPONSE_STATUS"
assert_status "200" "$RESPONSE_STATUS" "workspace_overview"
WORKSPACE_OVERVIEW_OK=true
append_summary "workspace_overview_ok=$WORKSPACE_OVERVIEW_OK"

# 11. 拒绝路径: 不存在会话 404
echo "🚫 拒绝路径..."
request_json "GET" "$SERVIFY_URL/api/omni/sessions/does-not-exist-${TIMESTAMP}" "" "$ADMIN_ACCESS_TOKEN"
save_response "unknown-session" "$RESPONSE_BODY"
append_summary "unknown_session_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "404" ]; then
  echo "❌ 不存在会话应返回 404, got $RESPONSE_STATUS"
  exit 1
fi
UNKNOWN_SESSION_REJECTED=true
append_summary "unknown_session_rejected=$UNKNOWN_SESSION_REJECTED"

# 12. 拒绝路径: 空消息 400
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/messages" '{"content":""}' "$ADMIN_ACCESS_TOKEN"
save_response "empty-message" "$RESPONSE_BODY"
append_summary "empty_message_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "400" ]; then
  echo "❌ 空消息应返回 400, got $RESPONSE_STATUS"
  exit 1
fi
EMPTY_MESSAGE_REJECTED=true
append_summary "empty_message_rejected=$EMPTY_MESSAGE_REJECTED"

# 13. 拒绝路径: 未认证 401
request_json "GET" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID" "" ""
save_response "unauthenticated-session" "$RESPONSE_BODY"
append_summary "unauthenticated_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "401" ]; then
  echo "❌ 未认证访问应返回 401, got $RESPONSE_STATUS"
  exit 1
fi
UNAUTHENTICATED_REJECTED=true
append_summary "unauthenticated_rejected=$UNAUTHENTICATED_REJECTED"

OVERALL_STATUS=passed
append_summary "session_id=$SESSION_ID"
append_summary "agent_primary_id=$AGENT_PRIMARY_ID"
append_summary "agent_secondary_id=$AGENT_SECONDARY_ID"
append_summary "overall_status=$OVERALL_STATUS"

echo "✅ Workspace acceptance 测试通过"
