#!/usr/bin/env sh

set -eu

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"

# 历史快照豁免：weknora-acceptance-container 是 2026-09-29 全容器化 weknora 栈
# 一次性子集验收（decb1fa「P1-1 第四笔」），记录的是当时产品行为
# （fallback_query_strategy=fallback）。该值现亦为合法降级形态（编排失败走 legacy
# 兜底，见 test-weknora-integration.sh 的双形态口径），快照本可过校验，但它是
# 一次性历史验收、不追当前行为（docs/acceptance-weknora-docker.md 明确
# retained as history），仍不纳入当前行为门禁；当前 weknora 证据见
# scripts/test-results/weknora-acceptance。
HISTORICAL_SNAPSHOTS="scripts/test-results/weknora-acceptance-container/manifest.json"

MANIFESTS=$(git ls-files 'scripts/test-results/**/manifest.json' 2>/dev/null | sort || true)

if [ -z "$MANIFESTS" ]; then
  echo "No tracked acceptance manifests found under scripts/test-results; skipping."
  exit 0
fi

printf '%s\n' "$MANIFESTS" | while IFS= read -r manifest; do
  [ -n "$manifest" ] || continue
  case " $HISTORICAL_SNAPSHOTS " in
    *" $manifest "*)
      echo "Skipping historical snapshot (retained as history, not a current-behavior gate): $manifest"
      continue
      ;;
  esac
  echo "Validating acceptance manifest: $manifest"
  "$ROOT_DIR/scripts/validate-acceptance-manifest.sh" "$manifest"
done

echo "Acceptance evidence checks passed."
