#!/bin/bash

# Servify Unit Tests Runner
# 运行所有单元测试并生成覆盖率报告

set -e

echo "🧪 Running Servify Unit Tests..."
echo "================================"

# 确保在项目根目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
OUT_DIR="$SCRIPT_DIR/test-results"
GOWORK_CACHE_DIR="$PROJECT_ROOT/.cache"
export GOCACHE="$GOWORK_CACHE_DIR/gocache"
cd "$PROJECT_ROOT"

# 创建测试输出目录
mkdir -p "$OUT_DIR" "$GOWORK_CACHE_DIR"

# 运行所有 apps/server 模块测试（不仅 services/handlers）。
# -tags integration：仓库的大块集成测试（37 个文件，sqlite 内存库自足）
# 挂在 go:build integration 后，覆盖率门禁必须带上它们才真实——否则新
# 批次代码在统计里裸奔，100% 门禁形同虚设。
echo "📊 Running tests with coverage (no race, tags=integration)..."
go test -v -tags integration -coverprofile="$OUT_DIR/coverage.out" ./apps/server/...

# 生成覆盖率HTML报告
echo "📈 Generating coverage report..."
go tool cover -html="$OUT_DIR/coverage.out" -o "$OUT_DIR/coverage.html"

# 显示覆盖率概要
echo "📋 Coverage Summary:"
go tool cover -func="$OUT_DIR/coverage.out" | tail -1

# 全仓覆盖率收口刀：非 internal 的可测包（perfbench 负载引擎 / weknora-mock
# 协议 mock）与 internal 同等口径直跑（各包单测自证 100% 语句覆盖）。
echo "🧩 Running non-internal package tests (perfbench / weknora-mock)..."
go test -cover ./scripts/perfbench ./infra/compose/weknora-mock

# 运行基准测试
echo ""
echo "⚡ Running benchmark tests..."
go test -bench=. -benchmem ./apps/server/... > "$OUT_DIR/benchmark.txt"

echo ""
echo "✅ Test run completed!"
echo "📁 Results saved to $OUT_DIR"
echo "  - coverage.out: Raw coverage data"
echo "  - coverage.html: Coverage report (open in browser)"
echo "  - benchmark.txt: Benchmark results"

# 覆盖率阈值（默认 20%，可通过 TEST_COVERAGE_TARGET 环境变量覆盖）
COVERAGE=$(go tool cover -func="$OUT_DIR/coverage.out" | tail -1 | awk '{print $3}' | sed 's/%//')
TARGET=${TEST_COVERAGE_TARGET:-20.0}

echo ""
echo "🎯 Coverage Target: ${TARGET}%"
echo "📊 Actual Coverage: ${COVERAGE}%"

# 使用awk进行浮点数比较（避免bc依赖）
if awk "BEGIN {exit !($COVERAGE >= $TARGET)}"; then
	echo "✅ Coverage target achieved!"
	exit 0
else
	echo "❌ Coverage below target. Need to add more tests."
	exit 1
fi
