-- name: FindLoadCompany :one
SELECT * FROM companies WHERE ruc = $1 FOR SHARE;

-- name: EnsureLoadCompany :execrows
INSERT INTO companies (ruc, legal_name) VALUES ($1, $2)
ON CONFLICT (ruc) DO NOTHING;

-- name: FindLoadDriver :one
SELECT * FROM drivers WHERE document_type = $1 AND document_number = $2 FOR SHARE;

-- name: EnsureLoadDriver :execrows
INSERT INTO drivers (document_type, document_number, first_names, last_names, phone)
VALUES ($1, $2, $3, $4, $5) ON CONFLICT (document_type, document_number) DO NOTHING;

-- name: FindLoadLicenses :many
SELECT id FROM driver_licenses
WHERE driver_id = sqlc.arg(driver_id) AND license_number = sqlc.arg(license_number)
  AND (sqlc.narg(license_id)::uuid IS NULL OR id = sqlc.narg(license_id)::uuid)
ORDER BY id LIMIT 2 FOR SHARE;

-- name: EnsureLoadLicense :execrows
INSERT INTO driver_licenses (driver_id, license_number) VALUES ($1, $2)
ON CONFLICT (driver_id, license_number, issuing_country) DO NOTHING;

-- name: FindLoadVehicle :one
SELECT * FROM vehicles WHERE plate = $1 FOR SHARE;

-- name: EnsureLoadVehicle :execrows
INSERT INTO vehicles (plate, vehicle_type) VALUES ($1, $2)
ON CONFLICT (plate) DO NOTHING;
