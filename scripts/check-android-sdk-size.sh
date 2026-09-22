#!/usr/bin/env bash
# M1 验收③ / 设计文档 D9：SDK 集成增量体积门禁。
# 口径：demo（集成 SDK，含全部传递依赖 okhttp/serialization/coroutines/compose）
# 与 demo-baseline（同一宿主 UI、零 SDK）各自 release APK 的字节差 ≤ 1.5MB。
# release 不开 minify——上界口径（minify 只会更小）。
set -euo pipefail

LIMIT=1572864  # 1.5 * 1024 * 1024
ANDROID_DIR="$(cd "$(dirname "$0")/../sdk/android" && pwd)"

cd "$ANDROID_DIR"
./gradlew --no-daemon ":demo:assembleRelease" ":demo-baseline:assembleRelease" >/dev/null

find_apk() {
    find "$1/build/outputs/apk/release" -name '*.apk' 2>/dev/null | head -1
}

DEMO_APK=$(find_apk demo)
BASE_APK=$(find_apk demo-baseline)
if [ -z "$DEMO_APK" ] || [ -z "$BASE_APK" ]; then
    echo "FAIL: 未找到 release APK（demo=${DEMO_APK:-无} baseline=${BASE_APK:-无}）" >&2
    exit 1
fi

DEMO_SIZE=$(stat -c %s "$DEMO_APK")
BASE_SIZE=$(stat -c %s "$BASE_APK")
DELTA=$((DEMO_SIZE - BASE_SIZE))

echo "demo     = ${DEMO_SIZE} bytes ($DEMO_APK)"
echo "baseline = ${BASE_SIZE} bytes ($BASE_APK)"
echo "delta    = ${DELTA} bytes (limit ${LIMIT})"

if [ "$DELTA" -gt "$LIMIT" ]; then
    echo "FAIL: SDK 集成增量 ${DELTA} bytes 超过 1.5MB 门禁" >&2
    exit 1
fi

echo "OK: SDK 集成增量在 D9 门禁内"
