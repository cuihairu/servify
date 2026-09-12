package delivery

import (
	"strings"
	"testing"
	"time"
)

func TestSignatureHeaderRoundTrip(t *testing.T) {
	secret := "top-secret"
	body := []byte(`{"event":"ticket.created"}`)
	now := time.Unix(1_700_000_000, 0)

	header := SignatureHeader(secret, body, now)
	if !strings.HasPrefix(header, "t=1700000000,v1=") {
		t.Fatalf("unexpected header format: %s", header)
	}
	if !VerifySignature(secret, body, header, now.Add(time.Minute)) {
		t.Fatal("signature should verify within window")
	}
}

func TestVerifySignatureRejectsTampering(t *testing.T) {
	secret := "top-secret"
	body := []byte(`{"event":"ticket.created"}`)
	now := time.Unix(1_700_000_000, 0)
	header := SignatureHeader(secret, body, now)

	if VerifySignature("wrong-secret", body, header, now) {
		t.Fatal("wrong secret must fail")
	}
	if VerifySignature(secret, []byte(`{"event":"tampered"}`), header, now) {
		t.Fatal("tampered body must fail")
	}
	if VerifySignature(secret, body, "t=abc,v1=zz", now) {
		t.Fatal("malformed header must fail")
	}
	if VerifySignature(secret, body, header, now.Add(10*time.Minute)) {
		t.Fatal("stale timestamp must fail")
	}
}
