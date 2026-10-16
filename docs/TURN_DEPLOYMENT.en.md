> **English** | [中文](TURN_DEPLOYMENT.md)

# TURN Deployment Selection: Embedded pion/turn vs Standalone coturn

> Status: selection signed off (2026-09-22): **standalone coturn deployment + time-limited credentials**. Items 1–4 and item 6 of the section 5 checklist have landed (2026-09-22): configuration surface, `iceturn` credential package, server-side wiring, WS delivery of `webrtc-ice-config` + REST delivery endpoint `/api/v1/rtc/ice-servers` (the SDK `startCall` stub is removed, server-side delivery is the default, host override retained), coturn compose delivery assets, dual-document sync. Remaining: the delivery checklist section of `docs/deployment.md`, the TURN connectivity acceptance script.
> Background: the 2026-09-22 architecture review confirmed that TURN has no configuration surface ([deployment guide section 4](./webrtc-deployment.md) has faithfully recorded the current state); under cross-symmetric NAT scenarios the WebRTC real-time chain is unusable. This document answers "which option to pick, how credentials are managed, where the code extensions land", and **does not repeat operational detail** (installation, ports, verification are in [WebRTC / remote assistance deployment guide](./webrtc-deployment.md)).

## Conclusion First

1. **Option: standalone coturn deployment (separate container/process), no embedded pion/turn.** The decisive dimensions are independent network placement (TURN needs public UDP and should not be bound to the app container) and lifecycle decoupling (app rolling releases must not cut off in-flight media relay).
2. **Credential convention: time-limited HMAC credentials (coturn `use-auth-secret` mode)**, generated dynamically by the server per `static-auth-secret` and delivered via signaling; **static long-lived credentials do not enter production** (the `--lt-cred-mech --user` example in [deployment guide section 5](./webrtc-deployment.md) is the minimal usable form, suitable only for pure STUN/development; upgrade per section 4 of this document when TURN is enabled).
3. **Managed cloud TURN (Twilio NTS / Cloudflare Calls TURN etc.) is vetoed outright**: unreachable/unstable under China mainland private deployment, the same veto convention as the SaaS-only veto for knowledge base selection (see [KNOWLEDGE_BASE_LANDSCAPE](./KNOWLEDGE_BASE_LANDSCAPE.md)).
4. Landing scale: configuration surface + credential package + delivery endpoint + compose templates about 2–3 days (see section 5); real integration testing in customer data centers is counted separately.

## 1. Fact Chain (verified 2026-09-22)

| # | Fact | Location |
| --- | --- | --- |
| 1 | The server is one end of the PeerConnection (browser ↔ server direct ICE); each connection's `ICEServers` fills only a single STUN | `apps/server/internal/platform/realtime/webrtc_service.go` `CreatePeerConnection` |
| 2 | The WebRTC configuration surface is a single key, defaulting to Google STUN, unreachable from mainland data centers (timeouts slow down connection setup) | `config.yml` `webrtc.stun_server`; fallback value in `apps/server/internal/config/config.go` |
| 3 | `webrtc.stun_server` has no environment variable override (the env override allowlist contains no webrtc keys) | `apps/server/internal/app/bootstrap/config.go` `applyConfigEnvOverrides` |
| 4 | The client-side ICE configuration defaults to an empty array and relies on the host integrator to supply it; the REST delivery endpoint `startCall` is an explicit `unsupported()` stub | `sdk/packages/core/src/sdk.ts` (`remoteAssist.iceServers` fallback chain), `sdk/packages/core/src/api.ts` |
| 5 | pion/turn v5 is already in the dependency graph (indirect dependency of `pion/webrtc`); the embedded option adds no module cost | `apps/server/go.mod` |
| 6 | The delivery form is mainly single-machine Docker Compose private deployment | `docs/deployment.md` |
| 7 | TURN relay has no configuration surface: `ICEServer.Username/Credential` are not exposed | `apps/server/internal/platform/realtime/webrtc_service.go` |

**Topology convention** (consistent with [deployment guide section 1](./webrtc-deployment.md)): direct ICE happens between the browser and the Servify server, and **both** ends need ICE configuration — the server assembles it from config (fact 1), while the browser side currently relies on the host to supply it (fact 4). Therefore landing TURN must fill in both "server-side assembly" and "client-side delivery endpoint"; missing either still leaves one side of symmetric NAT unreachable.

## 2. Candidate Options and Hard Constraints

| Option | Form |
| --- | --- |
| A: embedded pion/turn | Run a TURN server inside the Go process with `pion/turn` (about 200 lines of self-built code) |
| B: standalone coturn | Separate container/process (the de facto industry standard; same as Jitsi/Matrix/Nextcloud) |
| C: managed cloud TURN | SaaS such as Twilio NTS / Cloudflare Calls TURN |

Hard constraints:

- **Delivery form**: private single-machine compose (fact 6); customer data center network conditions are uncontrollable;
- **Product bottom line**: remote assistance/voice is the customer service real-time chain; **a session interruption is an incident**;
- **Network reachability**: the main delivery scenario is mainland China (fact 2 corroborates); C is out directly.

## 3. Comparison and Recommendation

| Dimension | A: embedded pion/turn | B: standalone coturn ✅ |
| --- | --- | --- |
| Deployment unit | Zero addition (starts/stops with the app) | 1 new compose service, host network + port ranges (3478, 5349, relay range such as 49160-49200/udp) |
| **Network placement** | ❌ the app container must directly hold public UDP, conflicting with the usual delivery of "app exposed through reverse proxy/LB" | ✅ a standalone container can sit on a public node/DMZ, and the app network topology stays unchanged |
| **Lifecycle coupling** | ❌ every app release/restart = all allocations cut off, in-flight real-time sessions interrupted | ✅ app rolling releases do not affect media relay |
| Maturity | library + about 200 lines of self-built server, little production-scale validation | 20 years of accumulated edge cases (TCP/TLS relay, IPv6, filtering, anti-abuse) |
| Attack surface isolation | ❌ the relay is a traffic abuse target, sharing life and death with the business process | ✅ separate process; the relay being hit does not touch the customer service core service |
| Credentials | self-written auth callback (session validation can be reused, but every allocation calls back to the app, coupling) | ✅ `use-auth-secret` natively supports time-limited credentials; about 30 lines of Go generation |
| Observability | ✅ can attach prometheus directly (following the MetricsAggregator bridge pattern) | ⚠️ no native prometheus (third-party exporter quality is mediocre); relies on logs + connection stats |
| Team familiarity | ✅ pure Go | C component; one more runbook/upgrade duty |

**The decisive dimensions (network placement, lifecycle decoupling) both point to B.** A's real advantage is "single-binary minimal delivery, least operational effort at small media volume", suitable for edge small-scale deployment scenarios — the servify delivery form (fact 6) and the product bottom line (the real-time chain must not break with a release) do not satisfy its premises, **not recommended**; if an "all-in-one single-machine image" delivery form appears in the future it can be re-evaluated.

## 4. Credential and Security Convention

**Mechanism** (coturn REST API / time-limited credentials, common community practice):

```
username  = <expiry 的 Unix 时间戳>
credential = base64(HMAC-SHA1(static_auth_secret, username))
```

- coturn side: `use-auth-secret` + `static-auth-secret=<随机串>`, replacing the `--lt-cred-mech --user` static credential mode of deployment guide section 5;
- TTL recommended at **5 minutes**: after expiry, existing allocations keep living until the connection ends; new allocations must fetch fresh credentials;
- The server holds the secret and generates credentials; the client (browser side) receives short-lived credentials via signaling/REST, so **leakage self-heals, no revocation process needed**;
- Keep the relay port range narrow (e.g. `min-port=49160 --max-port=49200`), and allow the range at the firewall;
- The secret is sensitive configuration, under the jurisdiction of the secure-config gate (see section 5); it must not enter demo/mock artifacts or documentation examples.

## 5. Landing Path (code extension checklist)

In dependency order:

1. ✅(2026-09-22)**Configuration surface**: extend the `webrtc` block to `stun_servers` (list, redundant multi-STUN) + `turn.url / turn.realm / turn.static_auth_secret / turn.ttl`.
   > Convention note: [deployment guide section 4](./webrtc-deployment.md) once proposed the minimal evolution path of "add an `ice_servers` list to the config (with username/credential)" — that path would induce **writing static credentials into the config file**; this document upgrades it to the `turn.*` convention above: the secret lives only on the server, credentials are generated at runtime. When landing, update deployment guide sections 2/4 in sync.
   Gate convention: zero-tolerance `Validate` plus an assembly-layer fallback, two-layer gate; any new configuration key must update the secure-config allowlist/template lint tests (see `internal/config/config_template_lint_test.go`, `TestValidate_SecureConfigIsValid`, etc.) and sync the three templates `config.yml` + `config.production.secure.example.yml` + `config.staging.example.yml`; add webrtc keys to the `applyConfigEnvOverrides` allowlist (fact 3).
2. ✅(2026-09-22)**Platform package**: add an ICE credential package under `internal/platform/` (or merge into `realtime`): credential generation (HMAC) + ICE config assembly, provider `none | coturn`, interface with a mock seam (coverage gate 100%; async side-effect tests poll per the existing convention).
3. ✅(2026-09-22)**Server-side assembly**: `CreatePeerConnection` fills in the complete `ICEServers` (STUN list + a TURN entry with short-lived credentials), converged in one place for voice/realtime.
4. ✅(2026-09-22)**Client-side delivery endpoint**: the minimal path delivers the ICE config during WS signaling connection setup (following the `webrtc-state-change` style message pattern); the REST surface (`/api/v1/rtc/ice-servers`, workspace authentication) as a follow-up item, which also removes the `unsupported` stub of SDK `startCall`. SDK `remoteAssist.iceServers` is retained as the host override entry; server-side delivery is the default.
5. ◐(compose assets 2026-09-22)**Delivery assets**: `infra/compose/docker-compose.coturn.yml` (host network + `use-auth-secret` mode + port ranges + env injection sharing the secret with the app); add a "remote assistance network requirements" section to the delivery checklist of `docs/deployment.md`; add a TURN connectivity acceptance script following the mock/real dual-mode idea of `test-ragflow-acceptance.sh`.
6. ✅(2026-09-22)**Documentation sync**: change the TURN row of the section 4 status table of the deployment guide to ✅, and upgrade the coturn example of section 5 to the auth-secret mode; update the status header of this document with the landing schedule.

## 6. Division of Labor with Existing Documentation

| Document | Responsibility |
| --- | --- |
| This document (TURN_DEPLOYMENT.md) | Selection sign-off, credential convention, code extension landing points |
| [webrtc-deployment.md](./webrtc-deployment.md) | Operational layer: topology, ports/firewall, coturn installation, end-to-end verification, troubleshooting |
| [architecture-review-2026.md](./architecture-review-2026.md) | AI/Agent direction review (this document is the follow-up decision on its WebRTC deployment gap) |

## References

- coturn official repository (`use-auth-secret` / `static-auth-secret` / REST API mechanism explanation): https://github.com/coturn/coturn
- Time-limited TURN credential mechanism (REST API for TURN Access): https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest
- pion/turn (the library for the embedded option, already in the dependency graph): https://github.com/pion/turn
- Trickle ICE online verification: https://webrtc.github.io/samples/src/content/peerconnection/trickle-ice/
