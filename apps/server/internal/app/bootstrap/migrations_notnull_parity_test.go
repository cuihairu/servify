package bootstrap

// 2026-10-10 列 NOT NULL 约束对账（守卫家族第五件：表级→列级→运行时索引级→
// tag 索引级→本件）：MigrationModels() 每个模型 not null tag 字段（schema.Field
// .NotNull）对应的链终态列定义必须含 NOT NULL。缺口实证：api_keys.prefix/
// key_hash 的 not null tag 由 baeea93（09-25）在基线捕获入库同一 commit 时加
// 入模型——基线列无 NOT NULL，后无迁移收紧，postgres 版本化路径全新库可空而
// AutoMigrate/sqlite 路径强制 NOT NULL（000019 修复时同族踩过 B3-1b 的
// NOT NULL 缺失）。修复迁移 000022；本守卫钉住「模型必空列必须配迁移收紧」。
//
// 口径边界：只对 not null tag 显式声明的字段——「非指针字段默认 NOT NULL」是
// dialector DataTypeOf 行为，schema.Field 不暴露，静态面不可见（如需钉住须
// gen-baseline 重捕获 diff，本守卫不覆盖）；列存在性由列级守卫负责（本件遇
// 缺列跳过不重复报）；PRIMARY KEY 语义等价非空。

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// setNotNullRe 匹配「ALTER TABLE "t" ALTER COLUMN "c" SET NOT NULL」收紧语句
// （000022 形态）：链里对既有列的后置收紧等价于列定义 NOT NULL。
var setNotNullRe = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_0-9]+)"?\s+ALTER COLUMN (?:IF EXISTS )?"?([a-z_0-9]+)"?\s+SET NOT NULL`)

// TestMigrationChainCoversModelNotNullColumns NOT NULL 约束对账：每个模型
// not null tag 字段的链终态列定义必须含 NOT NULL（或 PRIMARY KEY）。
func TestMigrationChainCoversModelNotNullColumns(t *testing.T) {
	// 链终态列定义原文（表 → 列 → 定义段，含 CREATE TABLE 段与 ADD COLUMN 段）
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
		for _, m := range columnAlterRe.FindAllStringSubmatch(sqlText, -1) {
			if colDefs[m[1]] == nil {
				continue
			}
			// ADD COLUMN 段：NOT NULL 标记只可能出现在 ADD 之后
			lower := strings.ToLower(m[0])
			if i := strings.Index(lower, "add"); i >= 0 {
				colDefs[m[1]][m[2]] = m[0][i:]
			}
		}
		// 后置收紧（SET NOT NULL）：并入收紧集合，对账时等价列定义 NOT NULL
		for _, m := range setNotNullRe.FindAllStringSubmatch(sqlText, -1) {
			if colDefs[m[1]] != nil {
				colDefs[m[1]][m[2]] += " NOT NULL"
			}
		}
	}
	if len(colDefs) == 0 {
		t.Fatal("迁移链解析出 0 张表列定义：embed FS 或正则失灵，本测试会假绿")
	}

	notNullTotal := 0
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		for _, f := range s.Fields {
			if !f.NotNull || f.DBName == "" {
				continue
			}
			notNullTotal++
			def, ok := colDefs[s.Table][f.DBName]
			if !ok {
				continue // 缺列由 TestMigrationChainCoversAllModelColumns 报，不重复
			}
			if !strings.Contains(def, "NOT NULL") && !strings.Contains(strings.ToUpper(def), "PRIMARY KEY") {
				t.Errorf("模型 %T 的 not null 列 %q 在迁移链终态表 %q 的列定义 %q 中无 NOT NULL：postgres 版本化路径全新库可空，与 AutoMigrate 路径约束漂移（须配迁移收紧，惯例参照 000022）", model, f.DBName, s.Table, strings.TrimSpace(def))
			}
		}
	}
	if notNullTotal == 0 {
		t.Fatal("52 模型解析出 0 个 not null tag 字段：schema.Parse 失灵，本测试会假绿")
	}
}
