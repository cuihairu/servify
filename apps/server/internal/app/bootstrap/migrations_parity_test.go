package bootstrap

// 2026-10-07 CI Integration 红修复的整类守卫：postgres 版本化路径（compose
// Integration、生产部署）不跑 GORM AutoMigrate，schema 全靠 migrations/*.up.sql。
// 模型进 MigrationModels() 冻结清单而漏写编号迁移，sqlite 测试路径会建表完全
// 掩盖缺口——全新库上运行时写表 42P01、引用该表的迁移标脏致容器致命循环。
// B1（conversation_events）与 B2-1（routing_assignments）先后踩中同一缺口，
// 修复见 000017 的前置建表节；本测试把「新模型必须配编号迁移」钉成静态对账。

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

var chainCreateTableRe = regexp.MustCompile(`CREATE TABLE (?:IF NOT EXISTS )?"([a-z_]+)"`)

// TestMigrationChainCoversAllModels 静态对账：MigrationModels() 每个模型的
// GORM 表名必须出现在迁移链某张 CREATE TABLE 中（只查表存在性；列/索引漂移
// 由基线再生成清单与 gen-baseline 同源捕获兜底）。扫描面非空断言防 embed FS
// 或正则失灵假绿。
func TestMigrationChainCoversAllModels(t *testing.T) {
	chainTables := map[string]bool{}
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
		for _, m := range chainCreateTableRe.FindAllStringSubmatch(string(sqlBytes), -1) {
			chainTables[m[1]] = true
		}
	}
	if len(chainTables) == 0 {
		t.Fatal("迁移链解析出 0 张表：embed FS 或 CREATE TABLE 正则失灵，本测试会假绿")
	}
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		if !chainTables[s.Table] {
			t.Errorf("模型 %T 的表 %q 不在任何迁移的 CREATE TABLE 中：postgres 版本化路径全新库缺表（新模型必须配编号迁移，惯例参照 000018/000019）", model, s.Table)
		}
	}
}
