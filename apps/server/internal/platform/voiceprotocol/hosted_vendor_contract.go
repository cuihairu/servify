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
//
// MapWebhook is the vendor's aggregate entry point: one webhook request maps
// to any mix of normalized call and media events (e.g. a Twilio status
// callback yields a call-signaling event and may append a DTMF event, or a
// recording callback yields a media event). The payload shape is
// vendor-defined (form values, JSON); the HTTP layer hands over whatever the
// vendor adapter expects.
type HostedVendorWebhookAdapter interface {
	CallSignalingAdapter
	WebhookPath() string
	ValidateSignature(ctx context.Context, url string, headers map[string]string, body []byte) error
	MapWebhook(ctx context.Context, payload interface{}) ([]CallEvent, []MediaEvent, error)
}
