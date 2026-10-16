# 03 Business Modules

Scope:

- conversation
- routing
- agent
- ticket
- customer
- automation
- analytics
- voice

## B1 conversation

- [x] Create `internal/modules/conversation/domain`
- [x] Create `internal/modules/conversation/application`
- [x] Create `internal/modules/conversation/infra`
- [x] Create `internal/modules/conversation/delivery`
- [x] Define `Conversation`
- [x] Define `Participant`
- [x] Define `ConversationMessage`
- [x] Define `ChannelBinding`
- [x] Define `ConversationStatus`
- [x] Implement CreateConversation
- [x] Implement ResumeConversation
- [x] Implement IngestTextMessage
- [x] Implement IngestSystemEvent
- [x] Publish `conversation.created`
- [x] Publish `conversation.message_received`
- [x] Add the `conversation` GORM repository, mapping the existing `Session` / `Message` models
- [x] `WebSocketHub` text-message persistence goes through the `conversation` adapter first, with the old direct DB write kept as fallback
- [x] The `WebSocketHub` "human agent has taken over" check goes through the `conversation` reader first, preventing the AI from replying first
- [x] `WebSocketHub` recent message history reads go through the `conversation` adapter first, reducing direct queries against the `Message` table

Acceptance:

- conversation becomes the unified interaction model

## B2 routing

- [x] Create `internal/modules/routing/domain`
- [x] Create `internal/modules/routing/application`
- [x] Create `internal/modules/routing/infra`
- [x] Create `internal/modules/routing/delivery`
- [x] Define `Assignment`
- [x] Define `QueueEntry`
- [x] Define `TransferRecord`
- [x] Define `AgentAvailabilityPolicy`
- [x] Define `SkillMatchPolicy`
- [x] Define `LoadBalancePolicy`
- [x] Implement RequestHumanHandoff
- [x] Implement AssignAgent
- [x] Implement AddToWaitingQueue
- [x] Implement CancelWaiting
- [x] Publish `routing.agent_assigned`
- [x] Publish `routing.transfer_completed`
- [x] Add the `routing` GORM repository, mapping the existing `TransferRecord` / `WaitingRecord` models
- [x] The `SessionTransferService` waiting queue path starts delegating to the `routing` adapter, with the old notification/message logic retained

Acceptance:

- Replaces the existing session transfer cross-dependencies

## B3 agent

- [x] Create `internal/modules/agent/domain`
- [x] Create `internal/modules/agent/application`
- [x] Create `internal/modules/agent/infra`
- [x] Create `internal/modules/agent/delivery`
- [x] Define `AgentProfile`
- [x] Define `AgentPresence`
- [x] Define `AgentLoad`
- [x] Add chat concurrency
- [x] Add voice concurrency
- [x] Implement GoOnline
- [x] Implement GoOffline
- [x] Implement MarkBusy
- [x] Implement MarkAway

Acceptance:

- The agent module supports future voice load

## B4 ticket

- [x] Create `internal/modules/ticket/domain`
- [x] Create `internal/modules/ticket/application`
- [x] Create `internal/modules/ticket/infra`
- [x] Create `internal/modules/ticket/delivery`
- [x] Add `query_service.go`
- [x] Migrate `ListTickets`
- [x] Migrate `GetTicketByID`
- [x] Add `command_service.go`
- [x] Migrate `CreateTicket`
- [x] Fold `CreateTicket` initial status history into the repository transaction
- [x] Migrate `UpdateTicket`
- [x] Fold the `UpdateTicket` main ticket update and status history into the repository transaction
- [x] Fold the `UpdateTicket` custom field sync into the module aggregate transaction
- [x] Migrate `AssignTicket`
- [x] Sink `AssignTicket` down to the module command and repository transaction
- [x] Sink `UnassignTicket` down to the module command and repository transaction
- [x] Migrate `AddComment`
- [x] Migrate `CloseTicket`
- [x] Fold the `CloseTicket` status history and agent load decrement into the repository transaction
- [x] Split out `StatusTransitionPolicy`
- [x] Split out `CustomFieldValidator`
- [x] Split out `BulkUpdateTickets`
- [x] Publish `ticket.created`
- [x] Publish `ticket.assigned`
- [x] Publish `ticket.closed`
- [x] Inject `eventbus` into the old `TicketService` runtime entrypoint
- [x] Replace the `TicketHandler` hard dependency on the old `TicketService` with an interface dependency
- [x] Add the ticket handler adapter and switch `main.go` to adapter injection
- [x] Sink `AddComment` from the adapter down to the module command implementation
- [x] Sink `AssignTicket` from the adapter down to the module command implementation, keeping the post-assignment SLA side effects
- [x] Sink `CloseTicket` from the adapter down to the module command implementation, keeping the post-close comment/SLA/CSAT side effects
- [x] Sink `BulkUpdateTickets` from the adapter down to the module command implementation
- [x] Sink `CreateTicket` from the adapter down to the module repository implementation, keeping the post-create auto-assignment/event/SLA side effects
- [x] Sink `UpdateTicket` from the adapter down to the module aggregate transaction implementation, keeping the post-update event/SLA side effects
- [x] Sink `ListTicketCustomFields` from the adapter down to direct data access
- [x] Sink `GetTicketStats` from the adapter down to the module `QueryService`
- [x] Extract `TicketOrchestrator` and switch to explicit `CommandService + Orchestrator` injection
- [x] Startup wiring no longer passes the whole `TicketService` to the ticket handler adapter
- [x] Move the ticket HTTP/adapter shared DTOs into the module `contract` package, keeping the `services` alias for compatibility
- [x] `SLAHandler` fetches tickets through the minimal ticket `ReaderServiceAdapter` read interface, no longer depending on the concrete `TicketService` type
- [x] Move handler-layer sqlite integration tests under the `integration` build tag; the default test chain keeps only stable unit tests
- [x] The old `TicketService` is converged into a module compatibility shell: all core reads/writes delegate to `application/infra/orchestration`, and legacy fallback branches are removed

Acceptance:

- The old TicketService is no longer a giant monolith

## B5 customer

- [x] Create `internal/modules/customer/domain`
- [x] Create `internal/modules/customer/application`
- [x] Create `internal/modules/customer/infra`
- [x] Create `internal/modules/customer/delivery`
- [x] Define `CustomerProfile`
- [x] Define `CustomerTag`
- [x] Define `CustomerNote`
- [x] Define `CustomerActivity`
- [x] Implement CreateCustomer
- [x] Implement UpdateCustomer
- [x] Implement AddNote
- [x] Implement UpdateTags
- [x] Implement GetCustomerActivity

Acceptance:

- The customer module stands on its own

## B6 automation

- [x] Create `internal/modules/automation/domain`
- [x] Create `internal/modules/automation/application`
- [x] Create `internal/modules/automation/infra`
- [x] Create `internal/modules/automation/delivery`
- [x] Define `Trigger`
- [x] Define `Condition`
- [x] Define `Action`
- [x] Define `Execution`
- [x] Subscribe to ticket events
- [x] Subscribe to conversation events
- [x] Subscribe to routing events
- [x] Migrate dry-run

Acceptance:

- automation executes on the event bus

## B7 analytics

- [x] Create `internal/modules/analytics/domain`
- [x] Create `internal/modules/analytics/application`
- [x] Create `internal/modules/analytics/infra`
- [x] Create `internal/modules/analytics/delivery`
- [x] DashboardReadModel
- [x] TicketTrendReadModel
- [x] AgentPerformanceReadModel
- [x] SatisfactionTrendReadModel
- [x] SLATrendReadModel
- [x] Scheduled aggregation first
- [x] Event-driven incremental aggregation next

Acceptance:

- Statistics are separated from business services

## B8 voice

- [x] Create `internal/modules/voice/domain`
- [x] Create `internal/modules/voice/application`
- [x] Create `internal/modules/voice/infra`
- [x] Create `internal/modules/voice/delivery`
- [x] Define `CallSession`
- [x] Define `MediaSession`
- [x] Define `VoiceParticipant`
- [x] Define `Recording`
- [x] Define `Transcript`
- [x] Abstract `RecordingProvider`
- [x] Abstract `TranscriptProvider`
- [x] Implement StartCall
- [x] Implement AnswerCall
- [x] Implement HoldCall
- [x] Implement ResumeCall
- [x] Implement EndCall
- [x] Implement TransferCall
- [x] Publish `call.started`
- [x] Publish `call.held`
- [x] Publish `call.resumed`
- [x] Publish `call.transferred`
- [x] Publish `call.ended`
- [x] Implement StartRecording
- [x] Implement StopRecording
- [x] Implement AppendTranscript
- [x] Publish `recording.started`
- [x] Publish `recording.stopped`
- [x] Publish `transcript.appended`
- [x] Assemble `VoiceCoordinator`
- [x] Wire into the runtime / WebRTC lifecycle

Acceptance:

- The voice module is the business landing point for SIP
- `voice` is not bound to a single protocol; signaling goes through `voiceprotocol.CallSignalingAdapter`, media through `voiceprotocol.MediaSessionAdapter`
- Recording and transcription are reserved via provider interfaces and are not bound to a single vendor
- Recording and transcription already exist as formal application use cases, not just interface declarations
- The runtime has wired the WebRTC lifecycle into the voice module via `VoiceCoordinator`
