// Package factory builds the storage.Provider selected by configuration.
//
// It lives in its own package (rather than inside storage) so that the s3
// sub-package can depend on the storage interface package without an import
// cycle: storage ← s3 ← factory.
package factory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"servify/apps/server/internal/platform/storage"
	"servify/apps/server/internal/platform/storage/s3"
)

// LocalConfig configures the local filesystem backend.
type LocalConfig struct {
	BaseDir string
	URLBase string
}

// S3Config configures the S3-compatible backend (mirrors config.S3UploadConfig).
type S3Config struct {
	Region               string
	Bucket               string
	Endpoint             string
	AccessKeyID          string
	SecretAccessKey      string
	ForcePathStyle       bool
	PresignExpirySeconds int
	PublicBaseURL        string
}

// FactoryConfig selects and parameterizes a storage backend.
// Provider "" or "local" selects the local filesystem; "s3" the object store.
type FactoryConfig struct {
	Provider string
	Local    LocalConfig
	S3       S3Config
}

// NewProvider builds the configured storage.Provider.
func NewProvider(ctx context.Context, cfg FactoryConfig) (storage.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "local":
		baseDir := cfg.Local.BaseDir
		if baseDir == "" {
			baseDir = "./uploads"
		}
		urlBase := cfg.Local.URLBase
		if urlBase == "" {
			urlBase = "/uploads"
		}
		return storage.NewLocalProvider(baseDir, urlBase), nil
	case "s3":
		return s3.NewFromConfig(ctx, s3.Config{
			Region:          cfg.S3.Region,
			Bucket:          cfg.S3.Bucket,
			Endpoint:        cfg.S3.Endpoint,
			AccessKeyID:     cfg.S3.AccessKeyID,
			SecretAccessKey: cfg.S3.SecretAccessKey,
			ForcePathStyle:  cfg.S3.ForcePathStyle,
			PresignExpiry:   time.Duration(cfg.S3.PresignExpirySeconds) * time.Second,
			PublicBaseURL:   cfg.S3.PublicBaseURL,
		})
	default:
		return nil, fmt.Errorf("unknown storage provider: %s", cfg.Provider)
	}
}
