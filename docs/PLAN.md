# LImExpress — MVP Plan

## Task Status (updated 2026-03-06)

| Task | Description | Status | Branch / PR |
|------|-------------|--------|-------------|
| M0-T1 | Go module + repo skeleton | ✅ Done | `chore/foundation` |
| M0-T2 | DB schema + migrations | ✅ Done | `chore/foundation` |
| M0-T3 | sqlc config + generated types | ✅ Done | `chore/foundation` |
| M0-T4 | Config skeleton (viper) | ✅ Done | `chore/foundation` |
| M0-T5 | CI skeleton | ✅ Done | `chore/foundation` |
| M1-T1 | Virtual key middleware | ✅ Done | PR #9 merged |
| M1-T2 | Budget admission | ✅ Done | PR #19 merged |
| M1-T3 | SSE streaming proxy | ✅ Done | PR #20 merged |
| M1-T4 | Post-charge accounting | ✅ Done | PR #21 merged |
| M1-T5 | Prometheus metrics + zap logging | ✅ Done | PR #10 merged |
| M1-T6 | Analytics query endpoints | 🔲 Not started | — |
| M2-T1 | Google Workspace OIDC login | ✅ Done | PR #11 merged |
| M2-T2 | Multi-org context middleware | 🔲 Not started | — |
| M2-T3 | Key lifecycle endpoints | 🔲 Not started | — |
| M2-T4 | Dashboard data endpoints | 🔲 Not started | — |
| M2-T5 | Portal shell + navigation | 🔄 In progress | `feature/e11-portal-shell-v2` |
| M2-T6 | Login / logout UI | 🔲 Not started | — |
| M2-T7 | Key management UI | 🔲 Not started | — |
| M2-T8 | Usage dashboard UI | 🔲 Not started | — |
| M3-T1 | Dockerfile + K8s manifests | 🔲 Not started | — |
| M3-T2 | Istio SSE configuration | 🔲 Not started | — |
| M3-T3 | Prometheus scrape + Datadog log config | 🔲 Not started | — |
| M3-T4 | Operations docs + runbooks | 🔲 Not started | — |
| M4-T1 | Security review + SECURITY.md | 🔲 Not started | — |
| M4-T2 | Test suite (unit + integration) | 🔲 Not started | — |
| M4-T3 | Load tests | 🔲 Not started | — |
| M4-T4 | Release checklist | 🔲 Not started | — |

**Legend:** ✅ Done · 🔄 In progress · 🔲 Not started

---

## Progress Summary (as of March 6, 2026)

### Overall status
- **M0 Foundation:** ✅ Complete (5/5).
- **M1 Gateway Core:** ✅ 5/6 done (M1-T6 analytics endpoints pending).
- **M2 Portal:** 🔄 2/8 done (OIDC + portal shell; key lifecycle, org context, UI work pending).
- **M3 Observability & Ops:** 🔲 Not started (0/4).
- **M4 Hardening & Release:** 🔲 Not started (0/4).

## Scope

Build a minimal LLM gateway with:
- Virtual keys (exportable once, stored hashed, revocable)
- Multi-org isolation with user + team attribution
- Budget enforcement: fixed-window (hour/day), smallest-remaining-wins; deny at admission, charge post-response
- SSE streaming proxy to OpenAI, Anthropic, Vertex — works behind Istio
- Server-rendered portal: Google Workspace OIDC login, key lifecycle, aggregated dashboards (last 90 days)
- Metadata-only logs (no bodies, no secrets); Prometheus metrics; JSON logs for Datadog

**Non-goals for MVP:** cross-provider routing/fallback, content logging, per-request drilldown, token bucket (Valkey), fine-grained RBAC, prompt evaluation.

---

## Milestones

| # | Milestone | What unlocks |
|---|-----------|-------------|
| M0 | Foundation | Everything else |
| M1 | Gateway Core | Load tests, integration tests |
| M2 | Portal | Frontend UI work, E2E flow |
| M3 | Observability & Ops | Production readiness |
| M4 | Hardening & Release | Ship |

---

## M0 — Foundation
**Owner:** Backend Lead (coordinate with all)
**Branch:** `chore/foundation`
**Unblocks:** all other work

### Tasks (must be sequential within M0)

**M0-T1 — Go module + repo skeleton**
- `go mod init`; establish package layout: `cmd/gateway`, `internal/gateway`, `internal/portal`, `internal/db`, `internal/budget`, `internal/proxy`, `internal/keys`, `internal/middleware`, `internal/metrics`
- Add `.gitignore` entries for build artifacts, `*.env`
- Acceptance: `go build ./...` succeeds from a clean clone

**M0-T2 — DB schema + migrations**
- Tables: `orgs`, `users`, `teams`, `memberships`, `virtual_keys`, `usage_events`, `usage_agg_hour`, `usage_agg_day`
- Use `golang-migrate`; migrations live in `db/migrations/`
- Acceptance: `migrate up` then `migrate down` then `migrate up` again succeeds against a local Postgres container

**M0-T3 — sqlc config + generated types**
- Define queries for key lookup, budget read/write, usage insert, aggregate upsert
- Acceptance: `sqlc generate` runs clean; generated code compiles

**M0-T4 — Config skeleton (viper)**
- Load from env + optional config file: server port, DB DSN, provider credentials, price table, budget defaults
- Acceptance: service starts and logs config at INFO (redacting secrets)

**M0-T5 — CI skeleton**
- GitHub Actions: `go build`, `go vet`, `go test ./...` (unit only), `sqlc generate` check
- Acceptance: CI green on a no-op PR

---

## M1 — Gateway Core
**Owner:** Backend Lead
**Blocked by:** M0
**Branch-per-task** (see naming below)

All M1 tasks can be worked in parallel once M0 merges, except where noted.

### M1-T1 — Virtual key middleware
`feature/e1-key-middleware`

- Parse key from `x-api-key` (Anthropic), `Authorization: Bearer` (OpenAI/Vertex)
- Constant-time hash compare against DB; resolve `org_id`, `user_id`, `team_id`
- Return `401` with minimal body on failure; never leak key bytes in logs
- Acceptance: unit tests for hash validation, org binding check, revoked key rejection

### M1-T2 — Budget admission
`feature/e2-budget-admission`

Depends on: M1-T1 (needs resolved IDs)

- Query fixed-window (hour/day) remaining for user and team
- Effective remaining = `min(user_remaining, team_remaining)`
- Deny with `429` if either is exhausted; include `Retry-After` header using window end
- Optional: concurrency cap check (active streams counter per user/team using atomic/DB)
- Acceptance: unit tests for smallest-remaining-wins logic; boundary cases (0 remaining, both exhausted, only one exhausted)

### M1-T3 — SSE streaming proxy
`feature/e3-sse-proxy`

Depends on: M1-T1 (needs org context to select upstream credential)

- Forward request to provider; stream response chunks as SSE
- Flush after each chunk; respect `ctx` cancellation (client disconnect closes upstream)
- Use tuned `http.Client` (no buffering, appropriate timeouts)
- Acceptance: smoke test through Istio with `curl --no-buffer`; client disconnect cancels upstream

### M1-T4 — Post-charge accounting
`feature/e4-accounting`

Depends on: M1-T3 (needs usage from completed response)

- Extract token counts from provider response (handle both streaming final-chunk and non-streaming)
- Compute cost via config price table (model → input/output $/token)
- Upsert `usage_agg_hour` and `usage_agg_day` for `(org, user, team, model, provider)`
- Insert `usage_events` row for audit
- Acceptance: integration test with mock upstream verifies aggregates updated; cost computed correctly for at least 2 models

### M1-T5 — Prometheus metrics + zap logging
`feature/e5-metrics-logging`

Can be worked in parallel with M1-T1.

- Metrics: `llmgw_requests_total{org,team,user,provider,model,status_class}`, `llmgw_tokens_total`, `llmgw_cost_total`, `llmgw_budget_denied_total`, `llmgw_stream_duration_seconds`
- `/metrics` endpoint; scrape annotations in K8s manifests (coordinate with Platform)
- zap JSON: request_id, org_id, user_id, team_id, model, provider, latency_ms, tokens, cost — never log body, never log key material
- Acceptance: automated assertion (grep) that no test log output contains request body or key strings

### M1-T6 — Analytics query endpoints
`feature/e6-analytics-api`

Depends on: M1-T4 (needs aggregates)

- `GET /api/v1/usage/daily` — cost+tokens by day (last 90 days), scoped to org
- `GET /api/v1/usage/top-users` — top N users by cost
- `GET /api/v1/usage/top-models` — top N models by cost
- Query params: `from`, `to`, `team_id` (optional filter)
- Acceptance: returns correct aggregates in integration test against seeded DB

---

## M2 — Portal
**Owner:** Backend Engineer (auth + key lifecycle) + Frontend (UI)
**Blocked by:** M0
**Backend and Frontend tracks are parallel once M0 merges**

### M2-Backend: Auth & Key Lifecycle

**M2-T1 — Google Workspace OIDC login**
`feature/e7-oidc-login`

- Standard Go OIDC flow; session cookie (Secure, HttpOnly, SameSite=Lax)
- CSRF token on all state-mutating forms
- Acceptance: login and logout work with a real (or mocked) Google IdP; session cookie not accessible to JS

**M2-T2 — Multi-org context middleware**
`feature/e8-org-context`

Depends on: M2-T1

- User may belong to multiple orgs; `active_org_id` stored in session or header
- All portal views scoped to active org; switching org updates session
- Acceptance: user in 2 orgs sees only their own data in each context; switching works

**M2-T3 — Key lifecycle endpoints**
`feature/e9-key-lifecycle`

Depends on: M2-T2

- `POST /portal/keys` — generate key, store hash, return plaintext once
- `GET /portal/keys` — list keys (status, created_at, last_used_at); never return plaintext
- `DELETE /portal/keys/:id` — revoke; immediate effect on gateway auth
- Roles: `org_admin` can create/revoke; regular members view only their own keys
- Acceptance: create → authenticate against gateway → revoke → authenticate fails

**M2-T4 — Dashboard data endpoints**
`feature/e10-dashboard-api`

Depends on: M1-T6 (analytics API) + M2-T2 (org context)

- Portal-facing wrappers over analytics API; enforce org scoping
- Acceptance: returns 90-day data for current org only; no cross-org leakage

### M2-Frontend: Portal UI

Can start with templ component scaffolding while backend tracks are in progress.

**M2-T5 — Portal shell + navigation**
`feature/e11-portal-shell`

No backend dependency to start.

- Base templ layout: nav bar, org switcher placeholder, flash messages
- Tailwind + daisyUI base styles
- Acceptance: renders at `/portal` with correct layout on all screen sizes

**M2-T6 — Login / logout UI**
`feature/e12-portal-auth-ui`

Depends on: M2-T1 (backend OIDC)

- Login page (Google button), redirect flow, logout button
- Acceptance: full browser login/logout flow works end-to-end

**M2-T7 — Key management UI**
`feature/e13-portal-keys-ui`

Depends on: M2-T3 (key lifecycle endpoints)

- Create key form: HTMX post, show secret in a dismissible one-time reveal modal
- Keys list: status badge, timestamps, revoke button with confirmation
- Acceptance: create → see plaintext once → refresh → plaintext gone; revoke changes status immediately (HTMX swap)

**M2-T8 — Usage dashboard UI**
`feature/e14-portal-dashboard-ui`

Depends on: M2-T4 (dashboard data)

- Tables: daily cost/tokens (last 90 days), top users, top models
- HTMX-driven date range filter (optional for MVP; static tables acceptable)
- Acceptance: data matches DB aggregates; no real-time drilldown required

---

## M3 — Observability & Ops
**Owner:** Platform Engineer
**Blocked by:** M0
**Parallel with M1 and M2**

**M3-T1 — Dockerfile + K8s manifests**
`feature/e15-deploy-artifacts`

- Multi-stage Dockerfile; Helm chart or Kustomize in `deploy/`
- Readiness probe on `/healthz`; liveness on `/healthz` (don't probe streaming endpoint)
- PDB, rolling update strategy, resource requests/limits
- Acceptance: `helm template` or `kustomize build` renders valid YAML; image builds

**M3-T2 — Istio SSE configuration**
`feature/e16-istio-sse`

- VirtualService timeout > SSE session max duration; disable response buffering
- Drain-aware shutdown (SIGTERM → drain active streams → exit)
- Acceptance: documented config + smoke test script that verifies streams survive 30s

**M3-T3 — Prometheus scrape + Datadog log config**
`feature/e17-observability-config`

Depends on: M1-T5 (metrics endpoint)

- Prometheus scrape annotations on pod spec
- Datadog agent config for JSON log parsing; log pipeline tags: `org_id`, `team_id`, `model`
- Acceptance: `curl /metrics` returns all defined metrics; Datadog log sample parses correctly

**M3-T4 — Operations docs + runbooks**
`docs/operations`

- `docs/OPERATIONS.md`: deploy steps, config reference, HPA guidance
- `docs/RUNBOOK.md`: diagnosing budget rejection spikes, streaming issues, provider outage behavior

---

## M4 — Hardening & Release
**Owner:** All (Security + QA lead; Backend reviews)
**Blocked by:** M1, M2, M3

**M4-T1 — Security review + SECURITY.md**
`docs/security`

Can start as docs-only immediately; code review after M1+M2 land.

- Threat model: key theft, budget bypass, cross-tenant leakage, enumeration, replay
- Review auth middleware, logging paths, key generation entropy, hash algorithm (argon2id or bcrypt)
- `docs/SECURITY.md` with threat model + checklist
- Acceptance: no known leaks in reviewed paths; checklist signed off

**M4-T2 — Test suite (unit + integration)**
`feature/e18-test-suite`

Blocked by: M1, M2

- Unit: budget logic (smallest-remaining-wins edge cases), key validation, accounting math
- Integration (testcontainers): migrations round-trip, key lifecycle DB flow, budget enforcement DB flow
- Mock-upstream proxy tests: streaming correctness, cancellation, accounting trigger
- Log assertion: grep test to verify no body/secret in log output
- Acceptance: `go test ./...` green; coverage >80% on `internal/budget` and `internal/keys`

**M4-T3 — Load tests**
`feature/e19-load-tests`

Blocked by: M1

- vegeta targets in `tests/load/`: key auth endpoint, proxy endpoint (non-streaming), streaming concurrency smoke
- Acceptance: defined p99 targets documented; no panics or goroutine leaks under 10 concurrent streams

**M4-T4 — Release checklist**
`docs/release`

- `docs/RELEASE_CHECKLIST.md` covering all MVP review gates (see AGENTS.md)
- Acceptance: every gate has a named owner and a linked test/artifact

---

## Parallel work summary

```
M0 (Foundation) ──────────────────────────────────────────► MERGE
                 │
                 ├── M1-T1 Key middleware
                 │       └── M1-T2 Budget admission
                 ├── M1-T3 SSE proxy ──────────── M1-T4 Accounting ── M1-T6 Analytics API
                 ├── M1-T5 Metrics/logging                                    │
                 │                                                             │
                 ├── M2-T1 OIDC ── M2-T2 Org ctx ── M2-T3 Key lifecycle ── M2-T4 Dashboard API
                 ├── M2-T5 Portal shell ── M2-T6 Login UI ── M2-T7 Keys UI ── M2-T8 Dashboard UI
                 │
                 └── M3-T1 Dockerfile/K8s
                     M3-T2 Istio SSE config
                     M4-T1 Security docs (docs portion starts now)
```

---

## Risks

| Risk | Impact | Mitigation |
|------|--------|-----------|
| SSE streaming through Istio | High — streaming is core feature | Spike test early (M1-T3 + M3-T2 in parallel) |
| Budget fixed-window drift under clock skew | Medium — over/under charge | Use DB `NOW()` consistently; document window boundaries |
| Cross-org data leakage | High — security/compliance | Integration tests with 2-org fixture from day 1 |
| Provider response format variation (streaming usage) | Medium — accounting accuracy | Per-provider parser; tested against recorded fixtures |
| Secret exposure in logs | High — key material or bodies | Automated log-grep assertions in CI (M4-T2) |
| DB migration conflicts (parallel contributors) | Medium — coordination overhead | Schema changes go through backend-engineer; single migration owner per PR |

---

## Branch naming reference

| Task | Branch |
|------|--------|
| M0 foundation | `chore/foundation` |
| M1-T1 | `feature/e1-key-middleware` |
| M1-T2 | `feature/e2-budget-admission` |
| M1-T3 | `feature/e3-sse-proxy` |
| M1-T4 | `feature/e4-accounting` |
| M1-T5 | `feature/e5-metrics-logging` |
| M1-T6 | `feature/e6-analytics-api` |
| M2-T1 | `feature/e7-oidc-login` |
| M2-T2 | `feature/e8-org-context` |
| M2-T3 | `feature/e9-key-lifecycle` |
| M2-T4 | `feature/e10-dashboard-api` |
| M2-T5 | `feature/e11-portal-shell` |
| M2-T6 | `feature/e12-portal-auth-ui` |
| M2-T7 | `feature/e13-portal-keys-ui` |
| M2-T8 | `feature/e14-portal-dashboard-ui` |
| M3-T1 | `feature/e15-deploy-artifacts` |
| M3-T2 | `feature/e16-istio-sse` |
| M3-T3 | `feature/e17-observability-config` |
| M3-T4 | `docs/operations` |
| M4-T1 | `docs/security` |
| M4-T2 | `feature/e18-test-suite` |
| M4-T3 | `feature/e19-load-tests` |
| M4-T4 | `docs/release` |

---

## Definition of Done (per task)

- Code compiles, `go vet` clean
- Unit tests pass; integration tests pass (testcontainers where DB is involved)
- No body or secret in log output (automated assertion or manual grep)
- PR description includes "how to test" runnable instructions
- Reviewer sign-off from relevant lead (backend lead for M1; backend engineer for M2-backend; PM sign-off for docs)

## MVP Review Gates (from AGENTS.md)

- [ ] Streaming smoke test passes behind Istio
- [ ] Budget enforcement tested: user+team, smallest-remaining-wins
- [ ] Multi-org isolation tests pass
- [ ] Logs verified metadata-only
- [ ] Dashboards show last 90 days aggregated usage
