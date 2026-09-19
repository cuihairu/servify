#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CACHE_DIR="$PROJECT_ROOT/.cache/gocache"

export GOCACHE="$CACHE_DIR"
export CGO_ENABLED="${CGO_ENABLED:-1}"

mkdir -p "$CACHE_DIR"
cd "$PROJECT_ROOT"

run_tier() {
  local name="$1"
  shift

  echo ""
  echo "==> Running Go race tier: $name"
  go test -race "$@"
}

run_tier "runtime-and-platform" \
  ./apps/server/internal/app/... \
  ./apps/server/internal/config \
  ./apps/server/internal/metrics \
  ./apps/server/internal/middleware \
  ./apps/server/internal/observability \
  ./apps/server/internal/platform/...

run_tier "modular-domain" \
  ./apps/server/internal/modules/...

run_tier "service-and-delivery" \
  ./apps/server/internal/platform/realtime/... \
  ./apps/server/internal/handlers/...
