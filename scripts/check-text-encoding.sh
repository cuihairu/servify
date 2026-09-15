#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# 仓库文本编码门禁：tracked 文本文件必须是 UTF-8，且不得携带 BOM 或 U+FFFD。
# 历史 mojibake（todo.md / README.md / admin 核心页面等）已于 2026-09 前清理，
# 本脚本防止在评审、交接、补丁流程中再次引入编码异常。

python3 - <<'PY'
import subprocess
import sys

BINARY_EXTENSIONS = (
    ".png", ".jpg", ".jpeg", ".gif", ".ico", ".webp",
    ".woff", ".woff2", ".ttf", ".eot", ".otf",
    ".mp3", ".mp4", ".pdf", ".zip", ".gz",
)

listing = subprocess.run(
    ["git", "ls-files", "-z"], capture_output=True, check=True
).stdout
paths = [p.decode("utf-8", "replace") for p in listing.split(b"\0") if p]

failures = []
scanned = 0
for path in paths:
    if path.lower().endswith(BINARY_EXTENSIONS):
        continue
    try:
        with open(path, "rb") as fh:
            data = fh.read()
    except OSError:
        continue
    scanned += 1
    issues = []
    if data.startswith(b"\xef\xbb\xbf"):
        issues.append("UTF-8 BOM")
    if b"\xef\xbf\xbd" in data:
        issues.append("U+FFFD replacement character")
    try:
        data.decode("utf-8")
    except UnicodeDecodeError as exc:
        issues.append(f"invalid UTF-8 at byte {exc.start}")
    if issues:
        failures.append(f"{path}: {', '.join(issues)}")

if failures:
    print("Text encoding check FAILED:")
    for line in failures:
        print(f"  {line}")
    sys.exit(1)

print(f"Text encoding check passed: {scanned} tracked text files are UTF-8 without BOM or U+FFFD.")
PY
