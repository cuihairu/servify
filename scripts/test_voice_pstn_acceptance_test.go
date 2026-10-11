package scripts

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestVoicePstnAcceptanceScriptWritesEvidence 真跑
// test-voice-pstn-acceptance.sh 全部断言（签名/幂等/录音/403/400 负例 +
// sqlite 落库 + 出站 webhook）。脚本要求外部服务，本测试自起真实服务：
// WORK_DIR 临时 config（voice.pstn.provider=twilio + 共享 auth token），
// sqlite 落库钉 TZ=UTC，ADMIN_TOKEN 由 /api/v1/auth/register 注册取得。
// CI 此前只校验已入库 manifest，从不重放脚本，陈旧证据也能蒙混过关。
func TestVoicePstnAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	projectRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	if out, buildErr := exec.Command("make", "-C", projectRoot, "build").CombinedOutput(); buildErr != nil {
		t.Fatalf("make build failed: %v\n%s", buildErr, string(out))
	}

	workDir := t.TempDir()
	accessToken := "pstn-acceptance-token"
	configYML := fmt.Sprintf(
		"voice:\n  pstn:\n    provider: twilio\n  twilio:\n    auth_token: %q\n",
		accessToken,
	)
	if writeErr := os.WriteFile(filepath.Join(workDir, "config.yml"), []byte(configYML), 0o644); writeErr != nil {
		t.Fatalf("write temp config: %v", writeErr)
	}

	port := freePort(t)
	dsn := filepath.Join(workDir, "pstn.sqlite")
	serverCmd := exec.Command(filepath.Join(projectRoot, "bin", "servify"))
	serverCmd.Dir = workDir
	serverCmd.Env = append(os.Environ(),
		"TZ=UTC",
		"DB_DRIVER=sqlite",
		"DB_DSN="+dsn,
		"SERVIFY_PORT="+port,
		"SERVIFY_JWT_SECRET=pstn-acceptance-dev-secret",
	)
	serverCmd.Stdout = t.Output()
	serverCmd.Stderr = t.Output()
	if startErr := serverCmd.Start(); startErr != nil {
		t.Fatalf("start server: %v", startErr)
	}
	defer func() {
		_ = serverCmd.Process.Kill()
		_, _ = serverCmd.Process.Wait()
	}()

	baseURL := "http://127.0.0.1:" + port
	ready := false
	for i := 0; i < 60; i++ {
		resp, getErr := http.Get(baseURL + "/health")
		if getErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(time.Second)
	}
	if !ready {
		t.Fatalf("server did not become healthy at %s", baseURL)
	}

	registerBody := `{"username":"pstn-admin","email":"pstn-admin@servify.io","password":"pstn-admin-123","name":"PSTN Admin","role":"admin"}`
	registerResp, postErr := http.Post(baseURL+"/api/v1/auth/register", "application/json", strings.NewReader(registerBody))
	if postErr != nil {
		t.Fatalf("register admin: %v", postErr)
	}
	defer func() { _ = registerResp.Body.Close() }()
	if registerResp.StatusCode != http.StatusCreated {
		t.Fatalf("register admin status = %d", registerResp.StatusCode)
	}
	var registerData struct {
		Token string `json:"token"`
	}
	decodeErr := json.NewDecoder(registerResp.Body).Decode(&registerData)
	if decodeErr != nil || registerData.Token == "" {
		t.Fatalf("register response missing token (decode=%v)", decodeErr)
	}

	evidenceDir := t.TempDir()
	scriptCmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"SERVIFY_URL=%q TWILIO_AUTH_TOKEN=%q SQLITE_DB=%q ADMIN_TOKEN=%q RECEIVER_PORT=%s EVIDENCE_DIR=%q bash ./test-voice-pstn-acceptance.sh",
		baseURL, accessToken, dsn, registerData.Token, freePort(t), evidenceDir,
	))
	scriptCmd.Dir = "."
	scriptCmd.Env = os.Environ()
	output, err := scriptCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected voice pstn acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"webhook-create.json",
		"receiver-payloads.jsonl",
	} {
		if _, statErr := os.Stat(filepath.Join(evidenceDir, name)); statErr != nil {
			t.Fatalf("expected evidence file %s: %v\noutput=%s", name, statErr, string(output))
		}
	}

	summary, err := os.ReadFile(filepath.Join(evidenceDir, "summary.txt"))
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	summaryText := string(summary)
	for _, want := range []string{
		"signature_ok=true",
		"duplicate_invite_ok=true",
		"answer_ok=true",
		"hangup_ok=true",
		"late_invite_short_circuit_ok=true",
		"recording_ok=true",
		"bad_signature_rejected=true",
		"missing_signature_rejected=true",
		"unknown_status_rejected=true",
		"db_call_asserted=true",
		"db_recording_asserted=true",
		"outbound_ok=true",
		"overall_status=passed",
	} {
		if !strings.Contains(summaryText, want) {
			t.Fatalf("expected %q in summary, got %s", want, summaryText)
		}
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	manifestText := string(manifest)
	for _, want := range []string{
		`"provider": "voice-pstn"`,
		`"overall": "passed"`,
		`"db_call_asserted": "true"`,
		`"db_recording_asserted": "true"`,
		`"outbound_ok": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}
}
