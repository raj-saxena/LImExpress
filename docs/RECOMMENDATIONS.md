# Recommendations

Audit date: March 6, 2026
Scope: current repository state compared with `docs/PLAN.md`

## High priority

1. Fail startup with non-zero exit code when DB initialization fails.
Current `cmd/gateway/main.go` logs an error and returns, which exits with status `0`. This can hide failed boots in orchestration/CI.
Suggested change: use fatal exit behavior (`os.Exit(1)` after logging, or `logger.Fatal`) for unrecoverable startup failures.

2. Add explicit migration round-trip test for `up -> down -> up`.
Plan acceptance for M0-T2 requires this exact sequence; current integration test validates `up` then `down` only.
Suggested change: extend `internal/db/db_test.go` with a final `MigrateUp` assertion after `MigrateDown`.

3. Add constraint on membership roles.
`memberships.role` is documented as `'member' | 'org_admin'` but currently has no DB `CHECK` constraint.
Suggested change: add `CHECK (role IN ('member', 'org_admin'))` in migration.

## Medium priority

4. Harden config tests against environment leakage.
`internal/config/config_test.go` manipulates process env with `os.Setenv` / `os.Unsetenv`; this is brittle as test suite grows.
Suggested change: use `t.Setenv(...)` consistently in all subtests.

5. Add timeout-bound context for DB ping during startup.
`internal/db/pool.go` uses caller context directly for `pool.Ping(ctx)`. If context is long-lived, a network issue can stall startup longer than intended.
Suggested change: wrap ping in `context.WithTimeout` (for example 5-10s) and return a clear timeout error.

## Next implementation priorities (from plan gaps)

1. Implement M1-T1 through M1-T4 first (key auth, budget admission, SSE proxy, post-charge accounting), because this establishes the core gateway behavior.
2. Add M1-T5 metrics/logging safeguards and log leak tests early, to avoid retrofitting observability/security constraints later.
3. Start M2 backend auth/key lifecycle once M1 core path is stable.
