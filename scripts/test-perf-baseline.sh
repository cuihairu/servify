#!/usr/bin/env bash
# P2-8 性能、容量与压测基线：真实起服（sqlite）+ 内嵌 mock LLM，跑 perfbench
# 四场景（ticket/conversation 高并发读写、AI 查询延迟、文件上传与知识同步、
# WebSocket 连接数），输出 JSON 基线数据并写入 acceptance manifest。
#
# 规模档位（PERF_SCALE）：smoke（默认，CI 驱动测试秒级）/ full（容量基线）。
# 用法：
#   PERF_SCALE=full make perf-baseline
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_ROOT"

SERVIFY_PORT="${SERVIFY_PORT:-18107}"
MOCK_PORT="${MOCK_PORT:-18108}"
SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
MOCK_URL="http://127.0.0.1:${MOCK_PORT}"
PERF_SCALE="${PERF_SCALE:-smoke}"
ADMIN_USERNAME="${ADMIN_USERNAME:-perf-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-perf-admin-123}"
WORK_DIR="$(mktemp -d /tmp/servify-perf-XXXXXX)"
DB_DSN="$WORK_DIR/perf.db"
EVIDENCE_DIR="${EVIDENCE_DIR:-$PROJECT_ROOT/scripts/test-results/perf-baseline}"

SERVER_PID=""
MOCK_PID=""

cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "$MOCK_PID" ] && kill "$MOCK_PID" 2>/dev/null || true
}
trap cleanup EXIT

step() { printf '\n### %s\n' "$*"; }
append_summary() { printf '%s\n' "$*" >> "$EVIDENCE_DIR/summary.txt"; }

# request_capture NAME METHOD URL [JSON_BODY] [AUTH_HEADER] —— 头+体都留证。
request_capture() {
  local name=$1 method=$2 url=$3 body=${4:-} auth=${5:-}
  local args=(-sS -o "$EVIDENCE_DIR/$name.body" -D "$EVIDENCE_DIR/$name.headers" -X "$method")
  [ -n "$body" ] && args+=(-H "Content-Type: application/json" -d "$body")
  [ -n "$auth" ] && args+=(-H "$auth")
  curl "${args[@]}" "$url" || return 1
  { cat "$EVIDENCE_DIR/$name.headers"; echo "--- body ---"; cat "$EVIDENCE_DIR/$name.body"; } > "$EVIDENCE_DIR/$name.txt"
}

# json_field FILE KEY —— python 提取响应体字段（体在 "--- body ---" 标记之后）；
# KEY 用点分路径，列表下标用数字（如 roles.0）。
json_field() {
  python3 - "$1" "$2" <<'PY'
import json, sys
raw = open(sys.argv[1], encoding="utf-8").read()
marker = "--- body ---"
idx = raw.find(marker)
if idx >= 0:
    raw = raw[idx + len(marker):].strip()
value = json.loads(raw)
for part in sys.argv[2].split("."):
    if isinstance(value, list):
        value = value[int(part)]
    elif isinstance(value, dict):
        value = value[part]
    else:
        raise SystemExit(f"cannot descend into {part!r}")
print(value)
PY
}

wait_for() {
  local url=$1 name=$2 tries=${3:-60}
  for _ in $(seq 1 "$tries"); do
    if curl -fsS "$url" > /dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "❌ $name 未就绪：$url" >&2
  return 1
}

mkdir -p "$EVIDENCE_DIR"
: > "$EVIDENCE_DIR/summary.txt"

step "预检"
for port in "$SERVIFY_PORT" "$MOCK_PORT"; do
  if curl -sS --max-time 2 "http://127.0.0.1:$port" > /dev/null 2>&1; then
    echo "❌ 端口 $port 已被占用" >&2
    exit 1
  fi
done
append_summary "perf_scale=$PERF_SCALE"
append_summary "servify_port=$SERVIFY_PORT"
append_summary "mock_port=$MOCK_PORT"
echo "✅ 端口空闲：servify=$SERVIFY_PORT mock=$MOCK_PORT"

step "构建"
if make build > "$EVIDENCE_DIR/build-output.txt" 2>&1; then
  append_summary "build_ok=true"
  echo "✅ build 完成"
else
  append_summary "build_ok=false"
  tail -20 "$EVIDENCE_DIR/build-output.txt" >&2
  exit 1
fi

step "mock LLM（OpenAI 兼容 chat/completions）"
python3 - "$MOCK_PORT" > "$EVIDENCE_DIR/mock-log.txt" 2>&1 <<'PY' &
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

port = int(sys.argv[1])

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'{"data":[]}')

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        self.rfile.read(length)
        body = json.dumps({
            "choices": [{
                "finish_reason": "stop",
                "message": {"role": "assistant", "content": "perf mock answer"},
            }],
            "usage": {"prompt_tokens": 1, "completion_tokens": 3, "total_tokens": 4},
        }).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        pass

ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY
MOCK_PID=$!
wait_for "$MOCK_URL/v1/chat/completions" "mock LLM" 20 > /dev/null 2>&1 || {
  # GET 探测 405 也算就绪：POST-only 路由对 GET 回 404/405 均说明端口已监听。
  if ! curl -sS --max-time 2 -o /dev/null "$MOCK_URL"; then
    echo "❌ mock LLM 未就绪" >&2
    exit 1
  fi
}
append_summary "mock_llm_ok=true"
echo "✅ mock LLM 就绪（$MOCK_URL）"

step "生成压测配置（放宽限流，AI 指向 mock，knowledge 置空）"
go run ./scripts/perfbench \
  -gen-config-src config.yml \
  -gen-config-dst "$WORK_DIR/config.yml" \
  -mock-url "$MOCK_URL"
echo "✅ 配置生成：$WORK_DIR/config.yml"

step "起服（sqlite，端口 $SERVIFY_PORT）"
bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
  _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
  > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
SERVER_PID=$!
wait_for "$SERVIFY_URL/ready" "servify" 60
append_summary "server_ready=true"
echo "✅ 服务就绪（$SERVIFY_URL）"

step "未认证负例"
if request_capture unauthenticated-tickets GET "$SERVIFY_URL/api/tickets" && grep -q "HTTP/1.1 401" "$EVIDENCE_DIR/unauthenticated-tickets.headers"; then
  append_summary "unauthenticated_tickets_rejected_401=true"
  echo "✅ 未认证 401"
else
  append_summary "unauthenticated_tickets_rejected_401=false"
  echo "❌ 未认证请求未被拒绝" >&2
  exit 1
fi

step "注册首个用户（自动 admin）并登录"
request_capture register-admin POST "$SERVIFY_URL/api/v1/auth/register" \
  '{"username":"'"$ADMIN_USERNAME"'","email":"'"$ADMIN_USERNAME"'@servify.io","password":"'"$ADMIN_PASSWORD"'","name":"Perf Admin","role":"admin"}'
if ! grep -q "HTTP/1.1 200" "$EVIDENCE_DIR/register-admin.headers" && ! grep -q "HTTP/1.1 201" "$EVIDENCE_DIR/register-admin.headers"; then
  append_summary "admin_ready=false"
  echo "❌ admin 注册失败" >&2
  exit 1
fi
request_capture login-admin POST "$SERVIFY_URL/api/v1/auth/login" \
  '{"username":"'"$ADMIN_USERNAME"'","password":"'"$ADMIN_PASSWORD"'"}'
TOKEN="$(json_field "$EVIDENCE_DIR/login-admin.txt" token)"
AUTH="Authorization: Bearer $TOKEN"
ROLE="$(json_field "$EVIDENCE_DIR/login-admin.txt" user.role)"
append_summary "admin_ready=true"
append_summary "login_role=$ROLE"
echo "✅ admin 登录（role=$ROLE）"

step "perfbench 四场景（scale=$PERF_SCALE）"
RESULTS_JSON="$EVIDENCE_DIR/results.json"
if go run ./scripts/perfbench -base "$SERVIFY_URL" -token "$TOKEN" -scale "$PERF_SCALE" -out "$RESULTS_JSON" \
  2>&1 | tee "$EVIDENCE_DIR/perfbench-output.txt"; then
  append_summary "perfbench_ok=true"
else
  append_summary "perfbench_ok=false"
  echo "❌ perfbench 失败" >&2
  exit 1
fi

step "校验场景结果"
python3 - "$RESULTS_JSON" "$EVIDENCE_DIR/summary.txt" "$PERF_SCALE" <<'PY'
import json
import sys

results_path, summary_path, scale = sys.argv[1], sys.argv[2], sys.argv[3]
with open(results_path, encoding="utf-8") as fh:
    results = {item["scenario"]: item for item in json.load(fh)}

expected = ["tickets-mixed", "ai-query", "upload-knowledge", "ws-connections"]
missing = [name for name in expected if name not in results]
if missing:
    raise SystemExit(f"missing scenarios: {missing}")

lines = []
failed = False
for name in expected:
    r = results[name]
    lines.append(
        f"scenario={name} ops={r['ops']} concurrency={r['concurrency']} errors={r['errors']} "
        f"p50_ms={r['p50_ms']:.2f} p95_ms={r['p95_ms']:.2f} p99_ms={r['p99_ms']:.2f} "
        f"max_ms={r['max_ms']:.2f} ops_per_sec={r['ops_per_sec']:.2f}"
    )
    if r["errors"] > 0:
        failed = True

ws = results["ws-connections"]
lines.append(f"ws_max_conns={ws.get('ws_max_conns', 0)} ws_target={ws.get('ws_target', 0)} "
             f"ws_server_reported={ws.get('ws_server_reported', 0)} ws_roundtrip_ms={ws.get('ws_roundtrip_ms', 0):.2f}")
if ws.get("ws_max_conns", 0) != ws.get("ws_target", 0):
    failed = True
    lines.append("ws_max_conns_mismatch=true")
if ws.get("ws_server_reported", -1) != ws.get("ws_max_conns", -2):
    failed = True
    lines.append("ws_stats_reconciliation_mismatch=true")

for line in lines:
    print(line)
    summary_path and open(summary_path, "a", encoding="utf-8").write(line + "\n")
open(summary_path, "a", encoding="utf-8").write(f"scale={scale}\noverall_status={'failed' if failed else 'passed'}\n")
if failed:
    raise SystemExit("scenario checks failed")
PY
append_summary "step=scenario_checks"
echo "✅ 四场景通过"

step "write manifest"
python3 - "$EVIDENCE_DIR" "$PERF_SCALE" <<'PY'
import json
import os
import sys

evidence_dir, scale = sys.argv[1], sys.argv[2]
summary = open(os.path.join(evidence_dir, "summary.txt"), encoding="utf-8").read()

def check(name):
    return "true" if f"{name}=true" in summary else "false"

files = sorted(
    name for name in os.listdir(evidence_dir)
    if os.path.isfile(os.path.join(evidence_dir, name)) and name != "manifest.json"
)
manifest = {
    "provider": "perf",
    "mode": "runtime-baseline",
    "scale": scale,
    "generated_at": __import__("datetime").datetime.now().isoformat(),
    "status": {"overall": "passed" if "overall_status=passed" in summary else "failed"},
    "checks": {
        "build_ok": check("build_ok"),
        "server_ready": check("server_ready"),
        "mock_llm_ok": check("mock_llm_ok"),
        "unauthenticated_tickets_rejected_401": check("unauthenticated_tickets_rejected_401"),
        "admin_ready": check("admin_ready"),
        "perfbench_ok": check("perfbench_ok"),
        "tickets_scenario_passed": "true" if "scenario=tickets-mixed" in summary else "false",
        "ai_query_scenario_passed": "true" if "scenario=ai-query" in summary else "false",
        "upload_knowledge_scenario_passed": "true" if "scenario=upload-knowledge" in summary else "false",
        "ws_connections_passed": "true" if "ws_max_conns_mismatch" not in summary else "false",
        "ws_stats_reconciled": "true" if "ws_stats_reconciliation_mismatch" not in summary else "false",
    },
    "evidence_files": files,
}
with open(os.path.join(evidence_dir, "manifest.json"), "w", encoding="utf-8") as fh:
    json.dump(manifest, fh, ensure_ascii=False, indent=2)
print("manifest checks:", json.dumps(manifest["checks"], ensure_ascii=False))
PY

append_summary "step=manifest"
append_summary "overall_status=passed"
echo ""
echo "✅ perf 基线完成（scale=$PERF_SCALE）：$EVIDENCE_DIR"
