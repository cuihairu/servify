#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

failures=0

check_cmd() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then
    echo "[ok] found $name: $(command -v "$name")"
  else
    echo "[missing] $name"
    failures=1
  fi
}

echo "==> Checking required commands"
check_cmd git
check_cmd go
check_cmd sh

echo
echo "==> Checking optional commands"
check_cmd bash
check_cmd node
check_cmd npm
check_cmd make

echo
echo "==> Inspecting shell and path resolution"
if command -v sh >/dev/null 2>&1; then
  echo "sh path: $(command -v sh)"
fi
if command -v bash >/dev/null 2>&1; then
  echo "bash path: $(command -v bash)"
fi
if command -v node >/dev/null 2>&1; then
  echo "node path: $(command -v node)"
fi
if command -v npm >/dev/null 2>&1; then
  echo "npm path: $(command -v npm)"
fi

echo
echo "==> Checking repository hygiene"
bash "$ROOT_DIR/scripts/check-repo-hygiene.sh" || failures=1

echo
echo "==> Checking generated assets manifest"
bash "$ROOT_DIR/scripts/verify-generated-assets.sh" || failures=1

echo
echo "==> Checking git safe.directory status"
repo_safe="false"
if git config --global --get-all safe.directory 2>/dev/null | grep -Fx "$ROOT_DIR" >/dev/null 2>&1; then
  repo_safe="true"
fi
echo "safe.directory contains repo path: $repo_safe"

echo
echo "==> Checking WSL availability"
if command -v wsl >/dev/null 2>&1; then
  echo "wsl command is available"
  wsl -l -v 2>/dev/null || true
else
  echo "wsl command is not available"
fi

if [ -n "${WSL_DISTRO_NAME:-}" ]; then
  echo
  echo "==> WSL environment detected: $WSL_DISTRO_NAME"
  echo "current repo path: $PWD"
  echo "if git reports dubious ownership, run:"
  echo "  git config --global --add safe.directory \"$PWD\""
fi

echo
echo "==> Tool versions"
git --version || true
go version || true
node --version || true
npm --version || true

if [ "$failures" -ne 0 ]; then
  echo
  echo "Local environment check failed."
  exit 1
fi

echo
echo "Local environment check passed."
