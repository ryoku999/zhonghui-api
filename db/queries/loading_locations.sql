-- name: ListLoadingLocations :many
SELECT id, name, is_active
FROM loading_locations
WHERE (sqlc.arg(is_admin)::boolean OR is_active)
  AND (NOT sqlc.arg(only_assigned)::boolean OR EXISTS (
      SELECT 1 FROM user_loading_locations
      WHERE loading_location_id = loading_locations.id AND user_id = sqlc.arg(user_id)
  ))
  AND strpos(lower(name), lower(sqlc.arg(search)::text)) > 0
ORDER BY name, id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: GetLoadingLocation :one
SELECT * FROM loading_locations WHERE id = $1;

-- name: LockLoadingLocation :one
SELECT * FROM loading_locations WHERE id = $1 FOR SHARE;

-- name: CreateLoadingLocation :one
INSERT INTO loading_locations (name) VALUES ($1)
RETURNING *;

-- name: UpdateLoadingLocation :one
UPDATE loading_locations
SET name = coalesce(sqlc.narg(name), name),
    is_active = coalesce(sqlc.narg(is_active), is_active),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: ListLocationUsers :many
SELECT u.id, u.username, u.display_name, u.is_active, u.role
FROM user_loading_locations a
JOIN users u ON u.id = a.user_id
WHERE a.loading_location_id = sqlc.arg(location_id)
ORDER BY u.username, u.id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: AssignLocationUser :execrows
INSERT INTO user_loading_locations (loading_location_id, user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveLocationUser :execrows
DELETE FROM user_loading_locations
WHERE loading_location_id = $1 AND user_id = $2;

-- name: AuditLocationChange :exec
INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, request_id)
VALUES ($1, 'loading_location', $2, $3, $4);
