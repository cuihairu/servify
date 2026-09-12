package factory

import (
	"context"
	"strings"
	"testing"

	"servify/apps/server/internal/platform/storage"
)

func TestFactoryDefaultsToLocal(t *testing.T) {
	for _, provider := range []string{"", "local", "LOCAL"} {
		p, err := NewProvider(context.Background(), FactoryConfig{Provider: provider})
		if err != nil {
			t.Fatalf("provider %q: %v", provider, err)
		}
		if _, ok := p.(*storage.LocalProvider); !ok {
			t.Fatalf("provider %q: expected *storage.LocalProvider, got %T", provider, p)
		}
	}
}

func TestFactoryUnknownProviderRejected(t *testing.T) {
	_, err := NewProvider(context.Background(), FactoryConfig{Provider: "gcs"})
	if err == nil || !strings.Contains(err.Error(), "unknown storage provider") {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
}

func TestFactoryS3RequiresBucketAndRegion(t *testing.T) {
	_, err := NewProvider(context.Background(), FactoryConfig{Provider: "s3", S3: S3Config{Region: "us-east-1"}})
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("expected missing bucket error, got %v", err)
	}
	_, err = NewProvider(context.Background(), FactoryConfig{Provider: "s3", S3: S3Config{Bucket: "b"}})
	if err == nil || !strings.Contains(err.Error(), "region") {
		t.Fatalf("expected missing region error, got %v", err)
	}
}
