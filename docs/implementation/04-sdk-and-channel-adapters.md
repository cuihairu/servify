# 04 SDK And Channel Adapters

Scope:

- sdk core contracts
- current web sdk refactor
- future api sdk reservation
- future app sdk reservation
- channel adapters
- sip adapter

Constraints:

- The web SDK shipped first; the API and app SDKs have since been implemented (`sdk/packages/api-client`, `sdk/packages/app-core`, `sdk/packages/react-native`, `sdk/android`, `sdk/ios`; acceptance records in `sdk/ACCEPTANCE-M3.md` / `sdk/ACCEPTANCE-M4.md`)
- The directory layout and contracts now serve all surfaces

## S1 sdk target structure

- [x] Review the current `sdk/packages` structure
- [x] Confirm the existing package mapping:
  - [x] `core`
  - [x] `react`
  - [x] `vue`
  - [x] `vanilla`
- [x] Design the target structure:
  - [x] `core`
  - [x] `transport-http`
  - [x] `transport-websocket`
  - [x] `web-vanilla`
  - [x] `web-react`
  - [x] `web-vue`
  - [x] `api-client`
  - [x] `app-core`

Acceptance:

- The target structure is documented
- Existing packages are not required to be renamed all at once

## S2 sdk core contracts

- [x] Add a contracts layer to `sdk/packages/core`
- [x] Define `ClientSession`
- [x] Define `Transport`
- [x] Define `EventEmitter`
- [x] Define `AuthProvider`
- [x] Define `Capability`
- [x] Define a unified error model

Acceptance:

- core contains no browser-specific UI logic
- Basic capability, transport, and web binding tests have been added so later API/App SDKs can reuse the contract

## S3 transport split

- [x] Further extract websocket logic from the generalized core into the transport contract
- [x] Reserve `transport-http`
- [x] Reserve a reconnect policy
- [x] Reserve a token refresh hook

Acceptance:

- transport is decoupled from framework bindings

## S4 current web sdk refactor

- [x] `react` depends only on core contracts
- [x] `vue` depends only on core contracts
- [x] `vanilla` depends only on core contracts
- [x] Consolidate browser-specific logic into the web binding

Acceptance:

- core can still be reused by future api/app SDKs

## S5 future api sdk reservation

- [x] Create the `sdk/packages/api-client` placeholder
- [x] Add a README describing the intended purpose
- [x] Agreed usage scenarios:
  - [x] server-to-server api calls
  - [x] admin automation clients
  - [x] bot integrations

Acceptance:

- Make clear this is a reserved design with no concrete features implemented

## S6 future app sdk reservation

- [x] Create the `sdk/packages/app-core` placeholder
- [x] Add a README describing the intended purpose
- [x] Agreed usage scenarios:
  - [x] mobile app session
  - [x] push token integration
  - [x] reconnect and offline queue

Acceptance:

- Make clear this is a reserved design with no concrete features implemented

## S7 sdk capability negotiation

- [x] Define the capability model
- [x] First batch of capabilities:
  - [x] chat
  - [x] realtime
  - [x] knowledge
  - [x] remote_assist
  - [x] voice
- [x] Implement capability exposure in the web sdk first

Acceptance:

- The future api/app SDKs can share the capability protocol

## S8 channel adapter contracts

- [x] Define a unified `InboundEvent` on the server side
- [x] Define a unified `OutboundEvent` on the server side
- [x] Agree on adapter responsibilities
- [x] The web adapter maps to conversation
- [x] Reserve telegram/wecom/whatsapp adapter contracts

Acceptance:

- Newly added channels do not require changes to the ticket business core
- The web channel already has default message-mapping tests; later channels only need to implement the adapter contract

## S9 sip adapter

- [x] Create `internal/platform/sip`
- [x] Define `SIPAdapter`
- [x] Define `InboundCall`
- [x] Map invite -> inbound event
- [x] Map hangup -> inbound event
- [x] Map dtmf -> inbound event
- [x] Reserve the `sip-ws` signaling adapter
- [x] Reserve the `pstn-provider` signaling adapter
- [x] Abstract `voiceprotocol.CallSignalingAdapter`
- [x] Abstract `voiceprotocol.MediaSessionAdapter`

Acceptance:

- SIP connects through the adapter and does not enter chat logic branches
- A default SIP adapter exists and can normalize invite, hangup, and dtmf into the unified inbound event
- SIP, SIP-WS, and PSTN webhooks can share signaling adapter semantics
- The runtime registers signaling/media adapters uniformly via `voiceprotocol.Registry` and exposes a unified protocol event entrypoint
