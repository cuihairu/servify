package bootstrap

// 2026-10-19 列 NOT NULL 反向对账（NOT NULL 守卫家族第七件，与
// migrations_notnull_parity_test.go 的前向件互补）：前向件只守「模型 not null
// tag → 迁移 NOT NULL」；本件守反向「迁移 NOT NULL → 模型字段声明 not null」。
//
// 缺口实证：000025 的 knowledge_bases.is_public 声明 NOT NULL，模型
// KnowledgeBase.IsPublic 只带 `default:false`（无 not null）。GORM v1.25.7 的
// NOT NULL 只来自 tag（migrator.FullDataTypeOf 仅 `if field.NotNull` 才追加），
// 非指针字段不会隐式 NOT NULL——于是 postgres 上 AutoMigrate 列可空而版本化
// 库 NOT NULL，cmd/schema-parity 运行时门 DRIFT（本地 sqlite PRAGMA 不暴露
// 该面，必须真 postgres 才现形）。本守卫把该面钉成静态门，未来迁移加 NOT NULL
// 不同步模型 tag 时立即红。
//
// 口径边界：只扫模型表（MigrationModels 解析出的表名集合）中含 "NOT NULL" 字面
// 的链终态列定义；inline PRIMARY KEY 的列由主键语义豁免（等价非空）；列存在性与
// 前向 tag 面由列级/前向守卫负责，本件不重复。缺列不报（同前向件口径）。

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// TestMigrationChainNotNullColumnsCoveredByModel NOT NULL 反向对账：每个模型表
// 的迁移链终态 NOT NULL 列，对应模型字段必须声明 not null（或为 primary key）。
func TestMigrationChainNotNullColumnsCoveredByModel(t *testing.T) {
	// 链终态列定义（表 → 列 → 定义段），解析与 family 前向件同构。
	colDefs := map[string]map[string]string{}
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
		sqlText := string(sqlBytes)
		for _, loc := range columnCreateRe.FindAllStringSubmatchIndex(sqlText, -1) {
			rest := sqlText[loc[1]-1:]
			depth, end := 0, -1
			for i, r := range rest {
				if r == '(' {
					depth++
				} else if r == ')' {
					depth--
					if depth == 0 {
						end = i
						break
					}
				}
			}
			if end < 0 {
				t.Fatalf("%s: unbalanced CREATE TABLE body for %q", entry.Name(), sqlText[loc[2]:loc[3]])
			}
			table := sqlText[loc[2]:loc[3]]
			if colDefs[table] == nil {
				colDefs[table] = map[string]string{}
			}
			for _, seg := range splitTopLevel(rest[1:end]) {
				seg = strings.TrimSpace(seg)
				if seg == "" || columnKeywordRe.MatchString(seg) {
					continue
				}
				if cm := columnNameRe.FindStringSubmatch(seg); cm != nil {
					colDefs[table][cm[1]] = seg
				}
			}
		}
		for _, loc := range columnAlterRe.FindAllStringSubmatchIndex(sqlText, -1) {
			table, col := sqlText[loc[2]:loc[3]], sqlText[loc[4]:loc[5]]
			if colDefs[table] == nil {
				continue
			}
			lower := strings.ToLower(sqlText[loc[0]:loc[1]])
			i := strings.Index(lower, "add")
			segText := sqlText[loc[0]+i:]
			if j := strings.IndexAny(segText, ";\n"); j >= 0 {
				segText = segText[:j]
			}
			colDefs[table][col] = segText
		}
		// 后置收紧（SET NOT NULL）等价列定义 NOT NULL，视作本件检查面。
		for _, m := range setNotNullRe.FindAllStringSubmatch(sqlText, -1) {
			if colDefs[m[1]] != nil {
				colDefs[m[1]][m[2]] += " NOT NULL"
			}
		}
	}
	if len(colDefs) == 0 {
		t.Fatal("迁移链解析出 0 张表列定义：embed FS 或正则失灵，本测试会假绿")
	}

	// 模型字段面：表 → 列 → 是否 not null / primary key。
	modelNotNull := map[string]map[string]bool{}
	modelTables := map[string]bool{}
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		modelTables[s.Table] = true
		if modelNotNull[s.Table] == nil {
			modelNotNull[s.Table] = map[string]bool{}
		}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			modelNotNull[s.Table][f.DBName] = f.NotNull || f.PrimaryKey
		}
	}

	checked := 0
	for table, cols := range colDefs {
		if !modelTables[table] {
			continue
		}
		for col, def := range cols {
			upper := strings.ToUpper(def)
			if !strings.Contains(upper, "NOT NULL") {
				continue
			}
			// inline PRIMARY KEY 语义等价非空，豁免。
			if strings.Contains(upper, "PRIMARY KEY") {
				continue
			}
			checked++
			if !modelNotNull[table][col] {
				t.Errorf("模型表 %q 迁移链终态列 %q 含 NOT NULL（%q），但模型对应字段未声明 not null：GORM AutoMigrate 列可空 → postgres 漂移（须加 not null tag 或收紧迁移）", table, col, strings.TrimSpace(def))
			}
		}
	}
	if checked == 0 {
		t.Fatal("迁移链模型表解析出 0 条 NOT NULL 列：正则失灵，本测试会假绿")
	}
}
