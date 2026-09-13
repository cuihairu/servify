package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// syncBuffer 并发安全的输出缓冲：exec 的拷贝 goroutine 写、测试断言读，
// strings.Builder 本身不支持并发。
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func writeServerConfig(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "config.yml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func baseServerConfig(port int) string {
	return fmt.Sprintf(strings.Join([]string{
		"server:",
		"  host: 127.0.0.1",
		"  port: %d",
		"  environment: development",
		"event_bus:",
		"  provider: inmemory",
		"database:",
		"  host: 127.0.0.1",
		"  port: 1",
		"  user: u",
		"  password: p",
		"  name: n",
		"log:",
		"  level: info",
		"  format: json",
		"  output: stdout",
		"security:",
		"  audit:",
		"    enabled: false",
		"  token_revocation:",
		"    enabled: false",
		"",
	}, "\n"), port)
}

// TestServerMainSubprocess runs main() inside a child process with startup
// flags provided via SERVIFY_SERVER_ARGS (\x1f separated). SERVIFY_SERVER_FAULT
// (仅子进程测试设置) selects a seam fault injection for defensive branches.
func TestServerMainSubprocess(t *testing.T) {
	if os.Getenv("SERVIFY_SERVER_SUBPROCESS") != "1" {
		return
	}
	if a := os.Getenv("SERVIFY_SERVER_ARGS"); a != "" {
		os.Args = append([]string{"server"}, strings.Split(a, "\x1f")...)
	}
	applyServerFault(os.Getenv("SERVIFY_SERVER_FAULT"))
	main()
	os.Exit(0)
}

func runServerSubprocess(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestServerMainSubprocess$", "-test.timeout=5m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SERVIFY_SERVER_SUBPROCESS=1",
		"SERVIFY_SERVER_ARGS="+strings.Join(args, "\x1f"),
	)
	return cmd
}

func TestServerMainHappyPath(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))
	dbPath := filepath.Join(dir, "app.db")

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+dbPath,
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	var out syncBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 200*time.Millisecond, "server did not become healthy: %s", out.String())

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, "server exited with error: %s", out.String())
	case <-time.After(60 * time.Second):
		t.Fatalf("server did not exit after SIGTERM: %s", out.String())
	}

	// The runtime was migrated and booted: the sqlite database should exist
	// with the canonical schema in place.
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM sqlite_master WHERE type='table' AND name = 'users'").Scan(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestServerMainTracingSetupWarning(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	cfg := baseServerConfig(port) +
		"monitoring:\n  enabled: true\n  tracing:\n    enabled: true\n    endpoint: \"http://127.0.0.1:4317\"\n    insecure: true\n    sample_ratio: 0.1\n    service_name: servify\n"
	writeServerConfig(t, dir, cfg)

	cmd := runServerSubprocess(t, dir,
		"-db-driver=sqlite", "-dsn="+filepath.Join(dir, "app.db"),
		"-host=127.0.0.1", fmt.Sprintf("-port=%d", port),
	)
	// A malformed OTEL_RESOURCE_ATTRIBUTES makes resource.New fail inside
	// SetupTracing, which must only be logged as a warning by main.
	cmd.Env = append(cmd.Env, "OTEL_RESOURCE_ATTRIBUTES=not-a-valid-entry")
	var out syncBuffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, 200*time.Millisecond, "server did not become healthy: %s", out.String())

	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		require.NoError(t, err, "server exited with error: %s", out.String())
	case <-time.After(60 * time.Second):
		t.Fatalf("server did not exit after SIGTERM: %s", out.String())
	}
	assert.Contains(t, out.String(), "init tracing")
}

func TestServerMainLoadConfigFailure(t *testing.T) {
	dir := t.TempDir()
	writeServerConfig(t, dir, "{{{{ not yaml")

	cmd := runServerSubprocess(t, dir, "-db-driver=sqlite", "-dsn="+filepath.Join(dir, "x.db"))
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to load config")
}

func TestServerMainBadStartupFlagFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	cmd := runServerSubprocess(t, dir, "-port=not-a-number")
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to parse startup options")
}

func TestServerMainBuildAppFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	cfg := baseServerConfig(port) + "embedding:\n  provider: tei\n  tei:\n    base_url: \"\"\n"
	writeServerConfig(t, dir, cfg)

	cmd := runServerSubprocess(t, dir, "-db-driver=sqlite", "-dsn="+filepath.Join(dir, "x.db"))
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to build app")
}

func TestServerMainDatabaseFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	badDSN := filepath.Join(dir, "missing-dir", "x.db")
	cmd := runServerSubprocess(t, dir, "-db-driver=sqlite", "-dsn="+badDSN)
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to connect to database")
}

func TestServerMainMigrateFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	writeServerConfig(t, dir, baseServerConfig(port))

	dbPath := filepath.Join(dir, "conflict.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE VIEW users AS SELECT 1 AS id").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	cmd := runServerSubprocess(t, dir, "-db-driver=sqlite", "-dsn="+dbPath)
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to migrate database")
}

func TestServerMainBuildRuntimeFailure(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	cfg := baseServerConfig(port) +
		"fallback:\n  enabled: false\nweknora:\n  enabled: true\n  base_url: \"http://127.0.0.1:1\"\n"
	writeServerConfig(t, dir, cfg)

	cmd := runServerSubprocess(t, dir, "-db-driver=sqlite", "-dsn="+filepath.Join(dir, "x.db"))
	out, _ := cmd.CombinedOutput()
	assert.EqualValues(t, 1, cmd.ProcessState.ExitCode())
	assert.Contains(t, string(out), "Failed to build runtime")
}
