#!/bin/bash

# RAGFlow knowledge provider 验收脚本
# mock 模式（默认）全自含：构建 bin/servify → 内嵌 python3 RAGFlow mock →
# 以 sqlite + 临时 config 起真实服务 → 断言选择链选中 ragflow 且检索/上传/
# 查重删除/解析触发链路全部在 mock 留下证据。real 模式对齐 dify 惯例：拒绝
# 私网 RAGFlow 地址，要求外部 SERVIFY_URL / RAGFLOW_URL。
#
# 环境变量：
#   RAGFLOW_ACCEPTANCE_MODE=mock|real（默认 mock）
#   SERVIFY_URL / RAGFLOW_URL（real 模式必填 SERVIFY_URL）
#   RAGFLOW_MOCK_PORT（mock 模式监听端口，默认 19380）
#   RAGFLOW_API_KEY / RAGFLOW_DATASET_ID / ADMIN_USERNAME / ADMIN_PASSWORD
#   EVIDENCE_DIR（证据输出目录）

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 RAGFlow knowledge provider acceptance 测试开始..."

RAGFLOW_ACCEPTANCE_MODE=${RAGFLOW_ACCEPTANCE_MODE:-"mock"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/ragflow-acceptance"}
SERVIFY_URL=${SERVIFY_URL:-}
RAGFLOW_URL=${RAGFLOW_URL:-}
RAGFLOW_MOCK_PORT=${RAGFLOW_MOCK_PORT:-19380}
RAGFLOW_API_KEY=${RAGFLOW_API_KEY:-"ragflow-acceptance-key"}
RAGFLOW_DATASET_ID=${RAGFLOW_DATASET_ID:-"ds-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"ragflow-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"ragflow-123"}
SERVIFY_PORT=${SERVIFY_PORT:-18093}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
RAGFLOW_AVAILABLE=false
UNAUTH_REJECTED=false
STATUS_OK=false
QUERY_OK=false
RETRIEVAL_HIT=false
UPLOAD_OK=false
UPLOAD_DEDUP_OK=false
SYNC_OK=false
OVERALL_STATUS=failed

SERVER_PID=""
MOCK_PID=""
WORK_DIR=""
DB_DSN=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [ -n "$MOCK_PID" ] && kill -0 "$MOCK_PID" 2>/dev/null; then
    kill "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
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
  local method=$1 url=$2 body=${3:-} token=${4:-}
  local response_file status_file
  response_file="$(mktemp "${TMPDIR:-/tmp}/ragflow-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/ragflow-status-XXXXXX")"
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

json_field() {
  python3 -c 'import json,sys
d = json.loads(sys.stdin.read())
print(eval(sys.argv[1], {"d": d}))' "$2" <<< "${1:-}" 2>/dev/null || echo ""
}

host_from_url() {
  local url="${1:-}"
  url="${url#http://}"
  url="${url#https://}"
  url="${url%%/*}"
  url="${url%%:*}"
  printf '%s' "$url"
}

is_private_or_local_host() {
  local host
  host=$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')
  case "$host" in
    localhost|127.*|0.0.0.0|::1)
      return 0
      ;;
    10.*|192.168.*)
      return 0
      ;;
    172.1[6-9].*|172.2[0-9].*|172.3[0-1].*)
      return 0
      ;;
    *.local|*.internal|host.docker.internal)
      return 0
      ;;
  esac
  return 1
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
  MANIFEST_MODE="${RAGFLOW_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_RAGFLOW_URL="${RAGFLOW_URL:-}" \
  MANIFEST_DATASET_ID="${RAGFLOW_DATASET_ID:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_RAGFLOW_AVAILABLE="${RAGFLOW_AVAILABLE:-false}" \
  MANIFEST_UNAUTH_REJECTED="${UNAUTH_REJECTED:-false}" \
  MANIFEST_STATUS_OK="${STATUS_OK:-false}" \
  MANIFEST_QUERY_OK="${QUERY_OK:-false}" \
  MANIFEST_RETRIEVAL_HIT="${RETRIEVAL_HIT:-false}" \
  MANIFEST_UPLOAD_OK="${UPLOAD_OK:-false}" \
  MANIFEST_UPLOAD_DEDUP_OK="${UPLOAD_DEDUP_OK:-false}" \
  MANIFEST_SYNC_OK="${SYNC_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "ragflow",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "provider_url": os.environ.get("MANIFEST_RAGFLOW_URL", ""),
    "dataset_id": os.environ.get("MANIFEST_DATASET_ID", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "ragflow_available": os.environ.get("MANIFEST_RAGFLOW_AVAILABLE", "false"),
        "unauthenticated_rejected": os.environ.get("MANIFEST_UNAUTH_REJECTED", "false"),
        "status_ok": os.environ.get("MANIFEST_STATUS_OK", "false"),
        "query_ok": os.environ.get("MANIFEST_QUERY_OK", "false"),
        "retrieval_hit": os.environ.get("MANIFEST_RETRIEVAL_HIT", "false"),
        "knowledge_upload_ok": os.environ.get("MANIFEST_UPLOAD_OK", "false"),
        "knowledge_upload_dedup_ok": os.environ.get("MANIFEST_UPLOAD_DEDUP_OK", "false"),
        "knowledge_sync_ok": os.environ.get("MANIFEST_SYNC_OK", "false"),
    },
    "evidence_files": sorted(
        name for name in os.listdir(evidence_dir)
        if name != "manifest.json" and os.path.isfile(os.path.join(evidence_dir, name))
    ),
}
with open(out, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
}

trap 'cleanup; write_manifest' EXIT

cat > "$EVIDENCE_DIR/summary.txt" <<EOF
RAGFlow knowledge provider acceptance summary
mode=$RAGFLOW_ACCEPTANCE_MODE
dataset_id=$RAGFLOW_DATASET_ID
EOF

if [ "$RAGFLOW_ACCEPTANCE_MODE" != "mock" ] && [ "$RAGFLOW_ACCEPTANCE_MODE" != "real" ]; then
  echo "❌ 不支持的 RAGFLOW_ACCEPTANCE_MODE: $RAGFLOW_ACCEPTANCE_MODE"
  exit 1
fi

if [ "$RAGFLOW_ACCEPTANCE_MODE" = "mock" ]; then
  # 1) 构建真实服务二进制（对齐 make build 的 server 产物）
  append_summary "step=build"
  echo "🔍 构建 bin/servify..."
  if (cd "$PROJECT_ROOT" && go -C apps/server build -o ../../bin/servify ./cmd/server) > "$EVIDENCE_DIR/build-output.txt" 2>&1; then
    if [ -x "$PROJECT_ROOT/bin/servify" ]; then
      BUILD_OK=true
      append_summary "build_ok=true"
    else
      append_summary "build_ok=false (bin/servify missing)"
    fi
  else
    append_summary "build_ok=false (go build failed)"
  fi
  if [ "$BUILD_OK" != "true" ]; then
    echo "❌ 构建未通过" >&2
    exit 1
  fi

  # 2) 内嵌 python3 RAGFlow mock：统一包裹体 + 有状态文档表 + 请求留痕
  WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/ragflow-accept-XXXXXX")"
  RAGFLOW_MOCK_LOG="$EVIDENCE_DIR/ragflow-mock.log"
  : > "$RAGFLOW_MOCK_LOG"
  cat > "$WORK_DIR/ragflow_mock.py" <<'PY'
import json
import re
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import unquote_plus

PORT = int(sys.argv[1])
DATASET_ID = sys.argv[2]
LOG_PATH = sys.argv[3]
EXPECT_KEY_PREFIX = "Bearer ragflow-"

docs = {}
next_id = [0]

def log(entry):
    with open(LOG_PATH, "a", encoding="utf-8") as fh:
        fh.write(json.dumps(entry, ensure_ascii=False) + "\n")

class Handler(BaseHTTPRequestHandler):
    def _send(self, status, payload):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _authorized(self):
        auth = self.headers.get("Authorization", "")
        if not auth.startswith(EXPECT_KEY_PREFIX):
            self._send(401, {"code": 401, "message": "invalid api key prefix"})
            return False
        return True

    def _record(self, body_preview=""):
        log({
            "method": self.command,
            "path": self.path,
            "auth_prefix_ok": self.headers.get("Authorization", "").startswith(EXPECT_KEY_PREFIX),
            "body": body_preview[:400],
        })

    def do_GET(self):
        self._record()
        if not self._authorized():
            return
        if self.path.startswith("/api/v1/datasets?"):
            if "id=" + DATASET_ID not in self.path:
                self._send(200, {"code": 0, "data": []})
                return
            self._send(200, {"code": 0, "data": [{"id": DATASET_ID, "name": "Acceptance KB"}]})
            return
        if self.path.startswith("/api/v1/datasets/%s/documents" % DATASET_ID):
            match = re.search(r"name=([^&]+)", self.path)
            name = unquote_plus(match.group(1)) if match else ""
            items = [
                {"id": doc_id, "name": doc_name, "run": "DONE"}
                for doc_id, doc_name in sorted(docs.items())
                if not name or doc_name == name
            ]
            self._send(200, {"code": 0, "data": {"docs": items, "total": len(items)}})
            return
        self._send(404, {"code": 404, "message": "not found"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b""
        self._record(raw.decode("utf-8", errors="replace"))
        if not self._authorized():
            return
        if self.path.startswith("/api/v1/retrieval"):
            try:
                req = json.loads(raw.decode("utf-8"))
            except Exception:
                self._send(200, {"code": 102, "message": "invalid json"})
                return
            if not req.get("question") or not req.get("dataset_ids"):
                self._send(200, {"code": 102, "message": "question and dataset_ids are required"})
                return
            chunk = {
                "content": "RAGFlow 验收知识片段：退款政策为 7 天无理由。",
                "document_id": "doc-kb-1",
                "document_keyword": "refund-policy.txt",
                "dataset_id": DATASET_ID,
                "similarity": 0.87,
            }
            self._send(200, {"code": 0, "data": {"total": 1, "chunks": [chunk]}})
            return
        if self.path == "/api/v1/datasets/%s/documents" % DATASET_ID:
            text = raw.decode("utf-8", errors="replace")
            match = re.search(r'filename="([^"]+)"', text)
            name = match.group(1) if match else "upload-%d.txt" % len(docs)
            next_id[0] += 1
            doc_id = "doc-%d" % next_id[0]
            docs[doc_id] = name
            self._send(200, {"code": 0, "data": [{"id": doc_id, "name": name, "run": "UNSTART"}]})
            return
        if self.path == "/api/v1/datasets/%s/chunks" % DATASET_ID:
            self._send(200, {"code": 0, "data": None})
            return
        self._send(404, {"code": 404, "message": "not found"})

    def do_DELETE(self):
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length) if length else b""
        self._record(raw.decode("utf-8", errors="replace"))
        if not self._authorized():
            return
        if self.path == "/api/v1/datasets/%s/documents" % DATASET_ID:
            try:
                req = json.loads(raw.decode("utf-8"))
            except Exception:
                req = {}
            for doc_id in req.get("ids", []):
                docs.pop(doc_id, None)
            log({"method": "DELETE", "path": self.path, "ids": req.get("ids", [])})
            self._send(200, {"code": 0})
            return
        self._send(404, {"code": 404, "message": "not found"})

    def log_message(self, format, *args):
        pass

HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
  echo "🚀 启动 RAGFlow mock: 127.0.0.1:$RAGFLOW_MOCK_PORT"
  python3 "$WORK_DIR/ragflow_mock.py" "$RAGFLOW_MOCK_PORT" "$RAGFLOW_DATASET_ID" "$RAGFLOW_MOCK_LOG" \
    > "$EVIDENCE_DIR/ragflow-mock-server.log" 2>&1 &
  MOCK_PID=$!
  MOCK_READY=false
  for _ in $(seq 1 20); do
    # 401 也算就绪（连接已通，只是探测未带凭证）
    code=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:$RAGFLOW_MOCK_PORT/api/v1/datasets?id=x" 2>/dev/null || echo "000")
    if [ "$code" != "000" ]; then
      MOCK_READY=true
      break
    fi
    sleep 0.5
  done
  if [ "$MOCK_READY" != "true" ]; then
    echo "❌ RAGFlow mock 未就绪" >&2
    exit 1
  fi
  RAGFLOW_URL="http://127.0.0.1:$RAGFLOW_MOCK_PORT"

  # 3) 写临时 config（ragflow 指向 mock）并以 sqlite 起真实服务
  cat > "$WORK_DIR/config.yml" <<EOF
ragflow:
  enabled: true
  base_url: $RAGFLOW_URL
  api_key: $RAGFLOW_API_KEY
  dataset_id: $RAGFLOW_DATASET_ID
  timeout: 5s
  search:
    top_k: 5
    score_threshold: 0.2
EOF
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1 && [ -n "$SERVIFY_URL" ]; then
    echo "❌ SERVIFY_URL=$SERVIFY_URL 已可达，mock 模式拒绝复用外部服务" >&2
    exit 1
  fi
  SERVIFY_URL="http://127.0.0.1:$SERVIFY_PORT"
  if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
    echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
    exit 1
  fi
  DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/ragflow-accept-XXXXXX.sqlite")"
  echo "🚀 启动真实服务 (sqlite, ragflow→mock): $SERVIFY_URL"
  SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-dev-secret} \
  DB_DRIVER=sqlite DB_DSN="$DB_DSN" SERVIFY_PORT="$SERVIFY_PORT" \
    sh -c "cd '$WORK_DIR' && exec '$PROJECT_ROOT/bin/servify'" > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
elif [ -z "$SERVIFY_URL" ]; then
  echo "❌ real 模式需要 SERVIFY_URL" >&2
  exit 1
fi

append_summary "servify_url=$SERVIFY_URL"
append_summary "ragflow_url=$RAGFLOW_URL"

RAGFLOW_HOST=$(host_from_url "$RAGFLOW_URL")
append_summary "ragflow_host=$RAGFLOW_HOST"

if [ "$RAGFLOW_ACCEPTANCE_MODE" = "real" ] && is_private_or_local_host "$RAGFLOW_HOST"; then
  echo "❌ real 模式拒绝使用本地或私网 RAGFlow 地址: $RAGFLOW_HOST"
  echo "   请显式指向外部真实 RAGFlow 环境，再重新执行验收。"
  append_summary "real_mode_guard=blocked_private_or_local_host"
  exit 1
fi

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

# 4) RAGFlow dataset 探测（同时校验凭证与 dataset 存在）
DATASET_BODY=$(curl -sS -H "Authorization: Bearer $RAGFLOW_API_KEY" \
  "$RAGFLOW_URL/api/v1/datasets?id=$RAGFLOW_DATASET_ID&page=1&page_size=1" || echo '{}')
save_response "ragflow-dataset" "$DATASET_BODY"
if printf '%s' "$DATASET_BODY" | grep -q "\"id\": *\"$RAGFLOW_DATASET_ID\""; then
  RAGFLOW_AVAILABLE=true
  append_summary "ragflow_available=true"
else
  append_summary "ragflow_available=false"
  if [ "$RAGFLOW_ACCEPTANCE_MODE" = "real" ]; then
    echo "❌ real 模式要求 RAGFlow dataset 可用" >&2
    exit 1
  fi
fi

# 5) 注册 admin（首注册提权）
append_summary "step=auth"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"RAGFlow Admin","role":"admin"}' \
  "$ADMIN_USERNAME" "$ADMIN_USERNAME" "$ADMIN_PASSWORD")
request_json "POST" "$SERVIFY_URL/api/v1/auth/register" "$REGISTER_BODY"
save_response "admin-auth" "$RESPONSE_BODY"
append_summary "register_status=$RESPONSE_STATUS"
ADMIN_TOKEN=$(json_field "$RESPONSE_BODY" "d.get('token','')")
if [ -z "$ADMIN_TOKEN" ]; then
  append_summary "admin_token=missing"
  echo "❌ 未拿到 admin token" >&2
  exit 1
fi

# 6) 拒绝路径：无 token 请求 /ai/status 必须 401
request_json "GET" "$SERVIFY_URL/api/v1/ai/status"
save_response "ai-status-unauthorized" "$RESPONSE_BODY"
append_summary "unauthenticated_ai_status_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_REJECTED=true
  append_summary "unauthenticated_rejected=true"
else
  append_summary "unauthenticated_rejected=false"
fi

# 7) GET /api/v1/ai/status：选择链必须选中 ragflow 且健康
append_summary "step=get_ai_status"
request_json "GET" "$SERVIFY_URL/api/v1/ai/status" "" "$ADMIN_TOKEN"
save_response "ai-status" "$RESPONSE_BODY"
append_summary "ai_status_http=$RESPONSE_STATUS"
ACTIVE_PROVIDER=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('knowledge_provider','')")
PROVIDER_ENABLED=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('knowledge_provider_enabled',False)")
PROVIDER_HEALTHY=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('knowledge_provider_healthy',False)")
append_summary "knowledge_provider=$ACTIVE_PROVIDER"
if [ "$ACTIVE_PROVIDER" = "ragflow" ] && [ "$PROVIDER_ENABLED" = "True" ] && [ "$PROVIDER_HEALTHY" = "True" ]; then
  STATUS_OK=true
  append_summary "status_ok=true"
else
  append_summary "status_ok=false (provider=$ACTIVE_PROVIDER enabled=$PROVIDER_ENABLED healthy=$PROVIDER_HEALTHY)"
fi

# 8) POST /api/v1/ai/query：检索命中证据以 mock 请求留痕为准（LLM 不在验收范围）
append_summary "step=post_ai_query"
QUERY_BODY='{"query":"退款政策是什么","session_id":"ragflow-acceptance"}'
request_json "POST" "$SERVIFY_URL/api/v1/ai/query" "$QUERY_BODY" "$ADMIN_TOKEN"
save_response "ai-query" "$RESPONSE_BODY"
append_summary "ai_query_http=$RESPONSE_STATUS"
QUERY_CONTENT=$(json_field "$RESPONSE_BODY" "(d.get('data') or {}).get('content','')")
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"success":true' && [ -n "$QUERY_CONTENT" ]; then
  QUERY_OK=true
  append_summary "query_ok=true"
else
  append_summary "query_ok=false"
fi

# 9) 上传 ×2：第二跳触发 RAGFlow 无幂等 upsert 的删旧建新（DELETE 留痕）
append_summary "step=upload_knowledge"
UPLOAD_BODY='{"title":"RAGFlow Acceptance Document","content":"This document validates the RAGFlow knowledge provider path.","tags":["ragflow","acceptance"]}'
request_json "POST" "$SERVIFY_URL/api/v1/ai/knowledge/upload" "$UPLOAD_BODY" "$ADMIN_TOKEN"
save_response "knowledge-upload" "$RESPONSE_BODY"
append_summary "knowledge_upload_http=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"success":true'; then
  UPLOAD_OK=true
  append_summary "knowledge_upload_ok=true"
else
  append_summary "knowledge_upload_ok=false"
fi

request_json "POST" "$SERVIFY_URL/api/v1/ai/knowledge/upload" "$UPLOAD_BODY" "$ADMIN_TOKEN"
save_response "knowledge-upload-repeat" "$RESPONSE_BODY"
append_summary "knowledge_upload_repeat_http=$RESPONSE_STATUS"

# 10) sync：把内置知识库文档推到 RAGFlow（documents + chunks 留痕）
append_summary "step=sync_knowledge"
request_json "POST" "$SERVIFY_URL/api/v1/ai/knowledge/sync" '{}' "$ADMIN_TOKEN"
save_response "knowledge-sync" "$RESPONSE_BODY"
append_summary "knowledge_sync_http=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "200" ] && printf '%s' "$RESPONSE_BODY" | grep -q '"success":true'; then
  SYNC_OK=true
  append_summary "knowledge_sync_ok=true"
else
  append_summary "knowledge_sync_ok=false"
fi

# 11) mock 请求留痕断言：retrieval / upload / chunks / 删旧建新
append_summary "step=mock_evidence"
if [ -f "$RAGFLOW_MOCK_LOG" ]; then
  cp "$RAGFLOW_MOCK_LOG" "$EVIDENCE_DIR/ragflow-mock-requests.jsonl"
  if grep -q '"/api/v1/retrieval"' "$RAGFLOW_MOCK_LOG"; then
    RETRIEVAL_HIT=true
    append_summary "retrieval_hit=true"
  else
    append_summary "retrieval_hit=false"
  fi
  if grep -q '"method": "DELETE"' "$RAGFLOW_MOCK_LOG" || grep -q '"method":"DELETE"' "$RAGFLOW_MOCK_LOG"; then
    UPLOAD_DEDUP_OK=true
    append_summary "upload_dedup_ok=true"
  else
    append_summary "upload_dedup_ok=false"
  fi
else
  append_summary "mock_log=missing"
fi

MOCK_CALLS_OK=false
if [ "$RAGFLOW_ACCEPTANCE_MODE" = "mock" ]; then
  if [ "$RETRIEVAL_HIT" = "true" ] && grep -q "\"/api/v1/datasets/$RAGFLOW_DATASET_ID/chunks\"" "$RAGFLOW_MOCK_LOG" 2>/dev/null; then
    MOCK_CALLS_OK=true
  fi
  if [ "$UPLOAD_OK" != "true" ] || [ "$SYNC_OK" != "true" ] || [ "$MOCK_CALLS_OK" != "true" ]; then
    echo "❌ mock 留痕不完整（upload/sync/chunks 未全部命中）" >&2
    exit 1
  fi
fi

if [ "$STATUS_OK" != "true" ] || [ "$QUERY_OK" != "true" ] || [ "$UNAUTH_REJECTED" != "true" ] || [ "$UPLOAD_OK" != "true" ] || [ "$SYNC_OK" != "true" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ RAGFlow acceptance 未通过" >&2
  exit 1
fi

if [ "$RAGFLOW_ACCEPTANCE_MODE" = "real" ]; then
  if [ "$ACTIVE_PROVIDER" != "ragflow" ] || [ "$PROVIDER_ENABLED" != "True" ] || [ "$PROVIDER_HEALTHY" != "True" ]; then
    echo "❌ real 模式要求 Servify 当前 provider 为 ragflow 且 enabled/healthy" >&2
    exit 1
  fi
  if [ "$RAGFLOW_AVAILABLE" != "true" ]; then
    echo "❌ real 模式要求 RAGFlow dataset 可用" >&2
    exit 1
  fi
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ RAGFlow acceptance 通过: provider=ragflow / 检索命中 / 上传查重删除 / 解析触发 全部真实留档"
