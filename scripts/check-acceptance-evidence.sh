#!/usr/bin/env sh

set -eu

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

MANIFESTS=$(git ls-files 'scripts/test-results/**/manifest.json' 2>/dev/null | sort || true)

if [ -z "$MANIFESTS" ]; then
  echo "No tracked acceptance manifests found under scripts/test-results; skipping."
  exit 0
fi

printf '%s\n' "$MANIFESTS" | while IFS= read -r manifest; do
  [ -n "$manifest" ] || continue
  echo "Validating acceptance manifest: $manifest"
  "$ROOT_DIR/scripts/validate-acceptance-manifest.sh" "$manifest"
done

echo "Acceptance evidence checks passed."
