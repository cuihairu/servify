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
  backup-restore)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.db_restore_matches_backup // ""' "true"
    require_equals '.checks.db_survives_post_backup_damage // ""' "true"
    require_equals '.checks.db_sequence_no_collision // ""' "true"
    require_equals '.checks.files_restore_matches_backup // ""' "true"
    require_equals '.checks.files_verify_detects_tamper // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "db-manifest.json"
    require_file_listed "files-manifest.json"
    ;;
  public-surface)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.security_headers_ok // ""' "true"
    require_equals '.checks.cors_allowlisted_origin_echoed // ""' "true"
    require_equals '.checks.cors_foreign_origin_rejected // ""' "true"
    require_equals '.checks.cors_preflight_vary_origin // ""' "true"
    require_equals '.checks.oversized_body_rejected_413 // ""' "true"
    require_equals '.checks.rate_limit_enforced_429 // ""' "true"
    require_equals '.checks.uploads_file_served // ""' "true"
    require_equals '.checks.uploads_directory_404 // ""' "true"
    require_equals '.checks.websocket_foreign_origin_rejected // ""' "true"
    require_equals '.checks.websocket_allowlisted_origin_admitted // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "headers-health.txt"
    require_file_listed "cors-echo.txt"
    require_file_listed "cors-reject.txt"
    require_file_listed "cors-preflight.txt"
    require_file_listed "body-413.txt"
    require_file_listed "rate-limit-8.txt"
    require_file_listed "uploads-file.txt"
    require_file_listed "uploads-dir.txt"
    require_file_listed "ws-reject.txt"
    require_file_listed "ws-admit.txt"
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
  workspace)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.admin_auth_ok // ""' "true"
    require_equals '.checks.agents_ready // ""' "true"
    require_equals '.checks.visitor_ws_ingress_ok // ""' "true"
    require_equals '.checks.session_created // ""' "true"
    require_equals '.checks.session_detail_ok // ""' "true"
    require_equals '.checks.message_list_ok // ""' "true"
    require_equals '.checks.agent_message_ok // ""' "true"
    require_equals '.checks.agent_message_persisted // ""' "true"
    require_equals '.checks.assign_ok // ""' "true"
    require_equals '.checks.transfer_ok // ""' "true"
    require_equals '.checks.close_ok // ""' "true"
    require_equals '.checks.workspace_overview_ok // ""' "true"
    require_equals '.checks.unknown_session_rejected // ""' "true"
    require_equals '.checks.empty_message_rejected // ""' "true"
    require_equals '.checks.unauthenticated_rejected // ""' "true"
    require_equals '.metrics.status_after_transfer // ""' "transferred"
    require_equals '.metrics.status_after_close // ""' "closed"
    require_file_listed "summary.txt"
    require_file_listed "admin-auth.json"
    require_file_listed "agent-create-primary.json"
    require_file_listed "agent-create-secondary.json"
    require_file_listed "session-detail.json"
    require_file_listed "session-messages-visitor.json"
    require_file_listed "agent-message.json"
    require_file_listed "session-messages-after-agent.json"
    require_file_listed "session-assigned.json"
    require_file_listed "session-transferred.json"
    require_file_listed "session-closed.json"
    require_file_listed "workspace-overview.json"
    require_file_listed "unknown-session.json"
    require_file_listed "empty-message.json"
    require_file_listed "unauthenticated-session.json"
    ;;
  ticket)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.admin_auth_ok // ""' "true"
    require_equals '.checks.agents_ready // ""' "true"
    require_equals '.checks.ticket_created // ""' "true"
    require_equals '.checks.ticket_updated // ""' "true"
    require_equals '.checks.comment_added // ""' "true"
    require_equals '.checks.close_ok // ""' "true"
    require_equals '.checks.closed_state_verified // ""' "true"
    require_equals '.checks.stats_ok // ""' "true"
    require_equals '.checks.export_ok // ""' "true"
    require_equals '.checks.unknown_ticket_rejected // ""' "true"
    require_equals '.checks.create_missing_title_rejected // ""' "true"
    require_equals '.checks.unauthenticated_rejected // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "admin-auth.json"
    require_file_listed "ticket-customer.json"
    require_file_listed "ticket-agent.json"
    require_file_listed "agent-create.json"
    require_file_listed "ticket-created.json"
    require_file_listed "ticket-updated.json"
    require_file_listed "ticket-detail-after-update.json"
    require_file_listed "ticket-comment.json"
    require_file_listed "ticket-close.json"
    require_file_listed "ticket-detail-closed.json"
    require_file_listed "ticket-stats.json"
    require_file_listed "ticket-export.csv"
    require_file_listed "unknown-ticket.json"
    require_file_listed "ticket-create-missing-title.json"
    require_file_listed "unauthenticated-tickets.json"
    ;;
  voice-pstn)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.answer_ok // ""' "true"
    require_equals '.checks.signature_ok // ""' "true"
    require_equals '.checks.bad_signature_rejected // ""' "true"
    require_equals '.checks.missing_signature_rejected // ""' "true"
    require_equals '.checks.unknown_status_rejected // ""' "true"
    require_equals '.checks.late_invite_short_circuit_ok // ""' "true"
    require_equals '.checks.duplicate_invite_ok // ""' "true"
    require_equals '.checks.outbound_ok // ""' "true"
    require_equals '.checks.hangup_ok // ""' "true"
    require_equals '.checks.recording_ok // ""' "true"
    require_equals '.checks.db_call_asserted // ""' "true"
    require_equals '.checks.db_recording_asserted // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "webhook-create.json"
    require_file_listed "receiver-payloads.jsonl"
    ;;
  security-baseline)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.staging_example_rejected // ""' "true"
    require_equals '.checks.production_secure_ok // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "security-staging-rejected.txt"
    require_file_listed "security-production-passed.txt"
    ;;
  runtime-baseline)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.build_ok // ""' "true"
    require_equals '.checks.ready_ok // ""' "true"
    require_equals '.checks.metrics_ok // ""' "true"
    require_equals '.checks.platforms_ok // ""' "true"
    require_equals '.checks.unauthenticated_rejected // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "ready.json"
    require_file_listed "metrics.txt"
    require_file_listed "platforms.json"
    require_file_listed "platforms-unauthorized.json"
    ;;
  ai-fallback)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.build_ok // ""' "true"
    require_equals '.checks.status_ok // ""' "true"
    require_equals '.checks.query_fallback_ok // ""' "true"
    require_equals '.checks.metrics_ok // ""' "true"
    require_equals '.checks.log_evidence_ok // ""' "true"
    require_equals '.checks.unauthenticated_rejected // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "ai-status.json"
    require_file_listed "ai-query.json"
    require_file_listed "ai-metrics.json"
    require_file_listed "ai-query-unauthorized.json"
    require_file_listed "server-log.txt"
    ;;
  suggestion)
    require_equals '.status.overall // ""' "passed"
    require_equals '.checks.build_ok // ""' "true"
    require_equals '.checks.knowledge_docs_ok // ""' "true"
    require_equals '.checks.initial_questions_ok // ""' "true"
    require_equals '.checks.next_questions_get_ok // ""' "true"
    require_equals '.checks.next_questions_post_ok // ""' "true"
    require_equals '.checks.next_questions_reject_ok // ""' "true"
    require_file_listed "summary.txt"
    require_file_listed "admin-auth.json"
    require_file_listed "doc-private.json"
    require_file_listed "doc-public-billing.json"
    require_file_listed "doc-public-password.json"
    require_file_listed "doc-public-agent.json"
    require_file_listed "initial-questions.json"
    require_file_listed "next-questions-get.json"
    require_file_listed "next-questions-post.json"
    require_file_listed "next-questions-missing-query.json"
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
