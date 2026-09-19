#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
RULES_FILE="$ROOT_DIR/scripts/module-boundaries.rules"

has_error=0

if command -v rg >/dev/null 2>&1; then
  SEARCH_CMD=(rg)
  SEARCH_Q=(rg -q)
  SEARCH_Q_MULTILINE=(rg -q --multiline)
else
  SEARCH_CMD=(grep -R -n -E)
  SEARCH_Q=(grep -q -E)
  SEARCH_Q_MULTILINE=(grep -q -E)
fi

require_pattern() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if ! "${SEARCH_Q_MULTILINE[@]}" "$pattern" "$file"; then
    echo "$message"
    has_error=1
  fi
}

forbid_pattern() {
  local file="$1"
  local pattern="$2"
  local message="$3"
  if "${SEARCH_Q_MULTILINE[@]}" "$pattern" "$file"; then
    echo "$message"
    has_error=1
  fi
}

forbid_glob_pattern() {
  local glob="$1"
  local pattern="$2"
  local message="$3"
  if find apps/server/internal/handlers -type f -name "$glob" ! -name '*_test.go' -print0 | xargs -0 "${SEARCH_Q[@]}" "$pattern"; then
    echo "$message"
    has_error=1
  fi
}

check_handler_contract() {
  local label="$1"
  local file="$2"
  local field_name="$3"
  local contract_expr="$4"
  local constructor_pattern="$5"
  local forbidden_pattern="$6"

  require_pattern \
    "$file" \
    "${field_name}[[:space:]]+${contract_expr}" \
    "${label} handler must store ${contract_expr}."
  require_pattern \
    "$file" \
    "$constructor_pattern" \
    "${label} handler constructor must accept ${contract_expr}."
  forbid_pattern \
    "$file" \
    "$forbidden_pattern" \
    "${label} handler must not depend on its concrete legacy service."
}

check_runtime_contract() {
  local label="$1"
  local contract_field="$2"
  local contract_expr="$3"

  require_pattern \
    "apps/server/internal/app/server/router.go" \
    "${contract_field}[[:space:]]+${contract_expr}" \
    "Router dependencies must expose ${contract_expr} for ${label}."
  require_pattern \
    "apps/server/internal/app/server/runtime.go" \
    "${contract_field}[[:space:]]+${contract_expr}" \
    "Runtime must keep ${label} behind ${contract_expr}."
}

if [[ ! -f "$RULES_FILE" ]]; then
  echo "Rules file not found: $RULES_FILE"
  exit 1
fi

while IFS= read -r raw_line || [[ -n "$raw_line" ]]; do
  line="${raw_line#"${raw_line%%[![:space:]]*}"}"
  if [[ -z "$line" || "$line" == \#* ]]; then
    continue
  fi

  IFS='|' read -r kind a b c d e f <<<"$line"
  case "$kind" in
    handler)
      check_handler_contract "$a" "$b" "$c" "$d" "$e" "$f"
      ;;
    runtime)
      check_runtime_contract "$a" "$b" "$c"
      ;;
    require)
      require_pattern "$a" "$b" "$c"
      ;;
    forbid)
      forbid_pattern "$a" "$b" "$c"
      ;;
    *)
      echo "Unknown rule kind in $RULES_FILE: $kind"
      has_error=1
      ;;
  esac
done < "$RULES_FILE"

forbid_glob_pattern \
  '*.go' \
  'servify/apps/server/internal/modules/.*/application' \
  "Handlers must not import modules/*/application directly; route DTOs through delivery contracts."
forbid_glob_pattern \
  '*.go' \
  'servify/apps/server/internal/modules/.*/infra' \
  "Handlers must not import modules/*/infra directly."
forbid_glob_pattern \
  '*.go' \
  'gorm\.io/gorm' \
  "Handlers must not import gorm directly."

forbid_glob_pattern \
  '*.go' \
  'servify/apps/server/internal/services' \
  "Handlers must not import internal/services; consume realtime surfaces via consumer-side narrow contracts."

# 刀 18（P3-2 收官）：internal/services 包已整体删除（realtime 三件迁
# platform/realtime）。此禁令扫全部 go 文件（含测试），防止重建 services
# 包回潮。
if find apps/server -type f -name '*.go' -print0 | xargs -0 "${SEARCH_Q[@]}" 'servify/apps/server/internal/services'; then
  echo "internal/services package was removed (P3-2 knife 18); no go file may reference it."
  has_error=1
fi

if [[ "$has_error" -ne 0 ]]; then
  exit 1
fi

echo "Module boundary checks passed."
