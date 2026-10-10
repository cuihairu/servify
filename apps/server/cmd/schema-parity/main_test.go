package main

import (
	"database/sql"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestColumnShapeDiffer 表驱动覆盖七个字段的差异判定。
func TestColumnShapeDiffer(t *testing.T) {
	base := columnShape{
		dataType: "text", udtName: "text",
		charMax: sql.NullInt64{Int64: 16, Valid: true}, nullable: "NO",
		def:         sql.NullString{String: "x", Valid: true},
		numericPrec: sql.NullInt64{Int64: 10, Valid: true}, numScale: sql.NullInt64{},
	}
	cases := map[string]func(*columnShape){
		"equal":       func(*columnShape) {},
		"dataType":    func(c *columnShape) { c.dataType = "integer" },
		"udtName":     func(c *columnShape) { c.udtName = "int4" },
		"charMax":     func(c *columnShape) { c.charMax.Valid = false },
		"nullable":    func(c *columnShape) { c.nullable = "YES" },
		"def":         func(c *columnShape) { c.def.String = "y" },
		"numericPrec": func(c *columnShape) { c.numericPrec.Int64 = 12 },
		"numScale":    func(c *columnShape) { c.numScale.Valid = true },
	}
	for name, mutate := range cases {
		other := base
		mutate(&other)
		got := base.differ(other)
		if name == "equal" {
			if got {
				t.Errorf("%s: differ must be false", name)
			}
			continue
		}
		if !got {
			t.Errorf("%s: differ must be true", name)
		}
	}
}

func TestColumnShapeDescribe(t *testing.T) {
	got := columnShape{dataType: "text", udtName: "text", nullable: "NO"}.describe()
	for _, want := range []string{"type=text", "udt=text", "nullable=NO", "charMax=", "default=", "numPrec=", "numScale="} {
		assert.Contains(t, got, want)
	}
}

// probeThing is a minimal gorm model for modelTableSet tests.
type probeThing struct {
	ID uint `gorm:"primaryKey"`
}

func TestModelTableSetResolvesTableNames(t *testing.T) {
	tables, err := modelTableSet([]interface{}{&probeThing{}})
	require.NoError(t, err)
	assert.True(t, tables["probe_things"], "naming strategy must pluralize: %v", tables)
}

func TestModelTableSetRejectsNonStruct(t *testing.T) {
	_, err := modelTableSet([]interface{}{42})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse model int")
}

func TestChildDSNRewritesPath(t *testing.T) {
	dsn, err := childDSN("postgres://u:p@h:5432/db?sslmode=disable", scratchAutomigDB)
	require.NoError(t, err)
	assert.Equal(t, "postgres://u:p@h:5432/"+scratchAutomigDB+"?sslmode=disable", dsn)
}

func TestChildDSNRejectsUnparseable(t *testing.T) {
	_, err := childDSN("://bad", scratchAutomigDB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse admin dsn:")
}

// TestSchemaParityMainSubprocess re-enters the test binary as main() runner;
// the DSN comes from SCHEMA_PARITY_TEST_DSN (empty means verify the
// missing-DSN fatal).
func TestSchemaParityMainSubprocess(t *testing.T) {
	if os.Getenv("SCHEMA_PARITY_SUBPROCESS") != "1" {
		return
	}
	if dsn := os.Getenv("SCHEMA_PARITY_TEST_DSN"); dsn != "" {
		os.Args = append([]string{"schema-parity"}, "-dsn="+dsn)
	}
	main()
	os.Exit(0)
}

func TestMainMissingDSNFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSchemaParityMainSubprocess$")
	cmd.Env = append(filterEnv("SCHEMA_PARITY_DSN"),
		"SCHEMA_PARITY_SUBPROCESS=1",
		"SCHEMA_PARITY_DSN=",
	)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "missing DSN must exit non-zero, output: %s", out)
	assert.Contains(t, string(out), "-dsn or SCHEMA_PARITY_DSN is required")
}

func TestMainUnreachablePostgresFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSchemaParityMainSubprocess$")
	cmd.Env = append(filterEnv("SCHEMA_PARITY_DSN"),
		"SCHEMA_PARITY_SUBPROCESS=1",
		"SCHEMA_PARITY_TEST_DSN=postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable&connect_timeout=2",
	)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "unreachable postgres must exit non-zero, output: %s", out)
	assert.True(t, strings.Contains(string(out), "connect admin postgres"),
		"unexpected fatal output: %s", out)
}
