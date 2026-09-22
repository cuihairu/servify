#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Suggestion acceptance（客户侧推荐问题）测试开始..."

AI_ACCEPTANCE_MODE=${AI_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/suggestion-acceptance"}
ADMIN_USERNAME=${ADMIN_USERNAME:-"suggestion-admin"}
ADMIN_PASSWORD=${ADMIN_PASSWORD:-"suggestion-123"}

mkdir -p "$EVIDENCE_DIR"

BUILD_OK=false
DOCS_OK=false
INITIAL_OK=false
NEXT_GET_OK=false
NEXT_POST_OK=false
NEXT_REJECT_OK=false
OVERALL_STATUS=failed

SERVIFY_URL=${SERVIFY_URL:-}
SERVER_PID=""
DB_DSN=""

cleanup() {
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

# request_json 发送请求并把状态码写入 RESPONSE_STATUS、响应体写入 RESPONSE_BODY。
RESPONSE_STATUS=""
RESPONSE_BODY=""
request_json() {
  local method=$1 url=$2 body=${3:-} token=${4:-}
  local response_file status_file
  response_file="$(mktemp "${TMPDIR:-/tmp}/suggestion-resp-XXXXXX")"
  status_file="$(mktemp "${TMPDIR:-/tmp}/suggestion-status-XXXXXX")"
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
  MANIFEST_MODE="${AI_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_SERVIFY_URL="${SERVIFY_URL:-}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_BUILD_OK="${BUILD_OK:-false}" \
  MANIFEST_DOCS_OK="${DOCS_OK:-false}" \
  MANIFEST_INITIAL_OK="${INITIAL_OK:-false}" \
  MANIFEST_NEXT_GET_OK="${NEXT_GET_OK:-false}" \
  MANIFEST_NEXT_POST_OK="${NEXT_POST_OK:-false}" \
  MANIFEST_NEXT_REJECT_OK="${NEXT_REJECT_OK:-false}" \
  MANIFEST_EXPOSURE_CONVERSION_OK="${EXPOSURE_CONVERSION_OK:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "suggestion",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "servify_url": os.environ.get("MANIFEST_SERVIFY_URL", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "build_ok": os.environ.get("MANIFEST_BUILD_OK", "false"),
        "knowledge_docs_ok": os.environ.get("MANIFEST_DOCS_OK", "false"),
        "initial_questions_ok": os.environ.get("MANIFEST_INITIAL_OK", "false"),
        "next_questions_get_ok": os.environ.get("MANIFEST_NEXT_GET_OK", "false"),
        "next_questions_post_ok": os.environ.get("MANIFEST_NEXT_POST_OK", "false"),
        "next_questions_reject_ok": os.environ.get("MANIFEST_NEXT_REJECT_OK", "false"),
        "exposure_conversion_ok": os.environ.get("MANIFEST_EXPOSURE_CONVERSION_OK", "false"),
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
Suggestion acceptance summary
mode=$AI_ACCEPTANCE_MODE
EOF

if [ "$AI_ACCEPTANCE_MODE" = "real" ]; then
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

  # 2) 以 sqlite 启动真实服务（推荐策略为规则+公开知识库，无需外部依赖）
  if [ -z "$SERVIFY_URL" ]; then
    SERVIFY_PORT=${SERVIFY_PORT:-18095}
    SERVIFY_URL="http://127.0.0.1:${SERVIFY_PORT}"
    if curl -fsS "$SERVIFY_URL/health" > /dev/null 2>&1; then
      echo "❌ 端口 $SERVIFY_PORT 已被占用（$SERVIFY_URL 已可达），拒绝打到未知服务；请换 SERVIFY_PORT 或清理残留进程" >&2
      exit 1
    fi
    DB_DSN="$(mktemp -u "${TMPDIR:-/tmp}/suggestion-XXXXXX.sqlite")"
    echo "🚀 启动真实服务 (sqlite, bin/servify): $SERVIFY_URL"
    SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-dev-secret} \
    DB_DRIVER=sqlite DB_DSN="$DB_DSN" SERVIFY_PORT="$SERVIFY_PORT" \
      "$PROJECT_ROOT/bin/servify" > "$EVIDENCE_DIR/server-log.txt" 2>&1 &
    SERVER_PID=$!
  fi
fi

if [ -z "$SERVIFY_URL" ]; then
  echo "❌ mock 模式需要 SERVIFY_URL" >&2
  exit 1
fi
append_summary "servify_url=$SERVIFY_URL"

if ! wait_for "Servify Health" "$SERVIFY_URL/health" 30 2; then
  echo "❌ 服务未就绪" >&2
  exit 1
fi

# 3) 注册 admin（创建知识文档走 management 组，需要 admin token）
append_summary "step=auth"
REGISTER_BODY=$(printf '{"username":"%s","email":"%s@servify.io","password":"%s","name":"Suggestion Admin","role":"admin"}' \
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

# 4) 准备知识库数据：1 篇私有 + 3 篇公开（公开篇目之间 sleep，保证 updated_at 可分辨，
#    initial 接口按 recency 逆序返回；标题/内容互不包含对方关键词，保证 next 命中唯一）
append_summary "step=seed_knowledge_docs"
create_doc() {
  local name=$1 title=$2 category=$3 content=$4 tags=$5 is_public=$6
  local body
  body=$(python3 - "$title" "$category" "$content" "$tags" "$is_public" <<'PY'
import json, sys
print(json.dumps({
    "title": sys.argv[1],
    "category": sys.argv[2],
    "content": sys.argv[3],
    "tags": sys.argv[4].split(",") if sys.argv[4] else [],
    "is_public": sys.argv[5] == "true",
}, ensure_ascii=False))
PY
)
  request_json "POST" "$SERVIFY_URL/api/knowledge-docs" "$body" "$ADMIN_TOKEN"
  save_response "$name" "$RESPONSE_BODY"
  append_summary "${name}_status=$RESPONSE_STATUS"
  if [ "$RESPONSE_STATUS" = "201" ]; then
    return 0
  fi
  return 1
}

# 私有文档：任何客户侧响应都不得出现该标题
create_doc "doc-private" "内部故障处理手册私有版" "internal" "仅供内部坐席使用的故障处理流程" "内部" "false" || true
sleep 1.1
# 公开 1（最旧）
create_doc "doc-public-billing" "如何导出历史账单" "billing" "支持导出最近十二个月的账单明细 CSV 文件" "账单" "true" || true
sleep 1.1
# 公开 2
create_doc "doc-public-password" "如何重置登录密码" "account" "忘记密码时可在登录页点击忘记密码，通过邮箱验证后重置" "账户" "true" || true
sleep 1.1
# 公开 3（最新）
create_doc "doc-public-agent" "如何联系人工客服" "faq" "工作日九点到十八点可在会话中申请转接人工客服" "客服" "true" || true

# 公开文档的创建状态决定后续断言是否有意义
PUBLIC_CREATED=$(grep -cE '^doc-public-(billing|password|agent)_status=201$' "$EVIDENCE_DIR/summary.txt" || true)
PRIVATE_CREATED=$(grep -c '^doc-private_status=201$' "$EVIDENCE_DIR/summary.txt" || true)
if [ "$PUBLIC_CREATED" = "3" ] && [ "$PRIVATE_CREATED" = "1" ]; then
  DOCS_OK=true
  append_summary "docs_ok=true"
else
  append_summary "docs_ok=false (public=$PUBLIC_CREATED private=$PRIVATE_CREATED)"
fi

# 5) GET /public/suggestions/initial（匿名，无 Authorization）：
#    恰好 3 篇公开标题按 recency 逆序，私有标题不出现
append_summary "step=initial_questions"
request_json "GET" "$SERVIFY_URL/public/suggestions/initial?limit=8"
save_response "initial-questions" "$RESPONSE_BODY"
append_summary "initial_http=$RESPONSE_STATUS"
INITIAL_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    payload = json.load(sys.stdin)
    data = payload.get("data") or {}
    questions = [q.get("question", "") for q in (data.get("questions") or [])]
    want = ["如何联系人工客服", "如何重置登录密码", "如何导出历史账单"]
    private = "内部故障处理手册私有版"
    strategy = (data.get("meta") or {}).get("strategy", "")
    if questions == want and private not in questions and strategy == "public_knowledge_recency":
        print("ok")
    else:
        print("mismatch: questions=%r strategy=%r" % (questions, strategy))
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
append_summary "initial_check=$INITIAL_CHECK"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$INITIAL_CHECK" = "ok" ]; then
  INITIAL_OK=true
  append_summary "initial_ok=true"
else
  append_summary "initial_ok=false"
fi

# 6) GET /public/suggestions/next?query=密码（匿名）：
#    仅命中密码文档，账单/人工客服/私有标题不出现
append_summary "step=next_questions_get"
request_json "GET" "$SERVIFY_URL/public/suggestions/next?query=%E5%AF%86%E7%A0%81"
save_response "next-questions-get" "$RESPONSE_BODY"
append_summary "next_get_http=$RESPONSE_STATUS"
NEXT_GET_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    payload = json.load(sys.stdin)
    data = payload.get("data") or {}
    questions = [q.get("question", "") for q in (data.get("questions") or [])]
    strategy = (data.get("meta") or {}).get("strategy", "")
    if (questions and questions[0] == "如何重置登录密码"
            and "如何导出历史账单" not in questions
            and "内部故障处理手册私有版" not in questions
            and data.get("query") == "密码"
            and strategy == "public_knowledge_scored"):
        print("ok")
    else:
        print("mismatch: questions=%r strategy=%r" % (questions, strategy))
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
append_summary "next_get_check=$NEXT_GET_CHECK"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$NEXT_GET_CHECK" = "ok" ]; then
  NEXT_GET_OK=true
  append_summary "next_get_ok=true"
else
  append_summary "next_get_ok=false"
fi

# 7) POST /public/suggestions/next（匿名，JSON body）：命中账单文档
append_summary "step=next_questions_post"
request_json "POST" "$SERVIFY_URL/public/suggestions/next" '{"query":"账单","limit":2}'
save_response "next-questions-post" "$RESPONSE_BODY"
append_summary "next_post_http=$RESPONSE_STATUS"
NEXT_POST_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    payload = json.load(sys.stdin)
    data = payload.get("data") or {}
    questions = [q.get("question", "") for q in (data.get("questions") or [])]
    if "如何导出历史账单" in questions and "内部故障处理手册私有版" not in questions:
        print("ok")
    else:
        print("mismatch: questions=%r" % questions)
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
append_summary "next_post_check=$NEXT_POST_CHECK"
if [ "$RESPONSE_STATUS" = "200" ] && [ "$NEXT_POST_CHECK" = "ok" ]; then
  NEXT_POST_OK=true
  append_summary "next_post_ok=true"
else
  append_summary "next_post_ok=false"
fi

# 8) 拒绝路径：缺 query 必须 400
request_json "GET" "$SERVIFY_URL/public/suggestions/next"
save_response "next-questions-missing-query" "$RESPONSE_BODY"
append_summary "next_missing_query_status=$RESPONSE_STATUS"
if [ "$RESPONSE_STATUS" = "400" ]; then
  NEXT_REJECT_OK=true
  append_summary "next_reject_ok=true"
else
  append_summary "next_reject_ok=false"
fi

# 9) 曝光/转化归因（P2-0 RQ-5，仅真实服务 + 本地 sqlite 模式）：initial/next
#    带 session_id 产出曝光行，WS 发送匹配的客户消息触发转化回写，随后直读
#    sqlite 断言 suggestion_exposure_logs（mock/外部模式无 DB 落点，跳过）。
EXPOSURE_CONVERSION_OK=skipped
if [ -n "$DB_DSN" ] && [ -n "$SERVER_PID" ] && command -v node >/dev/null 2>&1 \
  && node -e 'process.exit(typeof WebSocket === "function" ? 0 : 1)' 2>/dev/null; then
  append_summary "step=exposure_conversion"
  EXPOSURE_CONVERSION_OK=false
  request_json "GET" "$SERVIFY_URL/public/suggestions/initial?limit=8&session_id=s-acc-1"
  save_response "initial-questions-session" "$RESPONSE_BODY"
  append_summary "initial_session_http=$RESPONSE_STATUS"
  request_json "GET" "$SERVIFY_URL/public/suggestions/next?query=%E5%AF%86%E7%A0%81&session_id=s-acc-1&limit=8"
  save_response "next-questions-session" "$RESPONSE_BODY"
  append_summary "next_session_http=$RESPONSE_STATUS"

  if node - "$SERVIFY_PORT" <<'NODE' 2>> "$EVIDENCE_DIR/ws-conversion.log"
const port = process.argv[2];
const ws = new WebSocket('ws://127.0.0.1:' + port + '/api/v1/ws?session_id=s-acc-1');
const timer = setTimeout(() => { console.error('ws connect timeout'); process.exit(1); }, 8000);
ws.addEventListener('open', () => {
  clearTimeout(timer);
  ws.send(JSON.stringify({
    type: 'text-message',
    data: { content: '如何重置登录密码' },
    session_id: 's-acc-1',
    timestamp: new Date().toISOString(),
  }));
  // 留出落库/归因处理时间再关闭，避免读泵提前被切断
  setTimeout(() => { ws.close(); process.exit(0); }, 800);
});
ws.addEventListener('error', () => { clearTimeout(timer); process.exit(1); });
NODE
  then
    EXPOSURE_CHECK=""
    for i in $(seq 1 10); do
      EXPOSURE_CHECK=$(SERVIFY_DB="$DB_DSN" python3 - <<'PY'
import os, sqlite3
try:
    conn = sqlite3.connect(os.environ["SERVIFY_DB"])
    rows = conn.execute(
        "SELECT kind, converted_question FROM suggestion_exposure_logs"
        " WHERE session_id = 's-acc-1' ORDER BY id"
    ).fetchall()
    kinds = [r[0] for r in rows]
    converted = [r[1] for r in rows if r[1]]
    if kinds == ["initial", "next"] and converted == ["如何重置登录密码"]:
        print("ok")
    else:
        print("pending: kinds=%r converted=%r" % (kinds, converted))
except Exception as exc:
    print("error: %s" % exc)
PY
)
      if [ "$EXPOSURE_CHECK" = "ok" ]; then
        EXPOSURE_CONVERSION_OK=true
        break
      fi
      sleep 1
    done
    append_summary "exposure_check=${EXPOSURE_CHECK:-no-run}"

    # 管理面出口（assist 权限组）聚合数与库内明细对账：匿名 3 行（步骤
    # 5-8 的 initial/next GET/next POST）+ 本步带 session 2 行 = 5；
    # 转化仅本步 WS 命中 1 次且归在 next（最近一次未转化曝光）。
    request_json "GET" "$SERVIFY_URL/api/assist/suggestions/exposure-summary" "" "$ADMIN_TOKEN"
    save_response "exposure-summary" "$RESPONSE_BODY"
    append_summary "exposure_summary_http=$RESPONSE_STATUS"
    EXPOSURE_SUMMARY_CHECK=$(printf '%s' "$RESPONSE_BODY" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin).get("data") or {}
    total = data.get("total_exposures", 0)
    converted = data.get("converted_exposures", 0)
    kinds = {k.get("kind"): k.get("converted_exposures") for k in (data.get("by_kind") or [])}
    if total == 5 and converted == 1 and kinds.get("next") == 1 and kinds.get("initial") == 0:
        print("ok")
    else:
        print("mismatch: total=%r converted=%r kinds=%r" % (total, converted, kinds))
except Exception as exc:
    print("parse_error: %s" % exc)
' 2>/dev/null || true)
    append_summary "exposure_summary_check=$EXPOSURE_SUMMARY_CHECK"

    if [ "$EXPOSURE_CHECK" = "ok" ] && [ "$EXPOSURE_SUMMARY_CHECK" = "ok" ]; then
      EXPOSURE_CONVERSION_OK=true
    fi
  else
    append_summary "ws_send=failed"
  fi
  append_summary "exposure_conversion_ok=$EXPOSURE_CONVERSION_OK"
else
  append_summary "exposure_conversion=skipped (external/mock mode or node WebSocket unavailable)"
fi

if [ "$DOCS_OK" != "true" ] || [ "$INITIAL_OK" != "true" ] || [ "$NEXT_GET_OK" != "true" ] || [ "$NEXT_POST_OK" != "true" ] || [ "$NEXT_REJECT_OK" != "true" ] || [ "$EXPOSURE_CONVERSION_OK" = "false" ]; then
  OVERALL_STATUS=failed
  append_summary "overall_status=failed"
  echo "❌ Suggestion acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Suggestion acceptance 通过: initial(recency/仅公开) / next GET+POST(评分命中/隐私隔离) / 400 拒绝路径 / 曝光-转化归因 全部真实留档"
