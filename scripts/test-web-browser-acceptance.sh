#!/bin/bash

# Web 真实浏览器验收（todo #205/#208 项）。
#
# 覆盖（真实 Chromium 驱动 + 真实服务 + 真实 demo-sdk 挂件）：
#   嵌入(embed)     —— host 页挂载 ServifyWidget，sw-trigger 渲染
#   鉴权(auth)      —— 访客会话 WS 建立（已连接）+ admin 登录 UI 成功
#   断线重连        —— context.setOffline 断网 → 未连接 → 恢复 → 已连接
#   来源展示        —— local 知识库命中 → ai-response 带 sources → sw-source-row
#   人工接管        —— /api/omni/sessions/:id/assign 接管 + 坐席回复达访客挂件
#   客户            —— /api/customers 建客户（工单创建 UI 的客户下拉源）
#   转派            —— /api/omni/sessions/:id/transfer 转派
#   工单            —— /api/v1/tickets 建单（session 关联）
#   两坐席 + 访客   —— agent A/B 两坐席 + 一个访客浏览器会话
# 可重放记录：version（服务版本）+ 网络故障 + 结果 全部落 EVIDENCE_DIR。
#
# 自包含：默认自建自启真实服务（sqlite + knowledge/embedding/ai 三 local 零出站，
# 与 test-local-knowledge-acceptance.sh 同口径）；SERVIFY_URL 提供时复用既有服务
# （外部栈需自备三 local 配置与已种知识库）。
#
# 浏览器工具链（缺失则整体 SKIP，不写证据——CI 无 Chromium 即走该路径，
# 入库证据由 check-acceptance-evidence.sh 单独校验）：
#   PLAYWRIGHT_NODE_PATH  playwright 模块所在 node_modules 父目录（必须）
#   CHROME_BIN            可执行 Chromium 路径（必须）
#   二者任一缺失 → SKIPPED（exit 0，不触碰 EVIDENCE_DIR）。
#
# 可选环境变量:
#   SERVIFY_URL            服务地址(默认自起 http://127.0.0.1:18123)
#   SERVIFY_PORT           自起端口(默认 18123)
#   HOST_PORT              挂件 host 页静态服务端口(默认 18124)
#   ADMIN_USERNAME/ADMIN_PASSWORD  管理员凭证(默认 web-browser-admin/<随机>)
#   WEB_BROWSER_ACCEPTANCE_MODE    证据模式标记(默认 real)
#
# 证据输出: scripts/test-results/web-browser/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Web 真实浏览器 acceptance 测试开始..."

WEB_BROWSER_ACCEPTANCE_MODE=${WEB_BROWSER_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/web-browser"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"web-browser-admin-${RANDOM}"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"WebBrowser!12345"}

# ---- 工具链预检：缺 Chromium/playwright 则整体 SKIP（不写证据，CI 走此路径） ----
if [ -z "${CHROME_BIN:-}" ] || [ -z "${PLAYWRIGHT_NODE_PATH:-}" ]; then
  echo "⚠️  浏览器工具链不完整（需 CHROME_BIN 与 PLAYWRIGHT_NODE_PATH）→ SKIP，不写证据"
  echo "    CHROME_BIN=${CHROME_BIN:-<unset>} PLAYWRIGHT_NODE_PATH=${PLAYWRIGHT_NODE_PATH:-<unset>}"
  echo "    本地跑法示例: CHROME_BIN=~/.../chrome PLAYWRIGHT_NODE_PATH=/path/to/node_modules bash scripts/test-web-browser-acceptance.sh"
  exit 0
fi
if ! command -v node > /dev/null 2>&1; then
  echo "⚠️  node 不可用 → SKIP"
  exit 0
fi
if [ ! -x "$CHROME_BIN" ]; then
  echo "⚠️  CHROME_BIN 不可执行（$CHROME_BIN）→ SKIP"
  exit 0
fi
if ! NODE_PATH="$PLAYWRIGHT_NODE_PATH" node -e "require('playwright')" > /dev/null 2>&1; then
  echo "⚠️  playwright 模块不可解析（PLAYWRIGHT_NODE_PATH=$PLAYWRIGHT_NODE_PATH）→ SKIP"
  exit 0
fi

mkdir -p "$EVIDENCE_DIR"
: > "$EVIDENCE_DIR/summary.txt"

BUILD_OK=false
BROWSER_OK=false
ADMIN_AUTH_OK=false
AGENT_CREATED_OK=false
CUSTOMER_CREATED_OK=false
KB_SEEDED_OK=false
ASSIGN_OK=false
AGENT_REPLY_OK=false
TRANSFER_OK=false
TICKET_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
HOST_PID=""
ADMIN_PID=""
DB_DSN=""
WORK_DIR=""
ADMIN_TOKEN=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$HOST_PID" ] && kill -0 "$HOST_PID" 2>/dev/null; then
    kill "$HOST_PID" 2>/dev/null || true
    wait "$HOST_PID" 2>/dev/null || true
  fi
  if [ -n "$ADMIN_PID" ] && kill -0 "$ADMIN_PID" 2>/dev/null; then
    kill "$ADMIN_PID" 2>/dev/null || true
    wait "$ADMIN_PID" 2>/dev/null || true
  fi
  if [ -n "$DB_DSN" ] && [ -f "$DB_DSN" ]; then
    rm -f "$DB_DSN" || true
  fi
  if [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR" || true
  fi
}

write_manifest() {
  MANIFEST_MODE="${WEB_BROWSER_ACCEPTANCE_MODE:-real}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_BROWSER_OK="${BROWSER_OK:-false}" \
  MANIFEST_ADMIN_AUTH_OK="${ADMIN_AUTH_OK:-false}" \
  MANIFEST_AGENT_CREATED_OK="${AGENT_CREATED_OK:-false}" \
  MANIFEST_CUSTOMER_CREATED_OK="${CUSTOMER_CREATED_OK:-false}" \
  MANIFEST_KB_SEEDED_OK="${KB_SEEDED_OK:-false}" \
  MANIFEST_ASSIGN_OK="${ASSIGN_OK:-false}" \
  MANIFEST_AGENT_REPLY_OK="${AGENT_REPLY_OK:-false}" \
  MANIFEST_TRANSFER_OK="${TRANSFER_OK:-false}" \
  MANIFEST_TICKET_OK="${TICKET_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "web-browser",
    "mode": os.environ.get("MANIFEST_MODE", "real"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "browser_ok": os.environ.get("MANIFEST_BROWSER_OK", "false"),
        "admin_auth_ok": os.environ.get("MANIFEST_ADMIN_AUTH_OK", "false"),
        "agent_created_ok": os.environ.get("MANIFEST_AGENT_CREATED_OK", "false"),
        "customer_created_ok": os.environ.get("MANIFEST_CUSTOMER_CREATED_OK", "false"),
        "kb_seeded_ok": os.environ.get("MANIFEST_KB_SEEDED_OK", "false"),
        "assign_ok": os.environ.get("MANIFEST_ASSIGN_OK", "false"),
        "agent_reply_ok": os.environ.get("MANIFEST_AGENT_REPLY_OK", "false"),
        "transfer_ok": os.environ.get("MANIFEST_TRANSFER_OK", "false"),
        "ticket_ok": os.environ.get("MANIFEST_TICKET_OK", "false"),
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

  local response_file
  response_file=$(mktemp)
  local args=(-sS -X "$method" -o "$response_file" -w '%{http_code}' "$url")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")
  [ -n "$auth_token" ] && args+=(-H "Authorization: Bearer $auth_token")
  RESPONSE_STATUS=$(curl "${args[@]}" 2>/dev/null | tr -d '\n' || true)
  RESPONSE_BODY=$(cat "$response_file")
  rm -f "$response_file"
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
  SERVIFY_PORT=${SERVIFY_PORT:-18123}
  SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/web-browser-XXXXXX.sqlite")"
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/web-browser-work-XXXXXX")"
  # 临时 config：knowledge/embedding/ai 三 local（零出站，同 local-knowledge 口径）
  # + upload local + 关限流/放开 CORS（浏览器 host 页跨源打服务端）。
  python3 - "$PROJECT_ROOT/config.yml" "$WORK_DIR/config.yml" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["enabled"] = False
cfg["security"]["cors"]["allowed_origins"] = ["*"]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = "./uploads"
cfg["knowledge"]["provider"] = "local"
cfg["knowledge"]["local"] = {
    "search": {"top_k": 5, "threshold": 0.2, "strategy": "semantic"},
    "indexing": {"chunk_size": 500, "chunk_overlap": 50},
}
cfg["embedding"]["provider"] = "local"
cfg["embedding"]["local"] = {"dimension": 256}
cfg["ai"]["provider"] = "local"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
  echo "🚀 启动真实服务 (sqlite + 三 local 零出站, cors *, 限流 off): $SERVIFY_URL"
  bash -c 'cd "$1" && exec env TZ=UTC SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-web-browser-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
fi
append_summary "servify_url=$SERVIFY_URL (sqlite local, three-local ai)"

wait_for "Servify Health" "$SERVIFY_URL/health" 30 2 || exit 1

# ---- 1. 管理员注册（全新 sqlite 实例，可直接注册 admin） ----
echo "🔑 管理员认证..."
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Web Browser Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY" ""
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
assert_status "201" "$RESPONSE_STATUS" "admin_register"
ADMIN_TOKEN=$(json_get "$RESPONSE_BODY" ".token")
[ -n "$ADMIN_TOKEN" ] || { echo "❌ 未拿到 admin token"; exit 1; }
ADMIN_AUTH_OK=true
append_summary "admin_auth_ok=true"

# ---- 2. 两坐席用户 + 坐席实体 + 上线 ----
echo "🧑‍💻 创建两坐席..."
declare -a AGENT_IDS=()
declare -a AGENT_TOKENS=()
declare -a AGENT_USERNAMES=()
for i in 1 2; do
  AN="web-browser-agent-${i}-${RANDOM}"
  AGENT_USERNAMES+=("$AN")
  request_json "POST" "$SERVIFY_URL/api/v1/auth/register" \
    "$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Browser Agent %d","role":"agent"}' \
      "$AN" "$AN" "$ADMIN_PASSWORD" "$i")" ""
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 坐席 $i 注册失败(HTTP $RESPONSE_STATUS)"; exit 1
  fi
  AUID=$(json_get "$RESPONSE_BODY" ".user.id")
  ATOK=$(json_get "$RESPONSE_BODY" ".token")
  AGENT_TOKENS+=("$ATOK")
  request_json "POST" "$SERVIFY_URL/api/agents" \
    "$(printf '{"user_id":%s,"department":"browser","skills":"browser","max_concurrent":5}' "$AUID")" "$ADMIN_TOKEN"
  save_response "agent-create-$i" "$RESPONSE_BODY"
  if [ "$RESPONSE_STATUS" != "201" ]; then
    echo "❌ 坐席 $i 实体创建失败(HTTP $RESPONSE_STATUS)"; exit 1
  fi
  AID=$(json_get "$RESPONSE_BODY" ".id")
  AGENT_IDS+=("$AID")
  # AgentGoOnline 的 path 参数语义是「用户ID」（handler @Param 注明），
  # 故用注册返回的 user id 上线，而非坐席实体 id。
  request_json "POST" "$SERVIFY_URL/api/agents/$AUID/online" '{}' "$ADMIN_TOKEN"
  append_summary "agent_${i}_id=$AID online_status=$RESPONSE_STATUS"
done
AGENT_CREATED_OK=true
append_summary "agent_created_ok=true"

# ---- 2b. 客户（工单创建 UI 需可选客户：工单表 customer_id 为 binding:required） ----
echo "👤 创建客户..."
CUSTOMER_NAME="Browser Customer 1"
CUSTOMER_USERNAME="web-browser-customer-${RANDOM}"
request_json "POST" "$SERVIFY_URL/api/customers" \
  "$(printf '{"username":"%s","email":"%s@servify.io","name":"%s","company":"Browser Corp"}' \
    "$CUSTOMER_USERNAME" "$CUSTOMER_USERNAME" "$CUSTOMER_NAME")" "$ADMIN_TOKEN"
save_response "customer-create" "$RESPONSE_BODY"
CUSTOMER_ID=$(json_get "$RESPONSE_BODY" ".id")
append_summary "customer_create_status=$RESPONSE_STATUS customer_id=$CUSTOMER_ID"
if [ "$RESPONSE_STATUS" != "201" ] || [ -z "$CUSTOMER_ID" ]; then
  echo "❌ 客户创建失败(HTTP $RESPONSE_STATUS)，工单 UI 面无法验收" >&2
  exit 1
fi
CUSTOMER_CREATED_OK=true
append_summary "customer_created_ok=true"

# ---- 3. 种 local 知识库（来源展示前提） ----
echo "📚 种知识库..."
request_json "PUT" "$SERVIFY_URL/api/v1/ai/knowledge-provider/enable" "" "$ADMIN_TOKEN"
KB_ENABLE_STATUS=$RESPONSE_STATUS
KB_DOC='{"title":"打印机故障排查指南","content":"打印机卡纸是最常见的办公设备故障。当打印机卡纸发生时，应先关闭打印机电源，然后打开前盖，沿走纸方向缓慢抽出卡住的纸张。打印机卡纸的预防方法包括：使用符合规格的纸张、不要超出发纸器的容量、定期清理走纸通道的碎屑。如果打印机频繁卡纸，可能是搓纸轮磨损，需要联系售后更换搓纸轮组件。打印机显示缺纸但纸盒有纸，通常是纸位传感器被灰尘遮挡，用干燥的软布清洁传感器即可恢复。打印机打印出现纵向黑条，多为硒鼓表面划伤，更换硒鼓可解决。打印机卡纸的进阶处理：如果纸张在定影器处卡住，切勿直接用手拉扯，应打开后盖并等待定影器冷却，再沿出纸方向取出。","tags":["local-knowledge"]}'
request_json "POST" "$SERVIFY_URL/api/v1/ai/knowledge/upload" "$KB_DOC" "$ADMIN_TOKEN"
save_response "kb-upload" "$RESPONSE_BODY"
KB_UPLOAD_STATUS=$RESPONSE_STATUS
request_json "POST" "$SERVIFY_URL/api/v1/ai/knowledge/sync" "" "$ADMIN_TOKEN"
KB_SYNC_STATUS=$RESPONSE_STATUS
if [ "$KB_ENABLE_STATUS" = "200" ] && [ "$KB_UPLOAD_STATUS" = "200" ] && [ "$KB_SYNC_STATUS" = "200" ]; then
  KB_SEEDED_OK=true
  append_summary "kb_seeded_ok=true"
else
  echo "❌ 知识库种档未全通过 (enable=$KB_ENABLE_STATUS upload=$KB_UPLOAD_STATUS sync=$KB_SYNC_STATUS)"
  exit 1
fi

# ---- 4. host 页 + demo-sdk 静态服务（真实 widget 挂件） ----
HOST_PORT=${HOST_PORT:-18124}
HOST_URL="http://127.0.0.1:${HOST_PORT}"
if curl -fsS "$HOST_URL/" > /dev/null 2>&1; then
  echo "❌ host 端口 $HOST_PORT 已被占用" >&2; exit 1
fi
SESSION_ID="web-browser-$(date +%s)-${RANDOM}"
# host 页用 data-session-id 固定会话，bash 与浏览器双端共享同一会话。
cat > "$WORK_DIR/index.html" <<HTML
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Servify Web Browser Acceptance Host</title>
</head>
<body>
<h1>Servify Web Browser Acceptance</h1>
<script src="/demo-sdk/servify-sdk.umd.js"></script>
<script src="/demo-sdk/widget.js"
  data-servify-widget
  data-base-url="${SERVIFY_URL}"
  data-session-id="${SESSION_ID}"
  data-primary-color="#667eea"></script>
</body>
</html>
HTML
mkdir -p "$WORK_DIR/www/demo-sdk"
# demo-sdk 静态资源（真实仓库产物：widget.js/umd/css）挂到 /demo-sdk/*
cp -r "$PROJECT_ROOT/apps/demo-sdk/widget.js" \
      "$PROJECT_ROOT/apps/demo-sdk/servify-sdk.umd.js" \
      "$PROJECT_ROOT/apps/demo-sdk/widget.css" \
      "$PROJECT_ROOT/apps/demo-sdk/index.d.ts" \
      "$WORK_DIR/www/demo-sdk/" 2>/dev/null || true
python3 - "$WORK_DIR/index.html" "$WORK_DIR/www/index.html" <<'PY'
import shutil, sys
shutil.copyfile(sys.argv[1], sys.argv[2])
PY
echo "🌐 启动 host 静态服务: $HOST_URL"
( cd "$WORK_DIR/www" && exec python3 -m http.server "$HOST_PORT" --bind 127.0.0.1 ) \
  > "$EVIDENCE_DIR/host-log.txt" 2>&1 &
HOST_PID=$!
wait_for "Host page" "$HOST_URL/index.html" 15 1 || exit 1
append_summary "host_url=$HOST_URL session_id=$SESSION_ID"

# ---- 4b. admin SPA：dist 构建（缺失时）+ 同源代理（静态 dist + 反代 /api） ----
# 服务端不挂载 admin SPA（无 catch-all），故管理端 UI 走同源代理：
# node admin-browser-proxy.mjs <dist> <admin-port> <servify-url>。
# dist 缺失且构建失败 → ADMIN_UI=0，驱动跳过管理端 UI 阶段（核心挂件/API 链路照常）。
ADMIN_DIST="$PROJECT_ROOT/apps/admin/dist"
ADMIN_UI=0
if [ -f "$ADMIN_DIST/index.html" ]; then
  ADMIN_UI=1
else
  echo "⏳ 构建 admin SPA（dist 缺失）..."
  if (cd "$PROJECT_ROOT/apps/admin" && npx -y pnpm@10.22.0 run build > "$EVIDENCE_DIR/admin-build.log" 2>&1) \
     && [ -f "$ADMIN_DIST/index.html" ]; then
    ADMIN_UI=1
    echo "✅ admin SPA 构建完成"
  else
    echo "⚠️  admin SPA 构建失败/不可用 → 管理端 UI 阶段跳过（核心挂件/API 链路照常）"
  fi
fi
append_summary "admin_ui=$ADMIN_UI"
ADMIN_BASE=""
if [ "$ADMIN_UI" = "1" ]; then
  ADMIN_PORT=${ADMIN_PORT:-18135}
  ADMIN_BASE="http://127.0.0.1:${ADMIN_PORT}"
  if curl -fsS "$ADMIN_BASE/" > /dev/null 2>&1; then
    echo "❌ admin 代理端口 $ADMIN_PORT 已被占用" >&2; exit 1
  fi
  node "$SCRIPT_DIR/admin-browser-proxy.mjs" "$ADMIN_DIST" "$ADMIN_PORT" "$SERVIFY_URL" \
    > "$EVIDENCE_DIR/admin-proxy.log" 2>&1 &
  ADMIN_PID=$!
  for i in $(seq 1 15); do curl -fsS "$ADMIN_BASE/" > /dev/null 2>&1 && break; sleep 1; done
  curl -fsS "$ADMIN_BASE/" > /dev/null 2>&1 || { echo "❌ admin 代理未就绪" >&2; exit 1; }
  append_summary "admin_base=$ADMIN_BASE"
fi

# ---- 5. 浏览器驱动（真实 Chromium + playwright） ----
echo "🧭 启动浏览器驱动..."
DRIVER_ENV=(
  "BASE=$SERVIFY_URL"
  "HOST=$HOST_URL"
  "SESSION_ID=$SESSION_ID"
  "ADMIN_USERNAME=$ADMIN_USERNAME"
  "ADMIN_PASSWORD=$ADMIN_PASSWORD"
  "ADMIN_TOKEN=$ADMIN_TOKEN"
  "AGENT_A_ID=${AGENT_IDS[0]}"
  "AGENT_B_ID=${AGENT_IDS[1]}"
  "AGENT_A_TOKEN=${AGENT_TOKENS[0]}"
  "AGENT_B_TOKEN=${AGENT_TOKENS[1]}"
  "AGENT_A_NAME=Browser Agent 1"
  "AGENT_B_NAME=Browser Agent 2"
  "CUSTOMER_ID=$CUSTOMER_ID"
  "CUSTOMER_NAME=$CUSTOMER_NAME"
  "ADMIN_BASE=$ADMIN_BASE"
  "ADMIN_UI=$ADMIN_UI"
  "EVIDENCE_DIR=$EVIDENCE_DIR"
  "CHROME_BIN=$CHROME_BIN"
  "PLAYWRIGHT_NODE_PATH=$PLAYWRIGHT_NODE_PATH"
  "SERVIFY_VERSION=$(curl -s -m 8 "$SERVIFY_URL/health" | python3 -c 'import sys,json;print(json.load(sys.stdin).get("version",""))' 2>/dev/null || true)"
)
if NODE_PATH="$PLAYWRIGHT_NODE_PATH" env "${DRIVER_ENV[@]}" node "$SCRIPT_DIR/web-browser-acceptance-driver.mjs" \
    > "$EVIDENCE_DIR/driver-output.txt" 2>&1; then
  BROWSER_OK=true
  append_summary "browser_ok=true"
  echo "✅ 浏览器驱动通过"
else
  append_summary "browser_ok=false"
  echo "❌ 浏览器驱动失败（详见 driver-output.txt）" >&2
  exit 1
fi

# 驱动写出的会话侧证据回读进 summary/manifest 判定（键名同驱动 rec 名）。
if grep -q '^assign=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null; then
  ASSIGN_OK=true
fi
if grep -q '^agent_reply=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null; then
  AGENT_REPLY_OK=true
fi
if grep -q '^transfer=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null; then
  TRANSFER_OK=true
fi
if grep -q '^ticket=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null; then
  TICKET_OK=true
fi
append_summary "assign_ok=$ASSIGN_OK"
append_summary "agent_reply_ok=$AGENT_REPLY_OK"
append_summary "transfer_ok=$TRANSFER_OK"
append_summary "ticket_ok=$TICKET_OK"

if [ "$BUILD_OK" != "true" ] || [ "$ADMIN_AUTH_OK" != "true" ] || [ "$AGENT_CREATED_OK" != "true" ] \
  || [ "$CUSTOMER_CREATED_OK" != "true" ] || [ "$KB_SEEDED_OK" != "true" ] || [ "$BROWSER_OK" != "true" ] \
  || [ "$ASSIGN_OK" != "true" ] || [ "$AGENT_REPLY_OK" != "true" ] \
  || [ "$TRANSFER_OK" != "true" ] || [ "$TICKET_OK" != "true" ] \
  || ! grep -q '^admin_login=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null \
  || ! grep -q '^admin_workspace=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null \
  || ! grep -q '^transfer_ui=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null \
  || ! grep -q '^ticket_ui=true' "$EVIDENCE_DIR/browser-summary.txt" 2>/dev/null; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ Web 真实浏览器 acceptance 未通过" >&2
  exit 1
fi

echo
echo "🎉 Web 真实浏览器 acceptance 通过"
OVERALL_STATUS=passed
append_summary "overall_status=passed"
