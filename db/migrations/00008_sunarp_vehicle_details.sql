-- +goose Up
ALTER TABLE vehicles
    ADD COLUMN serial_number text,
    ADD COLUMN vin text,
    ADD COLUMN engine_number text,
    ADD COLUMN previous_plate text,
    ADD COLUMN registry_office text,
    ADD COLUMN registry_notes text,
    ADD COLUMN registry_entries jsonb CHECK (jsonb_typeof(registry_entries) = 'array'),
    ADD COLUMN theft_alert text,
    ADD COLUMN sunarp_checked_at timestamptz;

-- +goose Down
ALTER TABLE vehicles
    DROP COLUMN sunarp_checked_at,
    DROP COLUMN theft_alert,
    DROP COLUMN registry_entries,
    DROP COLUMN registry_notes,
    DROP COLUMN registry_office,
    DROP COLUMN previous_plate,
    DROP COLUMN engine_number,
    DROP COLUMN vin,
    DROP COLUMN serial_number;
