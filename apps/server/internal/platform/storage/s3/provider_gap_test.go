package s3

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestNewFromConfigBuildsRealClient 覆盖 NewFromConfig 的成功分支:
// 只构建客户端,不发起网络请求(Endpoint 可指向任何地址)。
func TestNewFromConfigBuildsRealClient(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")

	p, err := NewFromConfig(context.Background(), Config{
		Region:          "us-east-1",
		Bucket:          "b",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Endpoint:        "http://127.0.0.1:9",
		ForcePathStyle:  true,
		URLBase:         "/uploads",
	})
	if err != nil {
		t.Fatalf("NewFromConfig() error = %v", err)
	}
	if p.bucket != "b" || p.urlBase != "/uploads" || p.publicBaseURL != "" {
		t.Fatalf("unexpected provider wiring: %+v", p)
	}
}

// TestNewFromConfigNoCredentials 覆盖不带静态凭证的构造分支。
func TestNewFromConfigNoCredentials(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")

	p, err := NewFromConfig(context.Background(), Config{Region: "eu-west-1", Bucket: "bucket"})
	if err != nil {
		t.Fatalf("NewFromConfig() error = %v", err)
	}
	if p.presignExpiry != time.Hour {
		t.Fatalf("default presign expiry = %v", p.presignExpiry)
	}
}

// TestNewFromConfigLoadDefaultConfigError 覆盖 awsconfig.LoadDefaultConfig 的错误分支:
// 指向一个共享配置文件里不存在的 profile。
func TestNewFromConfigLoadDefaultConfigError(t *testing.T) {
	cfgFile := t.TempDir() + "/aws.config"
	if err := writeFile(cfgFile, "[default]\nregion = us-east-1\n"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Setenv("AWS_CONFIG_FILE", cfgFile)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", cfgFile)
	t.Setenv("AWS_PROFILE", "missing-profile")

	_, err := NewFromConfig(context.Background(), Config{Region: "us-east-1", Bucket: "b"})
	if err == nil {
		t.Fatal("expected LoadDefaultConfig error for missing profile")
	}
}

// capturingPresigner 记录传给 PresignGetObject 的过期参数。
type capturingPresigner struct {
	expiry time.Duration
	url    string
	err    error
}

func (f *capturingPresigner) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, opts ...func(*s3.PresignOptions)) (*awsv4.PresignedHTTPRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	o := s3.PresignOptions{}
	for _, fn := range opts {
		fn(&o)
	}
	f.expiry = o.Expires
	return &awsv4.PresignedHTTPRequest{URL: f.url}, nil
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestPresignedURLExplicitExpiry 覆盖 expiresSeconds > 0 的过期覆盖分支。
func TestPresignedURLExplicitExpiry(t *testing.T) {
	presigner := &capturingPresigner{url: "https://signed.example/obj"}
	p := New(nil, nil, nil, presigner, Config{Bucket: "b", PresignExpiry: time.Hour})

	url, err := p.PresignedURL("k.txt", 120)
	if err != nil {
		t.Fatalf("PresignedURL() error = %v", err)
	}
	if url != presigner.url {
		t.Fatalf("url = %q, want %q", url, presigner.url)
	}
	if presigner.expiry != 120*time.Second {
		t.Fatalf("presign expiry = %v, want 2m", presigner.expiry)
	}

	// expiresSeconds <= 0 时使用配置默认值
	if _, err := p.PresignedURL("k.txt", 0); err != nil {
		t.Fatalf("PresignedURL() error = %v", err)
	}
	if presigner.expiry != time.Hour {
		t.Fatalf("default presign expiry = %v, want 1h", presigner.expiry)
	}
}

// TestPresignedURLError 覆盖 presign 失败分支。
func TestPresignedURLError(t *testing.T) {
	p := New(nil, nil, nil, &capturingPresigner{err: errors.New("no credentials")}, Config{Bucket: "b"})
	if _, err := p.PresignedURL("k.txt", 0); err == nil {
		t.Fatal("expected presign error")
	}
}

// TestNewDefaults 覆盖 New 的默认值分支。
func TestNewDefaults(t *testing.T) {
	p := New(nil, nil, nil, nil, Config{Bucket: "b"})
	if p.presignExpiry != time.Hour {
		t.Fatalf("presignExpiry = %v, want 1h", p.presignExpiry)
	}
	if p.urlBase != "/uploads" {
		t.Fatalf("urlBase = %q, want /uploads", p.urlBase)
	}
}

// TestSaveAndDeleteAgainstHTTPServer 用 httptest 充当 S3 兼容端点,
// 通过 NewFromConfig 构建的真实客户端跑一轮 Save/Delete 全链路。
func TestSaveAndDeleteAgainstHTTPServer(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = body
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")

	p, err := NewFromConfig(context.Background(), Config{
		Region:          "us-east-1",
		Bucket:          "bucket",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
		Endpoint:        srv.URL,
		ForcePathStyle:  true,
	})
	if err != nil {
		t.Fatalf("NewFromConfig() error = %v", err)
	}

	info, err := p.Save("2026/09/13/report.txt", strings.NewReader("hello world"), 11)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if info.URL != "/uploads/2026/09/13/report.txt" {
		t.Fatalf("stable URL = %q", info.URL)
	}
	if gotMethod != http.MethodPut || gotPath != "/bucket/2026/09/13/report.txt" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if string(gotBody) != "hello world" {
		t.Fatalf("streamed body = %q", string(gotBody))
	}

	if err := p.Delete("2026/09/13/report.txt"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("expected DELETE request, got %s", gotMethod)
	}
}
