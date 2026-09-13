#!/bin/bash

# Twilio PSTN webhook ingress acceptance 测试。
#
# 必需环境变量:
#   TWILIO_AUTH_TOKEN  与服务端 voice.twilio.auth_token 一致的签名 token
#
# 可选环境变量:
#   SERVIFY_URL   服务地址(默认 http://localhost:8080)
#   SQLITE_DB     sqlite 数据库路径,提供后追加落库断言(voice_calls/voice_recordings)
#   PSQL_CMD      psql 命令前缀(如 "ssh host psql -t -A servify"),提供后同上
#   ADMIN_TOKEN   管理面 JWT,提供后追加入站 webhook 断言(订阅 call.started
#                 + 本地 receiver 验证出站)
#
# 证据输出: scripts/test-results/voice-pstn-acceptance/(manifest.json 之外
# 的文件不入库,.gitignore 已排除)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Voice PSTN acceptance 测试开始..."

SERVIFY_URL=${SERVIFY_URL:-"http://localhost:8080"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/voice-pstn-acceptance"}

if [ -z "${TWILIO_AUTH_TOKEN:-}" ]; then
  echo "❌ 缺少 TWILIO_AUTH_TOKEN 环境变量(与服务端 voice.twilio.auth_token 一致)"
  exit 1
fi

mkdir -p "$EVIDENCE_DIR"

WEBHOOK_PATH="/public/voice/webhooks/twilio"
WEBHOOK_URL="${SERVIFY_URL}${WEBHOOK_PATH}"
TEST_CALL_ID="CA-pstn-acceptance-$(date +%s)"
TEST_RECORDING_ID="RE-pstn-acceptance-$(date +%s)"
TEST_RECORDING_URL="https://proof-of-life.example.com/${TEST_RECORDING_ID}.mp3"

SIGNATURE_OK=false
DUPLICATE_INVITE_OK=false
ANSWER_OK=false
HANGUP_OK=false
LATE_INVITE_SHORT_CIRCUIT_OK=false
RECORDING_OK=false
BAD_SIGNATURE_REJECTED=false
MISSING_SIGNATURE_REJECTED=false
UNKNOWN_STATUS_REJECTED=false
DB_CALL_ASSERTED=false
DB_RECORDING_ASSERTED=false
OUTBOUND_OK=false
OVERALL_STATUS=failed

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

write_manifest() {
  local exit_code=$?
  OVERALL_STATUS=${OVERALL_STATUS}
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS}" \
  MANIFEST_EXIT_CODE="$exit_code" \
  MANIFEST_SERVIFY_URL="$SERVIFY_URL" \
  MANIFEST_SIGNATURE_OK="$SIGNATURE_OK" \
  MANIFEST_DUPLICATE_INVITE_OK="$DUPLICATE_INVITE_OK" \
  MANIFEST_ANSWER_OK="$ANSWER_OK" \
  MANIFEST_HANGUP_OK="$HANGUP_OK" \
  MANIFEST_LATE_INVITE_SHORT_CIRCUIT_OK="$LATE_INVITE_SHORT_CIRCUIT_OK" \
  MANIFEST_RECORDING_OK="$RECORDING_OK" \
  MANIFEST_BAD_SIGNATURE_REJECTED="$BAD_SIGNATURE_REJECTED" \
  MANIFEST_MISSING_SIGNATURE_REJECTED="$MISSING_SIGNATURE_REJECTED" \
  MANIFEST_UNKNOWN_STATUS_REJECTED="$UNKNOWN_STATUS_REJECTED" \
  MANIFEST_DB_CALL_ASSERTED="$DB_CALL_ASSERTED" \
  MANIFEST_DB_RECORDING_ASSERTED="$DB_RECORDING_ASSERTED" \
  MANIFEST_OUTBOUND_OK="$OUTBOUND_OK" \
  MANIFEST_TEST_CALL_ID="$TEST_CALL_ID" \
  MANIFEST_TEST_RECORDING_ID="$TEST_RECORDING_ID" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
env = os.environ
payload = {
    "provider": "voice-pstn",
    "mode": "twilio-signature",
    "servify_url": env.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": env.get("MANIFEST_OVERALL_STATUS", "unknown"),
        "exit_code": env.get("MANIFEST_EXIT_CODE", "unknown"),
    },
    "checks": {
        "signature_ok": env.get("MANIFEST_SIGNATURE_OK", "false"),
        "duplicate_invite_ok": env.get("MANIFEST_DUPLICATE_INVITE_OK", "false"),
        "answer_ok": env.get("MANIFEST_ANSWER_OK", "false"),
        "hangup_ok": env.get("MANIFEST_HANGUP_OK", "false"),
        "late_invite_short_circuit_ok": env.get("MANIFEST_LATE_INVITE_SHORT_CIRCUIT_OK", "false"),
        "recording_ok": env.get("MANIFEST_RECORDING_OK", "false"),
        "bad_signature_rejected": env.get("MANIFEST_BAD_SIGNATURE_REJECTED", "false"),
        "missing_signature_rejected": env.get("MANIFEST_MISSING_SIGNATURE_REJECTED", "false"),
        "unknown_status_rejected": env.get("MANIFEST_UNKNOWN_STATUS_REJECTED", "false"),
        "db_call_asserted": env.get("MANIFEST_DB_CALL_ASSERTED", "false"),
        "db_recording_asserted": env.get("MANIFEST_DB_RECORDING_ASSERTED", "false"),
        "outbound_ok": env.get("MANIFEST_OUTBOUND_OK", "false"),
    },
    "metrics": {
        "test_call_id": env.get("MANIFEST_TEST_CALL_ID", ""),
        "test_recording_id": env.get("MANIFEST_TEST_RECORDING_ID", ""),
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

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Voice PSTN acceptance summary
servify_url=$SERVIFY_URL
EOF

echo "🗂️ 证据输出目录: $EVIDENCE_DIR"

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

# post_webhook FORM_K=V... [--signature SIG]
# 不带 --signature 时不带 X-Twilio-Signature 头;SIG 为 "invalid" 时发送垃圾签名。
post_webhook() {
  local response_file status_file
  response_file=$(mktemp)
  status_file=$(mktemp)
  trap 'rm -f "$response_file" "$status_file"' RETURN

  local -a curl_args=(-sS -X POST "$WEBHOOK_URL" -H "Content-Type: application/x-www-form-urlencoded")
  local signature_mode="none"
  local -a form_pairs=()
  local arg
  while [ $# -gt 0 ]; do
    arg=$1
    shift
    case "$arg" in
      --signature)
        signature_mode=$1
        shift
        ;;
      *)
        form_pairs+=("$arg")
        ;;
    esac
  done

  local form_body
  form_body=$(printf '%s\n' "${form_pairs[@]}" | python3 -c 'import sys, urllib.parse; print(urllib.parse.urlencode(dict(line.strip().split("=", 1) for line in sys.stdin if line.strip())))')

  if [ "$signature_mode" != "none" ]; then
    local signature
    if [ "$signature_mode" = "invalid" ]; then
      signature="invalid-signature"
    else
      signature=$(FORM_BODY="$form_body" TWILIO_AUTH_TOKEN="$TWILIO_AUTH_TOKEN" WEBHOOK_URL="$WEBHOOK_URL" python3 - <<'PY'
import hashlib
import hmac
import os
import sys
import urllib.parse

body = os.environ["FORM_BODY"]
token = os.environ["TWILIO_AUTH_TOKEN"]
url = os.environ["WEBHOOK_URL"]
params = urllib.parse.parse_qsl(body, keep_blank_values=True)
material = url + "".join(key + value for key, value in sorted(params))
digest = hmac.new(token.encode(), material.encode(), hashlib.sha1).digest()
print(__import__("base64").b64encode(digest).decode())
PY
      )
    fi
    curl_args+=(-H "X-Twilio-Signature: $signature")
  fi

  curl "${curl_args[@]}" --data "$form_body" -o "$response_file" -w "%{http_code}" > "$status_file"
  RESPONSE_STATUS=$(cat "$status_file")
  RESPONSE_BODY=$(cat "$response_file")
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

echo "🔍 检查服务状态..."
wait_for "Servify Health" "$SERVIFY_URL/health" 30 2

# 1. invite(queued)签名有效 → 200
echo "📞 invite(queued)..."
post_webhook "CallSid=$TEST_CALL_ID" "CallStatus=queued" "From=+15550001111" "To=+15550002222" --signature valid
assert_status "200" "$RESPONSE_STATUS" "invite"
SIGNATURE_OK=true
append_summary "signature_ok=$SIGNATURE_OK"

# 2. 重复 invite → 200(幂等短路,不重复建记录)
echo "📞 重复 invite(queued)..."
post_webhook "CallSid=$TEST_CALL_ID" "CallStatus=queued" --signature valid
assert_status "200" "$RESPONSE_STATUS" "duplicate_invite"
DUPLICATE_INVITE_OK=true
append_summary "duplicate_invite_ok=$DUPLICATE_INVITE_OK"

# 3. answer(in-progress)→ 200
echo "📞 answer(in-progress)..."
post_webhook "CallSid=$TEST_CALL_ID" "CallStatus=in-progress" --signature valid
assert_status "200" "$RESPONSE_STATUS" "answer"
ANSWER_OK=true
append_summary "answer_ok=$ANSWER_OK"

# 4. hangup(completed)→ 200
echo "📞 hangup(completed)..."
post_webhook "CallSid=$TEST_CALL_ID" "CallStatus=completed" --signature valid
assert_status "200" "$RESPONSE_STATUS" "hangup"
HANGUP_OK=true
append_summary "hangup_ok=$HANGUP_OK"

# 5. 已结束通话的迟到 invite → 200 且不回退状态
echo "📞 迟到 invite(幂等)..."
post_webhook "CallSid=$TEST_CALL_ID" "CallStatus=queued" --signature valid
assert_status "200" "$RESPONSE_STATUS" "late_invite"
LATE_INVITE_SHORT_CIRCUIT_OK=true
append_summary "late_invite_short_circuit_ok=$LATE_INVITE_SHORT_CIRCUIT_OK"

# 6. 录音完成回调 → 200
echo "🎙️ recording callback..."
post_webhook "CallSid=$TEST_CALL_ID" "RecordingSid=$TEST_RECORDING_ID" "RecordingUrl=$TEST_RECORDING_URL" "RecordingStatus=completed" --signature valid
assert_status "200" "$RESPONSE_STATUS" "recording"
RECORDING_OK=true
append_summary "recording_ok=$RECORDING_OK"

# 7. 错误签名 → 403
echo "🚫 错误签名..."
post_webhook "CallSid=CA-bad-sig" "CallStatus=queued" --signature invalid
assert_status "403" "$RESPONSE_STATUS" "bad_signature"
BAD_SIGNATURE_REJECTED=true
append_summary "bad_signature_rejected=$BAD_SIGNATURE_REJECTED"

# 8. 缺签名头 → 403
echo "🚫 缺签名头..."
post_webhook "CallSid=CA-no-sig" "CallStatus=queued"
assert_status "403" "$RESPONSE_STATUS" "missing_signature"
MISSING_SIGNATURE_REJECTED=true
append_summary "missing_signature_rejected=$MISSING_SIGNATURE_REJECTED"

# 9. 未知 CallStatus → 400
echo "🚫 未知 CallStatus..."
post_webhook "CallSid=CA-bad-status" "CallStatus=teleported" --signature valid
assert_status "400" "$RESPONSE_STATUS" "unknown_status"
UNKNOWN_STATUS_REJECTED=true
append_summary "unknown_status_rejected=$UNKNOWN_STATUS_REJECTED"

# 10. 落库断言(可选:SQLITE_DB / PSQL_CMD)
db_query_defined=false
if [ -n "${SQLITE_DB:-}" ]; then
  # python3 内置 sqlite3 模块,不依赖 sqlite3 CLI
  DB_QUERY() { SQLITE_DB="$SQLITE_DB" QUERY="$1" python3 -c 'import os, sqlite3; print(sqlite3.connect(os.environ["SQLITE_DB"]).execute(os.environ["QUERY"]).fetchone()[0])'; }
  db_query_defined=true
elif [ -n "${PSQL_CMD:-}" ]; then
  DB_QUERY() { $PSQL_CMD -c "$1"; }
  db_query_defined=true
fi
if [ "$db_query_defined" = "true" ]; then
  echo "🗄️ 落库断言..."
  CALL_STATUS=$(DB_QUERY "SELECT status FROM voice_calls WHERE id='$TEST_CALL_ID';" | tr -d '[:space:]')
  if [ "$CALL_STATUS" != "ended" ]; then
    echo "❌ voice_calls.status=$CALL_STATUS, want ended"
    exit 1
  fi
  DB_CALL_ASSERTED=true
  append_summary "db_call_asserted=$DB_CALL_ASSERTED"
  REC_URI=$(DB_QUERY "SELECT storage_uri FROM voice_recordings WHERE id='$TEST_RECORDING_ID';" | tr -d '[:space:]')
  if [ "$REC_URI" != "$TEST_RECORDING_URL" ]; then
    echo "❌ voice_recordings.storage_uri=$REC_URI, want $TEST_RECORDING_URL"
    exit 1
  fi
  DB_RECORDING_ASSERTED=true
  append_summary "db_recording_asserted=$DB_RECORDING_ASSERTED"
else
  echo "ℹ️ 未提供 SQLITE_DB/PSQL_CMD,跳过落库断言"
fi

# 11. 出站 webhook 断言(可选:ADMIN_TOKEN + 本地 receiver 收 call.started)
if [ -n "${ADMIN_TOKEN:-}" ]; then
  echo "📡 出站 webhook 断言..."
  RECEIVER_PORT=${RECEIVER_PORT:-18099}
  RECEIVED_FILE="$EVIDENCE_DIR/receiver-payloads.jsonl"
  : > "$RECEIVED_FILE"
  python3 - "$RECEIVER_PORT" "$RECEIVED_FILE" <<'PY' &
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

port = int(sys.argv[1])
out = open(sys.argv[2], "ab")

class Receiver(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        out.write(body + b"\n")
        out.flush()
        self.send_response(200)
        self.end_headers()

    def log_message(self, *args):
        pass

HTTPServer(("127.0.0.1", port), Receiver).serve_forever()
PY
  RECEIVER_PID=$!
  trap 'kill $RECEIVER_PID 2>/dev/null || true; write_manifest' EXIT

  OUT_CALL_ID="CA-pstn-outbound-$(date +%s)"
  CREATE_BODY=$(printf '{"name":"pstn-acceptance","url":"http://127.0.0.1:%s/hook","events":"call.started","description":"voice-pstn acceptance"}' "$RECEIVER_PORT")
  CREATE_RESPONSE=$(curl -sS -X POST "$SERVIFY_URL/api/webhooks" \
    -H "Authorization: Bearer $ADMIN_TOKEN" -H "Content-Type: application/json" --data "$CREATE_BODY")
  printf '%s\n' "$CREATE_RESPONSE" > "$EVIDENCE_DIR/webhook-create.json"
  ENDPOINT_ID=$(printf '%s' "$CREATE_RESPONSE" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("endpoint", {}).get("id", ""))' 2>/dev/null || true)
  if [ -z "$ENDPOINT_ID" ]; then
    echo "❌ webhook endpoint 创建失败: $CREATE_RESPONSE"
    exit 1
  fi
  sleep 2
  post_webhook "CallSid=$OUT_CALL_ID" "CallStatus=queued" --signature valid
  assert_status "200" "$RESPONSE_STATUS" "outbound_invite"

  OUTBOUND_HIT=false
  for _ in $(seq 1 30); do
    if grep -q "$OUT_CALL_ID" "$RECEIVED_FILE" 2>/dev/null; then
      OUTBOUND_HIT=true
      break
    fi
    sleep 1
  done
  curl -sS -X DELETE "$SERVIFY_URL/api/webhooks/$ENDPOINT_ID" -H "Authorization: Bearer $ADMIN_TOKEN" > /dev/null
  kill $RECEIVER_PID 2>/dev/null || true
  trap write_manifest EXIT
  if [ "$OUTBOUND_HIT" != "true" ]; then
    echo "❌ 本地 receiver 未收到 call.started"
    exit 1
  fi
  OUTBOUND_OK=true
  append_summary "outbound_ok=$OUTBOUND_OK"
else
  echo "ℹ️ 未提供 ADMIN_TOKEN,跳过出站 webhook 断言"
fi

OVERALL_STATUS=passed
append_summary "signature_ok=$SIGNATURE_OK"
append_summary "duplicate_invite_ok=$DUPLICATE_INVITE_OK"
append_summary "answer_ok=$ANSWER_OK"
append_summary "hangup_ok=$HANGUP_OK"
append_summary "late_invite_short_circuit_ok=$LATE_INVITE_SHORT_CIRCUIT_OK"
append_summary "recording_ok=$RECORDING_OK"
append_summary "bad_signature_rejected=$BAD_SIGNATURE_REJECTED"
append_summary "missing_signature_rejected=$MISSING_SIGNATURE_REJECTED"
append_summary "unknown_status_rejected=$UNKNOWN_STATUS_REJECTED"
append_summary "db_call_asserted=$DB_CALL_ASSERTED"
append_summary "db_recording_asserted=$DB_RECORDING_ASSERTED"
append_summary "outbound_ok=$OUTBOUND_OK"
append_summary "overall_status=$OVERALL_STATUS"

echo "✅ Voice PSTN acceptance 测试通过"
