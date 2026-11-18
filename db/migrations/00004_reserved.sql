-- +goose Up
-- Preserve the version recorded by existing databases.
SELECT 1;

-- +goose Down
SELECT 1;
