-- +goose Up
ALTER TABLE driver_licenses
    ADD COLUMN license_status text,
    ADD COLUMN issued_on date;

ALTER TABLE vehicles
    ADD COLUMN model_year smallint CHECK (model_year BETWEEN 1886 AND 9999),
    ADD COLUMN color text,
    ADD COLUMN registration_status text;

-- +goose Down
ALTER TABLE vehicles
    DROP COLUMN registration_status,
    DROP COLUMN color,
    DROP COLUMN model_year;

ALTER TABLE driver_licenses
    DROP COLUMN issued_on,
    DROP COLUMN license_status;
