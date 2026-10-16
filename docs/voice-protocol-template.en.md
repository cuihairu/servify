> **English** | [中文](voice-protocol-template.md)

# Voice Protocol Integration Template

When integrating a new protocol, prefer reusing the unified `voiceprotocol` contract instead of dragging protocol DTOs directly into the `voice` business layer.

## Minimal Implementation Checklist

1. Define the protocol payload
2. Implement `voiceprotocol.CallSignalingAdapter` or `voiceprotocol.MediaSessionAdapter`
3. Normalize protocol fields into:
   - `CallEvent`
   - `MediaEvent`
4. Register the adapter at the runtime assembly stage
5. Add for the protocol entry:
   - adapter unit tests
   - an HTTP -> runtime -> voice end-to-end test

## Additional Requirements for Hosted Webhook Providers

If the protocol is integrated via webhook, also implement:

- `voiceprotocol.HostedVendorWebhookAdapter`
- signature verification
- a fixed webhook path
- a provider event id deduplication strategy

## Required Mapping Items

- invite
- answer
- hangup
- transfer
- hold
- resume
- dtmf

## Integration Constraints

- The `voice` business layer must not depend on protocol-specific DTOs
- DTMF must first be normalized to `voiceprotocol.CallEventDTMF`
- provider callbacks only write back to the unified model, never directly to business state tables
- Every new protocol adds at least one `/api/voice/protocols` contract test
