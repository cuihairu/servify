package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/models"
	agentdelivery "servify/apps/server/internal/modules/agent/delivery"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func newAgentUnitRouter(svc *unitAgentService) (*gin.Engine, *AgentHandler) {
	gin.SetMode(gin.TestMode)
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	h := NewAgentHandler(svc, logger)
	r := gin.New()
	RegisterAgentRoutes(&r.RouterGroup, h)
	return r, h
}

func agentUnitRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentHandlerUnitCreateAgent(t *testing.T) {
	validBody := `{"user_id":1,"department":"sales","skills":"tech","max_concurrent":5}`
	agent := &models.Agent{UserID: 1, Department: "sales"}

	cases := []struct {
		name string
		svc  *unitAgentService
		body string
		want int
	}{
		{"success", &unitAgentService{agent: agent}, validBody, http.StatusCreated},
		{"invalid body", &unitAgentService{agent: agent}, `{`, http.StatusBadRequest},
		{"invalid input", &unitAgentService{createErr: errors.New("user_id is required")}, validBody, http.StatusBadRequest},
		{"conflict", &unitAgentService{createErr: errors.New("agent already exists")}, validBody, http.StatusConflict},
		{"not found", &unitAgentService{createErr: errors.New("user not found")}, validBody, http.StatusNotFound},
		{"internal", &unitAgentService{createErr: errors.New("boom")}, validBody, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodPost, "/agents", tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitGetAgent(t *testing.T) {
	agent := &models.Agent{UserID: 3}
	cases := []struct {
		name string
		svc  *unitAgentService
		path string
		want int
	}{
		{"success", &unitAgentService{agent: agent}, "/agents/3", http.StatusOK},
		{"bad id", &unitAgentService{agent: agent}, "/agents/abc", http.StatusBadRequest},
		{"not found", &unitAgentService{getErr: errors.New("agent not found")}, "/agents/3", http.StatusNotFound},
		{"internal", &unitAgentService{getErr: errors.New("boom")}, "/agents/3", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodGet, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitUpdateAgentStatus(t *testing.T) {
	agent := &models.Agent{UserID: 4, Status: "online"}
	body := `{"status":"busy"}`
	cases := []struct {
		name string
		svc  *unitAgentService
		path string
		body string
		want int
	}{
		{"success", &unitAgentService{agent: agent}, "/agents/4/status", body, http.StatusOK},
		{"bad id", &unitAgentService{agent: agent}, "/agents/x/status", body, http.StatusBadRequest},
		{"missing status", &unitAgentService{agent: agent}, "/agents/4/status", `{}`, http.StatusBadRequest},
		{"not found", &unitAgentService{statusErr: errors.New("agent not found")}, "/agents/4/status", body, http.StatusNotFound},
		{"invalid input", &unitAgentService{statusErr: errors.New("invalid status value")}, "/agents/4/status", body, http.StatusBadRequest},
		{"internal", &unitAgentService{statusErr: errors.New("boom")}, "/agents/4/status", body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodPut, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitOnlineOffline(t *testing.T) {
	agent := &models.Agent{UserID: 5}
	cases := []struct {
		name string
		svc  *unitAgentService
		path string
		want int
	}{
		{"online success", &unitAgentService{agent: agent}, "/agents/5/online", http.StatusOK},
		{"offline success", &unitAgentService{agent: agent}, "/agents/5/offline", http.StatusOK},
		{"online bad id", &unitAgentService{agent: agent}, "/agents/bad/online", http.StatusBadRequest},
		{"offline bad id", &unitAgentService{agent: agent}, "/agents/bad/offline", http.StatusBadRequest},
		{"online not found", &unitAgentService{statusErr: errors.New("agent not found")}, "/agents/5/online", http.StatusNotFound},
		{"offline not found", &unitAgentService{statusErr: errors.New("agent not found")}, "/agents/5/offline", http.StatusNotFound},
		{"online internal", &unitAgentService{statusErr: errors.New("boom")}, "/agents/5/online", http.StatusInternalServerError},
		{"offline internal", &unitAgentService{statusErr: errors.New("boom")}, "/agents/5/offline", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			method := http.MethodPost
			w := agentUnitRequest(r, method, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitListAndOnline(t *testing.T) {
	t.Run("online agents", func(t *testing.T) {
		svc := &unitAgentService{online: []*agentdelivery.AgentInfo{{UserID: 1}}}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodGet, "/agents/online", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("list default limit", func(t *testing.T) {
		svc := &unitAgentService{agents: []models.Agent{{UserID: 1}}}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodGet, "/agents", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("list explicit and invalid limit", func(t *testing.T) {
		svc := &unitAgentService{}
		r, _ := newAgentUnitRouter(svc)
		if w := agentUnitRequest(r, http.MethodGet, "/agents?limit=5", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if w := agentUnitRequest(r, http.MethodGet, "/agents?limit=nope", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("list error", func(t *testing.T) {
		svc := &unitAgentService{listErr: errors.New("boom")}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodGet, "/agents", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestAgentHandlerUnitAssignRelease(t *testing.T) {
	body := `{"session_id":"sess-1"}`
	cases := []struct {
		name string
		svc  *unitAgentService
		path string
		body string
		want int
	}{
		{"assign success", &unitAgentService{}, "/agents/6/assign-session", body, http.StatusOK},
		{"assign bad id", &unitAgentService{}, "/agents/x/assign-session", body, http.StatusBadRequest},
		{"assign missing session", &unitAgentService{}, "/agents/6/assign-session", `{}`, http.StatusBadRequest},
		{"assign not found", &unitAgentService{assignErr: errors.New("session not found")}, "/agents/6/assign-session", body, http.StatusNotFound},
		{"assign invalid", &unitAgentService{assignErr: errors.New("invalid session id")}, "/agents/6/assign-session", body, http.StatusBadRequest},
		{"assign internal", &unitAgentService{assignErr: errors.New("boom")}, "/agents/6/assign-session", body, http.StatusInternalServerError},
		{"release success", &unitAgentService{}, "/agents/6/release-session", body, http.StatusOK},
		{"release bad id", &unitAgentService{}, "/agents/x/release-session", body, http.StatusBadRequest},
		{"release missing session", &unitAgentService{}, "/agents/6/release-session", `{`, http.StatusBadRequest},
		{"release not found", &unitAgentService{releaseErr: errors.New("session not found")}, "/agents/6/release-session", body, http.StatusNotFound},
		{"release invalid", &unitAgentService{releaseErr: errors.New("invalid session")}, "/agents/6/release-session", body, http.StatusBadRequest},
		{"release internal", &unitAgentService{releaseErr: errors.New("boom")}, "/agents/6/release-session", body, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodPost, tc.path, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitRevokeTokens(t *testing.T) {
	cases := []struct {
		name string
		svc  *unitAgentService
		path string
		want int
	}{
		{"success", &unitAgentService{revokeVer: 2}, "/agents/7/revoke-tokens", http.StatusOK},
		{"bad id", &unitAgentService{}, "/agents/z/revoke-tokens", http.StatusBadRequest},
		{"not found", &unitAgentService{revokeErr: errors.New("agent not found")}, "/agents/7/revoke-tokens", http.StatusNotFound},
		{"invalid", &unitAgentService{revokeErr: errors.New("invalid agent")}, "/agents/7/revoke-tokens", http.StatusBadRequest},
		{"internal", &unitAgentService{revokeErr: errors.New("boom")}, "/agents/7/revoke-tokens", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodPost, tc.path, "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitStatsAndFindAvailable(t *testing.T) {
	t.Run("stats without agent id", func(t *testing.T) {
		svc := &unitAgentService{stats: &agentdelivery.AgentStats{Total: 3}}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodGet, "/agents/stats", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("stats with valid and invalid agent id", func(t *testing.T) {
		svc := &unitAgentService{}
		r, _ := newAgentUnitRouter(svc)
		if w := agentUnitRequest(r, http.MethodGet, "/agents/stats?agent_id=9", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if w := agentUnitRequest(r, http.MethodGet, "/agents/stats?agent_id=zz", ""); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
	})

	t.Run("stats error", func(t *testing.T) {
		svc := &unitAgentService{statsErr: errors.New("boom")}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodGet, "/agents/stats", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})

	findCases := []struct {
		name string
		svc  *unitAgentService
		want int
	}{
		{"success", &unitAgentService{info: &agentdelivery.AgentInfo{UserID: 1}}, http.StatusOK},
		{"not found sentinel", &unitAgentService{findErr: errors.New("no available agent found")}, http.StatusNotFound},
		{"not found generic", &unitAgentService{findErr: errors.New("agent not found")}, http.StatusNotFound},
		{"invalid input", &unitAgentService{findErr: errors.New("invalid priority")}, http.StatusBadRequest},
		{"internal", &unitAgentService{findErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range findCases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newAgentUnitRouter(tc.svc)
			w := agentUnitRequest(r, http.MethodGet, "/agents/find-available?skills=a&skills=b&priority=high", "")
			if w.Code != tc.want {
				t.Fatalf("status = %d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

func TestAgentHandlerUnitAuditSnapshotGuards(t *testing.T) {
	t.Run("snapshot get error is ignored on success", func(t *testing.T) {
		svc := &unitAgentService{getErr: errors.New("lookup failed")}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodPost, "/agents/8/online", "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("snapshot with zero user id", func(t *testing.T) {
		svc := &unitAgentService{}
		r, _ := newAgentUnitRouter(svc)
		w := agentUnitRequest(r, http.MethodPut, "/agents/0/status", `{"status":"online"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("nil agent service snapshot guard", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		h := &AgentHandler{}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/agents/1/online", nil)
		h.setAgentAuditSnapshot(c, 1, true)
	})

	t.Run("nil logger does not panic", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		h := NewAgentHandler(&unitAgentService{statusErr: errors.New("boom")}, nil)
		r := gin.New()
		r.POST("/agents/:id/online", h.AgentGoOnline)
		w := agentUnitRequest(r, http.MethodPost, "/agents/1/online", "")
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", w.Code)
		}
	})
}

func TestAgentHandlerUnitRegisterRoutesAndInfoJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &unitAgentService{online: []*agentdelivery.AgentInfo{{UserID: 2, Username: "a2"}}}
	r, h := newAgentUnitRouter(svc)
	if h == nil {
		t.Fatal("handler nil")
	}
	w := agentUnitRequest(r, http.MethodGet, "/agents/online", "")
	trimmed := strings.TrimSpace(w.Body.String())
	if !strings.HasPrefix(trimmed, "[") {
		data, _ := json.Marshal([]*agentdelivery.AgentInfo{})
		if trimmed != string(data) {
			t.Fatalf("unexpected body %s", trimmed)
		}
	}
}
