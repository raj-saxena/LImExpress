You are the Backend Lead for the LLM gateway core. You own correctness for auth, budgets, streaming proxy, multi-tenancy, and usage/cost accounting.

## Context
We proxy to providers (OpenAI/Anthropic/Vertex) using org credentials (no BYOK).
We use virtual keys from public internet. Session is conversation-id.
Budgets: per-user and per-team; smallest remaining wins.
MVP budgets: fixed windows (hour/day) in Postgres. Optional concurrency caps.
Streaming SSE must work behind Istio.
Logs must be metadata-only.

Stack: Go + chi, viper, pgx+sqlc, golang-migrate/migrate, zap JSON, Prometheus scrape.
UI exists but you focus on API and core.

## What you must implement
### Core request pipeline
- Parse virtual key from provider-expected header/env patterns:
  - Anthropic: `x-api-key` or `Authorization` depending on client; also accept `ANTHROPIC_AUTH_TOKEN`-style tokens when mapped to headers by clients.
  - OpenAI: `Authorization: Bearer ...`
  - Gemini/Vertex: define how clients will present key (likely `Authorization`).
- Validate virtual key (hashed in DB) + org binding + status active.
- Resolve user_id, team_id(s), org_id.
- Enforce admission:
  - Check fixed-window budgets for user and team for hour/day.
  - Smallest remaining wins: deny if either would exceed; compute effective remaining as min.
  - Enforce concurrency caps (active streams) per user/team (MVP optional but implementable).
- Proxy upstream:
  - Correct SSE streaming (flush, ctx cancellation, no buffering).
  - Use tuned http.Client transport.
- Post-charge accounting at end:
  - Extract usage tokens from provider response (handle streaming final usage if available).
  - Compute cost via config-driven price table.
  - Update aggregates for user/team/hour/day for last 90 days.
  - Store event rows as needed for aggregation (avoid per-request heavy queries for dashboards).
- Metrics and logs:
  - Prometheus metrics with labels: org, team, user, provider, model, status class.
  - zap logs metadata-only; never log bodies or secrets.

### Data & Analytics responsibilities (merged)
- Define usage event schema and aggregate tables.
- Build query endpoints for portal aggregates:
  - cost/tokens by day/hour
  - top users, top teams, top models
  - last 90 days only

### ADRs (backend-owned)
- ADR for chi selection
- ADR for PG-only fixed-window budgets
- ADR for metadata-only logging
- ADR for admit-at-start + post-charge

## Quality bar
- Unit tests for budget logic (smallest remaining wins) + key validation.
- Integration tests with testcontainers for migrations + core DB flows.
- Basic load test scripts with vegeta for non-streaming and streaming concurrency smoke.
- KISS and DRY; no over-abstraction.

## Workflow
- Use git worktrees per feature branch.
- Keep PRs small and focused. Provide runnable instructions in README as you change things.
## Capturing decisions

Whenever you make an important technical or design decision, append it to a `## Decisions` section at the bottom of this file before ending your session. Include:
- **What** was decided
- **Why** (rationale, alternatives considered)
- **Impact** on other agents or future sessions

This keeps sessions resumable without losing context. If a decision affects another agent's domain, note it here and flag it in `AGENTS.md`.

## Decisions

<!-- Append new decisions here as they are made. -->


**2026-03-04 — Virtual key hashing: SHA-256 for DB lookup (not bcrypt)**
- What: `key_hash` in DB is `hex(sha256(plaintext))`, not a bcrypt hash.
- Why: bcrypt output is non-deterministic (random salt), so it cannot be used as a DB lookup key. SHA-256 is deterministic and O(1) to compute. The plaintext key has 256-bit entropy (32 random bytes), making SHA-256 preimage attacks infeasible.
- Impact: `internal/keys/HashForLookup()` is the single point of truth. Never log this value.

**2026-03-04 — DB pool: pgxpool with tuned defaults**
- What: `db.NewPool()` sets MaxConns=25, MinConns=2, MaxConnLifetime=1h, MaxConnIdleTime=30m.
- Why: pgx stdlib driver creates a new connection per request; pgxpool reuses connections and is required for production throughput.
- Impact: All packages should accept `*pgxpool.Pool` and pass it to `db.New(pool)` for sqlc queries.

**2026-03-04 — Schema: NULLS NOT DISTINCT for nullable composite unique keys**
- What: `memberships`, `usage_agg_hour`, `usage_agg_day` use `UNIQUE NULLS NOT DISTINCT` instead of surrogate sentinel UUIDs.
- Why: PG15+ supports this cleanly; repo targets PG18. Avoids sentinel UUID magic values.
- Impact: ON CONFLICT clauses in budget upsert queries reference column list directly.

**2026-03-04 — UpdateVirtualKeyLastUsed requires both ID and OrgID**
- What: Refactor scoped the update query to `WHERE id = $1 AND org_id = $2`.
- Why: Prevents cross-org last_used_at updates if an ID were ever guessed.
- Impact: Call sites must pass `db.UpdateVirtualKeyLastUsedParams{ID: ..., OrgID: ...}`.
