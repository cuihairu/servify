#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "=== Phase 1 Smoke Tests ==="

echo
echo "1/7: Conversation handler — All endpoints"
go test -count=1 -run 'TestConversationWorkspaceHandler_' ./apps/server/internal/handlers/ -v

echo
echo "2/7: Conversation service — Create/Resume/Ingest/List/Assign/Transfer"
go test -count=1 -run 'TestService' ./apps/server/internal/modules/conversation/application/ -v

echo
echo "3/7: Conversation infra — Status mapping"
go test -count=1 -run 'TestMapConversationStatus' ./apps/server/internal/modules/conversation/infra/ -v

echo
echo "4/7: Ticket handler — Create/Get/List/Assign/Bulk"
go test -tags=integration -count=1 -run 'TestTicketHandler_Create_Get_List_Assign' ./apps/server/internal/handlers/ -v

echo
echo "5/7: Ticket handler — Custom Fields + Export"
go test -tags=integration -count=1 -run 'TestTicketHandler_CustomFields' ./apps/server/internal/handlers/ -v

echo
echo "6/7: Ticket handler — Related Conversations"
go test -tags=integration -count=1 -run 'TestTicketHandler_GetRelatedConversations' ./apps/server/internal/handlers/ -v

echo
echo "7/7: Go build — full project"
go build ./...

echo
echo "=== Voice & AI Smoke ==="
go test -tags=integration ./apps/server/internal/handlers -run 'TestVoiceHandler.*Integration' -count=1
go test -tags=integration ./apps/server/internal/modules/ai/delivery -run 'TestOrchestratedEnhancedAIServiceFallback.*Integration' -count=1

echo
echo "=== SDK Examples ==="
if [ -d sdk/scripts ]; then
  # fail-closed：sdk 在就必须过（治理断言失败不允许被静默吞掉）。
  npm -C sdk run test:examples
else
  echo "(SDK examples skipped — no sdk/scripts directory)"
fi

echo
echo "=== All smoke tests passed ==="
