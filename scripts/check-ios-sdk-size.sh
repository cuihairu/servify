#!/usr/bin/env bash
# M2 验收② / 设计文档 D9：iOS XCFramework ≤ 2MB。
# 口径：Release 配置 + BUILD_LIBRARY_FOR_DISTRIBUTION 的 iOS 静态 XCFramework，
# 分发形态 zip 后字节数 ≤ 2MB（SwiftUI/Combine 是系统库零增量，预算=自身代码+协议模型）。
# 仅 macOS 可跑（xcodebuild）；Linux 的 ios-swift job 不含此步。
#
# 核查记录（c83199b/ec1c422 两轮 dump 实锤）：SwiftPM 包无 framework target——
# xcodebuild 产物是 Objects-normal/arm64/ 下每源文件一个 .o + 同目录 swiftmodule
# 文件组（ServifyKit.swiftmodule/.swiftinterface/.package.swiftinterface，BLD=YES
# 生成 stable interface）。V1 静态分发：libtool 归档全部 .o + 手工组装 framework
# bundle（Modules/ServifyKit.swiftmodule/ 目录 + FMWK Info.plist）→ create-xcframework。
set -euo pipefail

LIMIT=$((2 * 1024 * 1024))  # 2097152
IOS_DIR="$(cd "$(dirname "$0")/../sdk/ios" && pwd)"

cd "$IOS_DIR"
rm -rf build
DD=build/dd
xcodebuild build \
    -scheme ServifyKit \
    -destination 'generic/platform=iOS' \
    -configuration Release \
    -derivedDataPath "$DD" \
    BUILD_LIBRARY_FOR_DISTRIBUTION=YES \
    SKIP_INSTALL=NO \
    >/dev/null

OBJDIR=$(find "$DD/Build/Intermediates.noindex" -type d -path '*Objects-normal/arm64' | head -1)
if [ -z "$OBJDIR" ] || ! ls "$OBJDIR"/*.o >/dev/null 2>&1; then
    echo "FAIL: 未找到 Objects-normal/arm64 对象目录（产物结构如下）" >&2
    find "$DD/Build" \( -name '*.o' -o -name '*.a' -o -name '*.swiftinterface' \) 2>/dev/null | head -20 >&2
    exit 1
fi

FW=build/ServifyKit.framework
mkdir -p "$FW/Modules/ServifyKit.swiftmodule"
libtool -static -o "$FW/ServifyKit" "$OBJDIR"/*.o
cp "$OBJDIR"/*.swiftmodule "$OBJDIR"/*.swiftinterface "$FW/Modules/ServifyKit.swiftmodule/"
cat > "$FW/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key><string>com.servify.ServifyKit</string>
    <key>CFBundleExecutable</key><string>ServifyKit</string>
    <key>CFBundleName</key><string>ServifyKit</string>
    <key>CFBundlePackageType</key><string>FMWK</string>
    <key>CFBundleShortVersionString</key><string>0.1.0</string>
    <key>CFBundleVersion</key><string>1</string>
    <key>MinimumOSVersion</key><string>15.0</string>
</dict>
</plist>
PLIST

xcodebuild -create-xcframework -framework "$FW" -output build/ServifyKit.xcframework >/dev/null

# -y 保留符号链接（xcframework 内 swiftmodule 结构含 symlink，解包后须可用）
(cd build && zip -qry ServifyKit.xcframework.zip ServifyKit.xcframework)
SIZE=$(wc -c < build/ServifyKit.xcframework.zip)

echo "xcframework(zip) = ${SIZE} bytes (limit ${LIMIT})"
if [ "$SIZE" -gt "$LIMIT" ]; then
    echo "FAIL: XCFramework ${SIZE} bytes 超过 2MB 门禁" >&2
    exit 1
fi

echo "OK: XCFramework 在 D9 门禁内"
