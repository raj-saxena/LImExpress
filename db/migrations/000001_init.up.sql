-- 000001_init.up.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE orgs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE teams (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (org_id, name),
    UNIQUE (org_id, id)  -- enables composite FK from child tables to enforce team belongs to same org
);

-- role: 'member' | 'org_admin'
-- team_id NULL means the membership is org-wide (not team-scoped).
-- NULLS NOT DISTINCT (PG15+): two rows with the same user_id, org_id, NULL team_id are considered equal.
CREATE TABLE memberships (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id  UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    team_id UUID,
    role    TEXT NOT NULL DEFAULT 'member',
    UNIQUE NULLS NOT DISTINCT (user_id, org_id, team_id),
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE CASCADE
);

-- status: 'active' | 'revoked'
CREATE TABLE virtual_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id      UUID,
    key_hash     TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,           -- first 8 chars of plaintext for display
    status       TEXT NOT NULL DEFAULT 'active',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE SET NULL (team_id)
);

CREATE INDEX idx_virtual_keys_user ON virtual_keys(user_id);

CREATE TABLE usage_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id         UUID,
    virtual_key_id  UUID NOT NULL REFERENCES virtual_keys(id) ON DELETE CASCADE,
    provider        TEXT NOT NULL,
    model           TEXT NOT NULL,
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    cost_usd        NUMERIC(12, 8) NOT NULL DEFAULT 0,
    conversation_id TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE SET NULL (team_id)
);

CREATE INDEX idx_usage_events_org_created ON usage_events(org_id, created_at DESC);

-- NULLS NOT DISTINCT: team_id NULL is a valid distinct key component (PG15+).
CREATE TABLE usage_agg_hour (
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id       UUID,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    window_start  TIMESTAMPTZ NOT NULL,   -- truncated to hour UTC
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd      NUMERIC(14, 8) NOT NULL DEFAULT 0,
    request_count INT NOT NULL DEFAULT 0,
    UNIQUE NULLS NOT DISTINCT (org_id, user_id, team_id, provider, model, window_start),
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE SET NULL (team_id)
);

CREATE INDEX idx_agg_hour_org ON usage_agg_hour(org_id, window_start DESC);

CREATE TABLE usage_agg_day (
    org_id        UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id       UUID,
    provider      TEXT NOT NULL,
    model         TEXT NOT NULL,
    window_start  TIMESTAMPTZ NOT NULL,   -- truncated to day UTC
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cost_usd      NUMERIC(14, 8) NOT NULL DEFAULT 0,
    request_count INT NOT NULL DEFAULT 0,
    UNIQUE NULLS NOT DISTINCT (org_id, user_id, team_id, provider, model, window_start),
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE SET NULL (team_id)
);

CREATE INDEX idx_agg_day_org ON usage_agg_day(org_id, window_start DESC);

-- NULLS NOT DISTINCT (PG15+): treats NULL user_id/team_id as equal for uniqueness,
-- so each (org, optional-user, optional-team) scope can have at most one policy row.
-- The unique constraint also creates an implicit index, making a separate lookup index redundant.
CREATE TABLE budget_policies (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id                 UUID NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    user_id                UUID REFERENCES users(id) ON DELETE CASCADE,
    team_id                UUID,
    max_cost_usd_hour      NUMERIC(12, 8),
    max_cost_usd_day       NUMERIC(12, 8),
    max_tokens_hour        BIGINT,
    max_tokens_day         BIGINT,
    max_concurrent_streams INT,
    UNIQUE NULLS NOT DISTINCT (org_id, user_id, team_id),
    FOREIGN KEY (org_id, team_id) REFERENCES teams(org_id, id) ON DELETE CASCADE
);
