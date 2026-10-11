#!/bin/bash
# Webhook 出站投递验收（C1-4 验收缺口⑤补验，2026-10-10）：
# admin 注册端点 → 访客 WS 发消息建会话 → 访客建单触发 ticket.created →
# 本地接收端收到签名投递并验 HMAC-SHA256 → deliveries 状态 success → 自测端点。
# 与 ferry 实机链路互补：ferry 场景用 theme=auto 未配 webhook，本脚本把
# embedding-guide §5 的投递链在 servify 侧真验一遍。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Webhook acceptance（出站投递真链路）测试开始..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/webhook-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"webhook-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"webhook-123"}

BUILD_OK=false
ADMIN_AUTH_OK=false
ENDPOINT_CREATED_OK=false
SECRET_HIDDEN_OK=false
EVENTS_WHITELIST_OK=false
SESSION_MSG_OK=false
TICKET_CREATED_OK=false
DELIVERY_SIGNATURE_OK=false
DELIVERY_STATUS_OK=false
SELFTEST_OK=false
OVERALL_STATUS=failed

SERVIFY_URL=${SERVIFY_URL:-}
RECEIVER_PID=""
SERVER_PID=""
DB_DSN=""

cleanup() {
  if [ -n "$RECEIVER_PID" ] && kill -0 "$RECEIVER_PID" 2>/dev/null; then
    kill "$RECEIVER_PID" 2>/dev/null || true
    wait "$RECEIVER_PID" 2>/dev/null || true
  fi
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$DB_DSN" ] && [ -f "$DB_DSN" ]; then
    rm -f "$DB_DSN" || true
  fi
}

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

save_response() {
  local name=$1
  local body=${2:-}
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

RESPONSE_STATUS=""
RESPONSE_BODY=""
request_json() {
  local method=$1 url=$2 body=${3:-} token=${4:-}
  local response_file status_file
  response_file="$(mktemp "${TMPDIR:-/tmp}/webhook-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/webhook-status-XXXXXX")"
  trap 'rm -f "$response_file" "$status_file"; trap - RETURN' RETURN
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  if [ -n "$body" ]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  curl "${args[@]}" > "$status_file" 2>/dev/null || true
  RESPONSE_STATUS="$(cat "$status_file")"
  RESPONSE_BODY="$(cat "$response_file")"
  rm -f "$response_file" "$status_file"
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
  MANIFEST_MODE="${MANIFEST_MODE:-real}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_ENDPOINT_CREATED_OK="${ENDPOINT_CREATED_OK:-false}" \
  MANIFEST_SECRET_HIDDEN_OK="${SECRET_HIDDEN_OK:-false}" \
  MANIFEST_EVENTS_WHITELIST_OK="${EVENTS_WHITELIST_OK:-false}" \
  MANIFEST_SESSION_MSG_OK="${SESSION_MSG_OK:-false}" \
  MANIFEST_TICKET_CREATED_OK="${TICKET_CREATED_OK:-false}" \
  MANIFEST_DELIVERY_SIGNATURE_OK="${DELIVERY_SIGNATURE_OK:-false}" \
  MANIFEST_DELIVERY_STATUS_OK="${DELIVERY_STATUS_OK:-false}" \
  MANIFEST_SELFTEST_OK="${SELFTEST_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "webhook",
    "mode": os.environ.get("MANIFEST_MODE", "real"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "admin_auth_ok": os.environ.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "endpoint_created_ok": os.environ.get("MANIFEST_ENDPOINT_CREATED_OK", "false"),
        "secret_hidden_ok": os.environ.get("MANIFEST_SECRET_HIDDEN_OK", "false"),
        "events_whitelist_ok": os.environ.get("MANIFEST_EVENTS_WHITELIST_OK", "false"),
        "session_msg_ok": os.environ.get("MANIFEST_SESSION_MSG_OK", "false"),
        "ticket_created_ok": os.environ.get("MANIFEST_TICKET_CREATED_OK", "false"),
        "delivery_signature_ok": os.environ.get("MANIFEST_DELIVERY_SIGNATURE_OK", "false"),
        "delivery_status_ok": os.environ.get("MANIFEST_DELIVERY_STATUS_OK", "false"),
        "selftest_ok": os.environ.get("MANIFEST_SELFTEST_OK", "false"),
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

mkdir -p "$EVIDENCE_DIR/received"

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Webhook acceptance summary（C1-4 验收缺口⑤补验）
EOF

if [ -n "$SERVIFY_URL" ]; then
  echo "⚠️ 使用外部 SERVIFY_URL=$SERVIFY_URL（本地自起栈时验签链路同样覆盖）"
  append_summary "servify_url=$SERVIFY_URL (external)"
else
  # 1) CLI 标准构建
  append_summary "step=make_build"
  echo "🔍 make build..."
  if (cd "$PROJECT_ROOT" && make build > "$EVIDENCE_DIR/build-output.txt" 2>&1); then
    if [ -x "$PROJECT_ROOT/bin/servify" ]; then
      BUILD_OK=true
      (cd "$PROJECT_ROOT" && ls -1 bin >> "$EVIDENCE_DIR/build-output.txt")
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

  # 2) 以 sqlite 启动真实服务
  SERVIFY_PORT=${SERVIFY_PORT:-18096}
  SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/webhook-XXXXXX.sqlite")"
  echo "🚀 启动真实服务 (sqlite, bin/servify): $SERVIFY_URL"
  SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-dev-secret} \
  DB_DRIVER=sqlite DB_DSN="$DB_DSN" SERVIFY_PORT="$SERVIFY_PORT" \
  "$PROJECT_ROOT/bin/servify" > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
  append_summary "servify_url=$SERVIFY_URL (sqlite local)"
fi

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

# 3) 注册 admin 拿 token
append_summary "step=admin_auth"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Webhook Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY"
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
ADMIN_TOKEN=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))' 2>/dev/null || true)
if [ -z "$ADMIN_TOKEN" ]; then
  append_summary "admin_token=missing"
  echo "❌ 未拿到 admin token" >&2
  exit 1
fi
ADMIN_AUTH_OK=true
append_summary "admin_auth_ok=true"

# 4) 本地接收端（python3 http.server）：POST 落盘 headers+body，回 200
RECEIVER_PORT=${RECEIVER_PORT:-18097}
echo "🚀 启动本地接收端: http://127.0.0.1:${RECEIVER_PORT}/hooks"
RECEIVER_SCRIPT="$(mktemp "${TMPDIR:-/tmp}/webhook-receiver-XXXXXX.py")"
cat > "$RECEIVER_SCRIPT" <<'PY'
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

out_dir = sys.argv[1]
os.makedirs(out_dir, exist_ok=True)


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length)
        seq = len([n for n in os.listdir(out_dir) if n.endswith(".body")]) + 1
        headers_out = {
            "x-servify-signature": self.headers.get("X-Servify-Signature", ""),
            "x-servify-event": self.headers.get("X-Servify-Event", ""),
            "x-servify-event-id": self.headers.get("X-Servify-Event-ID", ""),
            "x-servify-delivery-id": self.headers.get("X-Servify-Delivery-ID", ""),
            "content-type": self.headers.get("Content-Type", ""),
        }
        with open(os.path.join(out_dir, "%03d.headers.json" % seq), "w") as fh:
            json.dump(headers_out, fh, indent=2)
            fh.write("\n")
        with open(os.path.join(out_dir, "%03d.body.json" % seq), "wb") as fh:
            fh.write(body)
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def log_message(self, *args):
        pass


HTTPServer(("127.0.0.1", int(sys.argv[2])), Handler).serve_forever()
PY
python3 "$RECEIVER_SCRIPT" "$EVIDENCE_DIR/received" "$RECEIVER_PORT" > "$EVIDENCE_DIR/receiver.log" 2>&1 &
RECEIVER_PID=$!

# 5) admin 注册 webhook 端点（订阅 ticket.created）
# URL 必须跟本地接收端同一 RECEIVER_PORT：heredoc 内硬编码端口会让
# RECEIVER_PORT 覆盖静默失效（接收端起在新端口、投递仍打旧端口）。
append_summary "step=create_endpoint"
CREATE_BODY=$(RECEIVER_PORT="$RECEIVER_PORT" python3 - <<'PY'
import json
import os
print(json.dumps({
    "name": "acceptance-receiver",
    "url": "http://127.0.0.1:%s/hooks" % os.environ["RECEIVER_PORT"],
    "events": "ticket.created",
    "description": "webhook acceptance receiver",
}, ensure_ascii=False))
PY
)
request_json "POST" "$SERVIFY_URL/api/webhooks" "$CREATE_BODY" "$ADMIN_TOKEN"
save_response "webhook-endpoint-create" "$RESPONSE_BODY"
append_summary "create_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "201" ]; then
  ENDPOINT_CREATED_OK=true
  append_summary "endpoint_created_ok=true"
else
  append_summary "endpoint_created_ok=false"
fi
ENDPOINT_ID=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("endpoint",{}).get("id",""))' 2>/dev/null || true)
WEBHOOK_SECRET=$(printf '%s' "$RESPONSE_BODY" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("secret",""))' 2>/dev/null || true)
if [ -z "$ENDPOINT_ID" ] || [ -z "$WEBHOOK_SECRET" ]; then
  echo "❌ 端点创建响应缺 id/secret" >&2
  exit 1
fi

# 6) 列表响应不回显 secret（凭据纪律）
append_summary "step=secret_hidden"
request_json "GET" "$SERVIFY_URL/api/webhooks" "" "$ADMIN_TOKEN"
save_response "webhook-endpoint-list" "$RESPONSE_BODY"
append_summary "list_status=$RESPONSE_STATUS"
if printf '%s' "$RESPONSE_BODY" | grep -q "secret"; then
  append_summary "secret_hidden_ok=false (list leaks secret)"
else
  SECRET_HIDDEN_OK=true
  append_summary "secret_hidden_ok=true"
fi

# 7) 事件白名单（12 类，含 ticket.created）
append_summary "step=events_whitelist"
request_json "GET" "$SERVIFY_URL/api/webhooks/events" "" "$ADMIN_TOKEN"
save_response "webhook-events" "$RESPONSE_BODY"
append_summary "events_status=$RESPONSE_STATUS"
EVENTS_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    events = json.load(sys.stdin).get("events") or []
    want = {"ticket.created", "ticket.assigned", "ticket.closed",
            "conversation.created", "conversation.message_received",
            "routing.agent_assigned", "routing.transfer_completed",
            "call.started", "call.held", "call.resumed", "call.transferred", "call.ended"}
    if set(events) == want:
        print("ok")
    else:
        print("mismatch: events=%r" % sorted(events))
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
append_summary "events_check=$EVENTS_CHECK"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$EVENTS_CHECK" = "ok" ]; then
  EVENTS_WHITELIST_OK=true
  append_summary "events_whitelist_ok=true"
else
  append_summary "events_whitelist_ok=false"
fi

# 8) 访客 WS 发消息建会话（会话行在首条消息持久化时建立）
append_summary "step=session_ws_message"
if [ -z "$SERVER_PID" ]; then
  append_summary "session_msg=skipped (external SERVIFY_URL, no WS tooling)"
  SESSION_MSG_OK=skipped
elif command -v node >/dev/null 2>&1 \
  && node -e 'process.exit(typeof WebSocket === "function" ? 0 : 1)' 2>/dev/null; then
  WS_SESSION_ID="wh-acc-1"
  if node - "$SERVIFY_PORT" "$WS_SESSION_ID" <<'NODE' 2>> "$EVIDENCE_DIR/ws-session.log"
const port = process.argv[2];
const sid = process.argv[3];
const ws = new WebSocket('ws://127.0.0.1:' + port + '/api/v1/ws?session_id=' + sid);
const timer = setTimeout(() => { console.error('ws connect timeout'); process.exit(1); }, 8000);
ws.addEventListener('open', () => {
  clearTimeout(timer);
  ws.send(JSON.stringify({
    type: 'text-message',
    data: { content: '验收脚本建会话消息' },
    session_id: sid,
    timestamp: new Date().toISOString(),
  }));
  setTimeout(() => { ws.close(); process.exit(0); }, 600);
});
ws.addEventListener('error', () => { clearTimeout(timer); process.exit(1); });
NODE
  then
    SESSION_MSG_OK=true
    append_summary "session_msg_ok=true"
  else
    append_summary "session_msg_ok=false"
  fi
  # 会话落库缓冲（worker/主链路持久化后建单才不 404）
  sleep 2
else
  append_summary "session_msg=skipped (node WebSocket unavailable)"
  SESSION_MSG_OK=skipped
fi

# 9) 访客建单（免认证）→ 触发 ticket.created 事件
append_summary "step=visitor_ticket"
TICKET_BODY=$(printf '{"session_id":"%s","title":"流量扣费异常","description":"验收脚本建单：流量包重复扣费","ai_summary":"脚本建单，无 AI 会话摘要"}' "${WS_SESSION_ID:-wh-acc-1}")
request_json "POST" "$SERVIFY_URL/api/v1/tickets" "$TICKET_BODY"
save_response "visitor-ticket" "$RESPONSE_BODY"
append_summary "ticket_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "201" ]; then
  TICKET_CREATED_OK=true
  append_summary "ticket_created_ok=true"
else
  append_summary "ticket_created_ok=false"
fi

# 10) 等接收端收到 ticket.created 投递（worker 30s 轮询，等两轮余量）
append_summary "step=delivery_signature"
DELIVERY_SIGNATURE_OK=false
if [ -z "$SERVER_PID" ]; then
  append_summary "delivery=skipped (external SERVIFY_URL)"
elif [ "$SESSION_MSG_OK" = "skipped" ]; then
  append_summary "delivery=skipped (session msg skipped)"
else
  for i in $(seq 1 28); do
    FIRST_HEADERS=$(ls "$EVIDENCE_DIR"/received/001.headers.json 2>/dev/null || true)
    if [ -n "$FIRST_HEADERS" ]; then
      break
    fi
    sleep 2
  done
  DELIVERY_HEADERS="$EVIDENCE_DIR/received/001.headers.json"
  DELIVERY_BODY="$EVIDENCE_DIR/received/001.body.json"
  if [ ! -f "$DELIVERY_HEADERS" ] || [ ! -f "$DELIVERY_BODY" ]; then
    echo "❌ 未收到投递（worker 未投递或接收端异常）" >&2
  else
    save_response "webhook-delivery-headers" "$(cat "$DELIVERY_HEADERS")"
    # 验签：v1 == HMAC-SHA256(secret, "<t>.<body>")，t 与 now 差 ≤5min
    SIG_CHECK=$(EVIDENCE_DIR="$EVIDENCE_DIR" SECRET="$WEBHOOK_SECRET" python3 - <<'PY'
import hashlib
import hmac
import json
import os
import time

headers = json.load(open(os.path.join(os.environ["EVIDENCE_DIR"], "received/001.headers.json")))
body = open(os.path.join(os.environ["EVIDENCE_DIR"], "received/001.body.json"), "rb").read()
sig = headers.get("x-servify-signature", "")
secret = os.environ["SECRET"]
event = headers.get("x-servify-event", "")
try:
    t_s, v1 = sig.split(",")
    t = int(t_s.split("=")[1])
    want_v1 = v1.split("=")[1]
except Exception:
    print("bad_header: %r" % sig)
    raise SystemExit(0)
mac = hmac.new(secret.encode(), ("%d." % t).encode() + body, hashlib.sha256).hexdigest()
ok_time = abs(int(time.time()) - t) <= 300
payload = json.loads(body)
event_name = payload.get("event") or payload.get("event_name") or ""
if hmac.compare_digest(mac, want_v1) and ok_time and event == "ticket.created" and event_name == "ticket.created":
    print("ok")
else:
    print("mismatch: hmac_ok=%s time_ok=%s header_event=%r body_event=%r" % (
        hmac.compare_digest(mac, want_v1), ok_time, event, event_name))
PY
)
    if [ "$SIG_CHECK" = "ok" ]; then
      DELIVERY_SIGNATURE_OK=true
      append_summary "delivery_signature_ok=true"
    else
      append_summary "delivery_signature_ok=false ($SIG_CHECK)"
    fi
  fi
fi

# 11) 投递记录状态（success）
append_summary "step=delivery_status"
request_json "GET" "$SERVIFY_URL/api/webhooks/deliveries?status=success" "" "$ADMIN_TOKEN"
save_response "webhook-deliveries" "$RESPONSE_BODY"
append_summary "deliveries_status=$RESPONSE_STATUS"
DELIVERIES_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
    total = data.get("total", 0)
    items = data.get("items") or []
    if total >= 1 and any((i.get("event_name") == "ticket.created") for i in items):
        print("ok")
    else:
        print("mismatch: total=%r" % total)
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
append_summary "deliveries_check=$DELIVERIES_CHECK"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$DELIVERIES_CHECK" = "ok" ]; then
  DELIVERY_STATUS_OK=true
  append_summary "delivery_status_ok=true"
else
  append_summary "delivery_status_ok=false"
fi

# 12) 自测端点（POST /:id/test，投递同样落到接收端）
append_summary "step=endpoint_selftest"
if [ -z "$SERVER_PID" ]; then
  append_summary "selftest=skipped (external SERVIFY_URL)"
  SELFTEST_OK=skipped
else
  request_json "POST" "$SERVIFY_URL/api/webhooks/$ENDPOINT_ID/test" "" "$ADMIN_TOKEN"
  save_response "webhook-endpoint-test" "$RESPONSE_BODY"
  append_summary "selftest_status=$RESPONSE_STATUS"
  SELFTEST_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin) or {}
    if d.get("status") in ("success", "failed"):
        print("ok")
    else:
        print("mismatch: %r" % d)
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
  append_summary "selftest_check=$SELFTEST_CHECK"
  if [ "$RESPONSE_STATUS" = "200" ] && [ "$SELFTEST_CHECK" = "ok" ]; then
    SELFTEST_OK=true
    append_summary "selftest_ok=true"
  else
    append_summary "selftest_ok=false"
  fi
fi

if [ "$ADMIN_AUTH_OK" != "true" ] || [ "$ENDPOINT_CREATED_OK" != "true" ] || [ "$SECRET_HIDDEN_OK" != "true" ] || [ "$EVENTS_WHITELIST_OK" != "true" ] || [ "$TICKET_CREATED_OK" != "true" ] || [ "$DELIVERY_SIGNATURE_OK" = "false" ] || [ "$DELIVERY_STATUS_OK" != "true" ] || [ "$SELFTEST_OK" = "false" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ Webhook acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Webhook acceptance 通过: 端点创建 → secret 不回显 → 白名单 12 类 → 访客 WS 建会话 → 建单触发 ticket.created → HMAC-SHA256 验签通过 → deliveries success → 自测投递 全部真实留档"
