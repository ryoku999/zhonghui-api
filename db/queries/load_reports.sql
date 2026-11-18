-- name: SearchLoadRecords :many
SELECT l.id, l.loading_location_id, l.business_date, l.order_number, l.created_at,
       l.status, s.fields, u.display_name AS registrar
FROM load_records l
JOIN load_record_snapshots s ON s.load_record_id = l.id AND s.version = l.version
JOIN users u ON u.id = l.created_by
WHERE (NOT sqlc.arg(assigned_only)::boolean OR EXISTS (
    SELECT 1 FROM user_loading_locations a
    WHERE a.loading_location_id = l.loading_location_id AND a.user_id = sqlc.arg(viewer_id)
))
AND (sqlc.narg(date_from)::date IS NULL OR l.business_date >= sqlc.narg(date_from)::date)
AND (sqlc.narg(date_to)::date IS NULL OR l.business_date <= sqlc.narg(date_to)::date)
AND (sqlc.narg(location_id)::uuid IS NULL OR l.loading_location_id = sqlc.narg(location_id)::uuid)
AND (sqlc.narg(order_number)::integer IS NULL OR l.order_number = sqlc.narg(order_number)::integer)
AND (cardinality(sqlc.arg(load_ids)::uuid[]) = 0 OR l.id = ANY(sqlc.arg(load_ids)::uuid[]))
AND strpos(coalesce(s.fields->>'ruc',''), sqlc.arg(ruc)::text) > 0
AND strpos(lower(concat_ws(' ', s.fields->>'firstNames', s.fields->>'lastNames')), lower(sqlc.arg(driver)::text)) > 0
AND strpos(coalesce(s.fields->>'tractorPlate',''), sqlc.arg(tractor)::text) > 0
AND strpos(coalesce(s.fields->>'trailerPlate',''), sqlc.arg(trailer)::text) > 0
AND (strpos(lower(u.display_name), lower(sqlc.arg(registrar)::text)) > 0
     OR strpos(u.username, lower(sqlc.arg(registrar)::text)) > 0)
ORDER BY
    CASE WHEN sqlc.arg(sort)::text = 'date' AND NOT sqlc.arg(descending)::boolean THEN l.business_date END ASC,
    CASE WHEN sqlc.arg(sort)::text = 'date' AND sqlc.arg(descending)::boolean THEN l.business_date END DESC,
    CASE WHEN sqlc.arg(sort)::text = 'order' AND NOT sqlc.arg(descending)::boolean THEN l.order_number END ASC,
    CASE WHEN sqlc.arg(sort)::text = 'order' AND sqlc.arg(descending)::boolean THEN l.order_number END DESC,
    CASE WHEN sqlc.arg(sort)::text = 'location' AND NOT sqlc.arg(descending)::boolean THEN lower(s.fields->>'locationName') END ASC,
    CASE WHEN sqlc.arg(sort)::text = 'location' AND sqlc.arg(descending)::boolean THEN lower(s.fields->>'locationName') END DESC,
    l.business_date DESC, l.created_at DESC, l.id
LIMIT sqlc.arg(row_limit)::integer OFFSET sqlc.arg(row_offset)::integer;

-- name: LoadDashboardSummary :one
SELECT count(*) AS total,
       count(*) FILTER (WHERE l.status = 'REGISTERED') AS registered,
       count(*) FILTER (WHERE l.status = 'CANCELLED') AS cancelled,
       count(DISTINCT l.loading_location_id) AS active_locations
FROM load_records l
WHERE l.business_date BETWEEN sqlc.arg(date_from)::date AND sqlc.arg(date_to)::date
  AND (NOT sqlc.arg(assigned_only)::boolean OR EXISTS (
      SELECT 1 FROM user_loading_locations a
      WHERE a.loading_location_id = l.loading_location_id AND a.user_id = sqlc.arg(viewer_id)
  ));

-- name: SearchAuditEntries :many
SELECT a.id, a.actor_user_id, coalesce(u.display_name, '')::text AS actor_name,
       a.entity_type, a.entity_id, a.action, a.request_id, a.created_at
FROM audit_logs a
LEFT JOIN users u ON u.id = a.actor_user_id
WHERE (sqlc.arg(action_filter)::text = '' OR a.action = sqlc.arg(action_filter)::text)
  AND (sqlc.arg(entity_filter)::text = '' OR a.entity_type = sqlc.arg(entity_filter)::text)
  AND (sqlc.narg(start_at)::timestamptz IS NULL OR a.created_at >= sqlc.narg(start_at)::timestamptz)
  AND (sqlc.narg(end_at)::timestamptz IS NULL OR a.created_at < sqlc.narg(end_at)::timestamptz)
ORDER BY a.created_at DESC, a.id
LIMIT sqlc.arg(row_limit)::integer OFFSET sqlc.arg(row_offset)::integer;
