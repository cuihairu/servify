// Package twiliovoice implements the voiceprotocol.HostedVendorWebhookAdapter
// contract for Twilio Programmable Voice status callbacks — the first hosted
// vendor wired through the hosted-vendor-webhook protocol slot.
//
// Scope: this adapter consumes Twilio Call Status Callback and Recording
// Status Callback requests (form-urlencoded POSTs signed with
// X-Twilio-Signature). It does NOT answer calls: no TwiML voice response is
// produced (the webhook replies with an empty 200, which is all status
// callbacks require), and outbound calls are out of scope.
//
// Webhook path: fixed at /public/voice/webhooks/twilio — the path is part of
// the signed URL, so it must be stable for the lifetime of the integration.
//
// Deduplication strategy: Twilio retries webhooks that do not get a timely
// 2xx, and its callbacks carry no per-event id beyond CallSid / RecordingSid.
// Events therefore get deterministic IDs (twilio-<kind>-<CallSid>,
// twilio-recording-<RecordingSid>) and idempotency is enforced downstream by
// the voice coordinator's state guards (duplicate invite/hangup/hold/... on
// an already-arrived call short-circuits; recording completion is an upsert).
// A per-EventID replay ledger is intentionally out of scope.
package twiliovoice
