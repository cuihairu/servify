# Demo / Mock / In-Memory Boundaries

This inventory is used to answer a question that must be made clear before going live: which demo, mock, stub, and in-memory capabilities in the repository are still kept, why they are still here, and when they can exit.

Principles:

- Demo assets can be kept, but they must not be misjudged as the formal product surface.
- mock / stub can be used for local regression, CI acceptance, or adapter boundary testing, but they cannot be used to claim that an external platform is formally supported.
- If an in-memory capability carries runtime responsibility, its single-process boundary, data loss boundary, and replacement conditions must be made explicit.

## Runtime and Delivery Assets Still Kept

| Capability/asset | Location | Current reason for keeping | Usable for formal deployment | Exit condition |
| --- | --- | --- | --- | --- |
| Demo pages and Demo SDK static assets | `apps/demo/` `apps/demo-sdk/` `apps/server/internal/app/server/static.go` `scripts/sync-sdk-to-demo.sh` | Used for sales demos, SDK embedding examples, and generated asset drift verification; currently also carries the role of "minimal visible integration sample" | No. They are demo and integration examples, not a formal customer portal, and not a multi-tenant release surface; since P3-3, production no longer serves `/demo-sdk/*` (requests go through the SPA fallback), exposed only in non-production environments | When a formal embedded SDK documentation site, a standalone examples repository, or a formal customer portal replaces the current demo surface, the demo assets can be moved out of the main runtime or switched to a separate release |
| One-click demo and seed scripts | `scripts/demo-setup.sh` `scripts/seed-data.sh` | Used for local demos, acceptance preparation, and quickly generating minimal business data | No. Default accounts, demo keys, and sample data must not enter production | When the staging/preview environment has an independent fixture flow and demo data becomes a one-time environment initialization task, they can be downgraded to internal-only tools or moved out of the repository's main script entry |
| WeKnora mock service | `infra/compose/weknora-mock/` `infra/compose/docker-compose.weknora.yml` | Provides a repeatable upstream dependency for `G1-2` and local protocol regression, so that each acceptance does not have to depend on a real WeKnora environment | No. It only verifies protocol connectivity and the fallback/upload/sync chain, and does not represent real retrieval quality, capacity, or stability | When the team can repeatedly create a real WeKnora staging environment, and the acceptance script is switched to `WEKNORA_ACCEPTANCE_MODE=real` by default, the mock can be downgraded to compatibility-test-only |
| The `mock` mode of the WeKnora acceptance script | `scripts/test-weknora-integration.sh` | Used to first verify, in CI or locally, Servify's calling convention against the upstream protocol, evidence output, and fallback strategy | Conditionally usable. It is suitable for regression scripts, not as the sole evidence that "a real knowledge base is ready for production" | When `real` mode has become a mandatory precondition of the release gate, `mock` mode remains only for development regression |
| In-process event bus | `apps/server/internal/platform/eventbus/inmemory_bus.go` | The default implementation of event dispatch inside the current monolithic runtime; dead letter and basic metrics have been added, enough to support single-process deployment | Conditionally usable. Only applicable to development/demo and single-instance deployments that tolerate non-persistent events; since P3-3, production/staging startup validation (zero tolerance in `config.InsecureDefaults`) and the assembly layer (`bootstrap.BuildEventBus`) both directly reject `inmemory`, `redis` must be explicitly configured | When the deployment topology needs cross-instance consumption, event persistence, replay, or strict recovery guarantees, replace with an external message queue/bus |
| Knowledge base memory provider | `apps/server/internal/platform/knowledgeprovider/memory/provider.go` | As the local fallback for provider switching tests and no-external-dependency runs, ensuring the AI/knowledge chain can still regress without a network | Conditionally usable. Can only be treated as a degraded knowledge source, not as formal enterprise knowledge base capability | When all target environments have a real knowledge base provider, and release acceptance no longer accepts memory fallback as primary evidence, it can be narrowed down to test/dev-only |
| Voice recording/transcription mock provider | `apps/server/internal/modules/voice/provider/mock/` `apps/server/internal/app/server/voice_runtime.go` | Voice call/recording/transcript are already persisted; the mock remains as an optional provider keeping the chain regressable (the real Twilio/Deepgram providers are wired in, default `disabled`) | No. The mock only guarantees the command chain and persistence, and does not provide real media capture or speech recognition; production directly rejects mock (P0-2 guard) | When the acceptance of the real providers for recording and transcription is closed out (configuration switching, error observability, and vendor SLA), the mock is narrowed down to test-only |
| Telegram / WeChat channel routing stub | `apps/server/internal/platform/realtime/message_router.go` | Keeps the unified platform adapter interface, making it easy to connect real channels later; the current implementation is only a placeholder and does not block the Web main chain | No. This must not be used to claim that Telegram or WeChat has been formally connected | When the real webhook / send API / authentication / delivery receipt integration is complete, with minimal acceptance and fault handling added, delete the stub implementation or keep only the test stub |

## Test-Only Mocks, Not Part of the Default Runtime

The mocks below should still be kept, but they should not be counted into the problem ticket of "the runtime still depends on mock":

| Capability | Location | Reason for keeping | Exit condition |
| --- | --- | --- | --- |
| LLM mock provider | `apps/server/internal/platform/llm/mock/provider.go` | Used to stably cover the LLM interface contract, error branches, and streaming behavior | Only consider deletion when the test strategy no longer needs provider-level isolation; currently there is no value in deleting it |
| Knowledge provider mock | `apps/server/internal/platform/knowledgeprovider/mock/provider.go` | Used to cover the knowledge retrieval interface contract and error paths | Same as above, keep as a test fixture |

## Go-Live Judgment Convention

Currently acceptable boundaries:

- The demo pages, demo SDK, and seed scripts continue to be kept, but must serve only demos, documentation, and local regression.
- The WeKnora mock continues to be kept, but release readiness cannot rely only on mock evidence.
- The in-memory event bus can support the current single-process/single-instance baseline, but it is not a multi-instance production messaging system.

The parts that still must not be claimed as "formally productized":

- External provider capabilities for voice recording and transcription.
- External channel integration capabilities such as Telegram / WeChat.
- Any "production ready" conclusion derived from demo data, mock upstreams, or the memory provider.

## Suggested Enforcement Rules

- From now on, whenever a new demo/mock/stub/in-memory runtime capability is added, three things must be added in sync: the reason for keeping it, the prod-safe judgment, and the exit condition.
- If an acceptance document references mock or seed results, it must explicitly mark whether it is "protocol/regression evidence" or "real environment evidence".
- Before formally promising a channel or AI capability externally, first confirm that it no longer depends on the placeholder implementations in this inventory.
