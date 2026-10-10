#!/bin/bash

# 会话服务全链路（V1.0 B1 gate）acceptance 测试。
#
# 覆盖：访客 WS 进线 -> AI 首答（local 抽取基线，零知识时诚实空答，断言
# 落在 source/strategy/answer_id 链路可观测面）-> 转人工 handoff（进入
# 等待队列）-> 坐席接管 -> 坐席回复 -> 访客建单（session 关联）-> 关单
# -> 关会话 -> Service Timeline 投影断言（conversation_events 落库链路，
# docs/v1-convergence-plan.md §3.1/W6）。
#
# 自包含：默认自己构建并启动真实服务（sqlite、ai.provider=local 零出站，
# 同 translation 走查口径——仓库默认 openai 即便空 key 也会真实打到
# api.openai.com，自起形态必须显式 local 才对齐自包含声明）；SERVIFY_URL
# 提供时复用既有服务（外部栈需自备 ai.provider=local 配置）。
#
# 可选环境变量:
#   SERVIFY_URL            服务地址(默认自起 http://127.0.0.1:18093)
#   SERVIFY_PORT           自起端口(默认 18093)
#   ADMIN_USERNAME/ADMIN_PASSWORD  管理员凭证(默认 lifecycle-admin/<随机>)
#   LIFECYCLE_ACCEPTANCE_MODE 证据模式标记(默认 real)
#
# 证据输出: scripts/test-results/conversation-lifecycle/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 会话服务全链路 acceptance 测试开始..."

LIFECYCLE_ACCEPTANCE_MODE=${LIFECYCLE_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/conversation-lifecycle"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"lifecycle-admin-${RANDOM}"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"Lifecycle!12345"}

mkdir -p "$EVIDENCE_DIR"

: > "$EVIDENCE_DIR/summary.txt"

BUILD_OK=false
ADMIN_AUTH_OK=false
AGENT_CREATED_OK=false
VISITOR_INGRESS_OK=false
AI_FIRST_REPLY_OK=false
HANDOFF_QUEUED_OK=false
ASSIGN_OK=false
AGENT_REPLY_OK=false
TICKET_LINKED_OK=false
TICKET_CLOSE_OK=false
SESSION_CLOSE_OK=false
TIMELINE_PROJECTED_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
DB_DSN=""
WORK_DIR=""
ADMIN_TOKEN=""

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

write_manifest() {
  MANIFEST_MODE="${LIFECYCLE_ACCEPTANCE_MODE:-real}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_AGENT_CREATED_OK="${AGENT_CREATED_OK:-false}" \
  MANIFEST_VISITOR_INGRESS_OK="${VISITOR_INGRESS_OK:-false}" \
  MANIFEST_AI_FIRST_REPLY_OK="${AI_FIRST_REPLY_OK:-false}" \
  MANIFEST_HANDOFF_QUEUED_OK="${HANDOFF_QUEUED_OK:-false}" \
  MANIFEST_ASSIGN_OK="${ASSIGN_OK:-false}" \
  MANIFEST_AGENT_REPLY_OK="${AGENT_REPLY_OK:-false}" \
  MANIFEST_TICKET_LINKED_OK="${TICKET_LINKED_OK:-false}" \
  MANIFEST_TICKET_CLOSE_OK="${TICKET_CLOSE_OK:-false}" \
  MANIFEST_SESSION_CLOSE_OK="${SESSION_CLOSE_OK:-false}" \
  MANIFEST_TIMELINE_PROJECTED_OK="${TIMELINE_PROJECTED_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "conversation-lifecycle",
    "mode": os.environ.get("MANIFEST_MODE", "real"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "admin_auth_ok": os.environ.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "agent_created_ok": os.environ.get("MANIFEST_AGENT_CREATED_OK", "false"),
        "visitor_ingress_ok": os.environ.get("MANIFEST_VISITOR_INGRESS_OK", "false"),
        "ai_first_reply_ok": os.environ.get("MANIFEST_AI_FIRST_REPLY_OK", "false"),
        "handoff_queued_ok": os.environ.get("MANIFEST_HANDOFF_QUEUED_OK", "false"),
        "assign_ok": os.environ.get("MANIFEST_ASSIGN_OK", "false"),
        "agent_reply_ok": os.environ.get("MANIFEST_AGENT_REPLY_OK", "false"),
        "ticket_linked_ok": os.environ.get("MANIFEST_TICKET_LINKED_OK", "false"),
        "ticket_close_ok": os.environ.get("MANIFEST_TICKET_CLOSE_OK", "false"),
        "session_close_ok": os.environ.get("MANIFEST_SESSION_CLOSE_OK", "false"),
        "timeline_projected_ok": os.environ.get("MANIFEST_TIMELINE_PROJECTED_OK", "false"),
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

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

save_response() {
  local name=$1
  local body=${2:-}
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

json_get() {
  local json_input="${1:-}"
  local expr="${2:-.data}"
  JSON_INPUT="$json_input" EXPR="$expr" python3 - <<'PY'
import json, os
payload = os.environ.get("JSON_INPUT", "")
expr = os.environ.get("EXPR", ".data")
fallback = ""
if "//" in expr:
    expr, fallback = expr.split("//", 1)
    expr, fallback = expr.strip(), fallback.strip().strip('"')
try:
    data = json.loads(payload)
except Exception:
    print(fallback or "")
    raise SystemExit
current = data
for part in expr.lstrip(".").split("."):
    if isinstance(current, dict) and part in current:
        current = current[part]
    elif isinstance(current, list) and part.isdigit() and int(part) < len(current):
        current = current[int(part)]
    else:
        current = None
        break
print("" if current is None else current)
PY
}

request_json() {
  local method=$1
  local url=$2
  local body=$3
  local auth_token=$4

  local response_file status_file
  response_file=$(mktemp)
  status_file=$(mktemp)
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  [ -n "$auth_token" ] && args+=(-H "Authorization: Bearer $auth_token")
  RESPONSE_STATUS=$(curl "${args[@]}" 2>/dev/null | tr -d '\n' || true)
  RESPONSE_BODY=$(cat "$response_file")
  rm -f "$response_file" "$status_file"
}

# 访客 WS 客户端：发一条消息并收集服务端帧（含异步 ai-response）。
# 契约：内容放 data.content（WebSocketMessage.Data），顶层 content 被丢弃。
visitor_ws_roundtrip() {
  local ws_url=$1
  local payload=$2
  local read_seconds=${3:-8}
  local out_file=$4
  WS_URL="$ws_url" WS_PAYLOAD="$payload" READ_SECONDS="$read_seconds" OUT_FILE="$out_file" python3 - <<'PY'
import base64, hashlib, json, os, socket, struct, sys, time
from urllib.parse import urlparse

ws_url = os.environ["WS_URL"]
payload = os.environ["WS_PAYLOAD"]
read_seconds = float(os.environ["READ_SECONDS"])
out_file = os.environ["OUT_FILE"]

parsed = urlparse(ws_url)
host = parsed.hostname
port = parsed.port or (443 if parsed.scheme == "wss" else 80)
path = parsed.path + ("?" + parsed.query if parsed.query else "")

sock = socket.create_connection((host, port), timeout=10)
try:
    key = base64.b64encode(os.urandom(16)).decode()
    handshake = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}:{port}\r\n"
        "Upgrade: websocket\r\n"
        "Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\n"
        "Sec-WebSocket-Version: 13\r\n\r\n"
    )
    sock.sendall(handshake.encode())
    buf = b""
    sock.settimeout(10)
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            sys.stderr.write("ws handshake: connection closed\n")
            sys.exit(1)
        buf += chunk
    head, _, rest = buf.partition(b"\r\n\r\n")
    if " 101 " not in head.split(b"\r\n")[0].decode("latin-1"):
        sys.stderr.write("ws handshake failed\n")
        sys.exit(1)

    def send_text(text):
        data = text.encode()
        mask = os.urandom(4)
        header = bytearray([0x81])
        n = len(data)
        if n < 126:
            header.append(0x80 | n)
        elif n < 65536:
            header.append(0x80 | 126)
            header += struct.pack(">H", n)
        else:
            header.append(0x80 | 127)
            header += struct.pack(">Q", n)
        header += mask
        sock.sendall(bytes(header) + bytes(b ^ mask[i % 4] for i, b in enumerate(data)))

    frames = []

    def read_frames(deadline):
        sock.settimeout(1)
        carry = rest
        while time.time() < deadline:
            try:
                chunk = sock.recv(4096)
                if not chunk:
                    break
                carry += chunk
            except (socket.timeout, OSError):
                continue
            while True:
                if len(carry) < 2:
                    break
                b1, b2 = carry[0], carry[1]
                opcode = b1 & 0x0F
                ln = b2 & 0x7F
                offset = 2
                if ln == 126:
                    if len(carry) < 4:
                        break
                    ln = struct.unpack(">H", carry[2:4])[0]
                    offset = 4
                elif ln == 127:
                    if len(carry) < 10:
                        break
                    ln = struct.unpack(">Q", carry[2:10])[0]
                    offset = 10
                if len(carry) < offset + ln:
                    break
                data = carry[offset:offset + ln]
                carry = carry[offset + ln:]
                if opcode == 0x1:
                    try:
                        frames.append(json.loads(data.decode()))
                    except Exception:
                        pass
                elif opcode == 0x8:
                    return

    send_text(payload)
    read_frames(time.time() + read_seconds)
    with open(out_file, "w", encoding="utf-8") as f:
        json.dump(frames, f, ensure_ascii=False)
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
    if curl -fsS "$url" > /dev/null 2>&1; then
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

# ---- 0. 构建 + 自起服务（复用 SERVIFY_URL 时跳过） ----
echo "🔍 make build..."
if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1) && [ -x "$PROJECT_ROOT/bin/servify" ]; then
  BUILD_OK=true
  append_summary "build_ok=true"
else
  append_summary "build_ok=false"
  echo "❌ make build 未通过" >&2
  exit 1
fi

if [ -z "${SERVIFY_URL:-}" ]; then
  SERVIFY_PORT=${SERVIFY_PORT:-18093}
  SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/lifecycle-XXXXXX.sqlite")"
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/lifecycle-work-XXXXXX")"
  # 临时 config：ai.provider=local（零网络出站，同 translation 走查口径）。
  #    服务起在 WORK_DIR（viper 按 cwd 找 ./config.yml），落库 TZ 钉 UTC。
  python3 - "$PROJECT_ROOT/config.yml" "$WORK_DIR/config.yml" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

# 零依赖抽取式问答基线：无出站请求、无 key 要求（自起形态唯一选型）。
cfg["ai"]["provider"] = "local"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
  echo "🚀 启动真实服务 (sqlite + ai.provider=local, 零出站): $SERVIFY_URL"
  bash -c 'cd "$1" && exec env TZ=UTC SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-lifecycle-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
fi
append_summary "servify_url=$SERVIFY_URL (sqlite local, ai=local)"

wait_for "Servify Health" "$SERVIFY_URL/health" 30 2 || exit 1

# ---- 1. 管理员注册（自起 sqlite 库为全新实例，可直接注册 admin） ----
echo "🔑 管理员认证..."
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Lifecycle Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY" ""
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "admin_register"
ADMIN_TOKEN=$(json_get "$RESPONSE_BODY" ".token")
[ -n "$ADMIN_TOKEN" ] || { echo "❌ 未拿到 admin token"; exit 1; }
ADMIN_AUTH_OK=true
append_summary "admin_auth_ok=true"

# ---- 2. 坐席用户 + 坐席实体（接管目标） ----
echo "🧑‍💻 创建坐席..."
AGENT_USERNAME="lifecycle-agent-${RANDOM}"
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" \
  "$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Lifecycle Agent","role":"agent"}' \
    "$AGENT_USERNAME" "$AGENT_USERNAME" "$ADMIN_PASSWORD")" ""
save_response "agent-register" "$RESPONSE_BODY"
assert_status "201" "$RESPONSE_STATUS" "agent_register"
AGENT_USER_ID=$(json_get "$RESPONSE_BODY" ".user.id")
request_json "POST" "$SERVIFY_URL/api/agents" \
  "$(printf '{"user_id":%s,"department":"lifecycle","skills":"lifecycle","max_concurrent":5}' "$AGENT_USER_ID")" \
  "$ADMIN_TOKEN"
save_response "agent-create" "$RESPONSE_BODY"
assert_status "201" "$RESPONSE_STATUS" "agent_create"
AGENT_ID=$(json_get "$RESPONSE_BODY" ".id")
[ -n "$AGENT_ID" ] && [ "$AGENT_ID" != "0" ] || { echo "❌ 未拿到 agent id"; exit 1; }
AGENT_CREATED_OK=true
append_summary "agent_id=$AGENT_ID"

# ---- 3. 访客 WS 进线 + 消息（AI 首答窗口） ----
TIMESTAMP=$(date +%s)
SESSION_ID="lifecycle-${TIMESTAMP}-${RANDOM}"
VISITOR_CONTENT="你好，我想咨询退货政策 lifecycle ${SESSION_ID}"
echo "💬 访客 WS 进线: $SESSION_ID"
visitor_ws_roundtrip "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_ID}" \
  "$(printf '{"type":"text-message","data":{"content":"%s"}}' "$VISITOR_CONTENT")" 8 \
  "$EVIDENCE_DIR/visitor-first-round.json"
VISITOR_INGRESS_OK=true
append_summary "visitor_ingress_ok=true"

python3 - "$EVIDENCE_DIR/visitor-first-round.json" <<'PY' > "$EVIDENCE_DIR/ai-first-reply.txt"
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    frames = json.load(f)
for frame in frames:
    if frame.get("type") == "ai-response":
        data = frame.get("data") or {}
        if data.get("source") == "ai":
            print("strategy=" + str(data.get("strategy") or ""))
            print("next_action=" + str(data.get("next_action") or ""))
            print("answer_id=" + str(data.get("answer_id") or ""))
            print("content=" + str(data.get("content") or ""))
            break
PY
AI_STRATEGY=$(sed -n 's/^strategy=//p' "$EVIDENCE_DIR/ai-first-reply.txt")
AI_NEXT_ACTION=$(sed -n 's/^next_action=//p' "$EVIDENCE_DIR/ai-first-reply.txt")
AI_ANSWER_ID=$(sed -n 's/^answer_id=//p' "$EVIDENCE_DIR/ai-first-reply.txt")
AI_REPLY=$(sed -n 's/^content=//p' "$EVIDENCE_DIR/ai-first-reply.txt")
# ai.provider=local 是零知识抽取式基线：知识库为空时 content 恒为空（诚实空答，
# 不倾倒任意句子），因此断言落在链路可观测面——source=ai（经 AI 编排而非规则兜底）、
# strategy=llm（本地 LLM 链已处理）、answer_id 已落库（首答持久化）；置信门
# next_action=handoff 一并留档。覆盖「有知识→逐字抽取」由 local-knowledge 验收承担。
if [ "$AI_STRATEGY" = "llm" ] && [ "$AI_NEXT_ACTION" = "handoff" ] && [ -n "$AI_ANSWER_ID" ] && [ "$AI_ANSWER_ID" != "None" ]; then
  echo "✅ AI 首答链通（strategy=llm，next_action=handoff，answer_id=$AI_ANSWER_ID，content 长度=${#AI_REPLY}）"
  AI_FIRST_REPLY_OK=true
  append_summary "ai_first_reply_ok=true"
  append_summary "ai_first_reply_strategy=llm"
  append_summary "ai_first_reply_answer_id=$AI_ANSWER_ID"
  append_summary "ai_first_reply_content_len=${#AI_REPLY}"
else
  echo "❌ 未收到 AI 首答链证据（strategy=${AI_STRATEGY:-无}，next_action=${AI_NEXT_ACTION:-无}，answer_id=${AI_ANSWER_ID:-无}）"
  append_summary "ai_first_reply_ok=false"
  exit 1
fi

# ---- 4. 转人工 handoff（关键词触发，进等待队列） ----
# 现有语义：等待人工不改变会话 status（保持 active），等待状态由
# waiting_records 表 + waiting_notification WS 帧表达（见
# routing/delivery/handler_adapter.go addToWaitingQueue）。
echo "🙋 访客请求转人工..."
visitor_ws_roundtrip "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_ID}" \
  '{"type":"text-message","data":{"content":"转人工"}}' 6 \
  "$EVIDENCE_DIR/visitor-handoff-round.json"
append_summary "visitor_handoff_request=true"

HANDOFF_WAITING=$(python3 - "$EVIDENCE_DIR/visitor-handoff-round.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    frames = json.load(f)
types = [fr.get("type") for fr in frames]
ok = "waiting_notification" in types
if not ok:
    for fr in frames:
        if fr.get("type") == "ai-response":
            content = (fr.get("data") or {}).get("content") or ""
            if "等待队列" in content:
                ok = True
                break
print("yes" if ok else "no")
PY
)
if [ "$HANDOFF_WAITING" != "yes" ]; then
  echo "❌ 未收到等待队列确认帧（waiting_notification / 等待队列 ai-response）"
  exit 1
fi
HANDOFF_QUEUED_OK=true
echo "✅ handoff 完成（已进等待队列）"

# ---- 5. 坐席接管 ----
echo "🤝 坐席接管..."
request_json "POST" "$SERVIFY_URL/api/omni/sessions/${SESSION_ID}/assign" \
  "$(printf '{"agent_id":%s}' "$AGENT_ID")" "$ADMIN_TOKEN"
save_response "session-assign" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "session_assign"
ASSIGN_OK=true
append_summary "assign_ok=true"
echo "✅ 接管完成"

# ---- 6. 坐席回复 ----
AGENT_MESSAGE="您好，退货政策是 7 天无理由，请提供订单号 lifecycle。"
echo "💬 坐席回复..."
request_json "POST" "$SERVIFY_URL/api/omni/sessions/${SESSION_ID}/messages" \
  "$(M="$AGENT_MESSAGE" python3 -c 'import json,os; print(json.dumps({"content": os.environ["M"]}, ensure_ascii=False))')" \
  "$ADMIN_TOKEN"
save_response "agent-message" "$RESPONSE_BODY"
assert_status "201" "$RESPONSE_STATUS" "agent_message"
AGENT_REPLY_OK=true
append_summary "agent_reply_ok=true"

request_json "GET" "$SERVIFY_URL/api/omni/sessions/${SESSION_ID}/messages?limit=50" "" "$ADMIN_TOKEN"
save_response "session-messages" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "message_list"
printf '%s' "$RESPONSE_BODY" | grep -q "$AGENT_MESSAGE" || { echo "❌ 坐席回复未落库"; exit 1; }
echo "✅ 坐席回复已落库"

# ---- 7. 访客建单（公共面，session 关联） ----
echo "🎫 访客建单..."
TICKET_TITLE="lifecycle 退货工单 ${SESSION_ID}"
request_json "POST" "$SERVIFY_URL/api/v1/tickets" \
  "$(S="$SESSION_ID" T="$TICKET_TITLE" python3 -c 'import json,os; print(json.dumps({"session_id": os.environ["S"], "title": os.environ["T"], "description": "e2e lifecycle ticket"}, ensure_ascii=False))')" \
  ""
save_response "ticket-create" "$RESPONSE_BODY"
append_summary "ticket_create_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" != "200" ] && [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 访客建单失败(HTTP $RESPONSE_STATUS)"
  exit 1
fi
TICKET_ID=$(json_get "$RESPONSE_BODY" ".id")
[ -n "$TICKET_ID" ] && [ "$TICKET_ID" != "0" ] || { echo "❌ 未拿到 ticket id"; exit 1; }
TICKET_SESSION=$(json_get "$RESPONSE_BODY" ".session_id")
[ "$TICKET_SESSION" = "$SESSION_ID" ] || { echo "❌ 工单未关联会话: $TICKET_SESSION"; exit 1; }
TICKET_LINKED_OK=true
append_summary "ticket_id=$TICKET_ID session_link_ok=true"
echo "✅ 建单完成（ticket=$TICKET_ID，session 关联）"

# ---- 8. 关单（close 响应只回 message/ticket_id，状态回读确认） ----
echo "结束工单..."
request_json "POST" "$SERVIFY_URL/api/tickets/${TICKET_ID}/close" '{}' "$ADMIN_TOKEN"
save_response "ticket-close" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "ticket_close"
request_json "GET" "$SERVIFY_URL/api/tickets/${TICKET_ID}" "" "$ADMIN_TOKEN"
save_response "ticket-after-close" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "ticket_detail"
TICKET_STATUS=$(json_get "$RESPONSE_BODY" ".status")
[ "$TICKET_STATUS" = "closed" ] || { echo "❌ 工单状态应为 closed，got $TICKET_STATUS"; exit 1; }
TICKET_CLOSE_OK=true
append_summary "ticket_close_ok=true"
echo "✅ 关单完成（status=closed）"

# ---- 9. 关会话 ----
echo "结束会话..."
request_json "POST" "$SERVIFY_URL/api/omni/sessions/${SESSION_ID}/close" "" "$ADMIN_TOKEN"
save_response "session-close" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "session_close"
SESSION_CLOSE_OK=true
append_summary "session_close_ok=true"
echo "✅ 关会话完成"

# ---- 10. Service Timeline 投影断言（B1 落库链路） ----
echo "🕰️ 校验 Service Timeline..."
sleep 1
request_json "GET" "$SERVIFY_URL/api/omni/sessions/${SESSION_ID}/timeline?limit=100" "" "$ADMIN_TOKEN"
save_response "timeline" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "timeline"
TIMELINE_TYPES=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
items = payload.get("items") or []
print("\n".join(item.get("event_type", "") for item in items))
')
append_summary "timeline_event_types=$(printf '%s' "$TIMELINE_TYPES" | paste -sd, -)"

missing=""
for wanted in conversation.created ticket.created ticket.closed; do
  printf '%s' "$TIMELINE_TYPES" | grep -qx "$wanted" || missing="$missing $wanted"
done
if [ -n "$missing" ]; then
  echo "❌ Timeline 缺少事件:$missing"
  exit 1
fi
echo "✅ Timeline 投影完整（conversation.created/ticket.created/ticket.closed）"
TIMELINE_PROJECTED_OK=true
printf '%s' "$TIMELINE_TYPES" | grep -q "routing\." \
  && echo "✅ routing 事件已投影（$(printf '%s' "$TIMELINE_TYPES" | grep -c 'routing\.') 条）" \
  && append_summary "routing_events_projected=true" \
  || { echo "⚠️ routing 事件未进 Timeline（转接路径未走 routing.Service.AssignAgent 时属预期）"; append_summary "routing_events_projected=false"; }

if [ "$BUILD_OK" != "true" ] || [ "$ADMIN_AUTH_OK" != "true" ] || [ "$AGENT_CREATED_OK" != "true" ] \
  || [ "$VISITOR_INGRESS_OK" != "true" ] || [ "$AI_FIRST_REPLY_OK" != "true" ] \
  || [ "$HANDOFF_QUEUED_OK" != "true" ] || [ "$ASSIGN_OK" != "true" ] \
  || [ "$AGENT_REPLY_OK" != "true" ] || [ "$TICKET_LINKED_OK" != "true" ] \
  || [ "$TICKET_CLOSE_OK" != "true" ] || [ "$SESSION_CLOSE_OK" != "true" ] \
  || [ "$TIMELINE_PROJECTED_OK" != "true" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ 会话服务全链路 acceptance 未通过" >&2
  exit 1
fi

echo
echo "🎉 会话服务全链路 acceptance 通过"
OVERALL_STATUS=passed
append_summary "overall_status=passed"
