-- +goose Up
ALTER TABLE driver_licenses
    ADD COLUMN revalidation_on date,
    ADD COLUMN restrictions text[],
    ADD COLUMN issuing_center text,
    ADD COLUMN is_electronic boolean,
    ADD COLUMN mtc_checked_at timestamptz;

-- +goose Down
ALTER TABLE driver_licenses
    DROP COLUMN mtc_checked_at,
    DROP COLUMN is_electronic,
    DROP COLUMN issuing_center,
    DROP COLUMN restrictions,
    DROP COLUMN revalidation_on;
