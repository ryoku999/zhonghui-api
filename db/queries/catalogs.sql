-- name: SearchCompanies :many
SELECT id, ruc, legal_name
FROM companies
WHERE is_active
  AND (strpos(ruc, sqlc.arg(search)::text) > 0
       OR strpos(lower(legal_name), lower(sqlc.arg(search)::text)) > 0)
ORDER BY legal_name, id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: SearchDrivers :many
SELECT id, document_type, document_number, first_names, last_names, phone
FROM drivers
WHERE is_active
  AND (strpos(document_number, upper(sqlc.arg(search)::text)) > 0
       OR strpos(lower(concat_ws(' ', first_names, last_names)), lower(sqlc.arg(search)::text)) > 0)
ORDER BY last_names, first_names, id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: SearchLicenses :many
SELECT l.id, l.driver_id, l.license_number, l.license_class, l.category, l.expires_on, l.issuing_country
FROM driver_licenses l
JOIN drivers d ON d.id = l.driver_id
WHERE d.is_active
  AND (sqlc.narg(driver_id)::uuid IS NULL OR l.driver_id = sqlc.narg(driver_id)::uuid)
  AND strpos(l.license_number, upper(sqlc.arg(search)::text)) > 0
ORDER BY l.license_number, l.id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;

-- name: SearchVehicles :many
SELECT id, plate, vehicle_type, make, model
FROM vehicles
WHERE is_active
  AND (sqlc.arg(vehicle_type)::text = '' OR vehicle_type = sqlc.arg(vehicle_type)::text)
  AND strpos(plate, sqlc.arg(search)::text) > 0
ORDER BY plate, id
LIMIT sqlc.arg(page_limit)::integer OFFSET sqlc.arg(page_offset)::integer;
