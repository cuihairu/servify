#!/bin/bash

set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "Usage: $0 <manifest.json>" >&2
  exit 1
fi

MANIFEST_PATH="$1"

if [ ! -f "$MANIFEST_PATH" ]; then
  echo "❌ manifest 不存在: $MANIFEST_PATH" >&2
  exit 1
fi

json_get() {
  local json_input="${1:-}"
  local python_expr="${2:-}"
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$json_input" | jq -r "$python_expr" 2>/dev/null
    return $?
  fi

  JSON_INPUT="$json_input" python3 - "$python_expr" <<'PY'
import json
import os
import sys

expr = sys.argv[1].strip()
payload = os.environ.get("JSON_INPUT", "")

try:
    data = json.loads(payload)
except Exception:
    print("")
    sys.exit(1)

def query(obj, path):
    current = obj
    for raw_part in path.split("."):
        part = raw_part.strip()
        if not part:
            continue
        if isinstance(current, dict) and part in current:
            current = current[part]
        else:
            return None
    return current

fallback = ""
if "//" in expr:
    expr, fallback = expr.split("//", 1)
    expr = expr.strip()
    fallback = fallback.strip().strip('"')

result = None
if expr.startswith("."):
    result = query(data, expr.lstrip("."))

if result is None:
    print(fallback)
elif isinstance(result, bool):
    print("true" if result else "false")
else:
    print(result)
PY
}

MANIFEST_JSON=$(cat "$MANIFEST_PATH")
PROVIDER=$(json_get "$MANIFEST_JSON" '.provider // ""' || true)
MODE=$(json_get "$MANIFEST_JSON" '.mode // ""' || true)

if [ -z "$PROVIDER" ] || [ -z "$MODE" ]; then
  echo "❌ manifest 缺少 provider 或 mode" >&2
  exit 1
fi

declare -a CHECKS=()

require_equals() {
  local path="$1"
  local want="$2"
  local got
  got=$(json_get "$MANIFEST_JSON" "$path" || true)
  CHECKS+=("$path|$want|$got")
}

require_file_listed() {
  local name="$1"
  if ! MANIFEST_JSON="$MANIFEST_JSON" python3 - "$name" <<'PY'
import json
import os
import sys

payload = os.environ["MANIFEST_JSON"]
name = sys.argv[1]
data = json.loads(payload)
files = data.get("evidence_files", [])
sys.exit(0 if name in files else 1)
PY
  then
    echo "❌ manifest 缺少证据文件记录: $name" >&2
    exit 1
  fi
}

case "$PROVIDER" in
  auth-session)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.register_ok // ""' "true"
    require_equals '.checks.login_primary_ok // ""' "true"
    require_equals '.checks.login_secondary_ok // ""' "true"
    require_equals '.checks.refresh_ok // ""' "true"
    require_equals '.checks.old_refresh_rejected // ""' "true"
    require_equals '.checks.sessions_before_ok // ""' "true"
    require_equals '.checks.logout_others_ok // ""' "true"
    require_equals '.checks.sessions_after_logout_others_ok // ""' "true"
    require_equals '.checks.logout_current_ok // ""' "true"
    require_equals '.checks.post_logout_current_rejected // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "auth-register.json"
    require_file_listed "auth-login-primary.json"
    require_file_listed "auth-login-secondary.json"
    require_file_listed "auth-refresh.json"
    require_file_listed "auth-refresh-reuse-old.json"
    require_file_listed "auth-sessions-before.json"
    require_file_listed "auth-logout-others.json"
    require_file_listed "auth-sessions-after-logout-others.json"
    require_file_listed "auth-logout-current.json"
    require_file_listed "auth-sessions-after-logout-current.json"
    ;;
  dify)
    require_equals '.status.knowledge_provider // ""' "dify"
    require_equals '.status.knowledge_provider_enabled // ""' "true"
    require_equals '.status.knowledge_provider_healthy // ""' "true"
    require_equals '.checks.provider_available // ""' "true"
    require_equals '.checks.query_ok // ""' "true"
    require_equals '.checks.knowledge_upload_ok // ""' "true"
    require_equals '.checks.knowledge_sync_ok // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "ai-status.json"
    require_file_listed "ai-query.json"
    require_file_listed "knowledge-upload.json"
    require_file_listed "knowledge-sync.json"
    require_file_listed "ai-metrics.json"
    if [ "$MODE" = "real" ]; then
      require_equals '.checks.query_strategy // ""' "dify"
    fi
    ;;
  weknora)
    require_equals '.status.knowledge_provider // ""' "weknora"
    require_equals '.status.knowledge_provider_enabled // ""' "true"
    require_equals '.status.knowledge_provider_healthy // ""' "true"
    require_equals '.checks.provider_available // ""' "true"
    require_equals '.checks.knowledge_provider_disable_ok // ""' "true"
    require_equals '.checks.knowledge_provider_enable_ok // ""' "true"
    require_equals '.checks.circuit_breaker_reset_ok // ""' "true"
    require_equals '.checks.fallback_query_ok // ""' "true"
    require_equals '.checks.fallback_query_strategy // ""' "fallback"
    require_equals '.checks.knowledge_upload_ok // ""' "true"
    require_equals '.checks.knowledge_sync_ok // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "ai-status.json"
    require_file_listed "ai-query.json"
    require_file_listed "knowledge-provider-disable.json"
    require_file_listed "ai-query-after-disable.json"
    require_file_listed "knowledge-provider-enable.json"
    require_file_listed "circuit-breaker-reset.json"
    require_file_listed "knowledge-upload.json"
    require_file_listed "knowledge-sync.json"
    ;;
  *)
    echo "❌ 不支持的 provider: $PROVIDER" >&2
    exit 1
    ;;
esac

for item in "${CHECKS[@]}"; do
  IFS='|' read -r path want got <<<"$item"
  if [ "$got" != "$want" ]; then
    echo "❌ manifest 校验失败: $path expected=$want got=$got" >&2
    exit 1
  fi
done

echo "✅ manifest 校验通过: provider=$PROVIDER mode=$MODE"
