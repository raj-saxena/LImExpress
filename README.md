# LImExpress

A lightweight and efficient LLM gateway that allows the use of common coding tools against multiple LLM providers, with built-in user/team attribution, budgeting, and monitoring.

## Product summary

Build a minimal LLM gateway that provides:
	•	“Virtual keys” developers can export once (e.g., ANTHROPIC_AUTH_TOKEN=sk_vkey_...), usable from the public internet
	•	User + team attribution for all requests
	•	Budgets/limits per user and team, with rolling windows; smallest remaining wins
	•	Streaming (SSE) support for model responses
	•	Admission control at request start, post-charge accounting at end
	•	Metadata-only logs (no prompt/response content)
	•	In-app dashboards (aggregated only) for up to 3 months, plus long retention in Datadog (15 months)

## Deployment & infra

Traffic path: Cloud Armor → Istio Gateway → Gateway Service (this project) → Providers (OpenAI/Anthropic/Vertex)
	•	Runs in K8s
	•	Prometheus scrapes metrics
	•	Datadog agent scrapes logs (JSON)

## Multi-tenancy
	•	Multi-org support (hard isolation)
	•	Users are members of org(s) and teams within org
	•	A request must always be attributable to exactly one org (and a user/team within it)

## Auth & UX
	•	Browser login via Google Workspace (OIDC)
	•	Portal to manage keys: create / list / revoke; show secret only once
	•	Keys are used by CLI/tools from laptops on public internet
	•	Upstream calls use single org credentials per provider (not BYOK)

## Tech decisions (locked)
	•	Language: Go
	•	Server: net/http + chi
	•	Config: viper
	•	DB: Postgres (target “18”, treat as latest PG); SQL: pgx + sqlc
	•	Migrations: golang-migrate/migrate
	•	Logging: zap (JSON)
	•	Metrics: Prometheus
	•	Testing: testing + testify + httptest + testcontainers-go + vegeta
	•	UI: templ + HTMX + Tailwind + daisyUI
	•	Rate limiting/budgets MVP: fixed windows (hour/day) + optional burst control via concurrency limits (active streams per user/team)
	•	Token bucket is a later enhancement (likely with Valkey/Redis)

## Non-goals (explicitly out of scope)
	•	Cross-provider schema translation, fallback routing, model “smart routing”
	•	Prompt storage, prompt inspection, content logging, prompt evaluation
	•	Per-request deep tracing across providers (optional later)
	•	Fine-grained “conversation management” beyond using conversation-id for session-based budgeting
	•	Real-time per-request drilldown in portal (aggregates only)

## Style and engineering principles
	•	KISS, DRY, minimal abstractions
	•	Avoid enterprise patterns (no “clean architecture” ceremony, no factory jungles)
	•	Comments only where they prevent misunderstanding (esp. security/limits/streaming edge cases)
	•	Prefer small packages with clear boundaries over “framework within the app”
	•	Test critical invariants (auth, limits, streaming correctness, accounting)

## ADR requirement
	•	Add an ADR for choosing chi over Echo/Gin and for PG-only fixed-window budgets.

⸻

## Shared definitions (everyone uses the same terms)
	•	Virtual Key: a secret token a developer exports once; stored hashed; maps to an org, user, team(s), and policy.
	•	Session: identified by conversation-id (client-provided). Budgets may apply per conversation-id.
	•	Budget windows:
	•	Rolling (desired) but MVP implementation will be fixed hour/day windows.
	•	Smallest remaining wins:
	•	Admission is allowed only if both user and team budgets allow; effective remaining is min(user_remaining, team_remaining).