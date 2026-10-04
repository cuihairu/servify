#!/bin/bash

# 路由评分审计（V1.0 B2-1 gate）acceptance 测试。
#
# 覆盖（docs/v1-convergence-plan.md §6.3）：
#   - 有在线坐席时 TransferToHuman 直接分配（executeTransfer）；
#   - 分配评分审计落 routing_assignments：分数/八因子/理由/策略；
#   - 管理读口 GET /session-transfer/scoring/:session_id 分配理由可见；
#   - transfer_records（事实）与 routing_assignments（评分）双记录。
#
# 自包含：默认构建并自起真实服务（sqlite）；SERVIFY_URL 提供时复用。
# 证据输出: scripts/test-results/routing-scoring/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 路由评分审计 acceptance 测试开始..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/routing-scoring"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"scoring-admin-${RANDOM}"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"Scoring!12345"}

mkdir -p "$EVIDENCE_DIR"
: > "$EVIDENCE_DIR/summary.txt"

SERVER_PID=""
DB_DSN=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$DB_DSN" ] && [ -f "$DB_DSN" ]; then
    rm -f "$DB_DSN" || true
  fi
}
trap cleanup EXIT

append_summary() { printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"; }

save_response() { printf '%s\n' "${2:-}" > "$EVIDENCE_DIR/$1.json"; }

json_get() {
  local json_input="${1:-}"
  local expr="${2:-.data}"
  JSON_INPUT="$json_input" EXPR="$expr" python3 - <<'PY'
import json, os
payload = os.environ.get("JSON_INPUT", "")
expr = os.environ.get("EXPR", ".data")
try:
    data = json.loads(payload)
except Exception:
    print("")
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
  local method=$1 url=$2 body=$3 auth_token=$4
  local response_file
  response_file=$(mktemp)
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  [ -n "$auth_token" ] && args+=(-H "Authorization: Bearer $auth_token")
  RESPONSE_STATUS=$(curl "${args[@]}" 2>/dev/null | tr -d '\n' || true)
  RESPONSE_BODY=$(cat "$response_file")
  rm -f "$response_file"
}

# 访客 WS 进线（建会话；评分链路前提是会话存在于 conversation 模块，
# 首条 text-message 落库即建会话）。
visitor_ws_ingress() {
  local ws_url=$1 out_file=$2
  WS_URL="$ws_url" OUT_FILE="$out_file" python3 - <<'PY'
import base64, json, os, socket, struct, sys, time
from urllib.parse import urlparse

ws_url = os.environ["WS_URL"]
out_file = os.environ["OUT_FILE"]
parsed = urlparse(ws_url)
host, port = parsed.hostname, parsed.port or 80
path = parsed.path + ("?" + parsed.query if parsed.query else "")

sock = socket.create_connection((host, port), timeout=10)
try:
    key = base64.b64encode(os.urandom(16)).decode()
    sock.sendall((
        f"GET {path} HTTP/1.1\r\nHost: {host}:{port}\r\n"
        "Upgrade: websocket\r\nConnection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n"
    ).encode())
    buf = b""
    sock.settimeout(10)
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            sys.exit(1)
        buf += chunk
    head, _, rest = buf.partition(b"\r\n\r\n")
    if " 101 " not in head.split(b"\r\n")[0].decode("latin-1"):
        sys.exit(1)

    payload = json.dumps({"type": "text-message", "data": {"content": "scoring e2e ingress"}}, ensure_ascii=False).encode()
    mask = os.urandom(4)
    assert len(payload) < 126
    header = bytearray([0x81, 0x80 | len(payload)]) + mask
    sock.sendall(bytes(header) + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))
    # 等会话落库（text-message 持久化 + 投影异步链）。
    time.sleep(2)
    with open(out_file, "w", encoding="utf-8") as f:
        f.write("ok")
finally:
    try:
        sock.close()
    except OSError:
        pass
PY
}

wait_for() {
  local name=$1 url=$2
  for i in $(seq 1 30); do
    if curl -fsS "$url" > /dev/null 2>&1; then
      echo "✅ $name 可用"
      return 0
    fi
    sleep 2
  done
  echo "❌ $name 不可用: $url"
  return 1
}

assert_status() {
  local want=$1 actual=$2 message=$3
  if [ "$actual" != "$want" ]; then
    echo "❌ $message: expected HTTP $want got $actual"
    append_summary "${message}_status=$actual"
    exit 1
  fi
}

# ---- 0. 构建 + 自起服务 ----
echo "🔍 make build..."
(cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1) || { echo "❌ build 失败"; exit 1; }

if [ -z "${SERVIFY_URL:-}" ]; then
  SERVIFY_PORT=${SERVIFY_PORT:-18094}
  SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/scoring-XXXXXX.sqlite")"
  echo "🚀 启动真实服务: $SERVIFY_URL"
  SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-scoring-dev-secret} \
  DB_DRIVER=sqlite DB_DSN="$DB_DSN" SERVIFY_PORT="$SERVIFY_PORT" \
    "$PROJECT_ROOT/bin/servify" > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
fi
append_summary "servify_url=$SERVIFY_URL"
wait_for "Servify Health" "$SERVIFY_URL/health" || exit 1

# ---- 1. admin + 坐席（带技能）+ 坐席上线 ----
echo "🔑 管理员注册..."
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" \
  "$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Scoring Admin","role":"admin"}' \
    "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")" ""
assert_status "201" "$RESPONSE_STATUS" "admin_register"
ADMIN_TOKEN=$(json_get "$RESPONSE_BODY" ".token")
[ -n "$ADMIN_TOKEN" ] || { echo "❌ 未拿到 admin token"; exit 1; }

echo "🧑‍💻 创建带技能坐席..."
AGENT_USERNAME="scoring-agent-${RANDOM}"
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" \
  "$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Scoring Agent","role":"agent"}' \
    "$AGENT_USERNAME" "$AGENT_USERNAME" "$ADMIN_PASSWORD")" ""
assert_status "201" "$RESPONSE_STATUS" "agent_register"
AGENT_USER_ID=$(json_get "$RESPONSE_BODY" ".user.id")
request_json "POST" "$SERVIFY_URL/api/agents" \
  "$(printf '{"user_id":%s,"department":"billing","skills":"billing,vip","max_concurrent":5}' "$AGENT_USER_ID")" \
  "$ADMIN_TOKEN"
assert_status "201" "$RESPONSE_STATUS" "agent_create"
AGENT_ID=$(json_get "$RESPONSE_BODY" ".id")
[ -n "$AGENT_ID" ] && [ "$AGENT_ID" != "0" ] || { echo "❌ 未拿到 agent id"; exit 1; }

echo "🟢 坐席上线..."
# 注意：/agents/:id/online 的 :id 按 user_id 查（GetAgentByUserID），非实体 ID。
request_json "POST" "$SERVIFY_URL/api/agents/${AGENT_USER_ID}/online" "" "$ADMIN_TOKEN"
assert_status "200" "$RESPONSE_STATUS" "agent_online"
append_summary "agent_online=true agent_id=$AGENT_ID user_id=$AGENT_USER_ID"

# ---- 2. 访客 WS 进线（建会话） ----
TIMESTAMP=$(date +%s)
SESSION_ID="scoring-${TIMESTAMP}-${RANDOM}"
echo "💬 访客进线: $SESSION_ID"
visitor_ws_ingress "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_ID}" \
  "$EVIDENCE_DIR/visitor-ingress.txt"
append_summary "visitor_ingress_ok=true"

# ---- 3. TransferToHuman（在线坐席 → 直接分配，executeTransfer 评分） ----
echo "🔀 转人工（期望直接分配而非入队）..."
request_json "POST" "$SERVIFY_URL/api/session-transfer/to-human" \
  "$(printf '{"session_id":"%s","reason":"scoring_acceptance","target_skills":["billing"],"priority":"normal"}' "$SESSION_ID")" \
  "$ADMIN_TOKEN"
save_response "transfer-result" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "transfer_to_human"
NEW_AGENT_ID=$(json_get "$RESPONSE_BODY" ".new_agent_id")
[ -n "$NEW_AGENT_ID" ] && [ "$NEW_AGENT_ID" != "0" ] || { echo "❌ 未直接分配到坐席（结果=$RESPONSE_BODY）"; exit 1; }
append_summary "assigned_agent=$NEW_AGENT_ID"
echo "✅ 已分配坐席（agent=$NEW_AGENT_ID）"

# ---- 4. 评分审计断言（routing_assignments 落库 + 读口可见） ----
echo "🕰️ 校验评分审计..."
request_json "GET" "$SERVIFY_URL/api/session-transfer/scoring/${SESSION_ID}?limit=10" "" "$ADMIN_TOKEN"
save_response "scoring" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "scoring_list"

python3 - "$EVIDENCE_DIR/scoring.json" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as f:
    payload = json.load(f)
items = payload.get("data") or []
assert items, "scoring audit is empty"
top = items[0]
assert top["to_agent_id"] > 0, f"bad to_agent_id: {top}"
assert top["total_score"] > 0, f"bad total_score: {top}"
factors = top.get("factors") or {}
expected = {"skill", "language", "availability", "workload", "priority", "tier", "channel", "sla"}
missing = expected - set(factors)
assert not missing, f"missing factors: {missing}"
assert top.get("strategy") == "default_weighted_v1", f"bad strategy: {top.get('strategy')}"
assert isinstance(top.get("reasons"), list), "reasons must be a list"
print(f"SCORE total={top['total_score']:.3f} factors={json.dumps(factors, ensure_ascii=False)}")
print(f"REASONS={top['reasons']}")
PY
append_summary "scoring_audit_ok=true"
echo "✅ 评分审计完整（八因子 + 策略 + 理由）"

# ---- 5. 事实记录双写断言（transfer_records） ----
echo "📜 校验转接事实记录..."
request_json "GET" "$SERVIFY_URL/api/session-transfer/history/${SESSION_ID}" "" "$ADMIN_TOKEN"
save_response "transfer-history" "$RESPONSE_BODY"
assert_status "200" "$RESPONSE_STATUS" "transfer_history"
printf '%s' "$RESPONSE_BODY" | grep -q "scoring_acceptance" || { echo "❌ 转接事实记录缺失"; exit 1; }
append_summary "transfer_record_ok=true"
echo "✅ 转接事实与评分审计双记录完整"

echo
echo "🎉 路由评分审计 acceptance 通过"
append_summary "overall_status=passed"
