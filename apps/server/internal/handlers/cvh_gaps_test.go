package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"servify/apps/server/internal/config"
	"servify/apps/server/internal/models"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	analyticscontract "servify/apps/server/internal/modules/analytics/contract"
	authapp "servify/apps/server/internal/modules/auth/application"
	authdelivery "servify/apps/server/internal/modules/auth/delivery"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	"servify/apps/server/internal/modules/webhook/delivery"
	oidcplatform "servify/apps/server/internal/platform/auth/oidc"
)

// ---------------------------------------------------------------------------
// APIKeyHandler 长尾分支
// ---------------------------------------------------------------------------

type cvhAPIKeyListErrService struct {
	*fakeAPIKeyService
}

func (s *cvhAPIKeyListErrService) List(context.Context) ([]models.APIKey, error) {
	return nil, errors.New("db down")
}

func TestCvhAPIKeyGaps(t *testing.T) {
	t.Run("list service error", func(t *testing.T) {
		svc := &cvhAPIKeyListErrService{fakeAPIKeyService: &fakeAPIKeyService{}}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodGet, "/api/v1/api-keys", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list api keys")
	})

	t.Run("create invalid json", func(t *testing.T) {
		svc := &fakeAPIKeyService{}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodPost, "/api/v1/api-keys", `{`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})

	t.Run("create service error", func(t *testing.T) {
		svc := &fakeAPIKeyService{createErr: errors.New("boom")}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodPost, "/api/v1/api-keys", `{"name":"k"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to create api key")
	})

	t.Run("revoke invalid id", func(t *testing.T) {
		svc := &fakeAPIKeyService{}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodPost, "/api/v1/api-keys/abc/revoke", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid api key id")
	})

	t.Run("revoke generic error", func(t *testing.T) {
		svc := &fakeAPIKeyService{revokeErr: errors.New("boom")}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodPost, "/api/v1/api-keys/5/revoke", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to revoke api key")
	})

	t.Run("delete invalid id", func(t *testing.T) {
		svc := &fakeAPIKeyService{}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodDelete, "/api/v1/api-keys/abc", "")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid api key id")
	})

	t.Run("delete service error", func(t *testing.T) {
		svc := &fakeAPIKeyService{deleteErr: errors.New("boom")}
		w := doAPIKeyRequest(newAPIKeyTestRouter(t, svc), http.MethodDelete, "/api/v1/api-keys/5", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to delete api key")
	})
}

// ---------------------------------------------------------------------------
// WebhookHandler 长尾分支
// ---------------------------------------------------------------------------

var cvhWebhookNotFound = errors.New("webhook endpoint not found")

type cvhWebhookService struct {
	listErr    error
	createErr  error
	updateErr  error
	deleteErr  error
	rotateErr  error
	testErr    error
	deliverErr error
}

func (s *cvhWebhookService) ListEndpoints(context.Context) ([]models.WebhookEndpoint, error) {
	return nil, s.listErr
}

func (s *cvhWebhookService) CreateEndpoint(context.Context, *delivery.EndpointCreateRequest) (*models.WebhookEndpoint, string, error) {
	return nil, "", s.createErr
}

func (s *cvhWebhookService) UpdateEndpoint(context.Context, uint, *delivery.EndpointUpdateRequest) (*models.WebhookEndpoint, error) {
	return nil, s.updateErr
}

func (s *cvhWebhookService) DeleteEndpoint(context.Context, uint) error { return s.deleteErr }

func (s *cvhWebhookService) RotateEndpointSecret(context.Context, uint) (*models.WebhookEndpoint, string, error) {
	return nil, "", s.rotateErr
}

func (s *cvhWebhookService) TestEndpoint(context.Context, uint) (*models.WebhookDelivery, error) {
	return nil, s.testErr
}

func (s *cvhWebhookService) ListDeliveries(context.Context, delivery.DeliveryListQuery) ([]models.WebhookDelivery, int64, error) {
	return nil, 0, s.deliverErr
}

func (s *cvhWebhookService) RedeliverDelivery(context.Context, uint) (*models.WebhookDelivery, error) {
	return &models.WebhookDelivery{ID: 1}, nil
}

func (s *cvhWebhookService) SupportedEvents() []string { return []string{"ping"} }

func TestCvhWebhookListError(t *testing.T) {
	w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{listErr: errors.New("boom")}),
		http.MethodGet, "/api/v1/webhooks", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to list webhooks")
}

func TestCvhWebhookCreateErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		msg  string
	}{
		{"generic", errors.New("boom"), http.StatusBadRequest, "Failed to create webhook"},
		{"not found", cvhWebhookNotFound, http.StatusNotFound, "Failed to create webhook"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cvhWebhookService{createErr: tc.err}
			w := doWebhookRequest(t, newWebhookTestRouter(t, svc), http.MethodPost, "/api/v1/webhooks",
				[]byte(`{"name":"n","url":"https://x.example.com"}`))
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), tc.msg)
		})
	}
}

func TestCvhWebhookUpdateErrors(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{}), http.MethodPut, "/api/v1/webhooks/1", []byte(`{`))
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid request")
	})
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"generic", errors.New("boom"), http.StatusBadRequest},
		{"not found", cvhWebhookNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cvhWebhookService{updateErr: tc.err}
			w := doWebhookRequest(t, newWebhookTestRouter(t, svc), http.MethodPut, "/api/v1/webhooks/1", []byte(`{}`))
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to update webhook")
		})
	}
}

func TestCvhWebhookDeleteInvalidID(t *testing.T) {
	w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{}), http.MethodDelete, "/api/v1/webhooks/abc", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid id")
}

func TestCvhWebhookRotateSecretErrors(t *testing.T) {
	t.Run("invalid id", func(t *testing.T) {
		w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{}), http.MethodPost, "/api/v1/webhooks/abc/secret", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"generic", errors.New("boom"), http.StatusBadRequest},
		{"not found", cvhWebhookNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cvhWebhookService{rotateErr: tc.err}
			w := doWebhookRequest(t, newWebhookTestRouter(t, svc), http.MethodPost, "/api/v1/webhooks/1/secret", nil)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to rotate webhook secret")
		})
	}
}

func TestCvhWebhookTestEndpointErrors(t *testing.T) {
	t.Run("invalid id", func(t *testing.T) {
		w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{}), http.MethodPost, "/api/v1/webhooks/abc/test", nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"generic", errors.New("boom"), http.StatusBadRequest},
		{"not found", cvhWebhookNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &cvhWebhookService{testErr: tc.err}
			w := doWebhookRequest(t, newWebhookTestRouter(t, svc), http.MethodPost, "/api/v1/webhooks/1/test", nil)
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to send test event")
		})
	}
}

func TestCvhWebhookListDeliveriesError(t *testing.T) {
	svc := &cvhWebhookService{deliverErr: errors.New("boom")}
	w := doWebhookRequest(t, newWebhookTestRouter(t, svc), http.MethodGet, "/api/v1/webhooks/deliveries", nil)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "Failed to list deliveries")
}

func TestCvhWebhookRedeliverInvalidID(t *testing.T) {
	w := doWebhookRequest(t, newWebhookTestRouter(t, &cvhWebhookService{}), http.MethodPost, "/api/v1/webhooks/deliveries/abc/redeliver", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// AuditHandler.ExportCSV limit 兜底
// ---------------------------------------------------------------------------

func TestCvhAuditExportCSVLimitFloor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &stubAuditQueryService{items: []models.AuditLog{{ID: 1, Action: "tickets.create"}}, total: 1}
	r := gin.New()
	RegisterAuditRoutes(&r.RouterGroup, NewAuditHandler(svc))

	req := httptest.NewRequest(http.MethodGet, "/audit/logs/export?limit=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/csv")
	assert.Equal(t, 1000, svc.query.PageSize, "limit=0 应回落到默认 1000")
	assert.Contains(t, w.Body.String(), "tickets.create")
}

// ---------------------------------------------------------------------------
// EnhancedHealthHandler.Health degraded 分支
// ---------------------------------------------------------------------------

func TestCvhEnhancedHealthDegraded(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.Monitoring.HealthChecks.Database = true
	cfg.Monitoring.HealthChecks.Redis = false
	cfg.Monitoring.HealthChecks.KnowledgeProvider = false
	cfg.Monitoring.HealthChecks.WeKnora = false
	cfg.WeKnora.Enabled = false
	cfg.Dify.Enabled = false

	ai := aidelivery.NewAIService("", "")
	h := NewEnhancedHealthHandler(cfg, aidelivery.NewHandlerServiceAdapter(ai), nil, nil)

	r := dxcRouter()
	r.GET("/health", h.Health)
	w := dxcDo(r, http.MethodGet, "/health", "")

	assert.Equal(t, http.StatusOK, w.Code, "degraded 仍返回 200")
	assert.Contains(t, w.Body.String(), `"status":"degraded"`)
	assert.Contains(t, w.Body.String(), "database connection not initialized")
}

// ---------------------------------------------------------------------------
// SatisfactionHandler.ListSatisfactions 合法日期分支
// ---------------------------------------------------------------------------

func TestCvhSatisfactionListDateParamInteraction(t *testing.T) {
	// ShouldBindQuery 对 *time.Time 只认 RFC3339，而 handler 二次解析只认
	// YYYY-MM-DD：RFC3339 值能通过绑定、必然被 handler 判为非法格式，
	// YYYY-MM-DD 值则在绑定层就被拒。这里锁定该交互的实际行为。
	r, _, _ := newSatisfactionUnitRouter(&unitSatisfactionService{})
	w := satisfactionUnitRequest(r, http.MethodGet, "/satisfactions?date_from=2026-01-01T00:00:00Z", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Invalid date_from format")
}

// ---------------------------------------------------------------------------
// OpenConversationHandler.Messages 长尾分支
// ---------------------------------------------------------------------------

type cvhOpenConvMessages struct {
	*fakeOpenConversationReader
	msgErr error
}

func (f *cvhOpenConvMessages) ListRecentMessages(ctx context.Context, conversationID string, limit int) ([]conversationapp.ConversationMessageDTO, error) {
	if f.msgErr != nil {
		return nil, f.msgErr
	}
	return f.fakeOpenConversationReader.ListRecentMessages(ctx, conversationID, limit)
}

func TestCvhOpenConversationMessagesGaps(t *testing.T) {
	t.Run("limit floored to 50", func(t *testing.T) {
		reader := &cvhOpenConvMessages{fakeOpenConversationReader: &fakeOpenConversationReader{
			messages: []conversationapp.ConversationMessageDTO{{ID: "m1"}},
		}}
		w := dxcDo(newOpenConversationRouter(t, reader), http.MethodGet, "/api/v1/conversations/s1/messages?limit=0", "")
		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "m1")
	})

	t.Run("limit capped to 200", func(t *testing.T) {
		reader := &cvhOpenConvMessages{fakeOpenConversationReader: &fakeOpenConversationReader{}}
		w := dxcDo(newOpenConversationRouter(t, reader), http.MethodGet, "/api/v1/conversations/s1/messages?limit=500", "")
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("conversation not found", func(t *testing.T) {
		reader := &cvhOpenConvMessages{
			fakeOpenConversationReader: &fakeOpenConversationReader{},
			msgErr:                     gorm.ErrRecordNotFound,
		}
		w := dxcDo(newOpenConversationRouter(t, reader), http.MethodGet, "/api/v1/conversations/nope/messages", "")
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Conversation not found")
	})

	t.Run("service error", func(t *testing.T) {
		reader := &cvhOpenConvMessages{
			fakeOpenConversationReader: &fakeOpenConversationReader{},
			msgErr:                     errors.New("boom"),
		}
		w := dxcDo(newOpenConversationRouter(t, reader), http.MethodGet, "/api/v1/conversations/s1/messages", "")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to list messages")
	})
}

// ---------------------------------------------------------------------------
// StatisticsExportHandler 长尾分支
// ---------------------------------------------------------------------------

func cvhExportRouter(analytics *unitAnalyticsService, reader SatisfactionStatsReader, logger *logrus.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterStatisticsRoutes(&r.RouterGroup, NewStatisticsHandler(analytics, nil), NewStatisticsExportHandler(analytics, reader, logger))
	return r
}

func cvhExportGet(r *gin.Engine, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, query, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestCvhStatisticsExportBuildRowsErrors(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	cases := []struct {
		name      string
		analytics *unitAnalyticsService
		want      int
	}{
		{"time_range", &unitAnalyticsService{timeRangeErr: errors.New("boom")}, http.StatusInternalServerError},
		{"agent_performance", &unitAnalyticsService{agentPerfErr: errors.New("boom")}, http.StatusInternalServerError},
		{"ticket_category", &unitAnalyticsService{categoryErr: errors.New("boom")}, http.StatusInternalServerError},
		{"customer_source", &unitAnalyticsService{sourceErr: errors.New("boom")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := cvhExportRouter(tc.analytics, &stubSatisfactionStatsReader{}, logger)
			w := cvhExportGet(r, "/statistics/export?type="+tc.name+"&from=2026-01-01&to=2026-01-02")
			assert.Equal(t, tc.want, w.Code)
			assert.Contains(t, w.Body.String(), "Failed to export statistics")
			assert.Contains(t, w.Body.String(), "boom")
		})
	}
}

func TestCvhStatisticsExportTicketPriority(t *testing.T) {
	analytics := &unitAnalyticsService{category: []analyticscontract.CategoryStats{{Category: "billing", Count: 7}}}
	r := cvhExportRouter(analytics, &stubSatisfactionStatsReader{}, nil)
	w := cvhExportGet(r, "/statistics/export?type=ticket_priority&from=2026-01-01&to=2026-01-02")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "billing")
}

func TestCvhStatisticsExportSatisfactionGaps(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	t.Run("satisfaction service unavailable", func(t *testing.T) {
		r := cvhExportRouter(&unitAnalyticsService{}, nil, logger)
		w := cvhExportGet(r, "/statistics/export?type=satisfaction&from=2026-01-01&to=2026-01-02")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "satisfaction service unavailable")
	})

	t.Run("satisfaction reader error", func(t *testing.T) {
		reader := &stubSatisfactionStatsReader{err: errors.New("boom")}
		r := cvhExportRouter(&unitAnalyticsService{}, reader, logger)
		w := cvhExportGet(r, "/statistics/export?type=satisfaction&from=2026-01-01&to=2026-01-02")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to export statistics")
	})
}

func TestCvhStatisticsBuildRowsUnsupportedType(t *testing.T) {
	h := NewStatisticsExportHandler(nil, nil, nil)
	_, _, err := h.buildRows(context.Background(), "bogus-type", time.Now(), time.Now())
	assert.ErrorContains(t, err, `unsupported export type "bogus-type"`)
}

// ---------------------------------------------------------------------------
// AuthHandler.Login 两步验证挑战分支
// ---------------------------------------------------------------------------

type cvhTwoFactorLoginService struct {
	*stubAuthService
}

func (s *cvhTwoFactorLoginService) Login(context.Context, authdelivery.LoginInput, authdelivery.AuthSessionMetadata) (*authdelivery.LoginOutcome, error) {
	return &authdelivery.LoginOutcome{TwoFactorRequired: true, ChallengeToken: "ch-42", ExpiresIn: 300}, nil
}

func TestCvhAuthLoginTwoFactorChallenge(t *testing.T) {
	r := dxcRouter()
	r.POST("/auth/login", NewAuthHandler(&cvhTwoFactorLoginService{&stubAuthService{}}).Login)

	w := dxcDo(r, http.MethodPost, "/auth/login", `{"username":"u","password":"p"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		TwoFactorRequired bool   `json:"two_factor_required"`
		ChallengeToken    string `json:"challenge_token"`
		ExpiresIn         int    `json:"expires_in"`
	}
	dxcDecode(t, w, &resp)
	assert.True(t, resp.TwoFactorRequired)
	assert.Equal(t, "ch-42", resp.ChallengeToken)
	assert.Equal(t, 300, resp.ExpiresIn)
	assert.NotContains(t, w.Body.String(), `"token":`, "挑战步不应发放正式 token")
}

// ---------------------------------------------------------------------------
// OIDCHandler.resolveCallback 长尾分支
// ---------------------------------------------------------------------------

func TestCvhOIDCCallbackDisabledProvider(t *testing.T) {
	cfg := config.OIDCConfig{FrontendBaseURL: "http://fe.example.com"}
	h := NewOIDCHandler(nil, cfg, nil, "dev")
	w := performOIDC(t, h, "/callback", nil)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "error=oidc_disabled")
}

func TestCvhOIDCCallbackFlowCookieGaps(t *testing.T) {
	t.Run("missing cookie", func(t *testing.T) {
		h, _ := newOIDCHandler(t, nil)
		w := performOIDC(t, h, "/callback?state=st&code=c", nil)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_state")
	})

	t.Run("malformed base64 cookie", func(t *testing.T) {
		h, _ := newOIDCHandler(t, nil)
		headers := http.Header{"Cookie": []string{oidcFlowCookie + "=!!!"}}
		w := performOIDC(t, h, "/callback?state=st&code=c", headers)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_state")
	})

	t.Run("non-json cookie payload", func(t *testing.T) {
		h, _ := newOIDCHandler(t, nil)
		encoded := base64RawURL("not-json")
		headers := http.Header{"Cookie": []string{oidcFlowCookie + "=" + encoded}}
		w := performOIDC(t, h, "/callback?state=st&code=c", headers)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_state")
	})

	t.Run("missing code", func(t *testing.T) {
		h, _ := newOIDCHandler(t, nil)
		cookie := oidcFlowCookieFor(t, "st-9", "n-9", "v-9")
		headers := http.Header{"Cookie": []string{cookie}}
		w := performOIDC(t, h, "/callback?state=st-9", headers)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_failed")
	})

	t.Run("id token verification fails", func(t *testing.T) {
		h, _ := newOIDCHandler(t, func(_ *config.OIDCConfig, verifier *stubOIDCVerifier) {
			verifier.err = errors.New("bad id token")
		})
		cookie := oidcFlowCookieFor(t, "st-v", "n-v", "v-v")
		headers := http.Header{"Cookie": []string{cookie}}
		w := performOIDC(t, h, "/callback?state=st-v&code=abc", headers)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_failed")
	})

	t.Run("token exchange fails", func(t *testing.T) {
		cfg := config.GetDefaultConfig()
		cfg.JWT.Secret = "test-secret"
		cfg.JWT.ExpiresIn = time.Hour
		cfg.JWT.RefreshExpiresIn = 24 * time.Hour
		cfg.OIDC.FrontendBaseURL = "http://fe.example.com"

		provider := oidcplatform.New(&stubOIDCVerifier{claims: &oidcplatform.Claims{Subject: "s"}}, &oauth2.Config{
			ClientID:     "c",
			ClientSecret: "s",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://idp.example.com/authorize",
				TokenURL: "http://127.0.0.1:1/token", // 无监听端口 → 交换必败
			},
		}, nil)
		authSvc := authapp.NewService(newOIDCHandlerTestDB(t), cfg)
		h := NewOIDCHandler(provider, cfg.OIDC, authSvc, "dev")

		cookie := oidcFlowCookieFor(t, "st-x", "n-x", "v-x")
		headers := http.Header{"Cookie": []string{cookie}}
		w := performOIDC(t, h, "/callback?state=st-x&code=abc", headers)
		assert.Equal(t, http.StatusFound, w.Code)
		assert.Contains(t, w.Header().Get("Location"), "error=oidc_failed")
	})
}

func base64RawURL(raw string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}
