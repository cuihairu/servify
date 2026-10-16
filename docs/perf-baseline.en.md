> **English** | [中文](perf-baseline.md)

# Performance and Capacity Baseline (P2-8)

servify's first-version capacity baseline: answering "how much load can this service actually take", with numeric conclusions that are re-runnable and backed by evidence, not a feel-based "it runs".

## Baseline Environment

| Item | Value |
| --- | --- |
| Database | sqlite (local file, AutoMigrate) |
| LLM | embedded mock (OpenAI-compatible `chat/completions`, constant response, no network latency) |
| Knowledge base | no external provider (default state; `knowledge/upload` and `sync` returning 503 is the expected convention) |
| Rate limiting | relaxed to `requests_per_minute=100000 / burst=10000` (to keep the rate limiter from becoming the measured object) |
| Host | single machine (local 127.0.0.1 loopback, no network loss) |

Baseline convention note: the baseline measures the **relative capacity and latency shape of the application layer**, excluding real LLM round trips (the downstream latency of the AI scenario is the mock's sub-millisecond level). After attaching a real LLM, AI query latency should be estimated as p95 + LLM network latency.

## Toolchain

- `scripts/perfbench` (implemented with the Go standard library, zero third-party load-test dependencies): a four-scenario load generator + a hand-written WebSocket client (RFC 6455 handshake and frame codec).
- `scripts/test-perf-baseline.sh`: orchestration script — build → mock LLM → derive the load-test config (`perfbench -gen-config-src <源配置> -gen-config-dst <输出路径>`, the yaml Node API preserves production config comments and placeholders) → start the server on sqlite → 401 negative sample → admin → four scenarios → result verification → manifest write.
- Two scale tiers: `smoke` (seconds-level for CI-driven tests) and `full` (capacity baseline).

```bash
make perf-baseline                     # smoke 档（CI 同款）
PERF_SCALE=full make perf-baseline     # full 档（容量基线）
```

## Scenarios and Scale (full tier)

| Scenario | Load | Concurrency |
| --- | --- | --- |
| tickets-mixed | 600 rounds × (create ticket + post comment + list query) = 1800 ops | 16 |
| ai-query | 200 × `POST /api/v1/ai/query` | 16 |
| upload-knowledge | 100 × (multipart file upload + knowledge document upload) = 200 ops | 16 |
| ws-connections | 200 WebSocket connections (staircase connection setup, 25 per batch) + one text-message round trip per connection + `/api/v1/ws/stats` reconciliation | 25/batch |

## Baseline Data (2026-09-18, full tier)

Data source: `scripts/test-results/perf-baseline/results.json` (manifest `provider=perf`).

| Scenario | ops | Errors | p50 | p95 | p99 | max | Throughput |
| --- | --- | --- | --- | --- | --- | --- | --- |
| tickets-mixed | 1800 | 0 | 23.75ms | 267.47ms | 831.48ms | 1886.45ms | 208 ops/s |
| ai-query | 200 | 0 | 18.48ms | 1033.03ms | 1259.94ms | 2075.33ms | 83 ops/s |
| upload-knowledge | 200 | 0 | 8.59ms | 32.01ms | 35.89ms | 38.19ms | 1213 ops/s |
| ws-connections | 200 connections | 0 | 4.83ms (handshake p50) | 9.26ms | 10.90ms | 13.26ms | round trip 6.41ms |

WebSocket reconciliation: client connection setup 200/200, the server's `/api/v1/ws/stats` reports `connected_clients=200`, consistent.

## Conclusions and Interpretation

1. **Ticket main chain**: no errors at concurrency 16, p50 within 25ms; p99 tail latency is at the 0.8s level, the shape being sqlite's single write lock queuing under high-concurrency writes (SQLite serial writes). With another order of magnitude of concurrency pressure, the first bottleneck is database write throughput, not the application layer. In production deployment (postgres), this tail latency should converge significantly — this baseline does not include postgres data, and a comparison rerun is recommended after switching.
2. **AI query**: with the mock downstream at sub-millisecond latency, p95 still reaches 1.0s — the main latency comes from the query pipeline itself (session loading, timeout/retry protection paths, runtime assembly), not the LLM. This is the true cost floor of the current AI chain; after attaching a real LLM, the latency can only be higher.
3. **File upload**: the highest-throughput scenario (1213 ops/s, p99 36ms); multipart parsing and local disk writes are not a near-term bottleneck.
4. **WebSocket**: all 200 connections established and the server-side statistics reconcile exactly (reconciliation reads poll to the peak: hub registration completes asynchronously after the handshake 101, so an instantaneous snapshot on a slow machine may undercount), handshake p50 5ms, message round trip 6.4ms. The connection manager is under no pressure at this scale; probing the capacity ceiling (tens of thousands of connections) is not in this tier and needs a dedicated high-connection tier plus system-level fd tuning.
5. **Rate limiting**: before load testing it must be relaxed via `perfbench -gen-config`, otherwise the rate limiter tops out before the application layer, and the measured "capacity" is the rate limit threshold.

## Known Boundaries

- The baseline was obtained on sqlite; postgres (the production driver) data is not covered, which is a known difference, not an equivalence conclusion.
- The AI scenario uses a mock LLM; the latency and rate limiting (429) of a real LLM are not included.
- The empty knowledge provider is the expected convention: the upload/sync endpoints' 503 (`knowledge provider is not enabled`) has been handled as expected (perfbench records a note, not an error); this dimension should be retested after pgvector/WeKnora is attached.
- The full tier takes about 4 minutes locally; the smoke tier is seconds-level and, with CI (`TestPerfBaselineScriptWritesEvidence`), guards chain availability on every commit.

## Reproduction

```bash
PERF_SCALE=full make perf-baseline
bash scripts/validate-acceptance-manifest.sh scripts/test-results/perf-baseline/manifest.json
```

When comparing against the baseline, watch: order-of-magnitude changes in the same scenario's p95/p99 (>2x needs an explanation), whether the WS reconciliation still matches, and whether errors are 0.
