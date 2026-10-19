package bootstrap

// 2026-10-20e SQL 类型级对账（migrations_columns_parity_test.go 只对账列名
// 存在性的补齐面）：postgres 版本化路径下，迁移链终态的每列 SQL 类型必须等于
// GORM AutoMigrate 会产出的类型。实证：000029 long_poll_ms 把 Go int 误写成
// integer（GORM postgres 对 int 产 bigint），sqlite 无 int/bigint 之分、列名
// 守卫不看类型，静态面全绿照推，CI Integration 真库 schema-parity 当轮红
// （run 39336584235）。本守卫把该类漂移拉回本地。
//
// 口径边界（如实声明）：与 cmd/schema-parity 的运行时真库对账是同一纪律的
// 两半——本守卫静态快、可本地跑，但只覆盖「类型」一维；nullable/DEFAULT/
// 数值精度仍由 CI Integration 兜底。已接受的漂移走 exceptions 白名单，条目
// 与 cmd/schema-parity/main.go 的 allowlist 类型子集一一对应，两边必须一起
// 复审（这里红而那边绿 = 白名单漂移，先对表再改）。

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

var (
	columnTypeCreateRe = regexp.MustCompile(`(?s)CREATE TABLE (?:IF NOT EXISTS )?"?([a-z_]+)"?\s*\(`)
	// 类型 token：词 + 可选 " precision"（double precision）+ 可选 (N[,M])（varchar(64)/vector(1536)/numeric(10,2)）。
	columnTypeTokenRe   = regexp.MustCompile(`(?i)^"?([a-z_][a-z0-9_]*)"?\s+([a-z]+(?:\s+precision)?(?:\(\d+(?:,\s*\d+)?\))?)`)
	columnTypeAlterRe   = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+ADD (?:COLUMN )?(?:IF NOT EXISTS )?"?([a-z_]+)"?\s+([a-z]+(?:\s+precision)?(?:\(\d+(?:,\s*\d+)?\))?)`)
	columnTypeRenameRe  = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+RENAME COLUMN "?([a-z_]+)"? TO "?([a-z_]+)"?`)
	columnTypeDropColRe = regexp.MustCompile(`(?i)ALTER TABLE (?:IF EXISTS )?"?([a-z_]+)"?\s+DROP COLUMN (?:IF EXISTS )?"?([a-z_]+)"?`)
	columnTypeDropTabRe = regexp.MustCompile(`(?i)DROP TABLE (?:IF EXISTS )?"?([a-z_]+)"?`)
	columnTypeKeywordRe = regexp.MustCompile(`(?i)^(PRIMARY|CONSTRAINT|UNIQUE|FOREIGN|CHECK|INDEX|KEY)\b`)
)

// columnTypesFromChain 解析迁移链为「表 → 列 → SQL 类型」终态。类型归一：
// decimal/numeric 是 postgres 同一类型（OID 1700）的两个拼法，归并比较。
func columnTypesFromChain(t *testing.T) map[string]map[string]string {
	t.Helper()
	chain := map[string]map[string]string{}
	normalize := func(tok string) string {
		tok = strings.ToLower(tok)
		if tok == "decimal" {
			return "numeric"
		}
		return tok
	}
	add := func(table, column, typ string) {
		if chain[table] == nil {
			chain[table] = map[string]string{}
		}
		chain[table][column] = typ
	}
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
		for _, m := range columnTypeDropTabRe.FindAllStringSubmatch(sqlText, -1) {
			delete(chain, m[1])
		}
		for _, loc := range columnTypeCreateRe.FindAllStringSubmatchIndex(sqlText, -1) {
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
			for _, seg := range splitTopLevel(rest[1:end]) {
				seg = strings.TrimSpace(seg)
				if seg == "" || columnTypeKeywordRe.MatchString(seg) {
					continue
				}
				if tm := columnTypeTokenRe.FindStringSubmatch(seg); tm != nil {
					add(table, tm[1], normalize(tm[2]))
				}
			}
		}
		for _, m := range columnTypeAlterRe.FindAllStringSubmatch(sqlText, -1) {
			add(m[1], m[2], normalize(m[3]))
		}
		for _, m := range columnTypeDropColRe.FindAllStringSubmatch(sqlText, -1) {
			if chain[m[1]] != nil {
				delete(chain[m[1]], m[2])
			}
		}
		for _, m := range columnTypeRenameRe.FindAllStringSubmatch(sqlText, -1) {
			if chain[m[1]] != nil && chain[m[1]][m[2]] != "" {
				typ := chain[m[1]][m[2]]
				delete(chain[m[1]], m[2])
				chain[m[1]][m[3]] = typ
			}
		}
	}
	if len(chain) == 0 {
		t.Fatal("迁移链解析出 0 张表：embed FS 或 CREATE TABLE 正则失灵，本测试会假绿")
	}
	return chain
}

// expectedSQLType 由 GORM schema 字段推导 postgres 端类型（与
// gorm.io/driver/postgres 的 DataTypeOf 同口径）。
func expectedSQLType(f *schema.Field) string {
	// type: 标签原样参与（vector(1536) 等扩展类型），只有标准词表才走推导。
	switch f.DataType {
	case "bool", "int", "uint", "float", "string", "time", "bytes":
	default:
		return string(f.DataType)
	}
	fieldType := f.FieldType
	if fieldType.Kind() == reflect.Ptr {
		fieldType = fieldType.Elem()
	}
	switch fieldType.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intSQLType(intBits(f, fieldType), false)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return intSQLType(intBits(f, fieldType), f.PrimaryKey)
	case reflect.Float32:
		return "real"
	case reflect.Float64:
		return "numeric"
	case reflect.String:
		if f.Size > 0 {
			return "varchar(" + strconv.Itoa(f.Size) + ")"
		}
		return "text"
	case reflect.Struct:
		return "timestamptz" // time.Time / gorm.DeletedAt
	default:
		return "unknown(" + fieldType.Kind().String() + ")"
	}
}

// intSQLType 是 postgres 驱动对整数列的位宽口径：int/int64/uint → bigint。
func intSQLType(bits int, primaryKey bool) string {
	switch {
	case bits <= 16:
		return "smallint"
	case bits == 32:
		return "integer"
	case primaryKey:
		return "bigserial"
	default:
		return "bigint"
	}
}

func intBits(f *schema.Field, fieldType reflect.Type) int {
	if f.Size > 0 {
		return f.Size
	}
	return fieldType.Bits()
}

// typeExceptions 记录已评估接受的类型漂移，与 cmd/schema-parity/main.go
// allowlist 的类型子集一一对应（值即该表的理由）。
var typeExceptions = map[[2]string]string{
	{"api_keys", "prefix"}:                             "migration looser than size tag (text vs varchar(16))",
	{"api_keys", "key_hash"}:                           "migration looser than size tag (text vs varchar(64))",
	{"user_recovery_codes", "code_hash"}:               "migration looser than size tag (text vs varchar(64))",
	{"audit_logs", "prev_hash"}:                        "migration looser than size tag (text vs varchar(64))",
	{"audit_logs", "entry_hash"}:                       "migration looser than size tag (text vs varchar(64))",
	{"suggestion_exposure_logs", "kind"}:               "migration looser than size tag (text vs varchar(16))",
	{"suggestion_exposure_logs", "strategy"}:           "migration looser than size tag (text vs varchar(64))",
	{"suggestion_exposure_logs", "converted_question"}: "migration looser than size tag (text vs varchar(512))",
	{"push_tokens", "platform"}:                        "migration looser than size tag (text vs varchar(16))",
	{"agent_groups", "priority"}:                       "migration narrowed int (integer vs bigint)",
	{"quality_reviews", "duration_seconds"}:            "migration narrowed int (integer vs bigint)",
	{"quality_reviews", "message_count"}:               "migration narrowed int (integer vs bigint)",
	{"quality_reviews", "violation_count"}:             "migration narrowed int (integer vs bigint)",
	{"quality_reviews", "attempt_count"}:               "migration narrowed int (integer vs bigint)",
	{"quality_reviews", "llm_total_score"}:             "float mapping wording (double precision vs numeric)",
	{"quality_reviews", "manual_score"}:                "float mapping wording (double precision vs numeric)",
}

// TestMigrationChainColumnTypesMatchModels：MigrationModels() 每个模型每列的
// 迁移链 SQL 类型必须等于 GORM 端推导类型，白名单外漂移即红。
func TestMigrationChainColumnTypesMatchModels(t *testing.T) {
	chain := columnTypesFromChain(t)
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		chainCols := chain[s.Table]
		if chainCols == nil {
			continue // 表级/列级守卫已红，这里不重复报
		}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			got := chainCols[f.DBName]
			if got == "" {
				continue // 列名守卫已红，这里不重复报
			}
			want := expectedSQLType(f)
			if got == want {
				continue
			}
			reason, allowed := typeExceptions[[2]string{s.Table, f.DBName}]
			if allowed {
				continue
			}
			t.Errorf("模型 %T 的表 %q 列 %q 类型漂移：迁移链 %q ≠ GORM 将产出 %q（sqlite 无此分别，postgres 真库会漂移；如确属已接受漂移，同步登记本测试 typeExceptions 与 cmd/schema-parity allowlist，理由：%s）",
				model, s.Table, f.DBName, got, want, reason)
		}
	}
}

// TestTypeExceptionKeysStayHonest：白名单键必须真实存在于当前漂移面——模型
// 改型或列改名后，过期条目必须清走，不许留死条目掩护新漂移。
func TestTypeExceptionKeysStayHonest(t *testing.T) {
	chain := columnTypesFromChain(t)
	for _, model := range MigrationModels() {
		s, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse model %T: %v", model, err)
		}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			key := [2]string{s.Table, f.DBName}
			if _, isException := typeExceptions[key]; !isException {
				continue
			}
			got := chain[s.Table][f.DBName]
			want := expectedSQLType(f)
			if got == want {
				t.Errorf("白名单条目 %q.%q 已过期：迁移链 %q 已等于 GORM 类型 %q，漂移消失应删除条目", key[0], key[1], got, want)
			}
		}
	}
}

// TestExpectedSQLTypeMapping：推导函数的分派分支单测（无需真库）。
func TestExpectedSQLTypeMapping(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field func(*schema.Field)
		want  string
	}{
		{"custom type tag passthrough", func(f *schema.Field) { f.DataType = "vector(1536)" }, "vector(1536)"},
		{"bool", func(f *schema.Field) { f.DataType = "bool"; f.FieldType = reflect.TypeOf(false) }, "boolean"},
		{"plain int", func(f *schema.Field) { f.DataType = "int"; f.FieldType = reflect.TypeOf(int(0)) }, "bigint"},
		{"int32", func(f *schema.Field) { f.DataType = "int"; f.FieldType = reflect.TypeOf(int32(0)) }, "integer"},
		{"int16", func(f *schema.Field) { f.DataType = "int"; f.FieldType = reflect.TypeOf(int16(0)) }, "smallint"},
		{"uint primary key", func(f *schema.Field) {
			f.DataType = "uint"
			ft := reflect.TypeOf(uint(0))
			f.FieldType = ft
			f.PrimaryKey = true
		}, "bigserial"},
		{"uint non-key", func(f *schema.Field) { f.DataType = "uint"; f.FieldType = reflect.TypeOf(uint(0)) }, "bigint"},
		{"float64", func(f *schema.Field) { f.DataType = "float"; f.FieldType = reflect.TypeOf(float64(0)) }, "numeric"},
		{"float32", func(f *schema.Field) { f.DataType = "float"; f.FieldType = reflect.TypeOf(float32(0)) }, "real"},
		{"string with size", func(f *schema.Field) { f.DataType = "string"; f.FieldType = reflect.TypeOf(""); f.Size = 64 }, "varchar(64)"},
		{"string bare", func(f *schema.Field) { f.DataType = "string"; f.FieldType = reflect.TypeOf("") }, "text"},
		{"time struct", func(f *schema.Field) { f.DataType = "time"; f.FieldType = reflect.TypeOf(struct{}{}) }, "timestamptz"},
		{"unknown kind", func(f *schema.Field) { f.DataType = "bytes"; f.FieldType = reflect.TypeOf([]byte(nil)) }, "unknown(slice)"},
		{"uint16", func(f *schema.Field) { f.DataType = "uint"; f.FieldType = reflect.TypeOf(uint16(0)) }, "smallint"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &schema.Field{}
			tc.field(f)
			if got := expectedSQLType(f); got != tc.want {
				t.Fatalf("expectedSQLType = %q, want %q", got, tc.want)
			}
		})
	}
}
