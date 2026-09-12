package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"servify/apps/server/internal/config"
	"servify/apps/server/internal/platform/storage"

	"github.com/gin-gonic/gin"
)

func ginNewTest() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// TestRegisterUploadRoutes_LocalKeepsStaticServing 验证默认 local provider
// 走静态文件服务（与原硬编码行为一致）。
func TestRegisterUploadRoutes_LocalKeepsStaticServing(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.Upload.Provider = "local"
	cfg.Upload.StoragePath = t.TempDir()

	router := BuildRouter(Dependencies{Config: cfg})
	found := false
	for _, route := range router.Routes() {
		if route.Path == "/uploads/*filepath" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected /uploads/*filepath static route in local mode")
	}
}

// TestRegisterUploadRoutes_S3PresignsRedirects 验证 s3 模式下 /uploads/<key>
// 302 到现签 presigned URL（SigV4 本地签名，离线可测）。
func TestRegisterUploadRoutes_S3PresignsRedirects(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.Upload.Provider = "s3"
	cfg.Upload.S3.Region = "us-east-1"
	cfg.Upload.S3.Bucket = "servify-test"
	cfg.Upload.S3.AccessKeyID = "test-key"
	cfg.Upload.S3.SecretAccessKey = "test-secret"

	router := BuildRouter(Dependencies{Config: cfg})

	req := httptest.NewRequest(http.MethodGet, "/uploads/2026/09/12/1_report.pdf", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d body=%s", w.Code, w.Body.String())
	}
	location := w.Header().Get("Location")
	if !strings.Contains(location, "servify-test") || !strings.Contains(location, "X-Amz-Signature") {
		t.Fatalf("expected presigned URL mentioning bucket and signature, got %q", location)
	}
	if !strings.HasPrefix(location, "https://") {
		t.Fatalf("expected https presigned URL, got %q", location)
	}
}

// TestRegisterUploadRoutes_S3PublicBaseRedirects 验证配置 public_base_url 时
// 直接 302 到公共读地址，不走签名。
func TestRegisterUploadRoutes_S3PublicBaseRedirects(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.Upload.Provider = "s3"
	cfg.Upload.S3.Region = "us-east-1"
	cfg.Upload.S3.Bucket = "servify-test"
	cfg.Upload.S3.PublicBaseURL = "https://cdn.example.com/uploads"

	router := BuildRouter(Dependencies{Config: cfg})

	req := httptest.NewRequest(http.MethodGet, "/uploads/2026/09/12/img.png", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "https://cdn.example.com/uploads/2026/09/12/img.png" {
		t.Fatalf("expected public base redirect, got %q", got)
	}
}

// TestRegisterUploadRoutes_S3MisconfigFailsClosed 验证 s3 配置缺失时
// fail-closed：不上传路由、不注册 /uploads。
func TestRegisterUploadRoutes_S3MisconfigFailsClosed(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.JWT.Secret = "test-secret"
	cfg.Upload.Provider = "s3"
	// 缺 region/bucket
	cfg.Upload.S3 = config.S3UploadConfig{}

	router := BuildRouter(Dependencies{Config: cfg})
	for _, route := range router.Routes() {
		if route.Path == "/api/v1/upload" || route.Path == "/uploads/*filepath" {
			t.Fatalf("expected no upload routes when s3 misconfigured, found %s %s", route.Method, route.Path)
		}
	}
}

// TestUploadsPresignRedirectDirect 覆盖重定向 handler 的分支。
func TestUploadsPresignRedirectDirect(t *testing.T) {
	stub := stubPresignProvider{url: "https://signed.example/obj"}
	r := ginNewTest()
	r.GET("/uploads/*filepath", uploadsPresignRedirect(stub, config.S3UploadConfig{PresignExpirySeconds: 600}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/uploads/a/b.txt", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != stub.url {
		t.Fatalf("expected 302 to %q, got %d %q", stub.url, w.Code, w.Header().Get("Location"))
	}

	// public base 优先于 presign
	r2 := ginNewTest()
	r2.GET("/uploads/*filepath", uploadsPresignRedirect(stub, config.S3UploadConfig{PublicBaseURL: "https://cdn.example.com/base/"}))
	w2 := httptest.NewRecorder()
	r2.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/uploads/a/b.txt", nil))
	if w2.Code != http.StatusFound || w2.Header().Get("Location") != "https://cdn.example.com/base/a/b.txt" {
		t.Fatalf("expected public base redirect, got %d %q", w2.Code, w2.Header().Get("Location"))
	}

	// presign 失败 → 502
	r3 := ginNewTest()
	r3.GET("/uploads/*filepath", uploadsPresignRedirect(stubPresignProvider{err: context.DeadlineExceeded}, config.S3UploadConfig{}))
	w3 := httptest.NewRecorder()
	r3.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/uploads/a/b.txt", nil))
	if w3.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on presign failure, got %d", w3.Code)
	}

	// 空 key → 404
	r4 := ginNewTest()
	r4.GET("/uploads/*filepath", uploadsPresignRedirect(stub, config.S3UploadConfig{}))
	w4 := httptest.NewRecorder()
	r4.ServeHTTP(w4, httptest.NewRequest(http.MethodGet, "/uploads/", nil))
	if w4.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for empty key, got %d", w4.Code)
	}
}

type stubPresignProvider struct {
	url string
	err error
}

func (s stubPresignProvider) Save(key string, r io.Reader, size int64) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Key: key, Size: size}, nil
}
func (s stubPresignProvider) Open(key string) (io.ReadCloser, error) { return nil, nil }
func (s stubPresignProvider) Delete(key string) error                { return nil }
func (s stubPresignProvider) PresignedURL(key string, expiresSeconds int) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.url, nil
}
