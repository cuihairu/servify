#!/usr/bin/env bash
# M2 验收② / 设计文档 D9：iOS XCFramework ≤ 2MB。
# 口径：Release 配置 + BUILD_LIBRARY_FOR_DISTRIBUTION 的 iOS XCFramework，
# 分发形态 zip 后字节数 ≤ 2MB（SwiftUI/Combine 是系统库零增量，预算=自身代码+协议模型）。
# 仅 macOS 可跑（xcodebuild）；Linux 的 ios-swift job 不含此步。
set -euo pipefail

LIMIT=$((2 * 1024 * 1024))  # 2097152
IOS_DIR="$(cd "$(dirname "$0")/../sdk/ios" && pwd)"

cd "$IOS_DIR"
rm -rf build
xcodebuild build \
    -scheme ServifyKit \
    -destination 'generic/platform=iOS' \
    -configuration Release \
    -derivedDataPath build/dd \
    BUILD_LIBRARY_FOR_DISTRIBUTION=YES \
    SKIP_INSTALL=NO \
    >/dev/null

FRAMEWORK=$(find build/dd/Build/Products -maxdepth 2 -name 'ServifyKit.framework' -type d | head -1)
if [ -z "$FRAMEWORK" ]; then
    echo "FAIL: 未找到 ServifyKit.framework 构建产物" >&2
    exit 1
fi

xcodebuild -create-xcframework -framework "$FRAMEWORK" -output build/ServifyKit.xcframework >/dev/null

# -y 保留符号链接（xcframework 内 swiftmodule 结构含 symlink，解包后须可用）
(cd build && zip -qry ServifyKit.xcframework.zip ServifyKit.xcframework)
SIZE=$(wc -c < build/ServifyKit.xcframework.zip)

echo "xcframework(zip) = ${SIZE} bytes (limit ${LIMIT})"
if [ "$SIZE" -gt "$LIMIT" ]; then
    echo "FAIL: XCFramework ${SIZE} bytes 超过 2MB 门禁" >&2
    exit 1
fi

echo "OK: XCFramework 在 D9 门禁内"
