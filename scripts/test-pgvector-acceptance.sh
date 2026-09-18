#!/usr/bin/env bash
# P2-6 第八刀：pgvector 自建知识库链路真实验收。
#
# 前置（二选一，默认 A）：
#   A) runner-docker 上已有 pgvector 容器：
#      ssh runner-docker 'docker run -d --name servify-pgvector --restart unless-stopped \
#        -e POSTGRES_PASSWORD=servify_pg_pass -e POSTGRES_DB=servify -p 5433:5432 pgvector/pgvector:pg15'
#   B) 任意可达的 pg+pgvector：通过环境变量覆盖 PGVECTOR_HOST/PORT/USER/PASSWORD/DB，
#      并设 PSQL_MODE=local（容器名经 PGVECTOR_CONTAINER 覆盖）。
#
# 脚本自起：OpenAI 兼容 embedding mock（词袋 hash 向量，1536 维确定性，
# 协议链路真实）→ Servify（postgres driver 连 pg）→ 上传/检索/负例/psql 数据证据。

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
EVIDENCE_DIR="${EVIDENCE_DIR:-$PROJECT_ROOT/scripts/test-results/pgvector}"
mkdir -p "$EVIDENCE_DIR"
SUMMARY_FILE="$EVIDENCE_DIR/summary.txt"
: > "$SUMMARY_FILE"

SERVIFY_PORT="${SERVIFY_PORT:-18105}"
EMBEDDING_PORT="${EMBEDDING_PORT:-18106}"
PGVECTOR_HOST="${PGVECTOR_HOST:-192.168.5.5}"
PGVECTOR_PORT="${PGVECTOR_PORT:-5433}"
PGVECTOR_USER="${PGVECTOR_USER:-postgres}"
PGVECTOR_PASSWORD="${PGVECTOR_PASSWORD:-servify_pg_pass}"
PGVECTOR_DB="${PGVECTOR_DB:-servify}"
PSQL_MODE="${PSQL_MODE:-ssh}"
PGVECTOR_SSH_HOST="${PGVECTOR_SSH_HOST:-runner-docker}"
PGVECTOR_CONTAINER="${PGVECTOR_CONTAINER:-servify-pgvector}"

SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
AI_BASE="$SERVIFY_URL/api/v1/ai"

OVERALL_STATUS="failed"
SERVER_PID=""
EMBEDDING_PID=""

cleanup() {
  [ -n "$SERVER_PID" ] && kill "$SERVER_PID" 2>/dev/null || true
  [ -n "$EMBEDDING_PID" ] && kill "$EMBEDDING_PID" 2>/dev/null || true
}
trap cleanup EXIT

append_summary() {
  echo "$1" >> "$SUMMARY_FILE"
}

# psql_exec "SQL" —— 数据证据经 psql 单值查询（-tAc）。
# SQL 外再包一层双引号：ssh 把参数拼接后交给远端 shell，不引则空格截断
# （-tAc 只收到 "UPDATE"，其余成了 psql 的额外参数，调用还悄悄成功）。
psql_exec() {
  local sql="\"$1\""
  if [ "$PSQL_MODE" = "local" ]; then
    docker exec "$PGVECTOR_CONTAINER" psql -U "$PGVECTOR_USER" -d "$PGVECTOR_DB" -tAc "$sql"
  else
    ssh "$PGVECTOR_SSH_HOST" docker exec "$PGVECTOR_CONTAINER" psql -U "$PGVECTOR_USER" -d "$PGVECTOR_DB" -tAc "$sql"
  fi
}

request_capture() {
  local name=$1; shift
  local file="$EVIDENCE_DIR/$name.txt"
  printf '### %s\n' "$*" > "$file"
  local status
  status=$(curl -sS -o "$file.body" -w '%{http_code}' -D "$file.hdr" "$@" 2>/dev/null) || true
  cat "$file.hdr" >> "$file" 2>/dev/null || true
  cat "$file.body" >> "$file" 2>/dev/null || true
  rm -f "$file.hdr" "$file.body"
  RESPONSE_STATUS="$status"
}

# json_field FILE "expr" —— 从"HTTP 头+body"混合留证文件里解析 JSON 字段。
# 注意：按头/体分隔行定位 body（### 命令回显行里也可能出现 `{`）。
json_field() {
  python3 - "$1" "$2" <<'PY'
import json, sys
raw = open(sys.argv[1], encoding="utf-8", errors="replace").read()
body = raw
for sep in ("\r\n\r\n", "\n\n"):
    idx = raw.find(sep)
    if idx != -1:
        body = raw[idx + len(sep):]
        break
d = json.loads(body.strip())
print(eval(sys.argv[2], {"d": d}))
PY
}

write_manifest() {
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_PG_HOST="${PGVECTOR_HOST:-}" \
  MANIFEST_PG_PORT="${PGVECTOR_PORT:-}" \
  MANIFEST_PG_DB="${PGVECTOR_DB:-}" \
  MANIFEST_PSQL_MODE="${PSQL_MODE:-}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_EMBEDDING_MOCK_OK="${EMBEDDING_MOCK_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_PG_PREPARED="${PG_PREPARED:-false}" \
  MANIFEST_UNAUTH="${UNAUTH_REJECTED:-false}" \
  MANIFEST_ADMIN="${ADMIN_REGISTERED:-false}" \
  MANIFEST_STATUS_PGVECTOR="${STATUS_PGVECTOR:-false}" \
  MANIFEST_PROVIDER_ENABLED="${PROVIDER_ENABLED:-false}" \
  MANIFEST_DOCS_UPLOADED="${DOCS_UPLOADED:-false}" \
  MANIFEST_SYNC_ATTEMPTED="${SYNC_ATTEMPTED:-false}" \
  MANIFEST_PSQL_EXTENSION="${PSQL_EXTENSION_OK:-false}" \
  MANIFEST_PSQL_DOCS_INDEXED="${PSQL_DOCS_INDEXED_OK:-false}" \
  MANIFEST_PSQL_DIMS_1536="${PSQL_DIMS_OK:-false}" \
  MANIFEST_PSQL_SCHEMA_MIGRATIONS="${PSQL_SCHEMA_OK:-false}" \
  MANIFEST_QUERY_HIT_PRINTER="${QUERY_HIT_PRINTER:-false}" \
  MANIFEST_QUERY_NEGATIVE_FILTERED="${QUERY_NEGATIVE_FILTERED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json, os, sys

dst = sys.argv[1]
e = os.environ
manifest = {
    "provider": "pgvector",
    "mode": "runtime-pgvector-chain",
    "generated_at": __import__("datetime").datetime.now().isoformat(),
    "env": {
        "pg": f"{e.get('MANIFEST_PG_HOST','')}:{e.get('MANIFEST_PG_PORT','')}/{e.get('MANIFEST_PG_DB','')}",
        "embedding": "openai-compatible-mock (topic-embedding 1536d)",
        "psql_mode": e.get("MANIFEST_PSQL_MODE", ""),
    },
    "status": {"overall": e.get("MANIFEST_OVERALL_STATUS", "unknown")},
    "checks": {
        "build_ok": e.get("MANIFEST_BUILD_OK", "false"),
        "embedding_mock_ok": e.get("MANIFEST_EMBEDDING_MOCK_OK", "false"),
        "ready_ok": e.get("MANIFEST_READY_OK", "false"),
        "pg_prepared": e.get("MANIFEST_PG_PREPARED", "false"),
        "unauthenticated_ai_status_rejected_401": e.get("MANIFEST_UNAUTH", "false"),
        "admin_registered": e.get("MANIFEST_ADMIN", "false"),
        "ai_status_reports_pgvector": e.get("MANIFEST_STATUS_PGVECTOR", "false"),
        "knowledge_provider_enabled_healthy": e.get("MANIFEST_PROVIDER_ENABLED", "false"),
        "documents_uploaded_and_indexed": e.get("MANIFEST_DOCS_UPLOADED", "false"),
        "knowledge_sync_attempted": e.get("MANIFEST_SYNC_ATTEMPTED", "false"),
        "psql_vector_extension_present": e.get("MANIFEST_PSQL_EXTENSION", "false"),
        "psql_docs_indexed_with_embeddings": e.get("MANIFEST_PSQL_DOCS_INDEXED", "false"),
        "psql_embedding_dims_1536": e.get("MANIFEST_PSQL_DIMS_1536", "false"),
        "psql_schema_migrations_applied": e.get("MANIFEST_PSQL_SCHEMA_MIGRATIONS", "false"),
        "semantic_query_hits_expected_doc": e.get("MANIFEST_QUERY_HIT_PRINTER", "false"),
        "unrelated_query_filtered_out": e.get("MANIFEST_QUERY_NEGATIVE_FILTERED", "false"),
    },
    "evidence_files": sorted(f for f in os.listdir(os.path.dirname(dst)) if f.endswith((".txt", ".parsed"))),
}
with open(dst, "w", encoding="utf-8") as fh:
    json.dump(manifest, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
}

# 0) 预检。
append_summary "step=precheck"
if timeout 5 bash -c "cat < /dev/null > /dev/tcp/${PGVECTOR_HOST}/${PGVECTOR_PORT}" 2>/dev/null; then
  PG_PREPARED=true
  append_summary "pg_prepared=true"
  echo "✅ pg 可达 ${PGVECTOR_HOST}:${PGVECTOR_PORT}"
else
  echo "❌ pg 不可达 ${PGVECTOR_HOST}:${PGVECTOR_PORT}（先起 pgvector 容器，见脚本头注释）" >&2
  write_manifest
  exit 1
fi
if curl -fsS "http://127.0.0.1:${SERVIFY_PORT}/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用，拒绝打到未知服务" >&2
  exit 1
fi
if curl -fsS "http://127.0.0.1:${EMBEDDING_PORT}/healthz" > /dev/null 2>&1; then
  echo "❌ 端口 $EMBEDDING_PORT 已被占用" >&2
  exit 1
fi
# 库是持久的：清掉上次验收残留的 pgvector 文档，保证证据可重复。
psql_exec "DELETE FROM knowledge_docs WHERE provider_id='pgvector'" > /dev/null 2>&1 || true
append_summary "servify_url=$SERVIFY_URL"

# 1) 构建。
append_summary "step=make_build"
echo "🔍 make build..."
if (cd "$PROJECT_ROOT" && make build) > "$EVIDENCE_DIR/build-output.txt" 2>&1; then
  BUILD_OK=true
fi
[ "${BUILD_OK:-false}" = "true" ] || { echo "❌ make build 失败" >&2; write_manifest; exit 1; }

# 2) embedding mock（OpenAI /v1/embeddings 协议；词袋 hash 向量：同文本同向量，
#    词汇重叠越多余弦相似度越高，使语义检索可精确对账）。
append_summary "step=embedding_mock"
python3 - "$EMBEDDING_PORT" > "$EVIDENCE_DIR/embedding-mock-log.txt" 2>&1 <<'PY' &
import hashlib
import json
import math
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

DIM = 1536
PORT = int(sys.argv[1])


def tokenize(text):
    tokens, cur = [], []
    for ch in text:
        if "一" <= ch <= "鿿":
            if cur:
                tokens.append("".join(cur))
                cur = []
            tokens.append(ch)
        elif ch.isalnum():
            cur.append(ch.lower())
        else:
            if cur:
                tokens.append("".join(cur))
                cur = []
    if cur:
        tokens.append("".join(cur))
    return tokens


# 主题词表：命中主题词的 token 落到该主题的高权重维度，模拟真实
# embedding 的语义聚类（同主题 query 与文档相似度 >> 无关文本），
# 使 cosine 检索可在 orchestrator 硬编码 threshold=0.7 下稳定对账。
TOPICS = {
    0: "打印机卡纸硒鼓搓纸走纸缺纸黑条",
    1: "退货退款订单商品签收运费无理由",
    2: "账户密码登录验证安全两步冻结注销",
}
TOPIC_DIM = {tok: idx for idx, words in TOPICS.items() for tok in words}


def embed(text):
    vec = [0.0] * DIM
    for tok in tokenize(text):
        if tok in TOPIC_DIM:
            vec[TOPIC_DIM[tok]] += 3.0
        else:
            h = int(hashlib.md5(tok.encode("utf-8")).hexdigest(), 16)
            vec[8 + h % (DIM - 8)] += 1.0
    norm = math.sqrt(sum(v * v for v in vec)) or 1.0
    return [v / norm for v in vec]


class Handler(BaseHTTPRequestHandler):
    def _send(self, code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/healthz":
            self._send(200, {"status": "ok"})
        elif self.path.endswith("/models"):
            # openai provider 的 HealthCheck 探 {base_url}/models
            self._send(200, {
                "object": "list",
                "data": [{"id": "bag-of-words-1536", "object": "model", "owned_by": "acceptance-mock"}],
            })
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        try:
            req = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError:
            self._send(400, {"error": "invalid json"})
            return
        if self.path == "/v1/embeddings":
            inputs = req.get("input", [])
            if isinstance(inputs, str):
                inputs = [inputs]
            data = [
                {"object": "embedding", "index": i, "embedding": embed(str(text))}
                for i, text in enumerate(inputs)
            ]
            self._send(200, {
                "object": "list",
                "data": data,
                "model": req.get("model", "bag-of-words-1536"),
                "usage": {"prompt_tokens": 0, "total_tokens": 0},
            })
            return
        if self.path == "/v1/chat/completions":
            # 固定回答即可：检索结果由 knowledge provider 侧产生并回传 sources。
            self._send(200, {
                "id": "chatcmpl-mock",
                "object": "chat.completion",
                "model": req.get("model", "mock"),
                "choices": [{
                    "index": 0,
                    "finish_reason": "stop",
                    "message": {"role": "assistant", "content": "这是来自 mock LLM 的回答。"},
                }],
                "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
            })
            return
        self._send(404, {"error": "not found"})

    def log_message(self, fmt, *args):
        sys.stderr.write("embedding-mock: " + fmt % args + "\n")


HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
PY
EMBEDDING_PID=$!
for i in $(seq 1 10); do
  if curl -fsS "http://127.0.0.1:${EMBEDDING_PORT}/healthz" > /dev/null 2>&1; then
    EMBEDDING_MOCK_OK=true
    break
  fi
  sleep 0.5
done
[ "${EMBEDDING_MOCK_OK:-false}" = "true" ] || { echo "❌ embedding mock 未就绪" >&2; write_manifest; exit 1; }
append_summary "embedding_mock_ok=true"

# 3) 临时 config：embedding 指向 mock，pgvector 阈值放宽到词袋相似度量级
#    （真实 embedding 模型的 0.7 阈值对词袋向量没有意义；负例仍为 0 分被滤）。
append_summary "step=setup"
make_config() {
  local dst=$1
  python3 - "$PROJECT_ROOT/config.yml" "$dst" "$EMBEDDING_PORT" <<'PY'
import sys
import yaml

src, dst, emb_port = sys.argv[1], sys.argv[2], sys.argv[3]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["paths"] = [
    {"enabled": True, "prefix": "/api/", "requests_per_minute": 120, "burst": 60},
]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = "./uploads"
cfg["knowledge"]["provider"] = "pgvector"
cfg["knowledge"]["pgvector"]["search"]["top_k"] = 5
cfg["knowledge"]["pgvector"]["search"]["threshold"] = 0.15
cfg["knowledge"]["pgvector"]["search"]["strategy"] = "semantic"
cfg["knowledge"]["pgvector"]["indexing"]["chunk_size"] = 500
cfg["knowledge"]["pgvector"]["indexing"]["chunk_overlap"] = 50
cfg["embedding"]["provider"] = "openai"
cfg["embedding"]["openai"]["api_key"] = "acceptance-not-a-real-key"
cfg["embedding"]["openai"]["base_url"] = f"http://127.0.0.1:{emb_port}/v1"
cfg["embedding"]["openai"]["model"] = "bag-of-words-1536"
# LLM 也指向 mock：真实 OpenAI 无 key 会 401 使编排走 fallback、丢弃检索结果。
cfg["ai"]["openai"]["api_key"] = "acceptance-not-a-real-key"
cfg["ai"]["openai"]["base_url"] = f"http://127.0.0.1:{emb_port}/v1"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
}

start_server() {
  bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=postgres DB_HOST="$2" DB_PORT="$3" DB_USER="$4" DB_PASSWORD="$5" DB_NAME="$6" SERVIFY_PORT="$7" "$8"' \
    _ "$WORK_DIR" "$PGVECTOR_HOST" "$PGVECTOR_PORT" "$PGVECTOR_USER" "$PGVECTOR_PASSWORD" "$PGVECTOR_DB" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
    >> "$EVIDENCE_DIR/server-log.txt" 2>&1 &
  SERVER_PID=$!
}

wait_for() {
  local label=$1 url=$2 tries=$3 gap=$4
  for _ in $(seq 1 "$tries"); do
    if curl -fsS "$url" > /dev/null 2>&1; then
      echo "✅ $label 可用"
      return 0
    fi
    sleep "$gap"
  done
  return 1
}

make_config "$WORK_DIR/config.yml"
echo "🚀 启动 Servify (postgres → ${PGVECTOR_HOST}:${PGVECTOR_PORT}/${PGVECTOR_DB}, 端口 $SERVIFY_PORT)..."
start_server
if ! wait_for "Servify" "$SERVIFY_URL/ready" 30 1; then
  echo "❌ Servify 未就绪" >&2
  tail -30 "$EVIDENCE_DIR/server-log.txt" >&2 || true
  write_manifest
  exit 1
fi

request_capture "ready" "$SERVIFY_URL/ready"
if grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/ready.txt"; then
  READY_OK=true
fi

# 4) 负例：未认证 ai/status。
append_summary "step=ai_chain"
request_capture "unauthenticated-ai-status" "$AI_BASE/status"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_REJECTED=true
fi

# 注册用户 → 经 psql 提权为 admin → 重新登录。
# 注册端点忽略请求中的 role（防止自封管理员；首个用户自动 admin 是唯一例外），
# 因此提权走数据面：UPDATE users.role 后重新签发 token。
ST8_RUN="st8$(date +%H%M%S)"
request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"${ST8_RUN}\",\"email\":\"${ST8_RUN}@servify.io\",\"password\":\"Acceptance!12345\",\"name\":\"ST8 Admin\"}" \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$(json_field "$EVIDENCE_DIR/register-admin.txt" "d.get('token','') and ('yes' if d.get('user') else '')" 2>/dev/null || echo "")" = "yes" ]; then
  ADMIN_REGISTERED=true
fi
psql_exec "UPDATE users SET role='admin' WHERE username='${ST8_RUN}'" > /dev/null 2>&1 || true
request_capture "login-admin" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"${ST8_RUN}\",\"password\":\"Acceptance!12345\"}" \
  "$SERVIFY_URL/api/v1/auth/login"
ADMIN_TOKEN="$(json_field "$EVIDENCE_DIR/login-admin.txt" "d.get('token','') or (d.get('data') or {}).get('token','')" 2>/dev/null || echo "")"
if [ -n "$ADMIN_TOKEN" ] && [ "$ADMIN_TOKEN" != "None" ]; then
  LOGIN_ROLE="$(json_field "$EVIDENCE_DIR/login-admin.txt" "(d.get('user') or {}).get('role','')" 2>/dev/null || echo "?")"
  echo "login_role=$LOGIN_ROLE token_len=${#ADMIN_TOKEN}" >> "$SUMMARY_FILE"
  AUTH="Authorization: Bearer $ADMIN_TOKEN"
else
  AUTH=""
fi

# ai/status 应上报 pgvector。
request_capture "ai-status" -H "$AUTH" "$AI_BASE/status"
STATUS_PROVIDER="$(json_field "$EVIDENCE_DIR/ai-status.txt" "(d.get('data') or {}).get('knowledge_provider','')" 2>/dev/null || echo "")"
if [ "$STATUS_PROVIDER" = "pgvector" ]; then
  STATUS_PGVECTOR=true
fi

# enable → status healthy。
request_capture "knowledge-provider-enable" -X PUT -H "$AUTH" "$AI_BASE/knowledge-provider/enable"
request_capture "ai-status-enabled" -H "$AUTH" "$AI_BASE/status"
{
  echo "provider=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider','')")"
  echo "enabled=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider_enabled')")"
  echo "healthy=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider_healthy')")"
} > "$EVIDENCE_DIR/ai-status-enabled.parsed"
if grep -q 'provider=pgvector' "$EVIDENCE_DIR/ai-status-enabled.parsed" \
  && grep -q 'enabled=True' "$EVIDENCE_DIR/ai-status-enabled.parsed" \
  && grep -q 'healthy=True' "$EVIDENCE_DIR/ai-status-enabled.parsed"; then
  PROVIDER_ENABLED=true
fi

# 5) 上传三篇主题分离文档（打印机文档 >500 字触发 chunking）。
upload_doc() {
  local name=$1 title=$2
  python3 - "$title" > "$WORK_DIR/$name.json" <<PY
import json, sys

title = sys.argv[1]
if title == "打印机故障排查指南":
    body = (
        "打印机卡纸是最常见的办公设备故障。当打印机卡纸发生时，应先关闭打印机电源，"
        "然后打开前盖，沿走纸方向缓慢抽出卡住的纸张。打印机卡纸的预防方法包括：使用符合规格的纸张、"
        "不要超出发纸器的容量、定期清理走纸通道的碎屑。如果打印机频繁卡纸，可能是搓纸轮磨损，"
        "需要联系售后更换搓纸轮组件。打印机显示缺纸但纸盒有纸，通常是纸位传感器被灰尘遮挡，"
        "用干燥的软布清洁传感器即可恢复。打印机打印出现纵向黑条，多为硒鼓表面划伤，更换硒鼓可解决。"
        "打印机卡纸的进阶处理：如果纸张在定影器处卡住，切勿直接用手拉扯，应打开后盖并等待定影器冷却，"
        "再沿出纸方向取出，否则残留碎纸会加剧后续卡纸。双面打印时的打印机卡纸多发生在翻转通道，"
        "建议减少双面任务并检查翻转电机的运转声音是否异常。每月应对打印机走纸辊做一次清洁保养，"
        "用微湿的无尘布擦拭搓纸轮表面，待其完全干燥后再合上纸盒。若上述方法都无法解决打印机卡纸，"
        "请记录面板报错代码并联系官方售后，报错代码能帮助工程师快速定位故障部件。"
        "服务网点与备件：耗材硒鼓、定影组件和搓纸轮属于易损件，建议在购买打印机时同步备一套原厂耗材；"
        "超出自助维修范围的打印机卡纸故障，工程师上门时会先做整机检测再给出维修方案与费用明细。"
        "常见误区：用力拍打机身并不能减少打印机卡纸，震动反而可能使进纸机构错位；用剪刀伸入机身挑纸"
        "极易划伤感光鼓与定影膜，属于高危操作。打印作业排队过多时，先取消队列中所有任务再逐份打印，"
        "能显著降低连续出纸导致的打印机卡纸概率。潮湿季节纸张含水量升高，纸盒取出未用完的纸张后应密封保存，"
        "受潮纸张经过定影高温极易卷曲粘连，这是雨季打印机卡纸报修量翻倍的主要原因。"
    )
elif title == "退货政策说明":
    body = (
        "商品退货政策：自签收之日起七天内，商品未拆封且不影响二次销售的，可申请无理由退货。"
        "退货流程为：在订单详情页点击申请退货，填写退货原因，等待审核通过后按系统提供的地址寄回商品。"
        "退款将在仓库验收商品后三个工作日内原路退回。定制类商品、鲜活易腐商品不支持无理由退货。"
        "质量问题退货的运费由商家承担，非质量问题退货的运费由买家自行承担。"
    )
else:
    body = (
        "账户安全中心：为保障您的账户安全，请定期修改登录密码，密码应包含大小写字母、数字与符号。"
        "开启两步验证后，新设备登录需要输入手机验证码。如发现异常登录提醒，请立即修改密码并联系客服冻结账户。"
        "平台客服绝不会以任何理由索要您的密码或短信验证码。账户注销后，所有数据将在十五个自然日后永久删除。"
    )
print(json.dumps({"title": title, "content": body, "tags": ["st8-pgvector"]}, ensure_ascii=False))
PY
  request_capture "upload-$name" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d @"$WORK_DIR/$name.json" "$AI_BASE/knowledge/upload"
}

upload_doc printer "打印机故障排查指南"
upload_doc refund "退货政策说明"
upload_doc account "账户安全中心说明"

DOC_COUNT="$(psql_exec "SELECT count(*) FROM knowledge_docs WHERE provider_id='pgvector' AND embedding IS NOT NULL" 2>/dev/null | tr -d '[:space:]')"
if [ "${DOC_COUNT:-0}" -ge 3 ] \
  && grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-printer.txt" \
  && grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-refund.txt" \
  && grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-account.txt"; then
  DOCS_UPLOADED=true
fi

# sync 留档（pgvector 为本地自建，无外部数据源；如实记录行为）。
request_capture "knowledge-sync" -X POST -H "$AUTH" "$AI_BASE/knowledge/sync"
SYNC_ATTEMPTED=true
append_summary "sync_status=${RESPONSE_STATUS}"

# 6) psql 数据证据。
append_summary "step=psql_evidence"
EXT_INFO="$(psql_exec "SELECT extname||'='||extversion FROM pg_extension WHERE extname='vector'" 2>/dev/null | tr -d '[:space:]')"
echo "ext=$EXT_INFO" > "$EVIDENCE_DIR/psql-extension.txt"
case "$EXT_INFO" in
  vector=*) PSQL_EXTENSION_OK=true ;;
esac

{
  echo "docs_indexed=$DOC_COUNT"
  echo "chunked_rows=$(psql_exec "SELECT count(*) FROM knowledge_docs WHERE provider_id='pgvector' AND chunk_index>0" 2>/dev/null | tr -d '[:space:]')"
} > "$EVIDENCE_DIR/psql-docs.txt"
if [ "${DOC_COUNT:-0}" -ge 3 ]; then
  PSQL_DOCS_INDEXED_OK=true
fi

DIMS="$(psql_exec "SELECT DISTINCT vector_dims(embedding) FROM knowledge_docs WHERE provider_id='pgvector' AND embedding IS NOT NULL LIMIT 1" 2>/dev/null | tr -d '[:space:]')"
echo "dims=$DIMS" > "$EVIDENCE_DIR/psql-dims.txt"
if [ "$DIMS" = "1536" ]; then
  PSQL_DIMS_OK=true
fi

MIG_STATE="$(psql_exec "SELECT version||'|'||dirty FROM schema_migrations" 2>/dev/null | tr -d '[:space:]')"
echo "schema_migrations=$MIG_STATE" > "$EVIDENCE_DIR/psql-schema-migrations.txt"
if [ "$MIG_STATE" != "" ] && [ "$MIG_STATE" != "|" ]; then
  PSQL_SCHEMA_OK=true
fi

# 7) 语义检索对账：正例命中打印机文档，无关 query 被阈值滤除。
append_summary "step=semantic_search"
query_ai() {
  request_capture "$1" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d "{\"query\":$2}" "$AI_BASE/query"
  python3 - "$EVIDENCE_DIR/$1.txt" > "$EVIDENCE_DIR/$1.parsed" <<'PY'
import json, sys
raw = open(sys.argv[1], encoding="utf-8", errors="replace").read()
body = raw
for sep in ("\r\n\r\n", "\n\n"):
    idx = raw.find(sep)
    if idx != -1:
        body = raw[idx + len(sep):]
        break
d = json.loads(body.strip())
data = d.get("data") or {}
sources = data.get("sources") or []
for s in sources:
    print("title=" + str(s.get("title")))
    print("score=" + str(s.get("score")))
PY
}

# {"query":"..."} 的 JSON 引号处理：query 文本内无引号，直接拼接。
QUERY_PRINTER='"打印机卡纸了应该怎么处理"'
query_ai query-printer "$QUERY_PRINTER"
if grep -q 'title=打印机故障排查指南' "$EVIDENCE_DIR/query-printer.parsed"; then
  QUERY_HIT_PRINTER=true
fi

QUERY_UNRELATED='"今天天气预报怎么样适合穿什么衣服"'
query_ai query-unrelated "$QUERY_UNRELATED"
if [ ! -s "$EVIDENCE_DIR/query-unrelated.parsed" ]; then
  QUERY_NEGATIVE_FILTERED=true
fi

# 8) 汇总。
append_summary "step=overall"
ALL_OK=true
for v in BUILD_OK EMBEDDING_MOCK_OK READY_OK PG_PREPARED UNAUTH_REJECTED ADMIN_REGISTERED STATUS_PGVECTOR PROVIDER_ENABLED DOCS_UPLOADED PSQL_EXTENSION_OK PSQL_DOCS_INDEXED_OK PSQL_DIMS_OK PSQL_SCHEMA_OK QUERY_HIT_PRINTER QUERY_NEGATIVE_FILTERED; do
  [ "${!v:-false}" = "true" ] || { ALL_OK=false; echo "  ⚠️ $v 未通过"; }
done
if [ "$ALL_OK" = "true" ]; then
  OVERALL_STATUS="passed"
  append_summary "overall_status=passed"
  echo "✅ pgvector acceptance 通过: pg+pgvector 扩展/迁移、上传即索引(embedding 1536 维)、
     enable→status healthy、语义检索命中正确文档与无关 query 阈值过滤、psql 数据证据 全部真实留档"
  write_manifest
else
  append_summary "overall_status=failed"
  echo "❌ pgvector acceptance 存在未通过项" >&2
  write_manifest
  exit 1
fi
