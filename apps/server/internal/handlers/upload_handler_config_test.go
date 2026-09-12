package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"servify/apps/server/internal/platform/storage"

	"github.com/gin-gonic/gin"
)

// TestFileUploadHandlerConfigWhitelist 验证 NewFileUploadHandlerWithConfig 的
// 白名单来自配置：显式列表生效，空列表回退内建默认。
func TestFileUploadHandlerConfigWhitelist(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRouter := func(h *FileUploadHandler) *gin.Engine {
		r := gin.New()
		r.POST("/upload", h.Upload)
		return r
	}

	t.Run("explicit config whitelist is enforced", func(t *testing.T) {
		h := NewFileUploadHandlerWithConfig(stubStorageP{}, UploadHandlerConfig{
			MaxSize:     1 << 20,
			AllowedExts: []string{".png"},
		})
		r := newRouter(h)

		// png 允许
		req, err := buildMultipart("/upload", "file", "a.png", []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 for allowed ext, got %d body=%s", w.Code, w.Body.String())
		}

		// txt 不在显式白名单内，即使内建默认允许
		req2, _ := buildMultipart("/upload", "file", "a.txt", []byte("x"))
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for ext outside config whitelist, got %d", w2.Code)
		}
	})

	t.Run("empty whitelist falls back to built-in", func(t *testing.T) {
		h := NewFileUploadHandlerWithConfig(stubStorageP{}, UploadHandlerConfig{MaxSize: 1 << 20})
		r := newRouter(h)

		req, _ := buildMultipart("/upload", "file", "a.zip", []byte("x"))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected built-in whitelist to allow .zip, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

type stubStorageP struct{}

func (stubStorageP) Save(key string, _ io.Reader, size int64) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Key: key, Size: size, URL: "/uploads/" + key}, nil
}
func (stubStorageP) Open(key string) (io.ReadCloser, error) { return nil, nil }
func (stubStorageP) Delete(key string) error                { return nil }
func (stubStorageP) PresignedURL(key string, _ int) (string, error) {
	return "/uploads/" + key, nil
}
