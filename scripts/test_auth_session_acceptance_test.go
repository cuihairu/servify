package scripts

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestAuthSessionAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	type session struct {
		ID        string
		Status    string
		IsCurrent bool
	}

	var (
		mu                 sync.Mutex
		registerCount      int
		loginCount         int
		refreshCount       int
		oldRefreshRejected bool
		currentAccess      = "access-login-1"
		sessions           = []session{
			{ID: "sess-primary", Status: "active", IsCurrent: true},
		}
	)

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()

		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
		case "/api/v1/auth/register":
			registerCount++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"register-access","refresh_token":"register-refresh","user":{"id":1}}`))
		case "/api/v1/auth/login":
			loginCount++
			switch loginCount {
			case 1:
				currentAccess = "access-login-1"
				sessions = []session{
					{ID: "sess-primary", Status: "active", IsCurrent: true},
				}
				_, _ = w.Write([]byte(`{"token":"access-login-1","refresh_token":"refresh-login-1","user":{"id":1}}`))
			case 2:
				sessions = []session{
					{ID: "sess-secondary", Status: "active", IsCurrent: false},
					{ID: "sess-primary", Status: "active", IsCurrent: true},
				}
				_, _ = w.Write([]byte(`{"token":"access-login-2","refresh_token":"refresh-login-2","user":{"id":1}}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unexpected login"}`))
			}
		case "/api/v1/auth/refresh":
			refreshCount++
			if refreshCount == 1 {
				currentAccess = "access-refresh-1"
				_, _ = w.Write([]byte(`{"token":"access-refresh-1","refresh_token":"refresh-refresh-1","user":{"id":1}}`))
				return
			}
			oldRefreshRejected = true
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid refresh token"}`))
		case "/api/v1/auth/sessions":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+currentAccess {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			var items []string
			for _, sess := range sessions {
				items = append(items, fmt.Sprintf(`{"session_id":"%s","status":"%s","is_current":%t}`, sess.ID, sess.Status, sess.IsCurrent))
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"count":%d,"items":[%s]}`, len(sessions), strings.Join(items, ","))))
		case "/api/v1/auth/sessions/logout-others":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+currentAccess {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			for i := range sessions {
				if !sessions[i].IsCurrent {
					sessions[i].Status = "revoked"
				}
			}
			_, _ = w.Write([]byte(`{"count":1,"current_session_id":"sess-primary"}`))
		case "/api/v1/auth/sessions/logout-current":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+currentAccess {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			for i := range sessions {
				if sessions[i].IsCurrent {
					sessions[i].Status = "revoked"
				}
			}
			currentAccess = "revoked-token"
			_, _ = w.Write([]byte(`{"session_id":"sess-primary","status":"revoked","token_version":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf("AUTH_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-auth-session-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected auth acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"auth-register.json",
		"auth-login-primary.json",
		"auth-login-secondary.json",
		"auth-refresh.json",
		"auth-refresh-reuse-old.json",
		"auth-sessions-before.json",
		"auth-logout-others.json",
		"auth-sessions-after-logout-others.json",
		"auth-logout-current.json",
		"auth-sessions-after-logout-current.json",
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
		"mode=mock",
		"register_ok=true",
		"login_primary_ok=true",
		"login_secondary_ok=true",
		"refresh_ok=true",
		"old_refresh_rejected=true",
		"sessions_before_count=2",
		"logout_others_count=1",
		"sessions_after_logout_others_active_count=1",
		"sessions_after_logout_others_revoked_count=1",
		"logout_current_status=revoked",
		"post_logout_current_status=401",
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
		`"provider": "auth-session"`,
		`"mode": "mock"`,
		`"old_refresh_rejected": "true"`,
		`"logout_others_ok": "true"`,
		`"post_logout_current_rejected": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	if registerCount != 1 || loginCount != 2 || refreshCount != 2 || !oldRefreshRejected {
		t.Fatalf("unexpected request counts register=%d login=%d refresh=%d rejected=%v", registerCount, loginCount, refreshCount, oldRefreshRejected)
	}
}
