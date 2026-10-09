package bootstrap

// 2026-10-10 列 DEFAULT 对账（守卫家族第六件：表级→列级→运行时索引级→tag 索引
// 级→NOT NULL 级→本件，模型 tag 声明的 schema 面在此闭环）：MigrationModels()
// 每个模型 default tag 字段（schema.Field.HasDefaultValue）的链终态列定义必须
// 含 DEFAULT。当前链零缺口（探针验证 52 模型 diff=0，含全部 ADD COLUMN 段与
// 基线段）；本守卫钉住未来增量——新 default 字段只进模型不配迁移（或迁移漏写
// DEFAULT）时，postgres 版本化路径插入行为与 AutoMigrate 路径漂移（AutoMigrate
// 列带 DEFAULT，版本化库无）。
//
// 口径边界：只对 default tag 显式字段；缺列由列级守卫报不重复；GORM 对
// HasDefaultValue 字段在 AutoMigrate 建列时总是写 DEFAULT，语义与本守卫一致。

import (
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// TestMigrationChainCoversModelDefaultColumns DEFAULT 对账：每个模型 default
// tag 字段的链终态列定义必须含 DEFAULT（惯例：基线段同构捕获，增量段参照
// 000008/000016 的 ADD COLUMN ... DEFAULT 形态）。
func TestMigrationChainCoversModelDefaultColumns(t *testing.T) {
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
			// ADD COLUMN 段列定义 = ADD 起点到行尾/分号（columnAlterRe 匹配止于
			// 列名，类型与 DEFAULT 在其后，须扩读原文到语句尾）
			lower := strings.ToLower(sqlText[loc[0]:loc[1]])
			i := strings.Index(lower, "add")
			segText := sqlText[loc[0]+i:]
			if j := strings.IndexAny(segText, ";\n"); j >= 0 {
				segText = segText[:j]
			}
			colDefs[table][col] = segText
		}
	}
	if len(colDefs) == 0 {
		t.Fatal("迁移链解析出 0 张表列定义：embed FS 或正则失灵，本测试会假绿")
	}

	defaultTotal := 0
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		for _, f := range s.Fields {
			// bigserial 自增主键的 DefaultValue 是序列语义（非字面 DEFAULT），
			// 链形态 "id" bigserial 本身携带默认值，豁免
			if !f.HasDefaultValue || f.DBName == "" || f.PrimaryKey {
				continue
			}
			defaultTotal++
			def, ok := colDefs[s.Table][f.DBName]
			if !ok {
				continue // 缺列由 TestMigrationChainCoversAllModelColumns 报，不重复
			}
			if !strings.Contains(strings.ToUpper(def), "DEFAULT") {
				t.Errorf("模型 %T 的 default 列 %q 在迁移链终态表 %q 的列定义 %q 中无 DEFAULT：postgres 版本化路径插入行为与 AutoMigrate 路径漂移（须配迁移补齐）", model, f.DBName, s.Table, strings.TrimSpace(def))
			}
		}
	}
	if defaultTotal == 0 {
		t.Fatal("52 模型解析出 0 个 default tag 字段：schema.Parse 失灵，本测试会假绿")
	}
}
