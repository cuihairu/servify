package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"servify/apps/server/internal/models"
	assistdelivery "servify/apps/server/internal/modules/assist/delivery"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

var _ assistdelivery.HandlerService = (*cvhAssistService)(nil)

// cvhAssistService 远程协助服务 inline mock（管理面 + 访客面共用）。
type cvhAssistService struct {
	startSession *models.RemoteAssistSession
	startErr     error
	endSession   *models.RemoteAssistSession
	endErr       error
	getSession   *models.RemoteAssistSession
	getErr       error
	sessions     []models.RemoteAssistSession
	listErr      error
	annotation   *models.RemoteAssistAnnotation
	annotErr     error
	annotations  []models.RemoteAssistAnnotation
	listAnnotErr error
	deleteErr    error
	attach       *models.RemoteAssistSession
	attachErr    error

	startCmd    assistdelivery.StartCommand
	endCmd      assistdelivery.EndCommand
	annotCmd    assistdelivery.AnnotationCommand
	annotID     uint
	attachCmd   assistdelivery.RecordingMeta
	attachID    uint
	attachUID   uint
	endID       uint
	listConvID  string
	listLimit   int
	deleteID    uint
	getID       uint
}

func (s *cvhAssistService) StartSession(_ context.Context, cmd assistdelivery.StartCommand) (*models.RemoteAssistSession, error) {
	s.startCmd = cmd
	if s.startErr != nil {
		return nil, s.startErr
	}
	if s.startSession == nil {
		return &models.RemoteAssistSession{ID: 11, Status: "active"}, nil
	}
	return s.startSession, nil
}

func (s *cvhAssistService) EndSession(_ context.Context, id uint, cmd assistdelivery.EndCommand) (*models.RemoteAssistSession, error) {
	s.endID = id
	s.endCmd = cmd
	if s.endErr != nil {
		return nil, s.endErr
	}
	if s.endSession == nil {
		return &models.RemoteAssistSession{ID: id, Status: "ended"}, nil
	}
	return s.endSession, nil
}

func (s *cvhAssistService) GetSession(_ context.Context, id uint) (*models.RemoteAssistSession, error) {
	s.getID = id
	return s.getSession, s.getErr
}

func (s *cvhAssistService) ListSessions(_ context.Context, conversationSessionID string, limit int) ([]models.RemoteAssistSession, error) {
	s.listConvID = conversationSessionID
	s.listLimit = limit
	return s.sessions, s.listErr
}

func (s *cvhAssistService) AddAnnotation(_ context.Context, assistSessionID uint, cmd assistdelivery.AnnotationCommand) (*models.RemoteAssistAnnotation, error) {
	s.annotID = assistSessionID
	s.annotCmd = cmd
	return s.annotation, s.annotErr
}

func (s *cvhAssistService) ListAnnotations(_ context.Context, assistSessionID uint) ([]models.RemoteAssistAnnotation, error) {
	s.annotID = assistSessionID
	return s.annotations, s.listAnnotErr
}

func (s *cvhAssistService) DeleteAnnotation(_ context.Context, id uint) error {
	s.deleteID = id
	return s.deleteErr
}

func (s *cvhAssistService) AttachRecording(_ context.Context, id uint, customerUserID uint, meta assistdelivery.RecordingMeta) (*models.RemoteAssistSession, error) {
	s.attachID = id
	s.attachUID = customerUserID
	s.attachCmd = meta
	return s.attach, s.attachErr
}

func cvhAssistRouter(svc *cvhAssistService, middleware ...gin.HandlerFunc) *gin.Engine {
	r := dxcRouter()
	for _, mw := range middleware {
		r.Use(mw)
	}
	RegisterAssistRoutes(r.Group("/api"), NewAssistHandler(svc))
	return r
}

func cvhAssistRecordingRouter(svc *cvhAssistService, middleware ...gin.HandlerFunc) *gin.Engine {
	r := dxcRouter()
	for _, mw := range middleware {
		r.Use(mw)
	}
	r.POST("/api/v1/remote-assist/:id/recording", NewAssistRecordingHandler(svc).AttachRecording)
	return r
}

func TestCvhNewAssistHandlers(t *testing.T) {
	svc := &cvhAssistService{}
	h := NewAssistHandler(svc)
	assert.NotNil(t, h)
	assert.Equal(t, svc, h.service)

	rh := NewAssistRecordingHandler(svc)
	assert.NotNil(t, rh)
	assert.Equal(t, svc, rh.service)
}

func TestCvhRegisterAssistRoutes(t *testing.T) {
	r := cvhAssistRouter(&cvhAssistService{})
	routes := r.Routes()
	assert.Len(t, routes, 7)
	want := map[string]bool{
		"POST /api/remote-assist/sessions":                false,
		"GET /api/remote-assist/sessions":                 false,
		"GET /api/remote-assist/sessions/:id":             false,
		"POST /api/remote-assist/sessions/:id/end":        false,
		"GET /api/remote-assist/sessions/:id/annotations":  false,
		"POST /api/remote-assist/sessions/:id/annotations": false,
		"DELETE /api/remote-assist/annotations/:id":        false,
	}
	for _, rt := range routes {
		key := rt.Method + " " + rt.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, seen := range want {
		assert.True(t, seen, "route %s not registered", key)
	}
}

func TestCvhAssistStartSession(t *testing.T) {
	path := "/api/remote-assist/sessions"

	t.Run("success", func(t *testing.T) {
		svc := &cvhAssistService{}
		body := `{"conversation_session_id":"sess-1","agent_user_id":7,"tenant_id":"t1","workspace_id":"w1"}`
		w := dxcDo(cvhAssistRouter(svc), http.MethodPost, path, body)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "sess-1", svc.startCmd.ConversationSessionID)
		assert.Equal(t, uint(7), svc.startCmd.AgentUserID)
		assert.Equal(t, "t1", svc.startCmd.TenantID)
		assert.Equal(t, "w1", svc.startCmd.WorkspaceID)
		assert.Contains(t, w.Body.String(), `"status":"active"`)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvhAssistRouter(&cvhAssistService{}), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})

	t.Run("missing required field", func(t *testing.T) {
		w := dxcDo(cvhAssistRouter(&cvhAssistService{}), http.MethodPost, path, `{"agent_user_id":7}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	errorCases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", assistdelivery.ErrAssistNotFound, http.StatusNotFound},
		{"session required", assistdelivery.ErrAssistSessionRequired, http.StatusBadRequest},
		{"shape invalid", assistdelivery.ErrAssistShapeInvalid, http.StatusBadRequest},
		{"payload invalid", assistdelivery.ErrAssistPayloadInvalid, http.StatusBadRequest},
		{"forbidden", assistdelivery.ErrAssistForbidden, http.StatusForbidden},
		{"already ended", assistdelivery.ErrAssistAlreadyEnded, http.StatusConflict},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvhAssistService{startErr: tc.err}
			w := dxcDo(cvhAssistRouter(svc), http.MethodPost, path, `{"conversation_session_id":"sess-1"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to start remote assist")
		})
	}
}

func TestCvhAssistEndSession(t *testing.T) {
	path := "/api/remote-assist/sessions/9/end"

	t.Run("success with recording meta", func(t *testing.T) {
		svc := &cvhAssistService{}
		body := `{"outcome":"ended","recording_key":"rec.webm","recording_mime":"video/webm","recording_duration_ms":95,"recording_size":1024}`
		w := dxcDo(cvhAssistRouter(svc), http.MethodPost, path, body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, uint(9), svc.endID)
		assert.Equal(t, "ended", svc.endCmd.Outcome)
		assert.Equal(t, "rec.webm", svc.endCmd.RecordingKey)
		assert.Equal(t, "video/webm", svc.endCmd.RecordingMime)
		assert.Equal(t, int64(95), svc.endCmd.RecordingDurationMs)
		assert.Equal(t, int64(1024), svc.endCmd.RecordingSize)
	})

	cases := []struct {
		name string
		svc  *cvhAssistService
		id   string
		body string
		want int
	}{
		{"invalid id", &cvhAssistService{}, "abc", `{}`, http.StatusBadRequest},
		{"invalid json", &cvhAssistService{}, "9", `{`, http.StatusBadRequest},
		{"already ended", &cvhAssistService{endErr: assistdelivery.ErrAssistAlreadyEnded}, "9", `{"outcome":"ended"}`, http.StatusConflict},
		{"not found", &cvhAssistService{endErr: assistdelivery.ErrAssistNotFound}, "9", `{"outcome":"ended"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAssistRouter(tc.svc), http.MethodPost, "/api/remote-assist/sessions/"+tc.id+"/end", tc.body)
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusConflict || tc.want == http.StatusNotFound {
				assert.Contains(t, w.Body.String(), "Failed to end remote assist")
			}
		})
	}
}

func TestCvhAssistListSessions(t *testing.T) {
	t.Run("success with filters", func(t *testing.T) {
		svc := &cvhAssistService{sessions: []models.RemoteAssistSession{{ID: 1}, {ID: 2}}}
		w := dxcDo(cvhAssistRouter(svc), http.MethodGet, "/api/remote-assist/sessions?conversation_session_id=sess-1&limit=25", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "sess-1", svc.listConvID)
		assert.Equal(t, 25, svc.listLimit)
		var resp struct {
			Items []models.RemoteAssistSession `json:"items"`
			Total int                          `json:"total"`
		}
		dxcDecode(t, w, &resp)
		assert.Len(t, resp.Items, 2)
		assert.Equal(t, 2, resp.Total)
	})

	t.Run("default limit", func(t *testing.T) {
		svc := &cvhAssistService{}
		w := dxcDo(cvhAssistRouter(svc), http.MethodGet, "/api/remote-assist/sessions", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, svc.listConvID)
		assert.Equal(t, 100, svc.listLimit, "limit 缺省应为 100")
	})

	t.Run("service error", func(t *testing.T) {
		svc := &cvhAssistService{listErr: errors.New("boom")}
		w := dxcDo(cvhAssistRouter(svc), http.MethodGet, "/api/remote-assist/sessions", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list remote assist sessions")
	})
}

func TestCvhAssistGetSession(t *testing.T) {
	cases := []struct {
		name string
		svc  *cvhAssistService
		id   string
		want int
	}{
		{"success", &cvhAssistService{getSession: &models.RemoteAssistSession{ID: 9, Status: "ended"}}, "9", http.StatusOK},
		{"invalid id", &cvhAssistService{}, "abc", http.StatusBadRequest},
		{"forbidden", &cvhAssistService{getErr: assistdelivery.ErrAssistForbidden}, "9", http.StatusForbidden},
		{"generic error", &cvhAssistService{getErr: errors.New("boom")}, "9", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAssistRouter(tc.svc), http.MethodGet, "/api/remote-assist/sessions/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusOK {
				var got models.RemoteAssistSession
				dxcDecode(t, w, &got)
				assert.Equal(t, uint(9), got.ID)
			}
		})
	}
}

func TestCvhAssistAddAnnotation(t *testing.T) {
	path := "/api/remote-assist/sessions/9/annotations"
	asAgent := func(c *gin.Context) { c.Set("user_id", uint(7)); c.Next() }

	t.Run("success with payload", func(t *testing.T) {
		svc := &cvhAssistService{annotation: &models.RemoteAssistAnnotation{ID: 3, Shape: "rect"}}
		body := `{"timestamp_ms":12000,"shape":"rect","payload":{"x":0.1,"color":"#f5222d"}}`
		w := dxcDo(cvhAssistRouter(svc, asAgent), http.MethodPost, path, body)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, uint(9), svc.annotID)
		assert.Equal(t, int64(12000), svc.annotCmd.TimestampMs)
		assert.Equal(t, "rect", svc.annotCmd.Shape)
		assert.Contains(t, svc.annotCmd.Payload, `"x":0.1`)
		assert.Equal(t, uint(7), svc.annotCmd.CreatedBy, "标注人应取认证用户")
	})

	t.Run("payload omitted defaults to empty object", func(t *testing.T) {
		svc := &cvhAssistService{}
		w := dxcDo(cvhAssistRouter(svc, asAgent), http.MethodPost, path, `{"shape":"arrow"}`)
		assert.Equal(t, http.StatusCreated, w.Code)
		assert.Equal(t, "{}", svc.annotCmd.Payload)
	})

	cases := []struct {
		name string
		svc  *cvhAssistService
		id   string
		body string
		want int
	}{
		{"invalid id", &cvhAssistService{}, "abc", `{}`, http.StatusBadRequest},
		{"invalid json", &cvhAssistService{}, "9", `{`, http.StatusBadRequest},
		{"shape invalid", &cvhAssistService{annotErr: assistdelivery.ErrAssistShapeInvalid}, "9", `{"shape":"bad"}`, http.StatusBadRequest},
		{"payload invalid", &cvhAssistService{annotErr: assistdelivery.ErrAssistPayloadInvalid}, "9", `{"shape":"rect"}`, http.StatusBadRequest},
		{"not found", &cvhAssistService{annotErr: assistdelivery.ErrAssistNotFound}, "9", `{"shape":"rect"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAssistRouter(tc.svc, asAgent), http.MethodPost, "/api/remote-assist/sessions/"+tc.id+"/annotations", tc.body)
			assert.Equal(t, tc.want, w.Code)
			if tc.want != http.StatusBadRequest || tc.name == "shape invalid" || tc.name == "payload invalid" {
				assert.Contains(t, w.Body.String(), "Failed to add annotation")
			}
		})
	}
}

func TestCvhAssistListAnnotations(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &cvhAssistService{annotations: []models.RemoteAssistAnnotation{{ID: 1}, {ID: 2}, {ID: 3}}}
		w := dxcDo(cvhAssistRouter(svc), http.MethodGet, "/api/remote-assist/sessions/9/annotations", "")
		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Items []models.RemoteAssistAnnotation `json:"items"`
			Total int                             `json:"total"`
		}
		dxcDecode(t, w, &resp)
		assert.Len(t, resp.Items, 3)
		assert.Equal(t, 3, resp.Total)
	})

	t.Run("invalid id", func(t *testing.T) {
		w := dxcDo(cvhAssistRouter(&cvhAssistService{}), http.MethodGet, "/api/remote-assist/sessions/abc/annotations", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		svc := &cvhAssistService{listAnnotErr: errors.New("boom")}
		w := dxcDo(cvhAssistRouter(svc), http.MethodGet, "/api/remote-assist/sessions/9/annotations", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list annotations")
	})
}

func TestCvhAssistDeleteAnnotation(t *testing.T) {
	cases := []struct {
		name string
		svc  *cvhAssistService
		id   string
		want int
	}{
		{"success", &cvhAssistService{}, "5", http.StatusOK},
		{"invalid id", &cvhAssistService{}, "abc", http.StatusBadRequest},
		{"session required", &cvhAssistService{deleteErr: assistdelivery.ErrAssistSessionRequired}, "5", http.StatusBadRequest},
		{"forbidden", &cvhAssistService{deleteErr: assistdelivery.ErrAssistForbidden}, "5", http.StatusForbidden},
		{"not found", &cvhAssistService{deleteErr: assistdelivery.ErrAssistNotFound}, "5", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := dxcDo(cvhAssistRouter(tc.svc), http.MethodDelete, "/api/remote-assist/annotations/"+tc.id, "")
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusOK {
				var resp SuccessResponse
				dxcDecode(t, w, &resp)
				assert.Equal(t, "deleted", resp.Message)
				assert.Equal(t, uint(5), tc.svc.deleteID)
			}
		})
	}
}

func TestCvhAssistRecordingAttach(t *testing.T) {
	path := "/api/v1/remote-assist/9/recording"
	asVisitor := func(c *gin.Context) { c.Set("user_id", float64(12)); c.Next() }

	t.Run("success", func(t *testing.T) {
		svc := &cvhAssistService{attach: &models.RemoteAssistSession{ID: 9, RecordingKey: "rec.webm"}}
		body := `{"recording_key":"rec.webm","recording_mime":"video/webm","recording_duration_ms":95,"recording_size":1024}`
		w := dxcDo(cvhAssistRecordingRouter(svc, asVisitor), http.MethodPost, path, body)
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, uint(9), svc.attachID)
		assert.Equal(t, uint(12), svc.attachUID, "float64 型 user_id 应可解析")
		assert.Equal(t, "rec.webm", svc.attachCmd.Key)
		assert.Equal(t, "video/webm", svc.attachCmd.Mime)
		assert.Equal(t, int64(95), svc.attachCmd.DurationMs)
		assert.Equal(t, int64(1024), svc.attachCmd.Size)
	})

	t.Run("missing user forbidden", func(t *testing.T) {
		w := dxcDo(cvhAssistRecordingRouter(&cvhAssistService{}), http.MethodPost, path, `{"recording_key":"k"}`)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, w.Body.String(), "missing authenticated user")
	})

	t.Run("non-numeric user forbidden", func(t *testing.T) {
		badUser := func(c *gin.Context) { c.Set("user_id", "not-a-number"); c.Next() }
		w := dxcDo(cvhAssistRecordingRouter(&cvhAssistService{}, badUser), http.MethodPost, path, `{"recording_key":"k"}`)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid id", func(t *testing.T) {
		w := dxcDo(cvhAssistRecordingRouter(&cvhAssistService{}, asVisitor), http.MethodPost, "/api/v1/remote-assist/abc/recording", `{"recording_key":"k"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		w := dxcDo(cvhAssistRecordingRouter(&cvhAssistService{}, asVisitor), http.MethodPost, path, `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})

	errorCases := []struct {
		name string
		err  error
		want int
	}{
		{"not found", assistdelivery.ErrAssistNotFound, http.StatusNotFound},
		{"forbidden", assistdelivery.ErrAssistForbidden, http.StatusForbidden},
		{"generic", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range errorCases {
		t.Run("error "+tc.name, func(t *testing.T) {
			svc := &cvhAssistService{attachErr: tc.err}
			w := dxcDo(cvhAssistRecordingRouter(svc, asVisitor), http.MethodPost, path, `{"recording_key":"k"}`)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to attach recording")
		})
	}
}
