#!/bin/bash

# 流式取消与持久化 acceptance（todo.md「流式取消与持久化验收」项）。
#
# 覆盖四类流式中断面，断言访客看到的内容、落库状态与重试行为一致且无
# 错误详情泄露：
#   1. normal_stream   —— 正常流：ai-response-delta 增量拼接 == ai-response
#      终帧内容 == ai_answers.answer（三面一致）。
#   2. client_disconnect —— 访客发完消息立即断开（不读任何帧）：hub 侧
#      请求级 ctx（20s）不随连接断开取消，流照常走完并落库（ai_answers
#      恰 1 行、answer 为完整模拟回答）；随后重连重发同文，新开一行。
#   3. model_interrupt —— mock LLM 推 2 个 SSE 增量后裸断连接（无 [DONE]）：
#      provider 对 EOF 补发 Done-only chunk，orchestrator 以已有部分内容
#      finish——访客收到增量+终帧（部分内容），落库为同一部分内容；
#      原始错误只留服务端日志，任何 WS 帧不得携带错误详情。
#   4. duplicate_submit —— 同会话背靠背重发同文两帧：两转各自走完流，
#      ai_answers 恰 2 行；转人工关键词帧不经模型（mock 调用计数不变），
#      访客面为固定友好文案 + 等待队列。
#
# 自包含：默认自己构建并启动真实服务（sqlite、ai.provider=openai 指向
# 脚本自起的本地 OpenAI 兼容 SSE mock，零外网出站）；SERVIFY_URL 提供时
# 复用既有服务（外部栈需自备指向 mock 的 openai 配置）。
#
# 可选环境变量:
#   SERVIFY_URL            服务地址(默认自起 http://127.0.0.1:18110)
#   SERVIFY_PORT           自起端口(默认 18110)
#   MOCK_PORT              mock LLM 端口(默认 18111)
#   STREAMING_CANCEL_ACCEPTANCE_MODE 证据模式标记(默认 real)
#
# 证据输出: scripts/test-results/streaming-cancel/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"

echo "🧪 流式取消与持久化 acceptance 测试开始..."

STREAMING_CANCEL_ACCEPTANCE_MODE=${STREAMING_CANCEL_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/streaming-cancel"}
SERVIFY_PORT=${SERVIFY_PORT:-18110}
MOCK_PORT=${MOCK_PORT:-18111}
SERVIFY_URL=${SERVIFY_URL:-http://127.0.0.1:${SERVIFY_PORT}}
MOCK_BASE_URL="http://127.0.0.1:${MOCK_PORT}/v1"

mkdir -p "$EVIDENCE_DIR"
: > "$EVIDENCE_DIR/summary.txt"

BUILD_OK=false
MOCK_LLM_OK=false
SERVER_READY_OK=false
NORMAL_STREAM_OK=false
CLIENT_DISCONNECT_PERSIST_OK=false
WS_RECONNECT_RETRY_OK=false
MODEL_INTERRUPT_CONSISTENT_OK=false
NO_ERROR_DETAIL_LEAK_OK=false
DUPLICATE_SUBMIT_OK=false
TRANSFER_KEYWORD_NO_MODEL_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
MOCK_PID=""
MOCK_MODE_FILE=""
MOCK_COUNT_FILE=""
DB_DSN=""
WORK_DIR=""

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null || true
}
trap 'cleanup; write_manifest' EXIT

write_manifest() {
  MANIFEST_OVERALL_STATUS="$OVERALL_STATUS" \
  MANIFEST_MODE="$STREAMING_CANCEL_ACCEPTANCE_MODE" \
  MANIFEST_SERVIFY_URL="$SERVIFY_URL" \
  MANIFEST_BUILD_OK="$BUILD_OK" \
  MANIFEST_MOCK_LLM_OK="$MOCK_LLM_OK" \
  MANIFEST_SERVER_READY_OK="$SERVER_READY_OK" \
  MANIFEST_NORMAL_STREAM_OK="$NORMAL_STREAM_OK" \
  MANIFEST_CLIENT_DISCONNECT_PERSIST_OK="$CLIENT_DISCONNECT_PERSIST_OK" \
  MANIFEST_WS_RECONNECT_RETRY_OK="$WS_RECONNECT_RETRY_OK" \
  MANIFEST_MODEL_INTERRUPT_CONSISTENT_OK="$MODEL_INTERRUPT_CONSISTENT_OK" \
  MANIFEST_NO_ERROR_DETAIL_LEAK_OK="$NO_ERROR_DETAIL_LEAK_OK" \
  MANIFEST_DUPLICATE_SUBMIT_OK="$DUPLICATE_SUBMIT_OK" \
  MANIFEST_TRANSFER_KEYWORD_NO_MODEL_OK="$TRANSFER_KEYWORD_NO_MODEL_OK" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "streaming-cancel",
    "mode": os.environ.get("MANIFEST_MODE", "real"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "mock_llm_ok": os.environ.get("MANIFEST_MOCK_LLM_OK", "false"),
        "server_ready_ok": os.environ.get("MANIFEST_SERVER_READY_OK", "false"),
        "normal_stream_ok": os.environ.get("MANIFEST_NORMAL_STREAM_OK", "false"),
        "client_disconnect_persist_ok": os.environ.get("MANIFEST_CLIENT_DISCONNECT_PERSIST_OK", "false"),
        "ws_reconnect_retry_ok": os.environ.get("MANIFEST_WS_RECONNECT_RETRY_OK", "false"),
        "model_interrupt_consistent_ok": os.environ.get("MANIFEST_MODEL_INTERRUPT_CONSISTENT_OK", "false"),
        "no_error_detail_leak_ok": os.environ.get("MANIFEST_NO_ERROR_DETAIL_LEAK_OK", "false"),
        "duplicate_submit_ok": os.environ.get("MANIFEST_DUPLICATE_SUBMIT_OK", "false"),
        "transfer_keyword_no_model_ok": os.environ.get("MANIFEST_TRANSFER_KEYWORD_NO_MODEL_OK", "false"),
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

# ---- 1. mock OpenAI 兼容 SSE chat 服务（零外网出站） ----
MOCK_MODE_FILE="$(mktemp "${TMPDIR:-/tmp}/streammock-mode-XXXXXX")"
MOCK_COUNT_FILE="$(mktemp "${TMPDIR:-/tmp}/streammock-count-XXXXXX")"
echo normal > "$MOCK_MODE_FILE"
echo 0 > "$MOCK_COUNT_FILE"

MOCK_PORT="$MOCK_PORT" MOCK_MODE_FILE="$MOCK_MODE_FILE" MOCK_COUNT_FILE="$MOCK_COUNT_FILE" \
  MOCK_LOG="$EVIDENCE_DIR/mock-log.txt" python3 - <<'PY' > /dev/null 2>&1 &
import http.server
import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MOCK_PORT = int(os.environ["MOCK_PORT"])
MODE_FILE = os.environ["MOCK_MODE_FILE"]
COUNT_FILE = os.environ["MOCK_COUNT_FILE"]
MOCK_LOG = os.environ["MOCK_LOG"]

lock = threading.Lock()

# 与 provider 的 SSE 解析约定对齐：data: <json> 行 + data: [DONE]。
CHUNKS_NORMAL = ["这是模拟模型的", "完整回答，用于", "流式取消与持久化验收。"]
CHUNKS_ABORT = CHUNKS_NORMAL[:2]

def read_mode():
    try:
        with open(MODE_FILE, encoding="utf-8") as fh:
            return fh.read().strip()
    except OSError:
        return "normal"

def bump_count():
    with lock:
        try:
            with open(COUNT_FILE, encoding="utf-8") as fh:
                n = int(fh.read().strip() or "0")
        except OSError:
            n = 0
        n += 1
        with open(COUNT_FILE, "w", encoding="utf-8") as fh:
            fh.write(str(n))
        return n

def read_count():
    try:
        with open(COUNT_FILE, encoding="utf-8") as fh:
            return int(fh.read().strip() or "0")
    except OSError:
        return 0

def sse_chunk(text):
    evt = {"choices": [{"index": 0, "delta": {"content": text}, "finish_reason": None}]}
    return "data: " + json.dumps(evt, ensure_ascii=False) + "\n\n"

class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):
        with lock:
            with open(MOCK_LOG, "a", encoding="utf-8") as fh:
                fh.write(fmt % args + "\n")

    def do_GET(self):
        # HealthCheck 打点：GET {base}/models。
        if self.path.endswith("/models"):
            body = json.dumps({"data": []}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        self.send_error(404)

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        mode = read_mode()
        seq = bump_count()
        with lock:
            with open(MOCK_LOG, "a", encoding="utf-8") as fh:
                fh.write("chat#%d mode=%s body=%s\n" % (seq, mode, raw.decode("utf-8", "replace")))
        if not self.path.endswith("/chat/completions"):
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()

        def write_chunk(payload: bytes):
            # HTTP/1.1 chunked 包装：长度行 + 载荷 + 空行。
            self.wfile.write(("%x\r\n" % len(payload)).encode() + payload + b"\r\n")
            self.wfile.flush()

        if mode == "abort":
            # 模型中断：推 2 个增量后裸断连接（无 [DONE]、无 finish_reason）。
            for text in CHUNKS_ABORT:
                write_chunk(sse_chunk(text).encode("utf-8"))
            self.wfile.write(b"0\r\n\r\n")
            self.wfile.flush()
            self.close_connection = True
            return
        for text in CHUNKS_NORMAL:
            write_chunk(sse_chunk(text).encode("utf-8"))
        write_chunk(b"data: [DONE]\n\n")
        self.wfile.write(b"0\r\n\r\n")

server = ThreadingHTTPServer(("127.0.0.1", MOCK_PORT), Handler)
server.serve_forever()
PY
MOCK_PID=$!
sleep 1
if curl -fsS "$MOCK_BASE_URL/models" > /dev/null 2>&1; then
  echo "✅ mock LLM 可用: $MOCK_BASE_URL"
  MOCK_LLM_OK=true
  append_summary "mock_llm_ok=true"
else
  echo "❌ mock LLM 不可用: $MOCK_BASE_URL"
  append_summary "mock_llm_ok=false"
  exit 1
fi

# ---- 2. 构建并启动 Servify（sqlite + ai.provider=openai→本地 mock） ----
if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1) && [ -x "$PROJECT_ROOT/bin/servify" ]; then
  echo "✅ make build 成功"
  BUILD_OK=true
  append_summary "build_ok=true"
else
  echo "❌ make build 失败"
  append_summary "build_ok=false"
  exit 1
fi

if [ -z "${SERVIFY_URL_OVERRIDE:-}" ] && [ "$SERVIFY_URL" = "http://127.0.0.1:${SERVIFY_PORT}" ]; then
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/streamcancel-XXXXXX.sqlite")"
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/streamcancel-work-XXXXXX")"
  # 临时 config：ai.provider=openai 指向本地 mock（真实 OpenAI 协议链路、
  # 零外网出站）；无依据策略显式 direct（让模型链路进入流式路径）。
  python3 - "$PROJECT_ROOT/config.yml" "$WORK_DIR/config.yml" "$MOCK_BASE_URL" <<'PY'
import sys
import yaml

src, dst, base_url = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["ai"]["provider"] = "openai"
cfg["ai"]["openai"] = {
    "api_key": "streaming-cancel-acceptance-key",
    "base_url": base_url,
    "model": "mock-stream-model",
    "temperature": 0.7,
    "max_tokens": 1000,
    "timeout": "30s",
}
cfg["ai"].setdefault("no_evidence", {})
cfg["ai"]["no_evidence"]["zero_hit_action"] = "direct"
cfg["ai"]["no_evidence"]["low_confidence_action"] = "direct"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
  echo "🚀 启动 Servify (sqlite + ai=openai→本地mock, 零出站): $SERVIFY_URL"
  bash -c 'cd "$1" && exec env TZ=UTC SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-streamcancel-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
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
    return 1
  }
  wait_for "Servify Health" "$SERVIFY_URL/health" 30 2 || exit 1
fi
SERVER_READY_OK=true
append_summary "server_ready_ok=true"

# ---- 3. 访客 WS 帮手：发 payloads → 按 close_after 决定是否立即断开 → ----
# ----    读满 read_seconds 收集全部帧（不提前抢跑，尾写竞态免疫）。   ----
visitor_ws_session() {
  local ws_url=$1 payloads=$2 read_seconds=$3 close_after=$4 out_file=$5
  WS_URL="$ws_url" WS_PAYLOADS="$payloads" READ_SECONDS="$read_seconds" \
    CLOSE_AFTER="$close_after" OUT_FILE="$out_file" python3 - <<'PY'
import base64
import json
import os
import socket
import struct
import sys
import time
from urllib.parse import urlparse

ws_url = os.environ["WS_URL"]
payloads = json.loads(os.environ["WS_PAYLOADS"])
read_seconds = float(os.environ["READ_SECONDS"])
close_after = os.environ["CLOSE_AFTER"] == "1"
out_file = os.environ["OUT_FILE"]

parsed = urlparse(ws_url)
host = parsed.hostname
port = parsed.port or (443 if parsed.scheme == "wss" else 80)
path = parsed.path + ("?" + parsed.query if parsed.query else "")

sock = socket.create_connection((host, port), timeout=10)
key = base64.b64encode(os.urandom(16)).decode()
sock.sendall((
    f"GET {path} HTTP/1.1\r\n"
    f"Host: {host}:{port}\r\n"
    "Upgrade: websocket\r\n"
    "Connection: Upgrade\r\n"
    f"Sec-WebSocket-Key: {key}\r\n"
    "Sec-WebSocket-Version: 13\r\n\r\n"
).encode())
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

frames = []
carry = rest

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

def recv_frame(timeout):
    global carry
    sock.settimeout(timeout)
    while True:
        # 解析 carry 中已缓冲的帧；不足则再读一段。
        if len(carry) >= 2:
            b1, b2 = carry[0], carry[1]
            opcode = b1 & 0x0F
            masked = b2 & 0x80
            ln = b2 & 0x7F
            offset = 2
            if ln == 126:
                if len(carry) < 4:
                    pass
                else:
                    ln = struct.unpack(">H", carry[2:4])[0]
                    offset = 4
            elif ln == 127:
                if len(carry) < 10:
                    pass
                else:
                    ln = struct.unpack(">Q", carry[2:10])[0]
                    offset = 10
            mask_len = 4 if masked else 0
            total = offset + mask_len + ln
            if len(carry) >= total:
                payload = carry[offset + mask_len:total]
                if masked:
                    mask = carry[offset:offset + mask_len]
                    payload = bytes(b ^ mask[i % 4] for i, b in enumerate(payload))
                carry = carry[total:]
                return opcode, payload
        chunk = sock.recv(4096)
        if not chunk:
            return None, b""
        carry += chunk

try:
    for payload in payloads:
        if isinstance(payload, dict):
            payload = json.dumps(payload, ensure_ascii=False)
        send_text(payload)
        time.sleep(0.3)
    if close_after:
        # 客户端断线场景：发完立即裸断，不读任何帧。
        sock.close()
        with open(out_file, "w", encoding="utf-8") as fh:
            json.dump([], fh)
        sys.exit(0)
    deadline = time.time() + read_seconds
    while time.time() < deadline:
        try:
            opcode, data = recv_frame(max(0.1, deadline - time.time()))
        except (socket.timeout, TimeoutError):
            break
        if opcode is None:
            break
        if opcode == 8:
            break
        if opcode == 1:
            try:
                frames.append(json.loads(data.decode("utf-8")))
            except Exception:
                frames.append({"type": "raw", "data": data.decode("utf-8", "replace")})
finally:
    try:
        sock.close()
    except OSError:
        pass

with open(out_file, "w", encoding="utf-8") as fh:
    json.dump(frames, fh, ensure_ascii=False)
PY
}

mock_model_calls() {
  cat "$MOCK_COUNT_FILE"
}

sqlite_query() {
  sqlite3 "$DB_DSN" ".timeout 3000" "$1"
}

# ---- 场景 1：正常流（增量==终帧==落库 三面一致） ----
echo "▶ 场景 1: 正常流"
SESSION_NORMAL="streamcancel-normal-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_NORMAL}" \
  '[{"type":"text-message","data":{"content":"请介绍一下退货政策"}}]' 8 0 "$EVIDENCE_DIR/ws-normal.json"
python3 - "$EVIDENCE_DIR/ws-normal.json" "$DB_DSN" "$SESSION_NORMAL" <<'PY'
import json
import sqlite3
import sys

frames_path, db_path, session_id = sys.argv[1], sys.argv[2], sys.argv[3]
with open(frames_path, encoding="utf-8") as fh:
    frames = json.load(fh)

deltas = [fr["data"]["content_delta"] for fr in frames
          if fr.get("type") == "ai-response-delta" and (fr.get("data") or {}).get("content_delta")]
finals = [fr for fr in frames if fr.get("type") == "ai-response"
          and (fr.get("data") or {}).get("source") == "ai"]
joined = "".join(deltas)
assert deltas, "no content deltas received"
assert len(finals) == 1, f"expected exactly 1 ai-response, got {len(finals)}"
final_data = finals[0]["data"]
assert final_data.get("content") == joined, (
    f"final content != concatenated deltas: {final_data.get('content')!r} vs {joined!r}")
assert final_data.get("answer_id"), "missing answer_id on final ai-response"

conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
rows = conn.execute(
    "SELECT answer FROM ai_answers WHERE session_id = ?", (session_id,)).fetchall()
assert len(rows) == 1, f"expected 1 ai_answers row, got {len(rows)}"
assert rows[0][0] == joined, f"persisted answer != visitor content: {rows[0][0]!r}"
print("normal_stream_ok=true deltas=%d content_len=%d answer_id=%s"
      % (len(deltas), len(joined), final_data.get("answer_id")))
PY
NORMAL_STREAM_OK=true
append_summary "normal_stream_ok=true"

# ---- 场景 2：访客断线（发完立即断开，服务端照常走完并落库） + 重连重发 ----
echo "▶ 场景 2: 访客断线"
SESSION_DISCONNECT="streamcancel-disconnect-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_DISCONNECT}" \
  '[{"type":"text-message","data":{"content":"请介绍一下退货政策"}}]' 0 1 "$EVIDENCE_DIR/ws-disconnect.json"
sleep 4  # hub 请求级 ctx 不随断连接取消，等流走完落库
python3 - "$DB_DSN" "$SESSION_DISCONNECT" <<'PY'
import sqlite3
import sys

db_path, session_id = sys.argv[1], sys.argv[2]
full_answer = "这是模拟模型的完整回答，用于流式取消与持久化验收。"
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
rows = conn.execute(
    "SELECT answer FROM ai_answers WHERE session_id = ?", (session_id,)).fetchall()
assert len(rows) == 1, f"expected 1 persisted answer after client disconnect, got {len(rows)}"
assert rows[0][0] == full_answer, f"persisted answer not complete: {rows[0][0]!r}"
print("client_disconnect_persist_ok=true")
PY
CLIENT_DISCONNECT_PERSIST_OK=true
append_summary "client_disconnect_persist_ok=true"

# 重连重发（重试行为）：新会话新连接，同文重新走完整流。
SESSION_RETRY="streamcancel-retry-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_RETRY}" \
  '[{"type":"text-message","data":{"content":"请介绍一下退货政策"}}]' 8 0 "$EVIDENCE_DIR/ws-retry.json"
python3 - "$EVIDENCE_DIR/ws-retry.json" "$DB_DSN" "$SESSION_RETRY" <<'PY'
import json
import sqlite3
import sys

frames_path, db_path, session_id = sys.argv[1], sys.argv[2], sys.argv[3]
with open(frames_path, encoding="utf-8") as fh:
    frames = json.load(fh)
deltas = [fr["data"]["content_delta"] for fr in frames
          if fr.get("type") == "ai-response-delta" and (fr.get("data") or {}).get("content_delta")]
finals = [fr for fr in frames if fr.get("type") == "ai-response"
          and (fr.get("data") or {}).get("source") == "ai"]
joined = "".join(deltas)
assert deltas and len(finals) == 1, "retry round did not stream a complete answer"
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
rows = conn.execute(
    "SELECT answer FROM ai_answers WHERE session_id = ?", (session_id,)).fetchall()
assert len(rows) == 1 and rows[0][0] == joined, "retry answer not persisted consistently"
print("ws_reconnect_retry_ok=true")
PY
WS_RECONNECT_RETRY_OK=true
append_summary "ws_reconnect_retry_ok=true"

# ---- 场景 3：模型中断（mock 推 2 个增量后裸断，无 [DONE]） ----
echo "▶ 场景 3: 模型中断"
echo abort > "$MOCK_MODE_FILE"
SESSION_ABORT="streamcancel-abort-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_ABORT}" \
  '[{"type":"text-message","data":{"content":"请介绍一下退货政策"}}]' 8 0 "$EVIDENCE_DIR/ws-abort.json"
echo normal > "$MOCK_MODE_FILE"
python3 - "$EVIDENCE_DIR/ws-abort.json" "$DB_DSN" "$SESSION_ABORT" <<'PY'
import json
import sqlite3
import sys

frames_path, db_path, session_id = sys.argv[1], sys.argv[2], sys.argv[3]
with open(frames_path, encoding="utf-8") as fh:
    frames = json.load(fh)
deltas = [fr["data"]["content_delta"] for fr in frames
          if fr.get("type") == "ai-response-delta" and (fr.get("data") or {}).get("content_delta")]
finals = [fr for fr in frames if fr.get("type") == "ai-response"
          and (fr.get("data") or {}).get("source") == "ai"]
partial = "这是模拟模型的完整回答，用于"
joined = "".join(deltas)
assert deltas, "no deltas received before interruption"
assert joined == partial, f"expected exactly the 2 flushed chunks, got {joined!r}"
assert len(finals) == 1, f"expected partial final ai-response, got {len(finals)}"
assert finals[0]["data"].get("content") == joined, "partial final content != deltas"
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
rows = conn.execute(
    "SELECT answer FROM ai_answers WHERE session_id = ?", (session_id,)).fetchall()
assert len(rows) == 1, f"expected partial answer persisted, got {len(rows)} rows"
assert rows[0][0] == joined, f"persisted partial != visitor partial: {rows[0][0]!r}"
print("model_interrupt_consistent_ok=true content_len=%d" % len(joined))
PY
MODEL_INTERRUPT_CONSISTENT_OK=true
append_summary "model_interrupt_consistent_ok=true"

# ---- 场景 4：重复提交（同会话背靠背同文两帧，两转各自走完落库） ----
echo "▶ 场景 4: 重复提交"
SESSION_DUP="streamcancel-duplicate-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_DUP}" \
  '[{"type":"text-message","data":{"content":"请介绍一下退货政策"}}, {"type":"text-message","data":{"content":"请介绍一下退货政策"}}]' 10 0 "$EVIDENCE_DIR/ws-duplicate.json"
python3 - "$EVIDENCE_DIR/ws-duplicate.json" "$DB_DSN" "$SESSION_DUP" <<'PY'
import json
import sqlite3
import sys

frames_path, db_path, session_id = sys.argv[1], sys.argv[2], sys.argv[3]
with open(frames_path, encoding="utf-8") as fh:
    frames = json.load(fh)
finals = [fr for fr in frames if fr.get("type") == "ai-response"
          and (fr.get("data") or {}).get("source") == "ai"]
full = "这是模拟模型的完整回答，用于流式取消与持久化验收。"
assert len(finals) == 2, f"expected 2 ai-response frames for duplicate submit, got {len(finals)}"
for fr in finals:
    assert fr["data"].get("content") == full, "duplicate round content mismatch"
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
rows = conn.execute(
    "SELECT answer FROM ai_answers WHERE session_id = ?", (session_id,)).fetchall()
assert len(rows) == 2, f"expected 2 persisted answers, got {len(rows)}"
for row in rows:
    assert row[0] == full, "duplicate persisted answer mismatch"
print("duplicate_submit_ok=true")
PY
DUPLICATE_SUBMIT_OK=true
append_summary "duplicate_submit_ok=true"

# ---- 场景 5：转人工关键词（不经模型：mock 计数不变，访客面固定文案） ----
echo "▶ 场景 5: 转人工关键词"
CALLS_BEFORE_TRANSFER=$(mock_model_calls)
SESSION_TRANSFER="streamcancel-transfer-$(date +%s)-$RANDOM"
visitor_ws_session "$SERVIFY_URL/api/v1/ws?session_id=${SESSION_TRANSFER}" \
  '[{"type":"text-message","data":{"content":"转人工"}}]' 6 0 "$EVIDENCE_DIR/ws-transfer.json"
CALLS_AFTER_TRANSFER=$(mock_model_calls)
python3 - "$EVIDENCE_DIR/ws-transfer.json" "$CALLS_BEFORE_TRANSFER" "$CALLS_AFTER_TRANSFER" "$DB_DSN" "$SESSION_TRANSFER" <<'PY'
import json
import sqlite3
import sys

frames_path, before, after, db_path, session_id = (
    sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], sys.argv[5])
with open(frames_path, encoding="utf-8") as fh:
    frames = json.load(fh)
types = [fr.get("type") for fr in frames]
assert after == before, f"transfer keyword must not invoke the model: {before} -> {after}"
friendly = [fr for fr in frames if fr.get("type") == "ai-response"
            and "转接人工客服" in ((fr.get("data") or {}).get("content") or "")]
assert friendly, f"no fixed transfer copy in frames: {types}"
conn = sqlite3.connect(db_path)
conn.execute("PRAGMA busy_timeout = 3000")
try:
    waiting = conn.execute(
        "SELECT COUNT(*) FROM waiting_records WHERE session_id = ?", (session_id,)).fetchone()[0]
except sqlite3.OperationalError:
    waiting = 0
queued = waiting > 0 or "waiting_notification" in types
assert queued, f"transfer not queued (waiting_records={waiting}, frames={types})"
print("transfer_keyword_no_model_ok=true waiting_rows=%s" % waiting)
PY
TRANSFER_KEYWORD_NO_MODEL_OK=true
append_summary "transfer_keyword_no_model_ok=true"

# ---- 全局：无错误详情泄露（全部 WS 帧扫描 mock/provider 错误特征） ----
MOCK_PORT="$MOCK_PORT" python3 - "$EVIDENCE_DIR" <<'PY'
import glob
import json
import os
import sys

evidence_dir = sys.argv[1]
forbidden = [
    "stream failed", "EOF", "connection reset", "broken pipe",
    "127.0.0.1:%s" % os.environ.get("MOCK_PORT", "18111"),
    "streaming-cancel-acceptance-key", "mock-stream-model", "openai",
]
leaks = []
for path in sorted(glob.glob(os.path.join(evidence_dir, "ws-*.json"))):
    with open(path, encoding="utf-8") as fh:
        text = fh.read()
    for needle in forbidden:
        if needle in text:
            leaks.append((os.path.basename(path), needle))
assert not leaks, f"error detail leaked into visitor frames: {leaks}"
print("no_error_detail_leak_ok=true scanned=%d" % len(glob.glob(os.path.join(evidence_dir, "ws-*.json"))))
PY
NO_ERROR_DETAIL_LEAK_OK=true
append_summary "no_error_detail_leak_ok=true"

OVERALL_STATUS="passed"
append_summary "overall_status=passed"
echo "✅ streaming-cancel acceptance 通过: 正常流三面一致、断线照常落库、重连重发一致、"
echo "     模型中断部分内容一致落库、重复提交两转两行、转人工零模型调用、全帧无错误详情泄露"
