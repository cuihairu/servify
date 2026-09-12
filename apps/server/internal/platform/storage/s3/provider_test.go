package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakePutter struct {
	input *s3.PutObjectInput
	err   error
}

func (f *fakePutter) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.input = params
	if f.err != nil {
		return nil, f.err
	}
	return &s3.PutObjectOutput{}, nil
}

type fakeGetter struct {
	output *s3.GetObjectOutput
	err    error
}

func (f *fakeGetter) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.output, nil
}

type fakeDeleter struct {
	err error
}

func (f *fakeDeleter) DeleteObject(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &s3.DeleteObjectOutput{}, nil
}

type fakePresigner struct {
	url string
	err error
}

func (f *fakePresigner) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.PresignOptions)) (*awsv4.PresignedHTTPRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &awsv4.PresignedHTTPRequest{URL: f.url}, nil
}

func newTestProvider(putter PutObjecter, getter GetObjecter, deleter DeleteObjecter, presigner Presigner, cfg Config) *Provider {
	return New(putter, getter, deleter, presigner, cfg)
}

func TestS3ProviderSaveStreamsAndKeepsStableURL(t *testing.T) {
	putter := &fakePutter{}
	p := newTestProvider(putter, nil, nil, nil, Config{Bucket: "b", URLBase: "/uploads", PresignExpiry: time.Hour})

	body := strings.NewReader("hello")
	info, err := p.Save("2026/09/12/1_report.pdf", body, 5)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if info.URL != "/uploads/2026/09/12/1_report.pdf" {
		t.Fatalf("expected stable local-form URL, got %q", info.URL)
	}
	if info.Size != 5 {
		t.Fatalf("expected size 5, got %d", info.Size)
	}
	if putter.input == nil {
		t.Fatal("PutObject not called")
	}
	if aws.ToString(putter.input.Key) != "2026/09/12/1_report.pdf" {
		t.Fatalf("unexpected key %q", aws.ToString(putter.input.Key))
	}
	if putter.input.ContentLength == nil || *putter.input.ContentLength != 5 {
		t.Fatalf("expected ContentLength 5, got %v", putter.input.ContentLength)
	}
	if aws.ToString(putter.input.ContentType) != "application/pdf" {
		t.Fatalf("expected pdf content type, got %q", aws.ToString(putter.input.ContentType))
	}
	if putter.input.Body == nil {
		t.Fatal("expected body to be streamed")
	}
}

func TestS3ProviderSaveErrorPropagates(t *testing.T) {
	putter := &fakePutter{err: errors.New("boom")}
	p := newTestProvider(putter, nil, nil, nil, Config{Bucket: "b"})
	if _, err := p.Save("k.txt", strings.NewReader("x"), 1); err == nil {
		t.Fatal("expected error")
	}
}

func TestS3ProviderPresignedURLUsesPresigner(t *testing.T) {
	presigner := &fakePresigner{url: "https://bucket.s3.example/signed?X-Amz-Signature=abc"}
	p := newTestProvider(nil, nil, nil, presigner, Config{Bucket: "b", PresignExpiry: 2 * time.Minute})

	url, err := p.PresignedURL("2026/09/12/1_report.pdf", 0)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if url != presigner.url {
		t.Fatalf("unexpected url %q", url)
	}
}

func TestS3ProviderPresignedURLPrefersPublicBase(t *testing.T) {
	presigner := &fakePresigner{url: "https://should-not-be-used"}
	p := newTestProvider(nil, nil, nil, presigner, Config{Bucket: "b", PublicBaseURL: "https://cdn.example.com/uploads/"})

	url, err := p.PresignedURL("2026/09/12/file.png", 0)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	if url != "https://cdn.example.com/uploads/2026/09/12/file.png" {
		t.Fatalf("unexpected public url %q", url)
	}
}

func TestS3ProviderOpenReturnsBody(t *testing.T) {
	getter := &fakeGetter{output: &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("data"))}}
	p := newTestProvider(nil, getter, nil, nil, Config{Bucket: "b"})

	rc, err := p.Open("k.txt")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer rc.Close()
	data, _ := io.ReadAll(rc)
	if string(data) != "data" {
		t.Fatalf("unexpected body %q", string(data))
	}
}

func TestS3ProviderOpenError(t *testing.T) {
	getter := &fakeGetter{err: errors.New("missing")}
	p := newTestProvider(nil, getter, nil, nil, Config{Bucket: "b"})
	if _, err := p.Open("missing.txt"); err == nil {
		t.Fatal("expected error")
	}
}

func TestS3ProviderDeleteIgnoresNoSuchKey(t *testing.T) {
	p := newTestProvider(nil, nil, &fakeDeleter{}, nil, Config{Bucket: "b"})
	if err := p.Delete("gone.txt"); err != nil {
		t.Fatalf("delete of missing key should be idempotent, got %v", err)
	}

	p2 := newTestProvider(nil, nil, &fakeDeleter{err: fmt.Errorf("wrapped: %w", &types.NoSuchKey{})}, nil, Config{Bucket: "b"})
	if err := p2.Delete("gone.txt"); err != nil {
		t.Fatalf("NoSuchKey should be swallowed, got %v", err)
	}

	p3 := newTestProvider(nil, nil, &fakeDeleter{err: errors.New("network down")}, nil, Config{Bucket: "b"})
	if err := p3.Delete("gone.txt"); err == nil {
		t.Fatal("expected error for non-NoSuchKey failure")
	}
}

func TestS3NewFromConfigValidatesRequiredFields(t *testing.T) {
	if _, err := NewFromConfig(context.Background(), Config{Bucket: "b"}); err == nil {
		t.Fatal("expected missing region error")
	}
	if _, err := NewFromConfig(context.Background(), Config{Region: "us-east-1"}); err == nil {
		t.Fatal("expected missing bucket error")
	}
}
