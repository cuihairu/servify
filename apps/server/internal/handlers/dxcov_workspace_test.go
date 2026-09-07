package handlers

import (
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/services"

	"github.com/stretchr/testify/assert"
)

func dxcWorkspaceReader() *unitWorkspaceReader {
	return &unitWorkspaceReader{
		overview: &services.WorkspaceOverview{TotalActiveSessions: 4, OnlineAgents: 2},
	}
}

func TestDxcNewWorkspaceHandler(t *testing.T) {
	rd := dxcWorkspaceReader()
	h := NewWorkspaceHandler(rd)
	assert.NotNil(t, h)
	assert.Equal(t, rd, h.service)
}

func TestDxcWorkspaceHandlerGetOverview(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		want     int
		wantCode int
	}{
		{"default limit", "", 10, http.StatusOK},
		{"explicit limit", "?limit=3", 3, http.StatusOK},
		{"invalid limit", "?limit=abc", 10, http.StatusOK},
		{"zero limit", "?limit=0", 10, http.StatusOK},
		{"negative limit", "?limit=-2", 10, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := dxcWorkspaceReader()
			r := dxcRouter()
			r.GET("/omni/workspace", NewWorkspaceHandler(rd).GetOverview)
			w := dxcDo(r, http.MethodGet, "/omni/workspace"+tc.query, "")
			assert.Equal(t, tc.wantCode, w.Code)
			assert.Equal(t, tc.want, rd.lastLimit)
			if tc.wantCode == http.StatusOK {
				assert.Contains(t, w.Body.String(), `"total_active_sessions":4`)
			}
		})
	}

	t.Run("service error", func(t *testing.T) {
		r := dxcRouter()
		r.GET("/omni/workspace", NewWorkspaceHandler(&unitWorkspaceReader{err: errors.New("boom")}).GetOverview)
		w := dxcDo(r, http.MethodGet, "/omni/workspace", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to load workspace overview")
	})
}

func TestDxcRegisterWorkspaceRoutes(t *testing.T) {
	r := dxcRouter()
	RegisterWorkspaceRoutes(&r.RouterGroup, NewWorkspaceHandler(dxcWorkspaceReader()))
	w := dxcDo(r, http.MethodGet, "/omni/workspace", "")
	assert.Equal(t, http.StatusOK, w.Code)
}
