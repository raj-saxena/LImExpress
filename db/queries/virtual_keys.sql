-- name: GetVirtualKeyByHash :one
SELECT id, org_id, user_id, team_id, key_hash, prefix, status, created_at, last_used_at, revoked_at
FROM virtual_keys
WHERE key_hash = $1;

-- name: UpdateVirtualKeyLastUsed :exec
UPDATE virtual_keys
SET last_used_at = NOW()
WHERE id = $1;

-- name: RevokeVirtualKey :exec
UPDATE virtual_keys
SET status = 'revoked', revoked_at = NOW()
WHERE id = $1 AND org_id = $2;

-- name: ListVirtualKeysByUser :many
SELECT id, org_id, user_id, team_id, prefix, status, created_at, last_used_at, revoked_at
FROM virtual_keys
WHERE user_id = $1 AND org_id = $2
ORDER BY created_at DESC;

-- name: CreateVirtualKey :one
INSERT INTO virtual_keys (org_id, user_id, team_id, key_hash, prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, org_id, user_id, team_id, key_hash, prefix, status, created_at, last_used_at, revoked_at;

-- name: GetVirtualKey :one
SELECT id, org_id, user_id, team_id, prefix, status, created_at, last_used_at, revoked_at
FROM virtual_keys
WHERE id = $1 AND org_id = $2;

-- name: ListKeysByOrg :many
SELECT id, user_id, team_id, prefix, status, created_at, last_used_at, revoked_at
FROM virtual_keys
WHERE org_id = $1
ORDER BY created_at DESC;
