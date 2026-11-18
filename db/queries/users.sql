-- name: CreateUser :one
INSERT INTO users (username, password_hash, display_name, role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListUsers :many
SELECT id, username, display_name, role, is_active
FROM users ORDER BY username LIMIT $1 OFFSET $2;

-- name: LockUserManagement :exec
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;

-- name: CountActiveAdmins :one
SELECT count(*) FROM users WHERE is_active AND role = 'ADMIN';

-- name: UpdateUser :one
UPDATE users SET
    display_name = coalesce(sqlc.narg('display_name'), display_name),
    role = coalesce(sqlc.narg('role'), role),
    is_active = coalesce(sqlc.narg('is_active'), is_active),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: AuditUserChange :exec
INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, request_id)
VALUES ($1, 'user', $2, $3, $4);
