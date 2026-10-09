package bootstrap

// 2026-10-10 模型 tag 索引对账（守卫家族第三件：表级
// TestMigrationChainCoversAllModels → 列级 TestMigrationChainCoversAllModelColumns
// → 索引级 TestMigrationChainCoversCreateIndexes → 本件）：MigrationModels()
// 每个模型经 GORM index/uniqueIndex tag 声明的索引必须出现在迁移链中。缺口实证：
// WaitingRecord.TargetGroupID（gorm:"index"）由 000005 补列时漏配索引——postgres
// 版本化路径全新库（不跑 AutoMigrate）该列无索引，AutoMigrate/sqlite 路径有，
// 两路径形态不一致。修复迁移 000021 补齐；本守卫钉住「模型 tag 索引必须配迁移」。
//
// 口径边界：只对账模型 tag 声明的索引（AutoMigrate 建的面）；CreateIndexes 运行
// 时清单由姊妹守卫 TestMigrationChainCoversCreateIndexes 钉；belongs to 自动 FK
// 的隐式索引方向盲（与列级守卫同族边界，冻结清单探针验证零假阳性）。

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// TestMigrationChainCoversAllModelTagIndexes 模型 tag 索引对账：每个模型的
// schema.Parse ParseIndexes() 索引名必须出现在迁移链某条 CREATE INDEX 中
// （新 tag 索引必须配编号迁移，惯例参照 000021 / 基线 000001 的 tag 索引段）。
func TestMigrationChainCoversAllModelTagIndexes(t *testing.T) {
	chainIndexes := map[string]bool{}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	for _, entry := range entries {
		if len(entry.Name()) < 7 || entry.Name()[len(entry.Name())-7:] != ".up.sql" {
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

	tagIndexTotal := 0
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		for name := range s.ParseIndexes() {
			tagIndexTotal++
			if !chainIndexes[name] {
				t.Errorf("模型 %T 的 tag 索引 %q 不在迁移链任何 CREATE INDEX 中：postgres 版本化路径全新库缺索引（新 tag 索引必须配编号迁移，惯例参照 000021）", model, name)
			}
		}
	}
	if tagIndexTotal == 0 {
		t.Fatal("52 模型解析出 0 条 tag 索引：schema.Parse 失灵，本测试会假绿")
	}
}
