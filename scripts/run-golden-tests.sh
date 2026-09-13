#!/usr/bin/env bash
# Run the AI golden set regression.
#
# Modes (via SERVIFY_GOLDEN_MODE):
#   mock (default) - pure in-process mocks, zero network, zero secrets;
#                    wired into CI go-checks and `make test-golden`.
#   real           - same fixtures against a real OpenAI-compatible LLM;
#                    requires OPENAI_API_KEY (OPENAI_BASE_URL optional).
set -euo pipefail

MODE="${SERVIFY_GOLDEN_MODE:-mock}"

cd "$(dirname "$0")/../apps/server"

case "$MODE" in
  mock)
    exec go test ./internal/modules/ai/application/ -run 'TestGoldenSet' -count=1 -v
    ;;
  real)
    : "${OPENAI_API_KEY:?OPENAI_API_KEY is required for SERVIFY_GOLDEN_MODE=real}"
    export SERVIFY_GOLDEN_MODE=real
    exec go test ./internal/modules/ai/application/ -run 'TestGoldenSet' -count=1 -v
    ;;
  *)
    echo "unknown SERVIFY_GOLDEN_MODE: $MODE (expected mock|real)" >&2
    exit 2
    ;;
esac
