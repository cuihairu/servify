# 06 Voice And Protocol Expansion

Scope:

- Deepen the unified voice protocol entry
- Land signaling/media providers
- External recording/transcription providers
- Reserve common voice protocol extensions

Constraints:

- The `voice` business layer must not depend backward on concrete protocol DTOs
- Signaling and media must stay layered

## V1 protocol-end-to-end

- [x] Add invite -> answer -> hangup end-to-end tests for the `sip` protocol entry
- [x] Add invite -> dtmf -> hangup end-to-end tests for the `sip-ws` protocol entry
- [x] Add invite -> transfer -> hangup tests for the `pstn-provider` webhook entry
- [x] Add contract tests for `/api/voice/protocols`

Acceptance:

- New protocol entries have full HTTP/runtime/voice chain verification, not just adapter unit tests

## V2 call-control-semantics

- [x] Add hold event mapping in the unified protocol entry
- [x] Add resume event mapping in the unified protocol entry
- [x] Add transfer event mapping to the `voice` use case in the unified protocol entry
- [x] Clarify the dispatch strategy for DTMF across voice/conversation/automation

Current DTMF strategy:

- Protocol adapters first normalize events into `voiceprotocol.CallEventDTMF`
- The `voice` runtime receives and keeps the unified entry, and does not write call state directly
- If `conversation` or `automation` later needs to react, it should subscribe to unified `voice` events instead of depending backward on concrete protocol DTOs

Acceptance:

- Common call control events all flow through the unified normalization model

## V3 provider-implementation

- [x] Extract the recording provider from in-memory into a standalone mock/provider package
- [x] Extract the transcription provider from in-memory into a standalone mock/provider package
- [x] Add failure retry and error models for providers
- [x] Reserve async webhook/callback write-back interfaces

Acceptance:

- Recording and transcription providers can be swapped independently without affecting voice use cases

## V4 media-topology

- [x] Abstract RTP/SRTP adapter placeholders
- [x] Clarify the WebRTC and RTP bridge contract
- [x] Reserve conference/mixer capability interfaces
- [x] Reserve QoS / voice quality metrics collection points

Acceptance:

- Extending from point-to-point calls to more complex media topologies later will not require tearing down the current structure

## V5 protocol-reservation

- [x] Reserve `voiceprotocol.ProtocolH323`
- [x] Reserve `voiceprotocol.ProtocolFreeSWITCHEventSocket` or similar PBX adaptation capability
- [x] Reserve a hosted voice vendor webhook adapter contract
- [x] Write a minimal implementation template document for onboarding new protocols

Acceptance:

- The voice module is not implicitly designed as "SIP only"
