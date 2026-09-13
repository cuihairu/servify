package application

import (
	"context"
	"errors"
	"testing"

	"servify/apps/server/internal/models"
)

var (
	errWebhookEntropy = errors.New("entropy unavailable")
	errWebhookMarshal = errors.New("marshal boom")
)

func TestGenerateSecretPropagatesEntropyFailure(t *testing.T) {
	orig := readRandom
	readRandom = func(b []byte) (int, error) { return 0, errWebhookEntropy }
	defer func() { readRandom = orig }()

	secret, err := GenerateSecret()
	if !errors.Is(err, errWebhookEntropy) {
		t.Fatalf("expected entropy error, got %v", err)
	}
	if secret != "" {
		t.Fatalf("expected empty secret on failure, got %q", secret)
	}
}

func TestGenerateSecretDefaultHexEncoding(t *testing.T) {
	// seam 默认路径：真实 rand.Read 产出 32 字节 → 64 位十六进制。
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secret) != 64 {
		t.Fatalf("expected 64-char hex secret, got %d chars", len(secret))
	}
	for _, c := range secret {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			t.Fatalf("expected lowercase hex secret, got %q", secret)
		}
	}
}

func TestCreateEndpointSecretGenerationFailure(t *testing.T) {
	orig := newSecret
	newSecret = func() (string, error) { return "", errWebhookEntropy }
	defer func() { newSecret = orig }()

	repo := &fakeRepo{}
	svc := newTestService(repo, nil)
	ep, secret, err := svc.CreateEndpoint(context.Background(), EndpointRequest{Name: "ep", URL: "https://a.example.com"})
	if !errors.Is(err, errWebhookEntropy) {
		t.Fatalf("expected secret generation error, got %v", err)
	}
	if ep != nil || secret != "" {
		t.Fatalf("expected nil endpoint and empty secret, got %v %q", ep, secret)
	}
	if repo.createdCount != 0 {
		t.Fatalf("endpoint must not be persisted on secret failure, created=%d", repo.createdCount)
	}
}

func TestRotateEndpointSecretGenerationFailure(t *testing.T) {
	orig := newSecret
	newSecret = func() (string, error) { return "", errWebhookEntropy }
	defer func() { newSecret = orig }()

	repo := &fakeRepo{endpoints: []models.WebhookEndpoint{{ID: 3, Name: "ep", URL: "https://a.example.com"}}}
	svc := newTestService(repo, nil)
	ep, secret, err := svc.RotateEndpointSecret(context.Background(), 3)
	if !errors.Is(err, errWebhookEntropy) {
		t.Fatalf("expected secret generation error, got %v", err)
	}
	if ep != nil || secret != "" {
		t.Fatalf("expected nil endpoint and empty secret, got %v %q", ep, secret)
	}
}

func TestBuildPayloadMarshalFailure(t *testing.T) {
	orig := marshalEnvelope
	marshalEnvelope = func(v interface{}) ([]byte, error) { return nil, errWebhookMarshal }
	defer func() { marshalEnvelope = orig }()

	repo := &fakeRepo{ticket: &models.Ticket{ID: 7}}
	svc := newTestService(repo, nil)
	payload, err := svc.buildPayload(context.Background(), "ticket.updated", "ticket:7", "evt-1")
	if !errors.Is(err, errWebhookMarshal) {
		t.Fatalf("expected marshal error, got %v", err)
	}
	if payload != "" {
		t.Fatalf("expected empty payload on failure, got %q", payload)
	}
}
