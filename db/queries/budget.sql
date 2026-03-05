-- name: GetBudgetPolicy :one
-- Returns the most specific policy for the given user+team within an org.
-- Specificity: user+team > user-only > team-only > org-wide.
SELECT id, org_id, user_id, team_id,
       max_cost_usd_hour, max_cost_usd_day,
       max_tokens_hour, max_tokens_day,
       max_concurrent_streams
FROM budget_policies
WHERE org_id = $1
  AND (user_id = $2 OR user_id IS NULL)
  AND (team_id = $3 OR team_id IS NULL)
ORDER BY
    (user_id IS NOT NULL)::int DESC,
    (team_id IS NOT NULL)::int DESC
LIMIT 1;

-- name: GetCurrentWindowUsageHour :one
-- Returns aggregated usage for the current hour window for a user.
SELECT COALESCE(SUM(cost_usd), 0)::NUMERIC              AS cost_usd,
       COALESCE(SUM(input_tokens + output_tokens), 0)::BIGINT AS total_tokens,
       COALESCE(SUM(request_count), 0)::INT              AS request_count
FROM usage_agg_hour
WHERE org_id = $1
  AND user_id = $2
  AND window_start = date_trunc('hour', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

-- name: GetCurrentWindowUsageDay :one
-- Returns aggregated usage for the current day window for a user.
SELECT COALESCE(SUM(cost_usd), 0)::NUMERIC              AS cost_usd,
       COALESCE(SUM(input_tokens + output_tokens), 0)::BIGINT AS total_tokens,
       COALESCE(SUM(request_count), 0)::INT              AS request_count
FROM usage_agg_day
WHERE org_id = $1
  AND user_id = $2
  AND window_start = date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

-- name: GetTeamWindowUsageHour :one
SELECT COALESCE(SUM(cost_usd), 0)::NUMERIC              AS cost_usd,
       COALESCE(SUM(input_tokens + output_tokens), 0)::BIGINT AS total_tokens
FROM usage_agg_hour
WHERE org_id = $1
  AND team_id = $2
  AND window_start = date_trunc('hour', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

-- name: GetTeamWindowUsageDay :one
SELECT COALESCE(SUM(cost_usd), 0)::NUMERIC              AS cost_usd,
       COALESCE(SUM(input_tokens + output_tokens), 0)::BIGINT AS total_tokens
FROM usage_agg_day
WHERE org_id = $1
  AND team_id = $2
  AND window_start = date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC';

-- name: UpsertUsageAggHour :exec
INSERT INTO usage_agg_hour (org_id, user_id, team_id, provider, model, window_start, input_tokens, output_tokens, cost_usd, request_count)
VALUES ($1, $2, $3, $4, $5, date_trunc('hour', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC', $6, $7, $8, 1)
ON CONFLICT (org_id, user_id, team_id, provider, model, window_start)
DO UPDATE SET
    input_tokens  = usage_agg_hour.input_tokens  + EXCLUDED.input_tokens,
    output_tokens = usage_agg_hour.output_tokens + EXCLUDED.output_tokens,
    cost_usd      = usage_agg_hour.cost_usd      + EXCLUDED.cost_usd,
    request_count = usage_agg_hour.request_count + 1;

-- name: UpsertUsageAggDay :exec
INSERT INTO usage_agg_day (org_id, user_id, team_id, provider, model, window_start, input_tokens, output_tokens, cost_usd, request_count)
VALUES ($1, $2, $3, $4, $5, date_trunc('day', NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC', $6, $7, $8, 1)
ON CONFLICT (org_id, user_id, team_id, provider, model, window_start)
DO UPDATE SET
    input_tokens  = usage_agg_day.input_tokens  + EXCLUDED.input_tokens,
    output_tokens = usage_agg_day.output_tokens + EXCLUDED.output_tokens,
    cost_usd      = usage_agg_day.cost_usd      + EXCLUDED.cost_usd,
    request_count = usage_agg_day.request_count + 1;
