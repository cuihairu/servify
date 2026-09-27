#!/bin/bash

# 本地自含知识引擎（knowledge.provider=local + embedding.provider=local +
# ai.provider=local）验收：零外部依赖——sqlite 落库、进程内确定性哈希嵌入、
# 抽取式本地问答，无 postgres/无 embedding mock/无 LLM mock/零网络出站。
# 全链路真实留证：build → 起服 ready → 401 负例 → 首用户自动 admin →
# ai/status 报 local → enable→healthy → 上传三篇主题文档（真实分块+嵌入）→
# sync 留档 → sqlite3 数据证据（行数/chunking/维度 256）→ 语义检索正例命中
# 与无关 query 过滤 → 回答逐字来自知识原文（extractive 断言）。
# EVIDENCE_DIR 可被环境变量覆盖（合同测试用 t.TempDir() 隔离）。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/local-knowledge"}
SERVIFY_PORT=${SERVIFY_PORT:-18110}
SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
AI_BASE="$SERVIFY_URL/api/v1/ai"
ADMIN_USER="lk$(date +%H%M%S)"
ADMIN_PASSWORD="Acceptance!12345"
DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/local-knowledge-XXXXXX.sqlite")"
WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/local-knowledge-work-XXXXXX")"

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
READY_OK=false
UNAUTH_REJECTED=false
ADMIN_REGISTERED=false
STATUS_LOCAL=false
PROVIDER_ENABLED=false
DOCS_UPLOADED=false
SYNC_ATTEMPTED=false
SQLITE_DOCS_INDEXED_OK=false
SQLITE_CHUNKED_OK=false
SQLITE_DIMS_OK=false
QUERY_HIT_PRINTER=false
QUERY_ANSWER_EXTRACTIVE=false
QUERY_NEGATIVE_FILTERED=false
OVERALL_STATUS=failed

SERVER_PID=""

cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -f "$DB_DSN" || true
  rm -rf "$WORK_DIR" || true
}

SUMMARY_FILE="$EVIDENCE_DIR/summary.txt"
append_summary() {
  printf '%s\n' "$1" >> "$SUMMARY_FILE"
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
  MANIFEST_SERVIFY_PORT="${SERVIFY_PORT:-}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_READY_OK="${READY_OK:-false}" \
  MANIFEST_UNAUTH="${UNAUTH_REJECTED:-false}" \
  MANIFEST_ADMIN="${ADMIN_REGISTERED:-false}" \
  MANIFEST_STATUS_LOCAL="${STATUS_LOCAL:-false}" \
  MANIFEST_PROVIDER_ENABLED="${PROVIDER_ENABLED:-false}" \
  MANIFEST_DOCS_UPLOADED="${DOCS_UPLOADED:-false}" \
  MANIFEST_SYNC_ATTEMPTED="${SYNC_ATTEMPTED:-false}" \
  MANIFEST_SQLITE_DOCS_INDEXED="${SQLITE_DOCS_INDEXED_OK:-false}" \
  MANIFEST_SQLITE_CHUNKED="${SQLITE_CHUNKED_OK:-false}" \
  MANIFEST_SQLITE_DIMS_256="${SQLITE_DIMS_OK:-false}" \
  MANIFEST_QUERY_HIT_PRINTER="${QUERY_HIT_PRINTER:-false}" \
  MANIFEST_QUERY_ANSWER_EXTRACTIVE="${QUERY_ANSWER_EXTRACTIVE:-false}" \
  MANIFEST_QUERY_NEGATIVE_FILTERED="${QUERY_NEGATIVE_FILTERED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import datetime
import json
import os
import sys

dst = sys.argv[1]
e = os.environ
manifest = {
    "provider": "local-knowledge",
    "mode": "runtime-local-chain",
    "generated_at": datetime.datetime.now().isoformat(),
    "env": {
        "database": "sqlite (file, fresh temp db)",
        "knowledge": "local (knowledge_docs + in-process cosine)",
        "embedding": "local deterministic hashing (dimension 256)",
        "ai": "local extractive QA baseline (zero network egress)",
    },
    "status": {"overall": e.get("MANIFEST_OVERALL_STATUS", "unknown")},
    "checks": {
        "build_ok": e.get("MANIFEST_BUILD_OK", "false"),
        "ready_ok": e.get("MANIFEST_READY_OK", "false"),
        "unauthenticated_ai_status_rejected_401": e.get("MANIFEST_UNAUTH", "false"),
        "admin_registered": e.get("MANIFEST_ADMIN", "false"),
        "ai_status_reports_local": e.get("MANIFEST_STATUS_LOCAL", "false"),
        "knowledge_provider_enabled_healthy": e.get("MANIFEST_PROVIDER_ENABLED", "false"),
        "documents_uploaded_and_indexed": e.get("MANIFEST_DOCS_UPLOADED", "false"),
        "knowledge_sync_attempted": e.get("MANIFEST_SYNC_ATTEMPTED", "false"),
        "sqlite_docs_indexed_with_embeddings": e.get("MANIFEST_SQLITE_DOCS_INDEXED", "false"),
        "sqlite_chunking_evidence": e.get("MANIFEST_SQLITE_CHUNKED", "false"),
        "sqlite_embedding_dims_256": e.get("MANIFEST_SQLITE_DIMS_256", "false"),
        "semantic_query_hits_expected_doc": e.get("MANIFEST_QUERY_HIT_PRINTER", "false"),
        "query_answer_extractive_from_doc": e.get("MANIFEST_QUERY_ANSWER_EXTRACTIVE", "false"),
        "unrelated_query_filtered_out": e.get("MANIFEST_QUERY_NEGATIVE_FILTERED", "false"),
    },
    "evidence_files": sorted(f for f in os.listdir(os.path.dirname(dst)) if f.endswith((".txt", ".parsed"))),
}
with open(dst, "w", encoding="utf-8") as fh:
    json.dump(manifest, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
}
trap 'cleanup; write_manifest' EXIT

cat > "$SUMMARY_FILE" <<EOF
local knowledge acceptance summary
port=$SERVIFY_PORT
EOF

# 0) 预检：端口必须空闲（拒绝打到未知服务）。
append_summary "step=precheck"
if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
  echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），请换 SERVIFY_PORT 或清理残留进程" >&2
  exit 1
fi

# 1) CLI 标准构建。
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

# 2) 临时 config：knowledge/embedding/ai 三 local（零外部依赖组合）；
#    本地检索阈值放宽到词法校准量级，indexing 500/50 让长文档触发分块。
append_summary "step=setup"
make_config() {
  local dst=$1
  python3 - "$PROJECT_ROOT/config.yml" "$dst" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as fh:
    cfg = yaml.safe_load(fh)

cfg["security"]["rate_limiting"]["paths"] = [
    {"enabled": True, "prefix": "/api/", "requests_per_minute": 120, "burst": 60},
]
cfg["upload"]["provider"] = "local"
cfg["upload"]["storage_path"] = "./uploads"
cfg["knowledge"]["provider"] = "local"
cfg["knowledge"]["local"] = {
    "search": {"top_k": 5, "threshold": 0.2, "strategy": "semantic"},
    "indexing": {"chunk_size": 500, "chunk_overlap": 50},
}
cfg["embedding"]["provider"] = "local"
cfg["embedding"]["local"] = {"dimension": 256}
# 抽取式本地问答：无出站请求、无 key 要求（零网络出站链路的关键）。
cfg["ai"]["provider"] = "local"

with open(dst, "w", encoding="utf-8") as fh:
    yaml.safe_dump(cfg, fh, allow_unicode=True)
PY
}

start_server() {
  bash -c 'cd "$1" && exec env SERVIFY_JWT_SECRET="${SERVIFY_JWT_SECRET:-dev-secret}" DB_DRIVER=sqlite DB_DSN="$2" SERVIFY_PORT="$3" "$4"' \
    _ "$WORK_DIR" "$DB_DSN" "$SERVIFY_PORT" "$PROJECT_ROOT/bin/servify" \
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
echo "🚀 启动 Servify (sqlite + knowledge/embedding/ai 三 local, 端口 $SERVIFY_PORT)..."
start_server
if ! wait_for "Servify" "$SERVIFY_URL/ready" 30 1; then
  echo "❌ Servify 未就绪" >&2
  tail -30 "$EVIDENCE_DIR/server-log.txt" >&2 || true
  exit 1
fi

request_capture "ready" "$SERVIFY_URL/ready"
if grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/ready.txt"; then
  READY_OK=true
fi

# 3) 负例：未认证 ai/status。
append_summary "step=ai_chain"
request_capture "unauthenticated-ai-status" "$AI_BASE/status"
if [ "$RESPONSE_STATUS" = "401" ]; then
  UNAUTH_REJECTED=true
fi

# 注册 → sqlite3 数据面提权 admin（注册端点忽略请求中的 role，防自封；
# 与 pgvector 版的 psql 提权同构，sqlite 下直接更新库文件）→ 重新签发 token。
request_capture "register-admin" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"${ADMIN_USER}\",\"email\":\"${ADMIN_USER}@servify.io\",\"password\":\"${ADMIN_PASSWORD}\",\"name\":\"Local Knowledge Admin\"}" \
  "$SERVIFY_URL/api/v1/auth/register"
if [ "$(json_field "$EVIDENCE_DIR/register-admin.txt" "d.get('token','') and ('yes' if d.get('user') else '')" 2>/dev/null || echo "")" = "yes" ]; then
  ADMIN_REGISTERED=true
fi
sqlite3 "$DB_DSN" "UPDATE users SET role='admin' WHERE username='${ADMIN_USER}'" || true
request_capture "login-admin" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASSWORD}\"}" \
  "$SERVIFY_URL/api/v1/auth/login"
ADMIN_TOKEN="$(json_field "$EVIDENCE_DIR/login-admin.txt" "d.get('token','') or (d.get('data') or {}).get('token','')" 2>/dev/null || echo "")"
if [ -n "$ADMIN_TOKEN" ] && [ "$ADMIN_TOKEN" != "None" ]; then
  LOGIN_ROLE="$(json_field "$EVIDENCE_DIR/login-admin.txt" "(d.get('user') or {}).get('role','')" 2>/dev/null || echo "?")"
  echo "login_role=$LOGIN_ROLE token_len=${#ADMIN_TOKEN}" >> "$SUMMARY_FILE"
  AUTH="Authorization: Bearer $ADMIN_TOKEN"
else
  AUTH=""
fi

# ai/status 应上报 local。
request_capture "ai-status" -H "$AUTH" "$AI_BASE/status"
STATUS_PROVIDER="$(json_field "$EVIDENCE_DIR/ai-status.txt" "(d.get('data') or {}).get('knowledge_provider','')" 2>/dev/null || echo "")"
if [ "$STATUS_PROVIDER" = "local" ]; then
  STATUS_LOCAL=true
fi

# enable → status healthy。
request_capture "knowledge-provider-enable" -X PUT -H "$AUTH" "$AI_BASE/knowledge-provider/enable"
request_capture "ai-status-enabled" -H "$AUTH" "$AI_BASE/status"
{
  echo "provider=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider','')")"
  echo "enabled=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider_enabled')")"
  echo "healthy=$(json_field "$EVIDENCE_DIR/ai-status-enabled.txt" "(d.get('data') or {}).get('knowledge_provider_healthy')")"
} > "$EVIDENCE_DIR/ai-status-enabled.parsed"
if grep -q 'provider=local' "$EVIDENCE_DIR/ai-status-enabled.parsed" \
  && grep -q 'enabled=True' "$EVIDENCE_DIR/ai-status-enabled.parsed" \
  && grep -q 'healthy=True' "$EVIDENCE_DIR/ai-status-enabled.parsed"; then
  PROVIDER_ENABLED=true
fi

# 4) 上传三篇主题分离文档（打印机文档 >500 字触发 chunking）。
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
print(json.dumps({"title": title, "content": body, "tags": ["local-knowledge"]}, ensure_ascii=False))
PY
  request_capture "upload-$name" -X POST -H "$AUTH" -H "Content-Type: application/json" \
    -d @"$WORK_DIR/$name.json" "$AI_BASE/knowledge/upload"
}

upload_doc printer "打印机故障排查指南"
upload_doc refund "退货政策说明"
upload_doc account "账户安全中心说明"

if grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-printer.txt" \
  && grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-refund.txt" \
  && grep -q 'HTTP/1.1 200' "$EVIDENCE_DIR/upload-account.txt"; then
  DOCS_UPLOADED=true
fi

# sync 留档（local 为本地自含引擎，无外部数据源；如实记录行为）。
request_capture "knowledge-sync" -X POST -H "$AUTH" "$AI_BASE/knowledge/sync"
SYNC_ATTEMPTED=true
append_summary "sync_status=${RESPONSE_STATUS}"

# 5) sqlite3 数据证据：provider_id=local 行带嵌入、长文档分块、维度 256。
append_summary "step=sqlite_evidence"
sqlite_rows() {
  sqlite3 "$DB_DSN" "$1" 2>/dev/null | tr -d '[:space:]'
}
DOC_COUNT="$(sqlite_rows "SELECT count(*) FROM knowledge_docs WHERE provider_id='local' AND embedding IS NOT NULL")"
echo "docs_with_embedding=$DOC_COUNT" > "$EVIDENCE_DIR/sqlite-docs.txt"
if [ "${DOC_COUNT:-0}" -ge 3 ]; then
  SQLITE_DOCS_INDEXED_OK=true
fi

CHUNKED_ROWS="$(sqlite_rows "SELECT count(*) FROM knowledge_docs WHERE provider_id='local' AND chunk_index>0")"
echo "chunked_rows=$CHUNKED_ROWS" >> "$EVIDENCE_DIR/sqlite-docs.txt"
if [ "${CHUNKED_ROWS:-0}" -ge 1 ]; then
  SQLITE_CHUNKED_OK=true
fi

EMB_SAMPLE="$(sqlite3 "$DB_DSN" "SELECT embedding FROM knowledge_docs WHERE provider_id='local' AND embedding IS NOT NULL LIMIT 1" 2>/dev/null | tr -d '[:space:]')"
DIMS="$(printf '%s' "$EMB_SAMPLE" | EMB_SAMPLE="$EMB_SAMPLE" python3 -c 'import os,sys; s=os.environ.get("EMB_SAMPLE","").strip(); print(len(s[1:-1].split(",")) if s.startswith("[") and s.endswith("]") else 0)')"
echo "dims=$DIMS" > "$EVIDENCE_DIR/sqlite-dims.txt"
if [ "$DIMS" = "256" ]; then
  SQLITE_DIMS_OK=true
fi

# 6) 语义检索对账：正例命中打印机文档且回答逐字来自知识原文（extractive）；
#    无关 query 被阈值滤除。不带 session_id：retriever 把 ConversationID
#    透传为 SearchRequest.KnowledgeID，自建驱动按 workspace_id 过滤，
#    非空会话 id 会把全局摄入的文档滤没（与 pgvector 一致的既有语义）。
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
print("strategy=" + str(data.get("strategy")))
print("content=" + str(data.get("content") or ""))
for s in (data.get("sources") or []):
    print("title=" + str(s.get("title")))
    print("score=" + str(s.get("score")))
PY
}

# {"query":"..."} 的 JSON 引号处理：query 文本内无引号，直接拼接。
# 正例用文档原句：确定性哈希嵌入是词法级基线，过 0.7 校准门需要强词面
# 重合（K2 E2E 同口径）；语义泛化不足是已登记的基线缺口，不在本验收
# 目标内。
QUERY_PRINTER='"打印机卡纸是最常见的办公设备故障"'
query_ai query-printer "$QUERY_PRINTER"
if grep -q 'title=打印机故障排查指南' "$EVIDENCE_DIR/query-printer.parsed" \
  && grep -q '^strategy=local' "$EVIDENCE_DIR/query-printer.parsed"; then
  QUERY_HIT_PRINTER=true
fi

# extractive 断言：回答内容必须是打印机文档原文的子串（零编造）。
QUERY_ANSWER="$(grep '^content=' "$EVIDENCE_DIR/query-printer.parsed" | sed 's/^content=//')"
if python3 - "$WORK_DIR/printer.json" "$QUERY_ANSWER" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1], encoding="utf-8"))
answer = sys.argv[2].strip()
# 回答 = 知识上下文行（buildKnowledgePrompt 的 "- 标题: 原文句"）整句抽出；
# 剥掉检索层拼装的 "- 标题: " 引用前缀后，正文必须逐字来自文档原文（零编造）。
prefix = "- " + doc["title"] + ": "
if answer.startswith(prefix):
    answer = answer[len(prefix):]
sys.exit(0 if answer and answer in doc["content"] else 3)
PY
then
  QUERY_ANSWER_EXTRACTIVE=true
else
  QUERY_ANSWER_EXTRACTIVE=false
fi
echo "answer=${QUERY_ANSWER}" >> "$EVIDENCE_DIR/query-printer.parsed"
echo "answer_extractive=${QUERY_ANSWER_EXTRACTIVE}" >> "$EVIDENCE_DIR/query-printer.parsed"

QUERY_UNRELATED='"今天天气预报怎么样适合穿什么衣服"'
query_ai query-unrelated "$QUERY_UNRELATED"
# 负例：sources 为空（parsed 里没有 title=/score= 行），strategy 行除外。
if [ "$(grep -c '^title=' "$EVIDENCE_DIR/query-unrelated.parsed" || true)" = "0" ]; then
  QUERY_NEGATIVE_FILTERED=true
fi

# 7) 汇总。
append_summary "step=overall"
ALL_OK=true
for v in BUILD_OK READY_OK UNAUTH_REJECTED ADMIN_REGISTERED STATUS_LOCAL PROVIDER_ENABLED DOCS_UPLOADED SYNC_ATTEMPTED SQLITE_DOCS_INDEXED_OK SQLITE_CHUNKED_OK SQLITE_DIMS_OK QUERY_HIT_PRINTER QUERY_ANSWER_EXTRACTIVE QUERY_NEGATIVE_FILTERED; do
  [ "${!v:-false}" = "true" ] || { ALL_OK=false; echo "  ⚠️ $v 未通过"; }
done
if [ "$ALL_OK" = "true" ]; then
  OVERALL_STATUS="passed"
  append_summary "overall_status=passed"
  echo "✅ local knowledge acceptance 通过: sqlite+三local 全自含链路、上传即索引(嵌入 256 维/长文档分块)、
     enable→status healthy、语义检索命中正确文档且回答逐字来自原文、无关 query 阈值过滤 全部真实留档"
  write_manifest
else
  append_summary "overall_status=failed"
  echo "❌ local knowledge acceptance 存在未通过项" >&2
  write_manifest
  exit 1
fi
