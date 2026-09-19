package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// syscall_dup installs newFd onto fd 0 (or restores saved) and returns the
// previously saved descriptor.
func syscall_dup(newFd, targetFd int) (saved int, err error) {
	saved, err = syscall.Dup(targetFd)
	if err != nil {
		return 0, err
	}
	if err := syscall_dupTo(newFd, targetFd); err != nil {
		_ = syscall.Close(saved)
		return 0, err
	}
	return saved, nil
}

func syscall_dupTo(from, to int) error {
	return syscall.Dup3(from, to, 0)
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	dir := t.TempDir()
	resetViperForCLI(t, dir)

	out, err := executeCommandCapturingStdout(t, versionCmd)
	if err != nil {
		t.Fatalf("version cmd error = %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "Version:") || !strings.Contains(out, "Commit:") {
		t.Fatalf("unexpected version output: %s", out)
	}
}

func TestExecuteSuccessPath(t *testing.T) {
	dir := t.TempDir()
	resetViperForCLI(t, dir)

	rootCmd.SetArgs([]string{"--help"})
	Execute()
}

func TestDecodeTokenCommandVerifyConfigLoadFailure(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	viper.Set("server.port", "not-a-number")
	resetCLIFlags()

	token := createHS256JWT(map[string]interface{}{"sub": "x"}, "any")
	if _, err := executeCommand(t, decodeTokenCmd, "--verify", token); err == nil {
		t.Fatal("expected config load error during verify")
	}
}

func TestDecodeTokenCommandVerifyErrorPrinted(t *testing.T) {
	dir := t.TempDir()
	resetViperForCLI(t, dir)
	resetCLIFlags()

	header := base64URLEncode(`{"alg":"HS256","typ":"JWT"}`)
	payload := base64URLEncode(`{"sub":"print-verify-error"}`)
	// Valid header/payload but an invalid base64url signature: decode succeeds
	// while verify fails, printing the verify error instead of aborting.
	token := header + "." + payload + ".!!!"

	out, err := executeCommandCapturingStdout(t, decodeTokenCmd, "--verify", "--secret=any", "--token="+token)
	if err != nil {
		t.Fatalf("token-decode error = %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "Verify error") {
		t.Fatalf("expected verify error output, got %s", out)
	}
}

func base64URLEncode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestVerifyHS256InvalidPayloadAfterSignature(t *testing.T) {
	header := base64URLEncode(`{"alg":"HS256"}`)
	sig := base64URLEncode("sig")

	// Payload segment is not valid base64: signature decodes, then the JWT
	// decode inside verify fails.
	token := header + ".!!!." + sig
	sigValid, _, _, err := verifyHS256(token, "secret", time.Unix(1_700_000_000, 0))
	if err == nil {
		t.Fatal("expected payload decode error")
	}
	if sigValid {
		t.Fatal("signature should not validate for junk signature bytes")
	}
}

func TestCheckTimeClaimsJSONNumber(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	payload := map[string]interface{}{
		"nbf": json.Number(fmt.Sprintf("%d", now.Unix()-10)),
		"iat": json.Number(fmt.Sprintf("%d", now.Unix()-10)),
		"exp": json.Number(fmt.Sprintf("%d", now.Unix()+10)),
	}
	if !checkTimeClaims(payload, now) {
		t.Fatal("expected json.Number claims to validate")
	}

	badNumber := map[string]interface{}{"exp": json.Number("not-a-number")}
	if checkTimeClaims(badNumber, now) {
		t.Fatal("expected unparsable json.Number exp to fail")
	}
}

func TestReadStdinIfEmptyPipedInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	// readStdinIfEmpty reads /dev/stdin (fd 0), so the pipe must actually be
	// installed on file descriptor zero, not just swapped into os.Stdin.
	saved, err := syscall_dup(int(r.Fd()), 0)
	if err != nil {
		t.Fatalf("dup stdin: %v", err)
	}
	t.Cleanup(func() {
		_ = syscall_dupTo(saved, 0)
		_ = syscall.Close(saved)
	})

	if _, err := w.WriteString("piped-token-value\n"); err != nil {
		t.Fatalf("write pipe: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}

	if got := readStdinIfEmpty(""); got != "piped-token-value" {
		t.Fatalf("readStdinIfEmpty(pipe) = %q", got)
	}
}

func TestRunCheckObservabilityBaselineNonStrictContinues(t *testing.T) {
	dir := t.TempDir()
	path := writeCLIConfig(t, dir, "cli-test-secret")
	// Default config has monitoring enabled; disable it to force warnings.
	if err := os.WriteFile(path, []byte(strings.Join([]string{
		"server:",
		"  host: 127.0.0.1",
		"  port: 8080",
		"  environment: development",
		"event_bus:",
		"  provider: inmemory",
		"monitoring:",
		"  enabled: false",
		"jwt:",
		"  secret: cli-test-secret",
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out := &strings.Builder{}
	if err := runCheckObservabilityBaseline(path, false, dir, out); err != nil {
		t.Fatalf("runCheckObservabilityBaseline() error = %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "monitoring is disabled") && !strings.Contains(body, "asset missing") {
		t.Fatalf("expected warnings in output, got %s", body)
	}
	if !strings.Contains(body, "Continuing because strict mode is disabled.") {
		t.Fatalf("expected non-strict continuation output, got %s", body)
	}
}

func TestRunCheckObservabilityBaselineCleanPass(t *testing.T) {
	dir := t.TempDir()
	path := writeCLIConfig(t, dir, "cli-test-secret")

	out := &strings.Builder{}
	// Blank repo root falls back to the source-derived repository root where
	// the observability assets exist.
	if err := runCheckObservabilityBaseline(path, true, "", out); err != nil {
		t.Fatalf("runCheckObservabilityBaseline(strict) error = %v", err)
	}
	if !strings.Contains(out.String(), "passed") {
		t.Fatalf("expected pass output, got %s", out.String())
	}
}

// TestCLIWorker re-executes the test binary to execute the package-private
// run() startup path in a subprocess. Subprocess tests either inject seam
// faults via CLI_RUN_VARIANT (see applyCLIRunFault, expecting logrus.Fatalf
// to exit 1) or drive the real lifecycle ("smoke": SIGTERM → clean exit 0).
func TestCLIWorker(t *testing.T) {
	if os.Getenv("CLI_RUN_SUBPROCESS") != "1" {
		return
	}

	variant := os.Getenv("CLI_RUN_VARIANT")
	dir := os.Getenv("CLI_RUN_DIR")
	if variant == "" || dir == "" {
		return
	}
	if err := os.Chdir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "chdir:", err)
		os.Exit(3)
	}

	if variant == "execute-failure" {
		rootCmd.SetArgs([]string{"token", "--definitely-unknown-flag"})
		Execute()
		// Execute() should have exited via os.Exit(1); reaching this point
		// means the failure branch did not run.
		os.Exit(7)
	}

	applyCLIRunFault(variant)

	run(nil, nil)
	// Smoke variant: WaitForShutdownSignal returned on SIGTERM, the shutdown
	// section completed, and run() returned normally.
	os.Exit(0)
}

// baseCLIRunConfig 返回 CLI run 子进程测试用的最小配置；tracingEnabled 时附
// 带监控段（tracing endpoint 不可达或 OTEL_RESOURCE_ATTRIBUTES 非法时走
// "init tracing" Warn 分支）。
func baseCLIRunConfig(tracingEnabled bool, host string) string {
	body := []string{
		"server:",
		"  host: " + host,
		"  port: 8080",
		"  environment: development",
		"event_bus:",
		"  provider: inmemory",
		"database:",
		"  host: 127.0.0.1",
		"  port: 1",
		"  user: u",
		"  password: p",
		"  name: n",
		"jwt:",
		"  secret: cli-run-secret",
		"log:",
		"  level: info",
		"  format: json",
		"  output: stdout",
	}
	if tracingEnabled {
		body = append(body,
			"monitoring:",
			"  enabled: true",
			"  tracing:",
			"    enabled: true",
			"    endpoint: \"127.0.0.1:4317\"",
			"    insecure: true",
			"    sample_ratio: 0.1",
			"    service_name: cli-run",
		)
	}
	return strings.Join(body, "\n") + "\n"
}

func TestCLIRunConfigLoadFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yml"), []byte("{{{ not yaml"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=config-failure",
		"CLI_RUN_DIR="+dir,
	)
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("expected exit code 1, got %v\noutput:\n%s", cmd.ProcessState, out)
	}
	if !strings.Contains(string(out), "Failed to load config") {
		t.Fatalf("expected load failure message, got %s", out)
	}
}

func TestExecuteFailureExitsNonZero(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIWorker$", "-test.timeout=2m")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CLI_RUN_SUBPROCESS=1",
		"CLI_RUN_VARIANT=execute-failure",
		"CLI_RUN_DIR="+dir,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit, output:\n%s", out)
	}
	exitCode := 1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d\noutput:\n%s", exitCode, out)
	}
}
