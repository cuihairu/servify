#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Mobile probe acceptance 测试开始（SDK M0 验收②：全链路联调）..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/mobile-probe"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"mp-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"mp-admin-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18099}
MOCK_LLM_PORT=${MOCK_LLM_PORT:-18199}
PROBE_TIMEOUT=${PROBE_TIMEOUT:-60}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
PROBE_BUILD_OK=false
MOCK_LLM_OK=false
READY_OK=false
AGENT_ONLINE=false
VISITOR_ECHO_OK=false
AI_STREAMING_OK=false
AI_FINAL_OK=false
HANDOFF_OK=false
AGENT_REPLY_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
MOCK_PID=""
PROBE_PID=""
DB_DSN=""
WORK_DIR=""

VISITOR_SESSION="mp-acc-session-$(date +%s)"
SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
WS_URL="ws://127.0.0.1:${SERVIFY_PORT}/api/v1/ws?session_id=${VISITOR_SESSION}"
PROBE_LOG="$EVIDENCE_DIR/probe.log"
PROBE_BIN="$PROJECT_ROOT/sdk/android/build/install/servify-android-probe/bin/servify-android-probe"

cleanup() {
  for pid in "$PROBE_PID" "$SERVER_PID" "$MOCK_PID"; do
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
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

write_manifest() {
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_MODE="real" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_PROBE_BUILD_OK="${PROBE_BUILD_OK:-false}" \
  MANIFEST_MOCK_LLM_OK="${MOCK_LLM_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_AGENT_ONLINE="${AGENT_ONLINE:-false}" \
  MANIFEST_VISITOR_ECHO_OK="${VISITOR_ECHO_OK:-false}" \
  MANIFEST_AI_STREAMING_OK="${AI_STREAMING_OK:-false}" \
  MANIFEST_AI_FINAL_OK="${AI_FINAL_OK:-false}" \
  MANIFEST_HANDOFF_OK="${HANDOFF_OK:-false}" \
  MANIFEST_AGENT_REPLY_OK="${AGENT_REPLY_OK:-false}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL}" \
  MANIFEST_VISITOR_SESSION="${VISITOR_SESSION}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "mobile-probe",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "milestone": "SDK M0 验收②：建连 → AI 首答流式 → 转人工 → 坐席回复",
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "visitor_session": os.environ.get("MANIFEST_VISITOR_SESSION", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "probe_build_ok": os.environ.get("MANIFEST_PROBE_BUILD_OK", "false"),
        "mock_llm_ok": os.environ.get("MANIFEST_MOCK_LLM_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "agent_online": os.environ.get("MANIFEST_AGENT_ONLINE", "false"),
        "visitor_echo_ok": os.environ.get("MANIFEST_VISITOR_ECHO_OK", "false"),
        "ai_streaming_ok": os.environ.get("MANIFEST_AI_STREAMING_OK", "false"),
        "ai_final_ok": os.environ.get("MANIFEST_AI_FINAL_OK", "false"),
        "handoff_ok": os.environ.get("MANIFEST_HANDOFF_OK", "false"),
        "agent_reply_ok": os.environ.get("MANIFEST_AGENT_REPLY_OK", "false"),
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

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Mobile probe acceptance summary
visitor_session=$VISITOR_SESSION
servify_url=$SERVIFY_URL
mock_llm_port=$MOCK_LLM_PORT
EOF

# 1) 服务端二进制 + 探针发行物。
append_summary "step=build"
echo "🔍 make build..."
if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1); then
  if [ -x "$PROJECT_ROOT/bin/servify" ]; then
    BUILD_OK=true
  fi
fi
append_summary "build_ok=$BUILD_OK"
if [ "$BUILD_OK" != "true" ]; then
  echo "❌ make build 未通过" >&2
  exit 1
fi

echo "🔍 Android 探针 installDist..."
if (cd "$PROJECT_ROOT/sdk/android" && ./gradlew --no-daemon installDist > "$EVIDENCE_DIR/probe-build-output.txt" 2>&1); then
  if [ -x "$PROBE_BIN" ]; then
    PROBE_BUILD_OK=true
  fi
fi
append_summary "probe_build_ok=$PROBE_BUILD_OK"
if [ "$PROBE_BUILD_OK" != "true" ]; then
  echo "❌ 探针构建未通过" >&2
  exit 1
fi

# 2) OpenAI 兼容流式 mock LLM：/models 健康探测 + /v1/chat/completions
#    stream=true 时按 SSE 逐 chunk 输出（服务端 streamOnce 解析 data: 行与 [DONE]）。
append_summary "step=mock_llm"
python3 - "$MOCK_LLM_PORT" > "$EVIDENCE_DIR/mock-llm-log.txt" 2>&1 <<'PY' &
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1])
CHUNKS = ["根据退货政策，", "商品签收后 7 天内", "可无理由退货。"]
FULL = "".join(CHUNKS)

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("mock-llm: " + fmt % args + "\n")

    def _send_json(self, obj, status=200):
        body = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        # openai provider HealthCheck 探 {base_url}/models
        if self.path.endswith("/models"):
            self._send_json({"object": "list", "data": [{"id": "mock-llm", "object": "model", "owned_by": "acceptance-mock"}]})
        else:
            self._send_json({"error": "not found"}, 404)

    def do_POST(self):
        if not self.path.endswith("/chat/completions"):
            self._send_json({"error": "not found"}, 404)
            return
        length = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(length) or b"{}")
        if not req.get("stream"):
            self._send_json({
                "id": "chatcmpl-mock", "object": "chat.completion", "model": req.get("model", "mock"),
                "choices": [{"index": 0, "finish_reason": "stop",
                             "message": {"role": "assistant", "content": FULL}}],
            })
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for chunk in CHUNKS:
            evt = {"id": "chatcmpl-mock", "object": "chat.completion.chunk", "model": req.get("model", "mock"),
                   "choices": [{"index": 0, "finish_reason": None, "delta": {"content": chunk}}]}
            self.wfile.write(f"data: {json.dumps(evt)}\n\n".encode())
            self.wfile.flush()
        done = {"id": "chatcmpl-mock", "object": "chat.completion.chunk", "model": req.get("model", "mock"),
                "choices": [{"index": 0, "finish_reason": "stop", "delta": {}}]}
        self.wfile.write(f"data: {json.dumps(done)}\n\n".encode())
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()

ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
MOCK_PID=$!
if wait_for "mock LLM" "http://127.0.0.1:${MOCK_LLM_PORT}/models" 15 1; then
  MOCK_LLM_OK=true
else
  echo "❌ mock LLM 未就绪" >&2
  exit 1
fi
append_summary "mock_llm_ok=$MOCK_LLM_OK"

# 3) 起服（sqlite；ai.openai 指向 mock；websocket_allowed_origins 保持空数组=放行无 Origin 的原生 WS）。
append_summary "step=ready"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/mobile-probe-work-XXXXXX")"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/mobile-probe-XXXXXX.sqlite")"

make_config() {
  local dst=$1
  python3 - "$PROJECT_ROOT/config.yml" "$dst" "$MOCK_LLM_PORT" <<'PY'
import sys
import yaml

src, dst, port = sys.argv[1], sys.argv[2], int(sys.argv[3])
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["paths"] = [
    {"enabled": True, "prefix": "/api/", "requests_per_minute": 120, "burst": 60},
]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = "./uploads"
cfg["ai"]["openai"]["api_key"] = "acceptance-not-a-real-key"
cfg["ai"]["openai"]["base_url"] = f"http://127.0.0.1:{port}/v1"
cfg["ai"]["openai"]["model"] = "mock-llm"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
}

make_config "$WORK_DIR/config.yml"
bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
  _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
  >> "$EVIDENCE_DIR/server-log.txt" 2>&1 &
SERVER_PID=$!

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi
request_capture "ready" "$SERVIFY_URL/ready"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_RAW" | grep -q '"ready":true'; then
  READY_OK=true
fi
append_summary "ready_ok=$READY_OK"

# 4) 实体：admin（注册即发 token）、agent 用户、客服实体、上线。
append_summary "step=entities"
request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"MP Admin","role":"admin"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ admin 注册失败" >&2
  exit 1
fi
ADMIN_TOKEN="$(json_extract "d.get('token','')")"

TIMESTAMP=$(date +%s)
AGENT_USER="mp-agent-${TIMESTAMP}"
request_capture "register-agent" -X POST -H "Content-Type: application/json" \
  -d '{"username":"'"$AGENT_USER"'","email":"'"$AGENT_USER"'@servify.io","password":"Acceptance!12345","name":"MP Agent","role":"agent"}' \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ agent 用户注册失败" >&2
  exit 1
fi
AGENT_ID="$(json_extract "d.get('user',{}).get('id','')")"

request_capture "agent-create" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"user_id":'"$AGENT_ID"',"department":"acceptance","skills":"probe","max_concurrent":5}' \
  "$SERVIFY_URL/api/agents"
if [ "$RESPONSE_STATUS" != "201" ]; then
  echo "❌ 客服实体创建失败" >&2
  exit 1
fi

request_capture "agent-online" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$SERVIFY_URL/api/agents/$AGENT_ID/online"
if [ "$RESPONSE_STATUS" = "200" ]; then
  AGENT_ONLINE=true
fi
append_summary "agent_online=$AGENT_ONLINE agent_id=$AGENT_ID"

# 5) 探针后台跑全链路；出现转人工关键词后经 omni 注入坐席回复。
append_summary "step=probe_full_chain"
echo "🚀 启动探针（visitor_session=$VISITOR_SESSION）..."
env PROBE_WS_URL="$WS_URL" PROBE_TIMEOUT="$PROBE_TIMEOUT" "$PROBE_BIN" > "$PROBE_LOG" 2>&1 &
PROBE_PID=$!

# 等探针发出转人工关键词（最多 45s）。
for i in $(seq 1 45); do
  if grep -q "send-transfer-keyword" "$PROBE_LOG" 2>/dev/null; then
    break
  fi
  if ! kill -0 "$PROBE_PID" 2>/dev/null; then
    break
  fi
  sleep 1
done

if grep -q "send-transfer-keyword" "$PROBE_LOG" 2>/dev/null; then
  sleep 1
  echo "📨 注入坐席回复..."
  request_capture "agent-send-message" -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"content":"您好，我是坐席 MP Agent，退货问题我来为您处理。"}' \
    "$SERVIFY_URL/api/omni/sessions/$VISITOR_SESSION/messages"
  append_summary "agent_send_status=$RESPONSE_STATUS"
fi

wait "$PROBE_PID" || true

# 6) 判定：探针日志逐步对账。
append_summary "step=verdict"
if grep -q "PROBE_STEP visitor-echo-received" "$PROBE_LOG"; then
  VISITOR_ECHO_OK=true
fi
STREAM_DELTAS="$(grep -o 'streaming_deltas=[0-9]*' "$PROBE_LOG" | head -1 | cut -d= -f2 || echo 0)"
if [ "${STREAM_DELTAS:-0}" -ge 1 ] 2>/dev/null; then
  AI_STREAMING_OK=true
fi
if grep -q "PROBE_STEP ai-answer-complete" "$PROBE_LOG" && ! grep -q "PROBE_FAIL ai stream interrupted" "$PROBE_LOG"; then
  AI_FINAL_OK=true
fi
if grep -qE "handoff-state state=(AgentChatting|WaitingHuman)" "$PROBE_LOG"; then
  HANDOFF_OK=true
fi
if grep -q "PROBE_OK full chain" "$PROBE_LOG"; then
  AGENT_REPLY_OK=true
fi
append_summary "visitor_echo_ok=$VISITOR_ECHO_OK streaming_deltas=${STREAM_DELTAS:-0} ai_final_ok=$AI_FINAL_OK handoff_ok=$HANDOFF_OK agent_reply_ok=$AGENT_REPLY_OK"

FAILED=false
for check in BUILD_OK PROBE_BUILD_OK MOCK_LLM_OK READY_OK AGENT_ONLINE VISITOR_ECHO_OK AI_STREAMING_OK AI_FINAL_OK HANDOFF_OK AGENT_REPLY_OK; do
  if [ "${!check}" != "true" ]; then
    echo "❌ 未通过: $check" >&2
    FAILED=true
  fi
done
if [ "$FAILED" = "true" ]; then
  echo "—— 探针日志 ——" >&2
  cat "$PROBE_LOG" >&2 || true
  exit 1
fi

OVERALL_STATUS=passed
echo "✅ Mobile probe acceptance 通过: 建连 → 回显 → AI 首答流式(${STREAM_DELTAS:-0} 增量) → 转人工 → 坐席回复 全链路真实留档"
