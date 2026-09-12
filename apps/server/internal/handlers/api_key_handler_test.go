package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"servify/apps/server/internal/models"
	"servify/apps/server/internal/services"

	"github.com/gin-gonic/gin"
)

type fakeAPIKeyService struct {
	list      []models.APIKey
	plaintext string
	createErr error
	revokeErr error
	deleteErr error
}

func (f *fakeAPIKeyService) List(ctx context.Context) ([]models.APIKey, error) { return f.list, nil }
func (f *fakeAPIKeyService) Create(ctx context.Context, req *services.APIKeyCreateRequest, createdBy string) (*models.APIKey, string, error) {
	if f.createErr != nil {
		return nil, "", f.createErr
	}
	return &models.APIKey{ID: 1, Name: req.Name, Prefix: "sv_abc12345"}, f.plaintext, nil
}
func (f *fakeAPIKeyService) Revoke(ctx context.Context, id uint) (*models.APIKey, error) {
	if f.revokeErr != nil {
		return nil, f.revokeErr
	}
	now := time.Now()
	return &models.APIKey{ID: id, RevokedAt: &now}, nil
}
func (f *fakeAPIKeyService) Delete(ctx context.Context, id uint) error { return f.deleteErr }

func newAPIKeyTestRouter(t *testing.T, svc APIKeyService) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api/v1")
	RegisterAPIKeyRoutes(api, NewAPIKeyHandler(svc))
	return r
}

func doAPIKeyRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestAPIKeyRoutesLifecycleAndSecretHygiene(t *testing.T) {
	svc := &fakeAPIKeyService{plaintext: "sv_plain_once", list: []models.APIKey{{ID: 1, Name: "ci", Prefix: "sv_abc12345"}}}
	r := newAPIKeyTestRouter(t, svc)

	// 创建：明文一次性返回
	rec := doAPIKeyRequest(r, http.MethodPost, "/api/v1/api-keys", `{"name":"ci key"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sv_plain_once") {
		t.Fatal("create response should carry the plaintext once")
	}

	// 列表：永不回显明文
	rec = doAPIKeyRequest(r, http.MethodGet, "/api/v1/api-keys", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "sv_plain_once") {
		t.Fatalf("list must not leak plaintext: %d %s", rec.Code, rec.Body.String())
	}

	// 吊销
	rec = doAPIKeyRequest(r, http.MethodPost, "/api/v1/api-keys/1/revoke", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"revoked_at"`) {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}

	// 删除
	rec = doAPIKeyRequest(r, http.MethodDelete, "/api/v1/api-keys/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
}

func TestAPIKeyRoutesErrors(t *testing.T) {
	svc := &fakeAPIKeyService{createErr: errors.New("name required"), revokeErr: services.ErrAPIKeyNotFound, deleteErr: services.ErrAPIKeyNotFound}
	r := newAPIKeyTestRouter(t, svc)

	if rec := doAPIKeyRequest(r, http.MethodPost, "/api/v1/api-keys", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("create validation should 400, got %d", rec.Code)
	}
	if rec := doAPIKeyRequest(r, http.MethodPost, "/api/v1/api-keys/not-a-number/revoke", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id should 400, got %d", rec.Code)
	}
	if rec := doAPIKeyRequest(r, http.MethodPost, "/api/v1/api-keys/99/revoke", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("revoke missing should 404, got %d", rec.Code)
	}
	if rec := doAPIKeyRequest(r, http.MethodDelete, "/api/v1/api-keys/99", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing should 404, got %d", rec.Code)
	}
}

// 编译期保证生产实现满足 handler 依赖接口。
var _ APIKeyService = (*services.APIKeyService)(nil)
