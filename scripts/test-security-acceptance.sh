#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "🧪 Security baseline acceptance 测试开始..."

SECURITY_ACCEPTANCE_MODE=${SECURITY_ACCEPTANCE_MODE:-"real"}
EVIDENCE_DIR=${EVIDENCE_DIR:-"$PROJECT_ROOT/scripts/test-results/security-baseline"}
CONFIG_TEMPLATE=${CONFIG_TEMPLATE:-"$PROJECT_ROOT/config.production.secure.example.yml"}
STAGING_CONFIG=${STAGING_CONFIG:-"$PROJECT_ROOT/config.staging.example.yml"}
DEFAULT_CONFIG=${DEFAULT_CONFIG:-"$PROJECT_ROOT/config.yml"}

mkdir -p "$EVIDENCE_DIR"

STAGING_REJECTED=false
PRODUCTION_SECURE_OK=false
DEFAULT_CONFIG_STRICT_PASSED=false
OVERALL_STATUS=failed

append_summary() {
  printf '%s\n' "$1" >> "$EVIDENCE_DIR/summary.txt"
}

# run_baseline_strict 用给定配置执行严格模式安全基线校验,
# 输出原样留档到 $2,$3 起为附加 KEY=VALUE 环境变量(env 前缀传递,避免
# bash 前置赋值作用于函数时的行为差异)。退出码通过全局变量 BASELINE_RC 返回。
BASELINE_RC=0
run_baseline_strict() {
  local config_path=$1
  local evidence_name=$2
  shift 2
  BASELINE_RC=0
  env GIN_MODE=release "$@" go -C "$PROJECT_ROOT/apps/server" run ./cmd \
    -c "$config_path" check-security-baseline --strict \
    > "$EVIDENCE_DIR/$evidence_name" 2>&1 || BASELINE_RC=$?
}

# 生产安全模板的 database 节自 P2-2 起显式存在(${DB_*} 占位符,由配置
# 模板漂移门禁强制)——部署语义是纯环境变量注入,不再追加 deployment 段
# (追加会产生重复 yaml 键)。此处只注入非默认的 acceptance 占位凭证。

write_manifest() {
  MANIFEST_MODE="${SECURITY_ACCEPTANCE_MODE:-unknown}" \
  MANIFEST_CONFIG_TEMPLATE="${CONFIG_TEMPLATE#$PROJECT_ROOT/}" \
  MANIFEST_OVERALL_STATUS="${OVERALL_STATUS:-unknown}" \
  MANIFEST_STAGING_REJECTED="${STAGING_REJECTED:-false}" \
  MANIFEST_PRODUCTION_SECURE_OK="${PRODUCTION_SECURE_OK:-false}" \
  MANIFEST_DEFAULT_CONFIG_STRICT_PASSED="${DEFAULT_CONFIG_STRICT_PASSED:-false}" \
  python3 - "$EVIDENCE_DIR/manifest.json" <<'PY'
import json
import os
import sys

out = sys.argv[1]
evidence_dir = os.path.dirname(out)
payload = {
    "provider": "security-baseline",
    "mode": os.environ.get("MANIFEST_MODE", "unknown"),
    "config_template": os.environ.get("MANIFEST_CONFIG_TEMPLATE", ""),
    "status": {
        "overall": os.environ.get("MANIFEST_OVERALL_STATUS", "unknown"),
    },
    "checks": {
        "staging_example_rejected": os.environ.get("MANIFEST_STAGING_REJECTED", "false"),
        "production_secure_ok": os.environ.get("MANIFEST_PRODUCTION_SECURE_OK", "false"),
        "default_config_strict_passed": os.environ.get("MANIFEST_DEFAULT_CONFIG_STRICT_PASSED", "false"),
    },
    "evidence_files": sorted(
        name for name in os.listdir(evidence_dir)
        if name != "manifest.json" and os.path.isfile(os.path.join(evidence_dir, name))
    ),
}
with open(out, "w", encoding="utf-8") as fh:
    json.dump(payload, fh, ensure_ascii=False, indent=2)
    fh.write("\n")
PY
}
trap write_manifest EXIT

if [ ! -f "$CONFIG_TEMPLATE" ]; then
  echo "❌ 配置模板不存在: $CONFIG_TEMPLATE" >&2
  exit 1
fi

# 1) 负例:staging 示例配置必须被 strict 拒绝。注入基础设施凭证(${DB_*}/
#    ${SERVIFY_JWT_SECRET})让占位符过启动校验,但不注入 AI provider 凭证——
#    空的 ai.openai.api_key / dify.api_key / dify.dataset_id 被基线检查点名,
#    证明示例模板未经完整凭证注入不允许上预生产。
append_summary "step=staging_example_strict"
run_baseline_strict "$STAGING_CONFIG" "security-staging-rejected.txt" \
  "SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-security-acceptance-jwt-secret}" \
  "DB_HOST=${DB_HOST:-db.internal}" \
  "DB_USER=${DB_USER:-servify}" \
  "DB_NAME=${DB_NAME:-servify}" \
  "DB_PASSWORD=${DB_PASSWORD:-security-acceptance-only-password}"
if [ "$BASELINE_RC" -ne 0 ] && grep -qE "config validation failed|Security baseline check found" "$EVIDENCE_DIR/security-staging-rejected.txt"; then
  STAGING_REJECTED=true
  append_summary "staging_example_rejected=true"
else
  append_summary "staging_example_rejected=false"
fi

# 2) 正例:生产安全模板 + ${DB_*}/${SERVIFY_JWT_SECRET} 等环境变量注入,
#    strict 必须通过(与模板 ${ENV} 占位符一一对应,不改动配置文件本身)
append_summary "step=production_secure_strict"
run_baseline_strict "$CONFIG_TEMPLATE" "security-production-passed.txt" \
  "SERVIFY_JWT_SECRET=${SERVIFY_JWT_SECRET:-security-acceptance-jwt-secret}" \
  "OPENAI_API_KEY=${OPENAI_API_KEY:-security-acceptance-openai-key}" \
  "DIFY_API_KEY=${DIFY_API_KEY:-security-acceptance-dify-key}" \
  "DIFY_DATASET_ID=${DIFY_DATASET_ID:-security-acceptance-dataset-id}" \
  "DB_HOST=${DB_HOST:-db.internal}" \
  "DB_USER=${DB_USER:-servify}" \
  "DB_NAME=${DB_NAME:-servify}" \
  "DB_PASSWORD=${DB_PASSWORD:-security-acceptance-only-password}"
if [ "$BASELINE_RC" -eq 0 ] && grep -q "Security baseline check passed" "$EVIDENCE_DIR/security-production-passed.txt"; then
  PRODUCTION_SECURE_OK=true
  append_summary "production_secure_ok=true"
else
  append_summary "production_secure_ok=false"
fi

# 3) 口径留档:默认开发配置(config.yml)在 strict 下的结果只记录,不作硬性条件
if [ -f "$DEFAULT_CONFIG" ]; then
  append_summary "step=default_config_strict"
  run_baseline_strict "$DEFAULT_CONFIG" "security-default-config.txt"
  if [ "$BASELINE_RC" -eq 0 ]; then
    DEFAULT_CONFIG_STRICT_PASSED=true
  fi
  append_summary "default_config_strict_passed=$DEFAULT_CONFIG_STRICT_PASSED"
fi

if [ "$STAGING_REJECTED" != "true" ] || [ "$PRODUCTION_SECURE_OK" != "true" ]; then
  append_summary "overall_status=failed"
  OVERALL_STATUS=failed
  echo "❌ Security baseline acceptance 未通过" >&2
  exit 1
fi

OVERALL_STATUS=passed
append_summary "overall_status=passed"
echo "✅ Security baseline acceptance 通过: staging 负例被拒绝,生产安全配置 strict 校验通过"
