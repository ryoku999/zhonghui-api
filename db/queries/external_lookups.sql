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
    revalidation_on = coalesce(sqlc.narg(revalidation_on), l.revalidation_on),
    restrictions = coalesce(sqlc.narg(restrictions)::text[], l.restrictions),
    issuing_center = coalesce(sqlc.narg(issuing_center), l.issuing_center),
    is_electronic = coalesce(sqlc.narg(is_electronic), l.is_electronic),
    mtc_checked_at = sqlc.arg(checked_at),
    updated_at = now()
FROM drivers d
WHERE d.id = l.driver_id AND d.document_type = 'DNI'
  AND d.document_number = sqlc.arg(dni) AND d.is_active
  AND l.license_number = sqlc.arg(license_number)
  AND (sqlc.narg(license_id)::uuid IS NULL OR l.id = sqlc.narg(license_id)::uuid)
  AND (l.mtc_checked_at IS NULL OR l.mtc_checked_at < sqlc.arg(checked_at))
  AND (l.issuing_country IS NULL OR l.issuing_country = 'PE');

-- name: EnrichVehicle :exec
UPDATE vehicles SET
    make = coalesce(sqlc.narg(make), make),
    model = coalesce(sqlc.narg(model), model),
    model_year = coalesce(sqlc.narg(model_year), model_year),
    color = coalesce(sqlc.narg(color), color),
    registration_status = coalesce(sqlc.narg(registration_status), registration_status),
    serial_number = coalesce(sqlc.narg(serial_number), serial_number),
    vin = coalesce(sqlc.narg(vin), vin),
    engine_number = coalesce(sqlc.narg(engine_number), engine_number),
    previous_plate = coalesce(sqlc.narg(previous_plate), previous_plate),
    registry_office = coalesce(sqlc.narg(registry_office), registry_office),
    registry_notes = coalesce(sqlc.narg(registry_notes), registry_notes),
    registry_entries = coalesce(sqlc.narg(registry_entries)::jsonb, registry_entries),
    theft_alert = coalesce(sqlc.narg(theft_alert), theft_alert),
    sunarp_checked_at = sqlc.arg(checked_at),
    updated_at = now()
WHERE plate = sqlc.arg(plate) AND is_active
  AND (sunarp_checked_at IS NULL OR sunarp_checked_at < sqlc.arg(checked_at));
