// Package s3 implements the storage.Provider abstraction on top of any
// S3-compatible object store (AWS S3, MinIO, OSS S3 gateways, ...).
//
// URL semantics: ObjectInfo.URL is always the stable local form
// "<urlBase>/<key>" (e.g. "/uploads/2026/01/15/123_report.pdf") so links
// persisted in messages never expire. Direct object access goes through
// PresignedURL, which callers are expected to serve as a redirect
// (see the /uploads/* route wiring).
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsv4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"servify/apps/server/internal/platform/storage"
)

// Minimal client interfaces so tests can inject fakes without a live endpoint.
type (
	PutObjecter interface {
		PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	}
	GetObjecter interface {
		GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	}
	DeleteObjecter interface {
		DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	}
	Presigner interface {
		PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*awsv4.PresignedHTTPRequest, error)
	}
)

// Config configures the S3-backed provider.
type Config struct {
	Region          string
	Bucket          string
	Endpoint        string // custom endpoint (MinIO etc.); empty = AWS default
	AccessKeyID     string // empty → default credential chain (env, IAM role, ...)
	SecretAccessKey string
	ForcePathStyle  bool // required by most MinIO setups
	PresignExpiry   time.Duration
	URLBase         string // URL prefix for stable links, e.g. "/uploads"
	PublicBaseURL   string // optional public read base (CDN / public bucket); empty = presign
}

var _ storage.Provider = (*Provider)(nil)

// Provider stores objects in an S3-compatible bucket.
type Provider struct {
	putter    PutObjecter
	getter    GetObjecter
	deleter   DeleteObjecter
	presigner Presigner

	bucket        string
	urlBase       string
	presignExpiry time.Duration
	publicBaseURL string
}

// New wires a provider around explicit client interfaces (test seam).
func New(putter PutObjecter, getter GetObjecter, deleter DeleteObjecter, presigner Presigner, cfg Config) *Provider {
	expiry := cfg.PresignExpiry
	if expiry <= 0 {
		expiry = time.Hour
	}
	urlBase := cfg.URLBase
	if urlBase == "" {
		urlBase = "/uploads"
	}
	return &Provider{
		putter:        putter,
		getter:        getter,
		deleter:       deleter,
		presigner:     presigner,
		bucket:        cfg.Bucket,
		urlBase:       urlBase,
		presignExpiry: expiry,
		publicBaseURL: strings.TrimSuffix(cfg.PublicBaseURL, "/"),
	}
}

// NewFromConfig builds a provider with a real S3 client.
func NewFromConfig(ctx context.Context, cfg Config) (*Provider, error) {
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("s3 storage requires region")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3 storage requires bucket")
	}
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	s3Opts := []func(*s3.Options){
		func(o *s3.Options) { o.UsePathStyle = cfg.ForcePathStyle },
	}
	if cfg.Endpoint != "" {
		endpoint := cfg.Endpoint
		s3Opts = append(s3Opts, func(o *s3.Options) { o.BaseEndpoint = &endpoint })
	}
	client := s3.NewFromConfig(awsCfg, s3Opts...)
	return New(client, client, client, s3.NewPresignClient(client), cfg), nil
}

// Save streams r into the bucket under key.
func (p *Provider) Save(key string, r io.Reader, size int64) (*storage.ObjectInfo, error) {
	contentType := mime.TypeByExtension(filepath.Ext(key))
	input := &s3.PutObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
		Body:   r,
	}
	if size > 0 {
		input.ContentLength = aws.Int64(size)
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	if _, err := p.putter.PutObject(context.Background(), input); err != nil {
		return nil, fmt.Errorf("s3 put object %s: %w", key, err)
	}
	return &storage.ObjectInfo{Key: key, Size: size, URL: p.stableURL(key)}, nil
}

// Open returns a reader streaming the object body.
func (p *Provider) Open(key string) (io.ReadCloser, error) {
	out, err := p.getter.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get object %s: %w", key, err)
	}
	return out.Body, nil
}

// Delete removes the object; S3 deletes are idempotent, so a missing key is
// not an error (matching LocalProvider semantics).
func (p *Provider) Delete(key string) error {
	var nf *types.NoSuchKey
	if _, err := p.deleter.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}); err != nil && !errors.As(err, &nf) {
		return fmt.Errorf("s3 delete object %s: %w", key, err)
	}
	return nil
}

// PresignedURL returns a time-limited signed URL, or the public base URL when
// configured for public reads. expiresSeconds <= 0 uses the configured default.
func (p *Provider) PresignedURL(key string, expiresSeconds int) (string, error) {
	if p.publicBaseURL != "" {
		return p.publicBaseURL + "/" + key, nil
	}
	expiry := p.presignExpiry
	if expiresSeconds > 0 {
		expiry = time.Duration(expiresSeconds) * time.Second
	}
	req, err := p.presigner.PresignGetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3 presign %s: %w", key, err)
	}
	return req.URL, nil
}

func (p *Provider) stableURL(key string) string {
	return p.urlBase + "/" + strings.TrimPrefix(filepath.ToSlash(key), "/")
}
