-- name: InsertUsageEvent :one
INSERT INTO usage_events (org_id, user_id, team_id, virtual_key_id, provider, model, input_tokens, output_tokens, cost_usd, conversation_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, created_at;

-- name: GetDailyUsageByOrg :many
SELECT window_start::DATE            AS day,
       SUM(input_tokens)::BIGINT     AS input_tokens,
       SUM(output_tokens)::BIGINT    AS output_tokens,
       SUM(cost_usd)::NUMERIC(14, 8) AS cost_usd,
       SUM(request_count)::INT       AS request_count
FROM usage_agg_day
WHERE org_id = $1
  AND window_start >= $2
  AND window_start <  $3
GROUP BY window_start::DATE
ORDER BY day DESC;

-- name: GetTopUsersByOrg :many
SELECT u.id, u.email,
       SUM(a.cost_usd)::NUMERIC(14, 8) AS total_cost_usd,
       SUM(a.request_count)::INT        AS total_requests
FROM usage_agg_day a
JOIN users u ON u.id = a.user_id
WHERE a.org_id = $1
  AND a.window_start >= $2
  AND a.window_start <  $3
GROUP BY u.id, u.email
ORDER BY total_cost_usd DESC
LIMIT $4;

-- name: GetTopModelsByOrg :many
SELECT model, provider,
       SUM(cost_usd)::NUMERIC(14, 8) AS total_cost_usd,
       SUM(request_count)::INT        AS total_requests
FROM usage_agg_day
WHERE org_id = $1
  AND window_start >= $2
  AND window_start <  $3
GROUP BY model, provider
ORDER BY total_cost_usd DESC
LIMIT $4;
