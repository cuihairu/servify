#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Backup & restore drill acceptance 测试开始..."

EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/backup-restore"}

# 演练本体与 manifest 写入都在 Go 测试(TestBackupRestoreDrillSQLite)内完成,
# 脚本负责清场、驱动与留档校验。先移除旧留档:演练失败时不会把上一轮的
# manifest 误当本轮证据(check-acceptance-evidence.sh 按文件校验)。
rm -rf "$EVIDENCE_DIR"
mkdir -p "$EVIDENCE_DIR"

DRILL_RC=0
DRILL_LOG=$(mktemp)
cleanup() {
  if [ "$DRILL_RC" -ne 0 ]; then
    tail -n 40 "$DRILL_LOG" >&2 || true
  fi
  rm -f "$DRILL_LOG"
}
trap cleanup EXIT

EVIDENCE_DIR="$EVIDENCE_DIR" go -C "$PROJECT_ROOT/apps/server" test \
  ./internal/platform/recovery -run TestBackupRestoreDrillSQLite -count=1 -v \
  > "$DRILL_LOG" 2>&1 || DRILL_RC=$?

if [ "$DRILL_RC" -ne 0 ]; then
  echo "❌ 演练执行失败,日志尾部见上" >&2
  exit 1
fi

# manifest 逐项校验(provider=backup-restore 的 checks 与 evidence_files)。
"$SCRIPT_DIR/validate-acceptance-manifest.sh" "$EVIDENCE_DIR/manifest.json"

# summary 步骤对账:七步全部在案才认为演练完整走通。
for step in seeded db-backup damage db-restore files-backup files-damage files-restore; do
  if ! grep -q "^step=$step " "$EVIDENCE_DIR/summary.txt"; then
    echo "❌ summary.txt 缺少演练步骤: $step" >&2
    exit 1
  fi
done

echo "✅ Backup & restore drill acceptance 通过: 数据库与上传资产恢复对账一致,留档于 $EVIDENCE_DIR"
