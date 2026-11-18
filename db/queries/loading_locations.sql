-- name: ListActiveLoadingLocations :many
SELECT id, name
FROM loading_locations
WHERE is_active
ORDER BY name, id;
