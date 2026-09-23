#!/usr/bin/env bash
# iOS SDK 覆盖率门禁：Linux 测试面行级 100% 硬断言（M2 口径延续并脚本化）。
# 口径：llvm-cov 对 Linux 编译产物的行计数，剔除豁免面后未覆盖行必须为 0。
#
# 豁免面（与 ACCEPTANCE-M2 同源口径）：
# - Darwin 分支（ServifyChat.create / TicketHTTP fatalError 守卫）：Linux 编译得到
#   但仅 Darwin 执行，由 ios-macos job 的 CreateFactoryTests 覆盖；
# - 防御行（ticket body encode catch）：payload 为 property-list 类型，catch 面
#   无法经任何输入触达。
# 源码内以 "coverage-exempt" 注释锚定，本脚本反向校验行号清单未漂移。
#
# 前置：sdk/ios 下 `swift test --enable-code-coverage` 已生成 profraw
# （CI 的 ios-swift job 测试 step 带该 flag 后执行本脚本）。
set -euo pipefail

IOS_DIR="$(cd "$(dirname "$0")/../sdk/ios" && pwd)"
cd "$IOS_DIR"

command -v llvm-profdata >/dev/null || {
    # swift 工具链自带 llvm-*（工具链 bin 目录与 swift 同级；CI tarball 装在 ~/.swift，
    # 本机 tarball 常见装在 ~/swift）——按序探测
    for d in "$(command -v swift 2>/dev/null | xargs -r dirname)" \
             "$HOME/.swift/usr/bin" "$HOME/swift/usr/bin"; do
        if [ -n "$d" ] && [ -x "$d/llvm-profdata" ]; then
            export PATH="$d:$PATH"
            break
        fi
    done
}
command -v llvm-profdata >/dev/null || { echo "FAIL: 需要 llvm-profdata/llvm-cov（swift 工具链自带）" >&2; exit 1; }

COV_DIR=".build/debug/codecov"
PROFDATA=".build/coverage-merged.profdata"

llvm-profdata merge -sparse "$COV_DIR"/*.profraw -o "$PROFDATA"

python3 - <<'PYEOF'
import subprocess
import sys

PROFDATA = ".build/coverage-merged.profdata"

# ServifyChat.swift 豁免行（1-based，llvm-cov show 行号同源）：
# 22/30/31/33 = create 工厂 Darwin fatalError 面；163-165 = TicketHTTP Darwin 守卫
# （163/164 为锚定注释行，与 fatalError 同 region 故计 0）；
# 187/188 = ticket body encode 防御行（185/186 为其锚定注释行）
LINE_EXEMPT = {"ServifyChat.swift": {22, 30, 31, 33, 163, 164, 165, 185, 186, 187, 188}}
ANCHOR_WINDOW = 14

src_lines = open("Sources/ServifyKit/ServifyChat.swift", encoding="utf-8").read().splitlines()
for lineno in sorted(LINE_EXEMPT["ServifyChat.swift"]):
    lo, hi = max(0, lineno - 1 - ANCHOR_WINDOW), min(len(src_lines), lineno - 1 + ANCHOR_WINDOW + 1)
    if not any("coverage-exempt" in src_lines[i] for i in range(lo, hi)):
        print(f"FAIL: ServifyChat.swift:{lineno} 在豁免清单中但 ±{ANCHOR_WINDOW} 行内无 "
              "coverage-exempt 注释锚定——代码已移动，请更新 LINE_EXEMPT 清单", file=sys.stderr)
        sys.exit(1)

show = subprocess.run(
    ["llvm-cov", "show", ".build/debug/ServifyKitTests.so",
     "-instr-profile=" + PROFDATA,
     "--ignore-filename-regex=(Tests|\\.build|checkouts)"],
    capture_output=True, text=True, check=True).stdout

total = 0
missed_by_file = {}
cur = None
for line in show.splitlines():
    if line.endswith(".swift:") and "/" in line:
        cur = line.rsplit("Sources/ServifyKit/", 1)[-1].rstrip(":")
        continue
    if cur is None or "|" not in line:
        continue
    parts = line.split("|", 2)
    if len(parts) < 2 or not parts[0].strip().isdigit():
        continue
    if not parts[1].strip():  # 无计数行（注释/空白）不计入行级口径
        continue
    total += 1
    if parts[1].strip() == "0":
        lineno = int(parts[0].strip())
        if lineno not in LINE_EXEMPT.get(cur, set()):
            missed_by_file.setdefault(cur, []).append(lineno)

if missed_by_file:
    print("FAIL: 可覆盖面存在未覆盖行（豁免面之外）:", file=sys.stderr)
    for f, ls in missed_by_file.items():
        print(f"  {f}: {ls}", file=sys.stderr)
    sys.exit(1)

print(f"OK: iOS SDK 可覆盖面（Linux 测试面）行级 100%——全量 {total} 行可计数，"
      f"豁免面 {sum(len(v) for v in LINE_EXEMPT.values())} 行清单内，可覆盖面漏 0 行")
PYEOF
