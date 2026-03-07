# M4-T2 Test Plan (Unit + Integration + Performance)

## Scope and goal
Plan tests that prove service correctness and safety for critical paths, boundary conditions, and performance-sensitive behavior. This plan prioritizes risk coverage over raw code coverage.

## Critical invariants to prove
- Multi-org isolation: no cross-org access or mutation through API, portal, or DB flows.
- Virtual key security: only active keys authenticate; revoked/missing keys fail identically.
- Budget enforcement: user+team budgets enforced with smallest-remaining-wins semantics and fixed window behavior.
- Streaming correctness: SSE responses are forwarded without corruption and cancel correctly on client disconnect.
- Accounting integrity: usage/cost is recorded once per request completion and attributed to the right org/user/team/key.
- Metadata-only observability: logs/metrics never include prompt bodies, API keys, bearer tokens, or secrets.

## Test groups

### Group A: Auth, key status, and tenant isolation
Priority: P0

1) Unit tests (middleware)
- `VirtualKeyAuth` success path with active key and context injection.
- Missing key, malformed auth header, DB miss, revoked key, DB lookup error all return identical `401 {"error":"unauthorized"}`.
- `UpdateVirtualKeyLastUsed` failure does not fail request.
- Provider auth header stripping behavior for inbound client auth headers.

2) Integration tests (DB + HTTP path)
- Key lifecycle: create -> use successfully -> revoke -> use returns 401.
- Member cannot use key from another org even if key ID/hash exists.
- Ensure `UpdateVirtualKeyLastUsed` is org-scoped (`id + org_id`).

3) Edge cases
- Header precedence (`x-api-key` vs `Authorization`) behavior remains stable.
- Unknown path returns 404 without leaking provider details.

### Group B: Budget admission semantics and boundaries
Priority: P0

1) Unit tests (budget middleware)
- Exact-boundary behavior (`usage == limit` and `usage just below/above limit`) for hour/day and cost/tokens.
- Smallest-remaining-wins with asymmetric user/team exhaustion.
- Nil limit handling (unlimited dimension) combined with active limits.
- Missing team (`team_id` zero) and with-team behavior are both correct.

2) Integration tests (DB-driven)
- End-to-end budget enforcement with seeded usage aggregates.
- Denial response contract (`429`, `Retry-After`, body schema).
- Retry window transition correctness near hour/day rollover.

3) Edge/failure tests
- DB read error in budget checks fails closed vs pass-through per current policy; document and lock behavior.
- Numeric precision/rounding cases for `cost_usd` comparisons.

### Group C: Proxy streaming and cancellation
Priority: P0

1) Unit/handler tests (httptest upstream)
- SSE chunk forwarding preserves order and framing.
- Non-streaming responses copy body/status/headers.
- Client disconnect cancels upstream context promptly.
- Upstream transport failure returns 502 (except canceled context path).

2) Integration/smoke tests
- Streaming smoke path through full middleware chain (`auth -> budget -> proxy -> accounting`).
- Verify no goroutine leaks in repeated streaming/cancel loops.

3) Edge cases
- Mixed SSE event payloads where usage appears late/partial.
- OpenAI/Anthropic usage extraction with malformed non-fatal chunks.

### Group D: Accounting, analytics consistency, and portal-scoped data
Priority: P1

1) Unit tests (accounting)
- Post-charge writes exactly one usage event plus hour/day upserts when tokens > 0.
- Unknown model price handling (`cost=0`) still records token usage.
- Ensure accounting is non-blocking and does not affect client response.

2) Integration tests (DB consistency)
- After proxy completion, event and aggregates are mutually consistent.
- Org-scoped analytics endpoints return only active org data.
- 90-day default range and limit clamping are enforced.

3) Edge cases
- Concurrent requests for same user/team aggregate without lost updates.
- Team-null and team-present records aggregate correctly.

### Group E: Metadata-only logs and performance/load
Priority: P0 for leak checks, P1 for throughput targets

1) Leak-prevention tests
- Automated assertions that common logs do not contain:
  - request/response body fragments,
  - `x-api-key`, `authorization`, bearer tokens,
  - virtual key plaintext/prefix patterns.
- Negative tests using seeded fake secrets to prove detector catches leaks.

2) Performance/load tests (vegeta + Go benchmarks)
- Non-streaming proxy throughput baseline (RPS, p95/p99 latency).
- Streaming concurrency smoke (>=10 concurrent streams) with no panics/leaks.
- Budget middleware hot-path benchmark under concurrent requests.
- Accounting write-path benchmark under concurrent usage events.

3) Suggested MVP thresholds (initial, adjustable)
- No goroutine growth trend after 5-minute stream churn test.
- No test-time panics/data races (`go test -race ./...`).
- p99 latency regression budget tracked per scenario (baseline committed in docs).

## Agent split (max 5)

1) QA agent (owner)
- Owns this test plan and acceptance matrix.
- Implements Group B unit+integration matrix and Group E leak assertions.
- Produces `docs/TEST_PLAN.md` updates and CI test matrix docs.

2) Backend Lead agent
- Implements Group C full-chain integration tests and Group D accounting consistency tests.
- Adds race-focused and concurrency consistency tests for aggregates.

3) Backend Engineer agent
- Implements Group A key/auth integration tests and key lifecycle/revocation DB flow tests.
- Extends portal-scoped API tests for org isolation regressions.

4) Platform Engineer agent
- Implements Group E load harness (`tests/load/`) and repeatable local/CI execution recipes.
- Adds stream churn scripts and resource/leak observation instructions.

5) Security agent
- Defines and implements log/secret leakage rules and negative test corpus.
- Reviews auth error-shape and anti-enumeration assertions in Group A/E.

## Sequencing
1) QA and Security align on leak rules + auth error invariants.
2) Backend Engineer lands Group A integration tests.
3) Backend Lead lands Group C/D consistency + concurrency tests.
4) QA lands remaining Group B boundary/failure matrix.
5) Platform Engineer lands load harness and baseline metrics capture.

## Entry/exit criteria for M4-T2

Entry
- M1 and M2 features are merged and testable.

Exit
- `go test ./...` is green.
- `go test -race ./...` is green (or documented exception with follow-up ticket).
- Integration suite covering key lifecycle + budget enforcement + accounting consistency is green.
- Log leak assertions are green.
- Coverage target from plan remains met:
  - `internal/budget` >= 80%
  - `internal/keys` >= 80%

## Suggested test inventory by package
- `internal/middleware`: add `auth_test.go` (currently missing direct auth middleware tests).
- `internal/budget`: extend boundary and failure semantics.
- `internal/proxy`: extend SSE malformed/late-usage and churn cases.
- `internal/gateway`: expand accounting/analytics consistency and concurrent updates.
- `internal/db`: add lifecycle + budget integration flows beyond migration-only checks.
- `tests/load`: non-streaming, streaming, and budget-path load scenarios.

