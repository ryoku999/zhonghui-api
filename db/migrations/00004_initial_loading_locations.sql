-- +goose Up
INSERT INTO loading_locations (id, name) VALUES
    ('397c9d1b-b0c7-4bc7-a87a-bbdf1fe99eba', 'Huinchos'),
    ('b942cd2c-c743-4cb2-8f67-8e30d4a82d80', 'Embarque 2');

-- +goose Down
DELETE FROM loading_locations
WHERE id IN ('397c9d1b-b0c7-4bc7-a87a-bbdf1fe99eba', 'b942cd2c-c743-4cb2-8f67-8e30d4a82d80');
