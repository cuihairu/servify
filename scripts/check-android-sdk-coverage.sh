#!/usr/bin/env bash
# Android SDK 覆盖率门禁：JVM 单测可达面（逻辑层）100% 行级硬断言。
# 口径：jacoco report.xml 的 LINE 计数，剔除豁免面后 missed 必须为 0。
#
# 豁免面（Android framework/渲染层，JVM 单测无此运行时；D9 白名单冻结不引
# Robolectric），由编译期（build 任务）+ 真机手工项（sdk/ACCEPTANCE-*.md）覆盖：
# - 类级：ui/ 包（Compose 渲染）、EntryOrchestrator（Activity/View 桥）、
#   FloatingButtonView（自定义 View 绘制）
# - 行级：ServifyChat.kt 的 show/hide 桥接、send-rejected（okhttp 状态机不可注入）、
#   onClosed（MockWebServer close 族传播全断）——源码内以 "coverage-exempt" 注释
#   锚定，本脚本反向校验行号清单未漂移。
#
# 前置：./gradlew :servify-sdk:createDebugUnitTestCoverageReport 已生成报告
# （CI 在同一 job 的 build step 后执行；本地单独跑见脚本尾部提示）。
set -euo pipefail

MODULE_DIR="$(cd "$(dirname "$0")/../sdk/android/servify-sdk" && pwd)"
REPORT="$MODULE_DIR/build/reports/coverage/test/debug/report.xml"

if [ ! -f "$REPORT" ]; then
    echo "FAIL: 未找到覆盖率报告 $REPORT" >&2
    echo "先跑: cd sdk/android && ./gradlew :servify-sdk:createDebugUnitTestCoverageReport" >&2
    exit 1
fi

python3 - "$REPORT" "$MODULE_DIR" <<'PYEOF'
import sys
import xml.etree.ElementTree as ET

report_path, module_dir = sys.argv[1], sys.argv[2]

# 全文件豁免（framework/渲染层，JVM 单测无此运行时，见脚本头注释）；
# 按文件名匹配（EntryOrchestrator/FloatingButtonView 与 ServifyChat 同包，无法按包剔）
FILE_EXEMPT = {"ChatPanel.kt", "TicketForm.kt", "ChatText.kt", "ChatTheme.kt",
               "LazyDsl.kt", "EntryOrchestrator.kt", "FloatingButtonView.kt"}
# ServifyChat.kt 豁免行（1-based，与 jacoco report 行号同源）
LINE_EXEMPT = {"ServifyChat.kt": {149, 150, 151, 156, 157, 177, 178, 179, 503, 506, 507, 508}}
ANCHOR_WINDOW = 12  # 豁免行 ±N 行内必须出现 coverage-exempt 注释（防清单漂移）

tree = ET.parse(report_path)
root = tree.getroot()


def line_counter(el):
    for c in el.findall("counter"):
        if c.get("type") == "LINE":
            return int(c.get("missed")), int(c.get("covered"))
    return 0, 0


# --- 漂移校验：豁免行必须贴近 coverage-exempt 注释 ---
src_lines = open(f"{module_dir}/src/main/kotlin/servify/sdk/android/ServifyChat.kt",
                 encoding="utf-8").read().splitlines()
for lineno in sorted(LINE_EXEMPT["ServifyChat.kt"]):
    lo, hi = max(0, lineno - 1 - ANCHOR_WINDOW), min(len(src_lines), lineno - 1 + ANCHOR_WINDOW + 1)
    if not any("coverage-exempt" in src_lines[i] for i in range(lo, hi)):
        print(f"FAIL: ServifyChat.kt:{lineno} 在豁免清单中但 ±{ANCHOR_WINDOW} 行内无 coverage-exempt 注释锚定"
              "——代码已移动，请更新 LINE_EXEMPT 清单", file=sys.stderr)
        sys.exit(1)

# --- 逐包断言（类级豁免整类剔除） ---
total_missed = 0
gaps = []
for pkg in root.iter("package"):
    pname = pkg.get("name")
    for sf in pkg.findall("sourcefile"):
        fname = sf.get("name")
        missed = sorted(int(ln.get("nr")) for ln in sf.findall("line") if int(ln.get("ci")) == 0)
        if not missed:
            continue
        if fname in FILE_EXEMPT:
            continue
        if fname in LINE_EXEMPT:
            missed = [n for n in missed if n not in LINE_EXEMPT[fname]]
        if missed:
            total_missed += len(missed)
            gaps.append(f"{pname}/{fname}: 未覆盖行 {missed}")

if total_missed:
    print(f"FAIL: 可覆盖面存在 {total_missed} 行未覆盖（豁免面之外）:", file=sys.stderr)
    for g in gaps:
        print(f"  {g}", file=sys.stderr)
    sys.exit(1)

tm, tc = line_counter(root)
exempt = tm  # 全量 missed 全部来自豁免面（否则上面已 FAIL）
print(f"OK: Android SDK 可覆盖面（JVM 单测可达）行级 100%——全量 {tm + tc} 行，"
      f"豁免面 {exempt} 行（framework/渲染层，见脚本头注释），可覆盖面漏 0 行")
PYEOF
