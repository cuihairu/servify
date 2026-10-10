#!/bin/bash
# 聊天实时翻译验收（Phase 0/1 实机走查，2026-10-10）：
# ai.provider=local（零外部依赖 LLM）下走通全链路——
# admin 注册 → 签发 service API key（明文只回显一次）→ X-API-Key 换访客
# guest token → 双读向偏好（admin=agent 向 en / guest=visitor 向 ja，含
# 跨会话绑定 403 负例）→ 直译端点（确定性双跑）→ 访客 WS 消息触发
# hub 自动翻译（agent 读向）→ 坐席 REST 发消息触发 visitor 读向翻译 →
# 偏好清除。译文内容由抽取式 provider 决定（上下文回声），本脚本断言
# 的是链路接线：帧到达、original/target_lang/source_lang 正确、确定性。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Translation acceptance（实时翻译全链路）测试开始..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/translation-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"translation-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"translation-123"}
SESSION_ID=${SESSION_ID:-"trans-acc-1"}

BUILD_OK=false
READY_OK=false
UNAUTH_TRANSLATE_REJECTED=false
ADMIN_AUTH_OK=false
API_KEY_CREATED_OK=false
API_KEY_LIST_HIDES_PLAINTEXT_OK=false
GUEST_TOKEN_ISSUED_OK=false
AGENT_PREF_SET_OK=false
VISITOR_PREF_SET_OK=false
VISITOR_PREF_MISMATCH_REJECTED=false
PREF_ROUNDTRIP_OK=false
TRANSLATE_ENDPOINT_OK=false
TRANSLATE_DETERMINISTIC_OK=false
TRANSLATE_INVALID_LANG_REJECTED=false
WS_VISITOR_TO_AGENT_OK=false
AGENT_SEND_OK=false
WS_AGENT_TO_VISITOR_OK=false
PREF_DELETE_OK=false
OVERALL_STATUS=failed

SERVIFY_URL=${SERVIFY_URL:-}
SERVER_PID=""
SERVER_PID_SET=false
DB_DSN=""
WORK_DIR=""

cleanup() {
  if $SERVER_PID_SET && [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
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

save_response() {
  local name=$1
  local body=${2:-}
  printf '%s\n' "$body" > "$EVIDENCE_DIR/$name.json"
}

RESPONSE_STATUS=""
RESPONSE_BODY=""
request_json() {
  local method=$1 url=$2 body=${3:-} token=${4:-} api_key=${5:-}
  local response_file status_file
  response_file="$(mktemp "${TMPDIR:-/tmp}/trans-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/trans-status-XXXXXX")"
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  if [ -n "$body" ]; then
    args+=(-H "Content-Type: application/json" -d "$body")
  fi
  if [ -n "$token" ]; then
    args+=(-H "Authorization: Bearer $token")
  fi
  if [ -n "$api_key" ]; then
    args+=(-H "X-API-Key: $api_key")
  fi
  curl "${args[@]}" > "$status_file" 2>/dev/null || true
  RESPONSE_STATUS="$(cat "$status_file")"
  RESPONSE_BODY="$(cat "$response_file")"
  rm -f "$response_file" "$status_file"
}

json_field() {
  local json_input="${1:-}"
  local python_expr="${2:-}"
  JSON_INPUT="$json_input" EXPR="$python_expr" python3 - <<'PY'
import json, os, sys
try:
    data = json.loads(os.environ["JSON_INPUT"])
    result = eval(os.environ["EXPR"], {"d": data})
    print("" if result is None else result)
except Exception:
    print("")
PY
}

wait_for() {
  local name=$1 url=$2 max=$3 sleep_s=$4
  echo "⏳ 等待 $name 可用: $url (最多 ${max} 次，每次 ${sleep_s}s)"
  for _ in $(seq 1 "$max"); do
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
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_UNAUTH_TRANSLATE_REJECTED="${UNAUTH_TRANSLATE_REJECTED:-false}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_API_KEY_CREATED_OK="${API_KEY_CREATED_OK:-false}" \
  MANIFEST_API_KEY_LIST_HIDES_PLAINTEXT_OK="${API_KEY_LIST_HIDES_PLAINTEXT_OK:-false}" \
  MANIFEST_GUEST_TOKEN_ISSUED_OK="${GUEST_TOKEN_ISSUED_OK:-false}" \
  MANIFEST_AGENT_PREF_SET_OK="${AGENT_PREF_SET_OK:-false}" \
  MANIFEST_VISITOR_PREF_SET_OK="${VISITOR_PREF_SET_OK:-false}" \
  MANIFEST_VISITOR_PREF_MISMATCH_REJECTED="${VISITOR_PREF_MISMATCH_REJECTED:-false}" \
  MANIFEST_PREF_ROUNDTRIP_OK="${PREF_ROUNDTRIP_OK:-false}" \
  MANIFEST_TRANSLATE_ENDPOINT_OK="${TRANSLATE_ENDPOINT_OK:-false}" \
  MANIFEST_TRANSLATE_DETERMINISTIC_OK="${TRANSLATE_DETERMINISTIC_OK:-false}" \
  MANIFEST_TRANSLATE_INVALID_LANG_REJECTED="${TRANSLATE_INVALID_LANG_REJECTED:-false}" \
  MANIFEST_WS_VISITOR_TO_AGENT_OK="${WS_VISITOR_TO_AGENT_OK:-false}" \
  MANIFEST_AGENT_SEND_OK="${AGENT_SEND_OK:-false}" \
  MANIFEST_WS_AGENT_TO_VISITOR_OK="${WS_AGENT_TO_VISITOR_OK:-false}" \
  MANIFEST_PREF_DELETE_OK="${PREF_DELETE_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "translation",
    "mode": os.environ.get("MANIFEST_MODE", "real"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "llm_provider": "local",
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": os.environ.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_translate_rejected_401": os.environ.get("MANIFEST_UNAUTH_TRANSLATE_REJECTED", "false"),
        "admin_auth_ok": os.environ.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "api_key_created_ok": os.environ.get("MANIFEST_API_KEY_CREATED_OK", "false"),
        "api_key_list_hides_plaintext_ok": os.environ.get("MANIFEST_API_KEY_LIST_HIDES_PLAINTEXT_OK", "false"),
        "guest_token_issued_ok": os.environ.get("MANIFEST_GUEST_TOKEN_ISSUED_OK", "false"),
        "agent_pref_set_ok": os.environ.get("MANIFEST_AGENT_PREF_SET_OK", "false"),
        "visitor_pref_set_ok": os.environ.get("MANIFEST_VISITOR_PREF_SET_OK", "false"),
        "visitor_pref_mismatch_rejected_403": os.environ.get("MANIFEST_VISITOR_PREF_MISMATCH_REJECTED", "false"),
        "pref_roundtrip_ok": os.environ.get("MANIFEST_PREF_ROUNDTRIP_OK", "false"),
        "translate_endpoint_ok": os.environ.get("MANIFEST_TRANSLATE_ENDPOINT_OK", "false"),
        "translate_deterministic_ok": os.environ.get("MANIFEST_TRANSLATE_DETERMINISTIC_OK", "false"),
        "translate_invalid_lang_rejected_400": os.environ.get("MANIFEST_TRANSLATE_INVALID_LANG_REJECTED", "false"),
        "ws_visitor_to_agent_translation_ok": os.environ.get("MANIFEST_WS_VISITOR_TO_AGENT_OK", "false"),
        "agent_send_ok": os.environ.get("MANIFEST_AGENT_SEND_OK", "false"),
        "ws_agent_to_visitor_translation_ok": os.environ.get("MANIFEST_WS_AGENT_TO_VISITOR_OK", "false"),
        "pref_delete_ok": os.environ.get("MANIFEST_PREF_DELETE_OK", "false"),
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

mkdir -p "$EVIDENCE_DIR"

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
Translation acceptance summary（Phase 0/1 实机走查，ai.provider=local）
EOF

if [ -n "$SERVIFY_URL" ]; then
  echo "⚠️ 使用外部 SERVIFY_URL=$SERVIFY_URL（外部栈需自备 ai.provider=local 配置）"
  append_summary "servify_url=$SERVIFY_URL (external)"
else
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

  # 2) 临时 config：ai.provider=local（零网络出站）；限流放宽到走查量级。
  #    服务起在 WORK_DIR（viper 按 cwd 找 ./config.yml），落库 TZ 钉 UTC。
  append_summary "step=setup"
  SERVIFY_PORT=${SERVIFY_PORT:-18108}
  SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/translation-XXXXXX.sqlite")"
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/translation-work-XXXXXX")"
  python3 - "$PROJECT_ROOT/config.yml" "$WORK_DIR/config.yml" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["paths"] = [
    {"enabled": True, "prefix": "/api/", "requests_per_minute": 120, "burst": 60},
]
# 零依赖翻译引擎：无出站请求、无 key 要求（与 local-knowledge 同款组合）。
cfg["ai"]["provider"] = "local"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
  echo "🚀 启动真实服务 (sqlite + ai.provider=local): $SERVIFY_URL"
  bash -c 'cd "$1" && exec env TZ=UTC SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
  SERVER_PID_SET=true
  append_summary "servify_url=$SERVIFY_URL (sqlite local, ai=local)"
fi

if ! wait_for "Servify" "$SERVIFY_URL/ready" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi
READY_OK=true
append_summary "ready_ok=true"

# 3) 负例：未认证直译端点。
append_summary "step=unauth_translate"
request_json "POST" "$SERVIFY_URL/api/v1/translation/translate" '{"text":"hello","target_lang":"en"}'
save_response "unauthenticated-translate" "$RESPONSE_BODY"
append_summary "unauth_translate_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_TRANSLATE_REJECTED=true
  append_summary "unauthenticated_translate_rejected_401=true"
else
  append_summary "unauthenticated_translate_rejected_401=false"
fi

# 4) 注册 admin（空库首个用户，role=admin 被端点接受）拿 token。
append_summary "step=admin_auth"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Translation Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY"
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
ADMIN_TOKEN=$(json_field "$RESPONSE_BODY" "d.get('token','')" 2>/dev/null || true)
if [ -z "$ADMIN_TOKEN" ]; then
  append_summary "admin_token=missing"
  echo "❌ 未拿到 admin token" >&2
  exit 1
fi
ADMIN_AUTH_OK=true
append_summary "admin_auth_ok=true"

# 5) 签发 service API key（明文只在此响应出现一次）。
append_summary "step=api_key"
request_json "POST" "$SERVIFY_URL/api/api-keys" '{"name":"translation-acceptance","scopes":""}' "$ADMIN_TOKEN"
save_response "api-key-create" "$RESPONSE_BODY"
append_summary "api_key_create_status=$RESPONSE_STATUS"
API_KEY_PLAINTEXT=$(json_field "$RESPONSE_BODY" "d.get('plaintext','')" 2>/dev/null || true)
if [ "$RESPONSE_STATUS" = "201" ] && printf '%s' "$API_KEY_PLAINTEXT" | grep -q "^sv_"; then
  API_KEY_CREATED_OK=true
  append_summary "api_key_created_ok=true"
else
  append_summary "api_key_created_ok=false"
  echo "❌ API key 签发失败" >&2
  exit 1
fi

# 6) 列表不回显明文（凭据纪律，与 webhook 端点 secret 同款检查）。
request_json "GET" "$SERVIFY_URL/api/api-keys" "" "$ADMIN_TOKEN"
save_response "api-key-list" "$RESPONSE_BODY"
append_summary "api_key_list_status=$RESPONSE_STATUS"
if printf '%s' "$RESPONSE_BODY" | grep -qF "$API_KEY_PLAINTEXT"; then
  append_summary "api_key_list_hides_plaintext_ok=false (list leaks plaintext)"
else
  API_KEY_LIST_HIDES_PLAINTEXT_OK=true
  append_summary "api_key_list_hides_plaintext_ok=true"
fi

# 7) service 凭据换访客 guest token（X-API-Key → access_token）。
append_summary "step=guest_token"
request_json "POST" "$SERVIFY_URL/api/v1/guest/session" "{\"session_id\":\"$SESSION_ID\"}" "" "$API_KEY_PLAINTEXT"
save_response "guest-session" "$RESPONSE_BODY"
append_summary "guest_session_status=$RESPONSE_STATUS"
GUEST_TOKEN=$(json_field "$RESPONSE_BODY" "d.get('access_token','')" 2>/dev/null || true)
if [ "$RESPONSE_STATUS" = "200" ] || [ "$RESPONSE_STATUS" = "201" ]; then
  if [ -n "$GUEST_TOKEN" ]; then
    GUEST_TOKEN_ISSUED_OK=true
    append_summary "guest_token_issued_ok=true"
  else
    append_summary "guest_token_issued_ok=false (missing access_token)"
  fi
else
  append_summary "guest_token_issued_ok=false"
fi
if [ "$GUEST_TOKEN_ISSUED_OK" != "true" ]; then
  echo "❌ 访客 token 签发失败" >&2
  exit 1
fi

# 8) 双读向偏好：admin → agent 读向 en；guest（end_user，sid 绑定）→ visitor 读向 ja。
append_summary "step=pref_dual_viewer"
request_json "PUT" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" '{"target_lang":"en"}' "$ADMIN_TOKEN"
save_response "pref-agent-put" "$RESPONSE_BODY"
append_summary "agent_pref_put_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] \
  && [ "$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)" = "en" ]; then
  AGENT_PREF_SET_OK=true
  append_summary "agent_pref_set_ok=true"
else
  append_summary "agent_pref_set_ok=false"
fi

request_json "PUT" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" '{"target_lang":"ja"}' "$GUEST_TOKEN"
save_response "pref-visitor-put" "$RESPONSE_BODY"
append_summary "visitor_pref_put_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] \
  && [ "$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)" = "ja" ]; then
  VISITOR_PREF_SET_OK=true
  append_summary "visitor_pref_set_ok=true"
else
  append_summary "visitor_pref_set_ok=false"
fi

# 9) 负例：guest token 打其他会话的偏好 → 403（会话绑定强制）。
request_json "PUT" "$SERVIFY_URL/api/v1/translation/preferences/other-session" '{"target_lang":"en"}' "$GUEST_TOKEN"
save_response "pref-visitor-mismatch" "$RESPONSE_BODY"
append_summary "visitor_pref_mismatch_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "403" ]; then
  VISITOR_PREF_MISMATCH_REJECTED=true
  append_summary "visitor_pref_mismatch_rejected_403=true"
else
  append_summary "visitor_pref_mismatch_rejected_403=false"
fi

# 10) 读回校验：同一会话两个读向各自一条（agent=en / visitor=ja）。
append_summary "step=pref_roundtrip"
request_json "GET" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" "" "$ADMIN_TOKEN"
save_response "pref-agent-get" "$RESPONSE_BODY"
AGENT_LANG=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)
request_json "GET" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" "" "$GUEST_TOKEN"
save_response "pref-visitor-get" "$RESPONSE_BODY"
VISITOR_LANG=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)
append_summary "pref_roundtrip agent=$AGENT_LANG visitor=$VISITOR_LANG"
if [ "$AGENT_LANG" = "en" ] && [ "$VISITOR_LANG" = "ja" ]; then
  PREF_ROUNDTRIP_OK=true
  append_summary "pref_roundtrip_ok=true"
else
  append_summary "pref_roundtrip_ok=false"
fi

# 11) 直译端点：本地抽取式 provider，确定性双跑。
append_summary "step=translate_endpoint"
TRANSLATE_BODY='{"text":"您好，我的订单三天了还没发货","target_lang":"en"}'
request_json "POST" "$SERVIFY_URL/api/v1/translation/translate" "$TRANSLATE_BODY" "$ADMIN_TOKEN"
save_response "translate-direct" "$RESPONSE_BODY"
append_summary "translate_status=$RESPONSE_STATUS"
TRANSLATE_TEXT_1=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('text','')" 2>/dev/null || true)
TRANSLATE_PROVIDER=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('provider','')" 2>/dev/null || true)
TRANSLATE_TARGET=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)
TRANSLATE_SOURCE=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('source_lang','')" 2>/dev/null || true)
append_summary "translate provider=$TRANSLATE_PROVIDER target=$TRANSLATE_TARGET source=$TRANSLATE_SOURCE text_len=${#TRANSLATE_TEXT_1}"
if [ "$RESPONSE_STATUS" = "200" ] \
  && [ -n "$TRANSLATE_TEXT_1" ] \
  && [ "$TRANSLATE_PROVIDER" = "local" ] \
  && [ "$TRANSLATE_TARGET" = "en" ] \
  && [ "$TRANSLATE_SOURCE" = "auto" ]; then
  TRANSLATE_ENDPOINT_OK=true
  append_summary "translate_endpoint_ok=true"
else
  append_summary "translate_endpoint_ok=false"
fi

request_json "POST" "$SERVIFY_URL/api/v1/translation/translate" "$TRANSLATE_BODY" "$ADMIN_TOKEN"
save_response "translate-direct-repeat" "$RESPONSE_BODY"
TRANSLATE_TEXT_2=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('text','')" 2>/dev/null || true)
if [ -n "$TRANSLATE_TEXT_2" ] && [ "$TRANSLATE_TEXT_2" = "$TRANSLATE_TEXT_1" ]; then
  TRANSLATE_DETERMINISTIC_OK=true
  append_summary "translate_deterministic_ok=true"
else
  append_summary "translate_deterministic_ok=false"
fi

request_json "POST" "$SERVIFY_URL/api/v1/translation/translate" '{"text":"hello","target_lang":"not valid!"}' "$ADMIN_TOKEN"
save_response "translate-invalid-lang" "$RESPONSE_BODY"
append_summary "translate_invalid_lang_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "400" ]; then
  TRANSLATE_INVALID_LANG_REJECTED=true
  append_summary "translate_invalid_lang_rejected_400=true"
else
  append_summary "translate_invalid_lang_rejected_400=false"
fi

# 12) WS 全链路（node 内置 WebSocket；单连接双方向：先发访客消息等 agent
#     读向帧，收到后 bash 再触发坐席 REST 发送，等 visitor 读向帧——同一
#     会话连接保证坐席帧广播时监听者已就位，帧留档 JSONL）。
append_summary "step=ws_translation"
if ! command -v node >/dev/null 2>&1 \
  || ! node -e 'process.exit(typeof WebSocket === "function" ? 0 : 1)' 2>/dev/null; then
  append_summary "ws=failed (node WebSocket unavailable)"
  echo "❌ node WebSocket 不可用，翻译全链路走查无法执行" >&2
  exit 1
fi

SIGNAL_DIR="$(mktemp -d "${TMPDIR:-/tmp}/trans-ws-signal-XXXXXX")"
WS_CLIENT_JS="$(mktemp "${TMPDIR:-/tmp}/trans-ws-client-XXXXXX.js")"
cat > "$WS_CLIENT_JS" <<'NODE'
// 单连接双方向走查：连上即发访客 text-message；收到 agent 读向（en）的
// message-translated（original == 已发文本）后落 phase1 信号文件（告知
// bash 触发坐席 REST 发送）；收到 visitor 读向（ja）帧后收尾退出。
const fs = require('fs');
const signalDir = process.argv[2];
const port = process.argv[3];
const sid = process.argv[4];
const sentText = '您好，我的订单三天了还没发货';
const ws = new WebSocket('ws://127.0.0.1:' + port + '/api/v1/ws?session_id=' + sid);
const frames = [];
let phase1Done = false;
let timer = null;

function finish(ok) {
  if (timer) clearTimeout(timer);
  frames.forEach((f) => console.log(JSON.stringify(f)));
  process.exit(ok ? 0 : 1);
}

timer = setTimeout(() => finish(false), 30000);
ws.addEventListener('open', () => {
  ws.send(JSON.stringify({
    type: 'text-message',
    data: { content: sentText },
    session_id: sid,
    timestamp: new Date().toISOString(),
  }));
});
ws.addEventListener('message', (ev) => {
  let frame;
  try {
    frame = JSON.parse(ev.data);
  } catch (err) {
    return;
  }
  frames.push(frame);
  if (frame.type !== 'message-translated' || !frame.data) {
    return;
  }
  if (!phase1Done && frame.data.target_lang === 'en' && frame.data.original === sentText) {
    phase1Done = true;
    try {
      fs.writeFileSync(signalDir + '/phase1-ok', '1');
    } catch (err) {
      finish(false);
    }
    return;
  }
  if (phase1Done && frame.data.target_lang === 'ja') {
    setTimeout(() => finish(true), 300);
  }
});
ws.addEventListener('error', () => finish(false));
ws.addEventListener('close', () => finish(false));
NODE

echo "🧵 WS 双方向：访客消息 → hub 自动翻译（agent 读向 en）→ 坐席发送 → visitor 读向（ja）"
NODE_WS_PID=""
if node "$WS_CLIENT_JS" "$SIGNAL_DIR" "$SERVIFY_PORT" "$SESSION_ID" \
  > "$EVIDENCE_DIR/ws-frames.jsonl" 2>> "$EVIDENCE_DIR/server-log.txt" &
then
  NODE_WS_PID=$!
fi

# 等 phase1 信号（agent 读向帧已收到，且会话行已随首条消息落库）。
for _ in $(seq 1 25); do
  [ -f "$SIGNAL_DIR/phase1-ok" ] && break
  kill -0 "$NODE_WS_PID" 2>/dev/null || break
  sleep 1
done
if [ -f "$SIGNAL_DIR/phase1-ok" ]; then
  WS_VISITOR_TO_AGENT_OK=true
  append_summary "ws_visitor_to_agent_ok=true"
else
  append_summary "ws_visitor_to_agent_ok=false"
fi
rm -rf "$SIGNAL_DIR"

echo "🧵 坐席 REST 发消息 → visitor 读向翻译（ja）"
request_json "POST" "$SERVIFY_URL/api/omni/sessions/$SESSION_ID/messages" \
  '{"content":"您好，我的订单三天了还没发货"}' "$ADMIN_TOKEN"
save_response "agent-send" "$RESPONSE_BODY"
append_summary "agent_send_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "201" ]; then
  AGENT_SEND_OK=true
  append_summary "agent_send_ok=true"
else
  append_summary "agent_send_ok=false"
fi

if [ -n "$NODE_WS_PID" ] && wait "$NODE_WS_PID"; then
  WS_AGENT_TO_VISITOR_OK=true
  append_summary "ws_agent_to_visitor_ok=true"
else
  append_summary "ws_agent_to_visitor_ok=false"
fi
rm -f "$WS_CLIENT_JS"

# 13) 偏好清除（幂等）：visitor 读向清空后 GET 回空串。
append_summary "step=pref_delete"
request_json "DELETE" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" "" "$GUEST_TOKEN"
save_response "pref-delete" "$RESPONSE_BODY"
request_json "GET" "$SERVIFY_URL/api/v1/translation/preferences/$SESSION_ID" "" "$GUEST_TOKEN"
save_response "pref-visitor-get-after-delete" "$RESPONSE_BODY"
VISITOR_LANG_AFTER=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('target_lang','')" 2>/dev/null || true)
append_summary "pref_delete_status=$RESPONSE_STATUS visitor_lang_after=$VISITOR_LANG_AFTER"
if [ "$RESPONSE_STATUS" = "200" ] && [ -z "$VISITOR_LANG_AFTER" ]; then
  PREF_DELETE_OK=true
  append_summary "pref_delete_ok=true"
else
  append_summary "pref_delete_ok=false"
fi

if [ "$READY_OK" != "true" ] || [ "$UNAUTH_TRANSLATE_REJECTED" != "true" ] || [ "$ADMIN_AUTH_OK" != "true" ] \
  || [ "$API_KEY_CREATED_OK" != "true" ] || [ "$API_KEY_LIST_HIDES_PLAINTEXT_OK" != "true" ] \
  || [ "$GUEST_TOKEN_ISSUED_OK" != "true" ] || [ "$AGENT_PREF_SET_OK" != "true" ] \
  || [ "$VISITOR_PREF_SET_OK" != "true" ] || [ "$VISITOR_PREF_MISMATCH_REJECTED" != "true" ] \
  || [ "$PREF_ROUNDTRIP_OK" != "true" ] || [ "$TRANSLATE_ENDPOINT_OK" != "true" ] \
  || [ "$TRANSLATE_DETERMINISTIC_OK" != "true" ] || [ "$TRANSLATE_INVALID_LANG_REJECTED" != "true" ] \
  || [ "$WS_VISITOR_TO_AGENT_OK" != "true" ] || [ "$AGENT_SEND_OK" != "true" ] \
  || [ "$WS_AGENT_TO_VISITOR_OK" != "true" ] || [ "$PREF_DELETE_OK" != "true" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ Translation acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Translation acceptance 通过: 401 负例 → admin/API key/guest token 三级凭据 → 双读向偏好（含 403 绑定负例）→ 直译端点确定性双跑 → 访客 WS→agent 读向帧 → 坐席 REST→visitor 读向帧 → 偏好清除 全部真实留档"
