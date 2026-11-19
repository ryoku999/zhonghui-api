-- +goose Up
ALTER TABLE load_records
    ALTER COLUMN trailer_id DROP NOT NULL;

-- +goose Down
-- This rollback requires every load to have a trailer.
ALTER TABLE load_records
    ALTER COLUMN trailer_id SET NOT NULL;
