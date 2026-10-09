package bootstrap

// 2026-10-10 列形级对账（10-07 表级守卫 TestMigrationChainCoversAllModels 的
// 补齐面）：postgres 版本化路径全新库上，每张模型的每一列都必须出现在迁移链
// 终态（基线 CREATE TABLE 列 ∪ 增量 ADD COLUMN 列 − DROP COLUMN ± RENAME）。
// 表级守卫漏掉列级漂移的实证：AuditLog.PrevHash/EntryHash（R2 哈希链，70ba271
// 加列时未配迁移，09-27~10-07 Integration 断链三天未真正跑完掩盖了它，至今
// 全新 postgres 库写审计日志必 42P01）。
//
// 口径边界（如实声明）：只对账「模型列 ⊆ 迁移链列」方向；chain-only 的
// WeKnora 兼容表（servify_weknora_mappings/knowledge_sync_logs/
// ai_service_metrics，000001 头部注释已声明为非 GORM 表）天然豁免；AutoMigrate
// 对 belongs to 关系自动展开的 FK 列不在 schema.Parse 的 s.Fields 中，该方向
// 盲（冻结清单 52 个模型探针验证零假阳性——均显式声明 FK 字段）。

import (
	"regexp"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

var (
	columnCreateRe  = regexp.MustCompile(`(?s)CREATE TABLE (?:IF NOT EXISTS )?"?([a-z_]+)"?\s*\(`)
	columnAlterRe   = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+ADD (?:COLUMN )?(?:IF NOT EXISTS )?"?([a-z_]+)"?`)
	columnDropTabRe = regexp.MustCompile(`(?i)DROP TABLE (?:IF EXISTS )?"?([a-z_]+)"?`)
	columnDropColRe = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+DROP COLUMN (?:IF EXISTS )?"?([a-z_]+)"?`)
	columnRenameRe  = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+RENAME COLUMN "?([a-z_]+)"? TO "?([a-z_]+)"?`)
	columnKeywordRe = regexp.MustCompile(`(?i)^(PRIMARY|CONSTRAINT|UNIQUE|FOREIGN|CHECK|INDEX|KEY)\b`)
	columnNameRe    = regexp.MustCompile(`^"?([a-z_][a-z0-9_]*)"?`)
)

// columnSetFromChain 解析迁移链为「表 → 列集合」终态（编号序执行，DROP/RENAME 参与终态演化）。
func columnSetFromChain(t *testing.T) map[string]map[string]bool {
	t.Helper()
	chain := map[string]map[string]bool{}
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
		for _, m := range columnDropTabRe.FindAllStringSubmatch(sqlText, -1) {
			delete(chain, m[1])
		}
		for _, loc := range columnCreateRe.FindAllStringSubmatchIndex(sqlText, -1) {
			// 匹配串以 '(' 收尾：loc[1]-1 指向该 '('，体内为括号后的内容
			rest := sqlText[loc[1]-1:]
			depth := 0
			end := -1
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
			if chain[table] == nil {
				chain[table] = map[string]bool{}
			}
			// 体内按括号深度 0 逗号分段（varchar(64)/numeric(10,2) 等类型内的逗号不打断分段）
			for _, seg := range splitTopLevel(rest[1:end]) {
				seg = strings.TrimSpace(seg)
				if seg == "" || columnKeywordRe.MatchString(seg) {
					continue
				}
				if cm := columnNameRe.FindStringSubmatch(seg); cm != nil {
					chain[table][cm[1]] = true
				}
			}
		}
		for _, m := range columnAlterRe.FindAllStringSubmatch(sqlText, -1) {
			if chain[m[1]] == nil {
				chain[m[1]] = map[string]bool{}
			}
			chain[m[1]][m[2]] = true
		}
		for _, m := range columnDropColRe.FindAllStringSubmatch(sqlText, -1) {
			delete(chain[m[1]], m[2])
		}
		for _, m := range columnRenameRe.FindAllStringSubmatch(sqlText, -1) {
			if chain[m[1]] != nil && chain[m[1]][m[2]] {
				delete(chain[m[1]], m[2])
				chain[m[1]][m[3]] = true
			}
		}
	}
	if len(chain) == 0 {
		t.Fatal("迁移链解析出 0 张表：embed FS 或 CREATE TABLE 正则失灵，本测试会假绿")
	}
	return chain
}

// splitTopLevel 按括号深度 0 的逗号分段。
func splitTopLevel(s string) []string {
	var segs []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				segs = append(segs, s[start:i])
				start = i + 1
			}
		}
	}
	segs = append(segs, s[start:])
	return segs
}

// TestMigrationChainCoversAllModelColumns 列形级对账：MigrationModels() 每个模型
// 的每个 GORM 列必须出现在迁移链终态列集合中（新列必须配编号迁移，惯例参照
// 000020 / 000008 / 000016）。
func TestMigrationChainCoversAllModelColumns(t *testing.T) {
	chain := columnSetFromChain(t)
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		chainCols := chain[s.Table]
		if chainCols == nil {
			t.Errorf("模型 %T 的表 %q 不在迁移链任何 CREATE TABLE 中（表级缺口，按 000017 惯例配前置建表节）", model, s.Table)
			continue
		}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			if !chainCols[f.DBName] {
				t.Errorf("模型 %T 的列 %q 不在迁移链终态表 %q 的列集合中：postgres 版本化路径全新库缺列，运行时写表必 42P01（新列必须配编号迁移，惯例参照 000020）", model, f.DBName, s.Table)
			}
		}
	}
}
