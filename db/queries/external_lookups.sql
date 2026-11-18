-- name: RecordExternalLookup :exec
INSERT INTO external_api_queries (requested_by, provider, lookup_type, outcome, status_code)
VALUES ($1, 'APISTED', $2, $3, $4);

-- name: EnrichCompany :exec
UPDATE companies SET
    tax_status = coalesce(sqlc.narg(tax_status), tax_status),
    tax_condition = coalesce(sqlc.narg(tax_condition), tax_condition),
    fiscal_address = coalesce(sqlc.narg(fiscal_address), fiscal_address),
    last_external_lookup_at = sqlc.arg(verified_at),
    updated_at = now()
WHERE ruc = sqlc.arg(ruc) AND is_active
  AND (last_external_lookup_at IS NULL OR last_external_lookup_at < sqlc.arg(verified_at));

-- name: EnrichLicense :exec
UPDATE driver_licenses l SET
    category = coalesce(sqlc.narg(category), l.category),
    license_status = coalesce(sqlc.narg(license_status), l.license_status),
    issued_on = coalesce(sqlc.narg(issued_on), l.issued_on),
    updated_at = now()
FROM drivers d
WHERE d.id = l.driver_id AND d.document_type = 'DNI'
  AND d.document_number = sqlc.arg(dni) AND d.is_active
  AND l.license_number = sqlc.arg(license_number)
  AND (l.issuing_country IS NULL OR l.issuing_country = 'PE');

-- name: EnrichVehicle :exec
UPDATE vehicles SET
    make = coalesce(sqlc.narg(make), make),
    model = coalesce(sqlc.narg(model), model),
    model_year = coalesce(sqlc.narg(model_year), model_year),
    color = coalesce(sqlc.narg(color), color),
    registration_status = coalesce(sqlc.narg(registration_status), registration_status),
    updated_at = now()
WHERE plate = sqlc.arg(plate) AND is_active;
