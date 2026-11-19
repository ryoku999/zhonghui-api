-- name: LockLoadRequest :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(request_scope)::text, 0));

-- name: FindLoadRequest :one
SELECT request_hash, load_record_id
FROM idempotency_keys WHERE user_id = $1 AND idempotency_key = $2;

-- name: SaveLoadRequest :exec
INSERT INTO idempotency_keys (user_id, idempotency_key, request_hash, load_record_id)
VALUES ($1, $2, $3, $4);

-- name: LockLoadAssignment :one
SELECT user_id FROM user_loading_locations
WHERE user_id = $1 AND loading_location_id = $2 FOR SHARE;

-- name: HasLoadAssignment :one
SELECT EXISTS (
    SELECT 1 FROM user_loading_locations WHERE user_id = $1 AND loading_location_id = $2
);

-- name: NextLoadOrder :one
INSERT INTO daily_order_counters (loading_location_id, business_date, last_number)
VALUES ($1, $2, 1)
ON CONFLICT (loading_location_id, business_date)
DO UPDATE SET last_number = daily_order_counters.last_number + 1
RETURNING last_number;

-- name: CreateLoadRecord :one
INSERT INTO load_records (loading_location_id, business_date, order_number, company_id, driver_id,
    driver_license_id, tractor_id, trailer_id, unit_description, notes, created_by, created_at, updated_at)
VALUES (sqlc.arg(loading_location_id), sqlc.arg(business_date), sqlc.arg(order_number), sqlc.arg(company_id),
    sqlc.arg(driver_id), sqlc.narg(driver_license_id), sqlc.arg(tractor_id), sqlc.narg(trailer_id),
    sqlc.narg(unit_description), sqlc.narg(notes), sqlc.arg(created_by), sqlc.arg(recorded_at), sqlc.arg(recorded_at))
RETURNING id;

-- name: GetLoadResult :one
SELECT l.id, l.loading_location_id, l.business_date, l.order_number, l.created_at, l.created_by,
       l.status, s.version, s.fields
FROM load_records l
JOIN load_record_snapshots s ON s.load_record_id = l.id
  AND s.version = CASE WHEN sqlc.arg(original)::boolean THEN 1 ELSE l.version END
WHERE l.id = sqlc.arg(id);

-- name: LockLoadRecord :one
SELECT * FROM load_records WHERE id = $1 FOR UPDATE;

-- name: UpdateLoadRecord :one
UPDATE load_records
SET unit_description = sqlc.narg(unit_description), notes = sqlc.narg(notes),
    status = sqlc.arg(status), version = version + 1, updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING version;

-- name: WriteLoadSnapshot :exec
INSERT INTO load_record_snapshots (load_record_id, version, fields) VALUES ($1, $2, $3);

-- name: WriteLoadProvenance :exec
INSERT INTO field_provenance (load_record_id, version, field_name, source, verification_status)
SELECT sqlc.arg(load_record_id)::uuid, sqlc.arg(version)::integer, entry.field_name,
       (sqlc.arg(sources)::text[])[entry.position::integer],
       (sqlc.arg(verification_statuses)::text[])[entry.position::integer]
FROM unnest(sqlc.arg(field_names)::text[]) WITH ORDINALITY AS entry(field_name, position);

-- name: CopyLoadProvenance :exec
INSERT INTO field_provenance (load_record_id, version, field_name, source, verification_status)
SELECT p.load_record_id, sqlc.arg(next_version)::integer, p.field_name, p.source, p.verification_status
FROM field_provenance p
WHERE p.load_record_id = sqlc.arg(load_id) AND p.version = sqlc.arg(previous_version);

-- name: AuditLoadChange :exec
INSERT INTO audit_logs (actor_user_id, entity_type, entity_id, action, request_id)
VALUES ($1, 'load_record', $2, $3, $4);
