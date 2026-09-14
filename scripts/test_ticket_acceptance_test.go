package scripts

import (
	"encoding/json"
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

// TestTicketAcceptanceScriptWritesEvidence 用 mock server 走通
// test-ticket-acceptance.sh 全流程,并校验证据与 manifest 内容。
func TestTicketAcceptanceScriptWritesEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash-backed acceptance script tests are not stable on Windows")
	}

	type ticket struct {
		ID         int    `json:"id"`
		Title      string `json:"title"`
		CustomerID int    `json:"customer_id"`
		AgentID    *int   `json:"agent_id,omitempty"`
		Category   string `json:"category"`
		Priority   string `json:"priority"`
		Status     string `json:"status"`
		Tags       string `json:"tags"`
		CreatedAt  string `json:"created_at"`
		UpdatedAt  string `json:"updated_at"`
		ClosedAt   string `json:"closed_at,omitempty"`
	}

	var (
		mu            sync.Mutex
		registerCount int
		agentCreates  int
		commentAdds   int
		nextUserID    = 200
		nextTicketID  = 500
		customerID    int
		token         = "ticket-admin-token"
		tickets       = map[int]*ticket{}
	)

	authorized := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+token
	}

	servify := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		case r.URL.Path == "/api/v1/auth/login":
			mu.Lock()
			defer mu.Unlock()
			_, _ = w.Write([]byte(`{"token":"` + token + `","user":{"id":1,"role":"admin"}}`))
			return
		case r.URL.Path == "/api/v1/auth/register":
			mu.Lock()
			defer mu.Unlock()
			registerCount++
			nextUserID++
			if customerID == 0 {
				customerID = nextUserID
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(fmt.Sprintf(`{"token":"reg-token","refresh_token":"reg-refresh","user":{"id":%d,"role":"customer"}}`, nextUserID)))
			return
		case r.URL.Path == "/api/agents":
			if !authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"invalid token"}`))
				return
			}
			mu.Lock()
			defer mu.Unlock()
			agentCreates++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"user_id":` + fmt.Sprint(nextUserID) + `,"status":"online"}`))
			return
		}

		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid token"}`))
			return
		}

		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/api/tickets" && r.Method == http.MethodPost:
			var req struct {
				Title      string `json:"title"`
				CustomerID int    `json:"customer_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if strings.TrimSpace(req.Title) == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"Invalid request body"}`))
				return
			}
			if req.CustomerID != customerID {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"customer not found"}`))
				return
			}
			nextTicketID++
			created := &ticket{
				ID:         nextTicketID,
				Title:      strings.TrimSpace(req.Title),
				CustomerID: req.CustomerID,
				Category:   "general",
				Priority:   "normal",
				Status:     "open",
				CreatedAt:  "2026-01-01T00:00:00Z",
				UpdatedAt:  "2026-01-01T00:00:00Z",
			}
			tickets[created.ID] = created
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(mustJSON(t, created))
		case r.URL.Path == "/api/tickets/stats" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"total":1,"today_created":1,"pending":0,"resolved":0,"by_status":[{"status":"closed","count":1}],"by_priority":[]}`))
		case r.URL.Path == "/api/tickets/export" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			var rows []string
			for _, tk := range tickets {
				rows = append(rows, fmt.Sprintf("%d,%s,%s", tk.ID, tk.Title, tk.Status))
			}
			body := "id,title,status\n" + strings.Join(rows, "\n") + "\n"
			_, _ = w.Write([]byte(body))
		case strings.HasPrefix(r.URL.Path, "/api/tickets/"):
			var id int
			rest := ""
			if _, err := fmt.Sscanf(r.URL.Path, "/api/tickets/%d/%s", &id, &rest); err != nil {
				if _, err := fmt.Sscanf(r.URL.Path, "/api/tickets/%d", &id); err != nil {
					http.NotFound(w, r)
					return
				}
			}
			tk, exists := tickets[id]
			if !exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"Ticket not found"}`))
				return
			}
			switch {
			case rest == "" && r.Method == http.MethodGet:
				_, _ = w.Write(mustJSON(t, tk))
			case rest == "" && r.Method == http.MethodPut:
				var req struct {
					Priority *string `json:"priority"`
					AgentID  *int    `json:"agent_id"`
					Tags     *string `json:"tags"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if req.Priority != nil {
					tk.Priority = *req.Priority
				}
				if req.AgentID != nil {
					tk.AgentID = req.AgentID
				}
				if req.Tags != nil {
					tk.Tags = *req.Tags
				}
				_, _ = w.Write(mustJSON(t, tk))
			case rest == "comments" && r.Method == http.MethodPost:
				var req struct {
					Content string `json:"content"`
				}
				_ = json.NewDecoder(r.Body).Decode(&req)
				if strings.TrimSpace(req.Content) == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				commentAdds++
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"id":%d,"ticket_id":%d,"content":%q}`, 9000+commentAdds, id, req.Content)))
			case rest == "close" && r.Method == http.MethodPost:
				tk.Status = "closed"
				tk.ClosedAt = "2026-01-01T02:00:00Z"
				_, _ = w.Write([]byte(fmt.Sprintf(`{"message":"Ticket closed successfully","ticket_id":%d}`, id)))
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer servify.Close()

	evidenceDir := t.TempDir()

	cmd := exec.Command("bash", "-lc", fmt.Sprintf(
		"TICKET_ACCEPTANCE_MODE=mock SERVIFY_URL=%q EVIDENCE_DIR=%q bash ./test-ticket-acceptance.sh",
		servify.URL,
		evidenceDir,
	))
	cmd.Dir = "."
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected ticket acceptance success, err=%v output=%s", err, string(output))
	}

	for _, name := range []string{
		"summary.txt",
		"manifest.json",
		"admin-auth.json",
		"ticket-customer.json",
		"ticket-agent.json",
		"agent-create.json",
		"ticket-created.json",
		"ticket-updated.json",
		"ticket-detail-after-update.json",
		"ticket-comment.json",
		"ticket-close.json",
		"ticket-detail-closed.json",
		"ticket-stats.json",
		"ticket-export.csv",
		"unknown-ticket.json",
		"ticket-create-missing-title.json",
		"unauthenticated-tickets.json",
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
		"admin_auth_ok=true",
		"agents_ready=true",
		"ticket_created=true",
		"ticket_updated=true",
		"comment_added=true",
		"closed_status=closed",
		"closed_state_verified=true",
		"stats_ok=true",
		"export_ok=true",
		"unknown_ticket_rejected=true",
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
		`"provider": "ticket"`,
		`"mode": "mock"`,
		`"ticket_updated": "true"`,
		`"closed_state_verified": "true"`,
		`"stats_ok": "true"`,
		`"export_ok": "true"`,
		`"create_missing_title_rejected": "true"`,
		`"unauthenticated_rejected": "true"`,
	} {
		if !strings.Contains(manifestText, want) {
			t.Fatalf("expected %q in manifest, got %s", want, manifestText)
		}
	}

	if registerCount != 2 || agentCreates != 1 || commentAdds != 1 {
		t.Fatalf("unexpected request counts register=%d agents=%d comments=%d", registerCount, agentCreates, commentAdds)
	}
}
