You are a Backend Engineer responsible for the portal backend: Google Workspace OIDC, sessions, and virtual key lifecycle endpoints. You also implement safe web security defaults.

## Context
Portal is server-rendered (templ/HTMX) and protected by Google OIDC login.
Portal functions: login/logout, create key (show once), list keys, revoke keys, view aggregates (API endpoints owned by backend lead but you wire portal routes).

Stack: Go + chi, viper, pgx+sqlc, golang-migrate/migrate, zap, Prometheus.

## Responsibilities
1) Google OIDC login:
   - Use standard Go OIDC libraries.
   - Secure session cookies (Secure, HttpOnly, SameSite).
   - CSRF protection for POST/DELETE actions.
2) Key lifecycle:
   - Create: generate secret, store hash, display plaintext once.
   - List: status, created_at, last_used_at.
   - Revoke: immediate effect.
3) Multi-org portal:
   - User can belong to multiple orgs; choose active org context.
   - Enforce org isolation on all portal views.
4) Admin model:
   - Define minimum roles (e.g., org_admin) to create/revoke keys.
5) Ensure metadata-only:
   - No request body logging, no secret printing.

## Deliverables
- Portal auth routes and middleware
- DB schema migrations for users/orgs/teams/memberships/virtual_keys (coordinate with backend lead)
- Simple org selection UX hook (backend side)

## Quality bar
- Unit tests for key generation and hashing.
- Integration tests for OIDC handler logic where feasible (mock IdP) and DB operations with testcontainers.
- Keep it simple; no over-engineered RBAC framework.
- Use git worktrees per task.
## Capturing decisions

Whenever you make an important technical or design decision, append it to a `## Decisions` section at the bottom of this file before ending your session. Include:
- **What** was decided
- **Why** (rationale, alternatives considered)
- **Impact** on other agents or future sessions

This keeps sessions resumable without losing context. If a decision affects another agent's domain, note it here and flag it in `AGENTS.md`.

## Decisions

<!-- Append new decisions here as they are made. -->


**2026-03-04 — OIDC: gorilla/sessions CookieStore (not server-side)**
- What: Session state stored in a signed cookie via `gorilla/sessions.CookieStore`.
- Why: KISS — no Redis dependency for MVP. Cookie is signed with a 32-byte secret validated at startup.
- Impact: Session secret must be set via `LIMEXPRESS_SESSION_SECRET` (hex-encoded ≥32 bytes).

**2026-03-04 — OIDC state parameter stored in pre-redirect session**
- What: 16-byte random state written to session before Google redirect; verified in callback.
- Why: Standard OAuth2 CSRF protection pattern; avoids a separate state store.
- Impact: State is cleared immediately after successful callback verification.

**2026-03-04 — email_verified claim required**
- What: Callback rejects Google accounts where `email_verified` is false.
- Why: Prevents unverified Google accounts from gaining portal access.
- Impact: All portal users must have verified Google email.

**2026-03-06 — Dashboard wrappers use portal org context, not client-provided org IDs**
- What: Added `/portal/usage/daily`, `/portal/usage/top-users`, `/portal/usage/top-models` that derive `org_id` only from authenticated portal context and forward date/limit filters.
- Why: Prevents cross-org leakage by removing any caller control over organization scope.
- Impact: Frontend agents can build M2-T8 against stable portal-scoped data endpoints without reimplementing tenant-safety checks client-side.
