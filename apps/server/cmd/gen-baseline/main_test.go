package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm/logger"
)

// TestCaptureLoggerLogModeReturnsSelf 覆盖 LogMode 与三个静默日志级别。
func TestCaptureLoggerLogModeReturnsSelf(t *testing.T) {
	l := &captureLogger{}
	if got := l.LogMode(logger.Silent); got != logger.Interface(l) {
		t.Fatal("LogMode must return the same capture logger")
	}
	l.Info(context.Background(), "info %s", "ignored")
	l.Warn(context.Background(), "warn ignored")
	l.Error(context.Background(), "error ignored")
	if len(l.batches) != 0 {
		t.Fatalf("non-trace levels must not record batches, got %v", l.batches)
	}
}

// TestCaptureLoggerTraceRecordsStatements 覆盖 Trace 的成功与失败两条路径：
// 语句按执行顺序记录，失败语句额外走 log.Printf。
func TestCaptureLoggerTraceRecordsStatements(t *testing.T) {
	l := &captureLogger{}
	l.Trace(context.Background(), time.Now(), func() (string, int64) { return "SELECT 1", 0 }, nil)
	l.Trace(context.Background(), time.Now(), func() (string, int64) {
		return "CREATE TABLE t (id int)", 0
	}, errors.New("boom"))
	if len(l.batches) != 2 || l.batches[0] != "SELECT 1" || l.batches[1] != "CREATE TABLE t (id int)" {
		t.Fatalf("unexpected captured batches: %v", l.batches)
	}
}

// TestIsDDL 表驱动覆盖大小写、空白与前缀分类。
func TestIsDDL(t *testing.T) {
	cases := []struct {
		stmt string
		want bool
	}{
		{"CREATE TABLE users (id int)", true},
		{"  create index idx_users on users(id)  ", true},
		{"ALTER TABLE users ADD COLUMN a int", true},
		{"COMMENT ON TABLE users IS 'x'", true},
		{"DROP INDEX idx_users", true},
		{"SELECT count(*) FROM pg_tables", false},
		{"INSERT INTO t VALUES (1)", false},
		{"", false},
		{"   ", false},
	}
	for _, tc := range cases {
		if got := isDDL(tc.stmt); got != tc.want {
			t.Errorf("isDDL(%q) = %v, want %v", tc.stmt, got, tc.want)
		}
	}
}

// TestGenBaselineMainSubprocess 在子进程内运行 main()，参数由
// GEN_BASELINE_TEST_DSN 注入（留空则验证缺 DSN 的 fatal）。
func TestGenBaselineMainSubprocess(t *testing.T) {
	if os.Getenv("GEN_BASELINE_SUBPROCESS") != "1" {
		return
	}
	if dsn := os.Getenv("GEN_BASELINE_TEST_DSN"); dsn != "" {
		os.Args = append([]string{"gen-baseline"}, "-dsn="+dsn)
	}
	main()
	os.Exit(0)
}

func TestMainMissingDSNFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestGenBaselineMainSubprocess$")
	cmd.Env = append(filterEnv("GEN_BASELINE_DSN"),
		"GEN_BASELINE_SUBPROCESS=1",
		"GEN_BASELINE_DSN=",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("missing DSN must exit non-zero, output: %s", out)
	}
	if !strings.Contains(string(out), "-dsn or GEN_BASELINE_DSN is required") {
		t.Fatalf("unexpected fatal output: %s", out)
	}
}

func TestMainUnreachablePostgresFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestGenBaselineMainSubprocess$")
	cmd.Env = append(filterEnv("GEN_BASELINE_DSN"),
		"GEN_BASELINE_SUBPROCESS=1",
		"GEN_BASELINE_TEST_DSN=postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable&connect_timeout=2",
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("unreachable postgres must exit non-zero, output: %s", out)
	}
	if !strings.Contains(string(out), "connect scratch postgres") {
		t.Fatalf("unexpected fatal output: %s", out)
	}
}

// filterEnv 去掉指定环境变量，避免外层环境污染子进程参数。
func filterEnv(key string) []string {
	env := os.Environ()[:0]
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, key+"=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}
