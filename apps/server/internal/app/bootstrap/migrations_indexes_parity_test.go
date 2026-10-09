package bootstrap

// 2026-10-10 运行时索引对账（列形级守卫 TestMigrationChainCoversAllModelColumns
// 的姊妹件）：CreateIndexes 清单的每条索引必须出现在迁移链中，钉住「新加运行
// 时索引必须配编号迁移」。现状口径（先误判后修正）：000001 基线已含全部 15 条
// ——gen-baseline 捕获 CreateIndexes 原始语句（无引号形态）入库，带引号 grep
// 会假阴性误报 MISSING；真正要防的是未来增量：RunStandalone 的 versioned 分支
// 只跑 RunMigrations、从不调用 CreateIndexes（仅 cmd/migrate 与
// cmd/gen-baseline 调），新索引若只写进 CreateIndexes 清单而不配迁移，postgres
// 版本化路径全新库即静默缺索引。本守卫从 migrate.go 源码动态提取清单（新加
// 索引忘配迁移自动报红）。

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	indexListEntryRe = regexp.MustCompile(`"CREATE (?:UNIQUE )?INDEX IF NOT EXISTS ([a-z_0-9]+)`)
	chainIndexRe     = regexp.MustCompile(`(?i)CREATE (?:UNIQUE )?INDEX (?:IF NOT EXISTS )"?([a-z_0-9]+)"?`)
)

// TestMigrationChainCoversCreateIndexes 运行时索引对账：migrate.go
// CreateIndexes 清单的每条索引必须出现在迁移链某条 CREATE INDEX 中
// （新索引必须配编号迁移；基线 000001 已含现有 15 条）。
func TestMigrationChainCoversCreateIndexes(t *testing.T) {
	src, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatalf("read migrate.go: %v", err)
	}
	runtimeIndexes := indexListEntryRe.FindAllStringSubmatch(string(src), -1)
	if len(runtimeIndexes) == 0 {
		t.Fatal("migrate.go 解析出 0 条 CreateIndexes 索引：清单结构变化（语句形态不再是 quoted CREATE INDEX），本测试会假绿，须同步更新提取正则")
	}

	chainIndexes := map[string]bool{}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, m := range chainIndexRe.FindAllStringSubmatch(string(sqlBytes), -1) {
			chainIndexes[m[1]] = true
		}
	}
	if len(chainIndexes) == 0 {
		t.Fatal("迁移链解析出 0 条 CREATE INDEX：embed FS 或正则失灵，本测试会假绿")
	}

	for _, m := range runtimeIndexes {
		if !chainIndexes[m[1]] {
			t.Errorf("CreateIndexes 运行时索引 %q 不在迁移链任何 CREATE INDEX 中：postgres 版本化路径全新库静默缺索引（新索引必须配编号迁移）", m[1])
		}
	}
}
