-- name: FindUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: LockUserForSession :one
SELECT * FROM users WHERE id = $1 FOR SHARE;

-- name: GetActiveSessionUser :one
SELECT u.* FROM users u
JOIN user_sessions s ON s.user_id = u.id
WHERE s.id = sqlc.arg(session_id) AND u.id = sqlc.arg(user_id) AND u.is_active
  AND s.revoked_at IS NULL AND s.expires_at > now();

-- name: CreateSession :exec
INSERT INTO user_sessions (id, user_id, refresh_token_hash, expires_at)
VALUES ($1, $2, $3, $4);

-- name: LockSession :one
SELECT * FROM user_sessions WHERE id = $1 AND user_id = $2 FOR UPDATE;

-- name: RotateSession :exec
UPDATE user_sessions
SET refresh_token_hash = $2, updated_at = now()
WHERE id = $1;

-- name: RevokeSession :exec
UPDATE user_sessions SET revoked_at = now(), updated_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE user_sessions SET revoked_at = now(), updated_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;
