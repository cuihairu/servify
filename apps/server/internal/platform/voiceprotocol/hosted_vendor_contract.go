package voiceprotocol

import "context"

// HostedVendorWebhookAdapter reserves the contract for future hosted voice
// providers that deliver call control through signed webhook events.
//
// ValidateSignature receives the full request URL as an explicit parameter:
// hosted providers (e.g. Twilio) sign the complete URL plus the sorted form
// parameters, so the URL is signing material and cannot be derived from
// headers or body alone. Callers assemble the URL (public base URL preferred,
// request host as fallback) and pass the raw headers/body untouched; the
// adapter owns only the cryptographic comparison.
type HostedVendorWebhookAdapter interface {
	CallSignalingAdapter
	WebhookPath() string
	ValidateSignature(ctx context.Context, url string, headers map[string]string, body []byte) error
}
