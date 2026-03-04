-- name: GetUserByEmail :one
SELECT id, email, created_at FROM users WHERE email = $1;

-- name: UpsertUser :one
INSERT INTO users (email) VALUES ($1)
ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
RETURNING id, email, created_at;

-- name: GetUserOrgs :many
SELECT o.id, o.name, m.role
FROM orgs o
JOIN memberships m ON m.org_id = o.id
WHERE m.user_id = $1
ORDER BY o.name;
