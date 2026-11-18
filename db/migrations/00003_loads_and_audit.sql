-- +goose Up
CREATE TABLE daily_order_counters (
    loading_location_id uuid NOT NULL REFERENCES loading_locations (id),
    business_date date NOT NULL,
    last_number integer NOT NULL CHECK (last_number > 0),
    PRIMARY KEY (loading_location_id, business_date)
);

CREATE TABLE load_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    loading_location_id uuid NOT NULL REFERENCES loading_locations (id),
    business_date date NOT NULL,
    order_number integer NOT NULL CHECK (order_number > 0),
    company_id uuid NOT NULL REFERENCES companies (id),
    driver_id uuid NOT NULL REFERENCES drivers (id),
    driver_license_id uuid,
    tractor_id uuid NOT NULL REFERENCES vehicles (id),
    trailer_id uuid NOT NULL REFERENCES vehicles (id),
    unit_description text,
    notes text,
    created_by uuid NOT NULL REFERENCES users (id),
    status text NOT NULL DEFAULT 'REGISTERED' CHECK (status IN ('REGISTERED', 'CANCELLED')),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (loading_location_id, business_date, order_number),
    FOREIGN KEY (driver_id, driver_license_id) REFERENCES driver_licenses (driver_id, id),
    CHECK (tractor_id <> trailer_id)
);

CREATE INDEX load_records_date_idx ON load_records (business_date, order_number, id);
CREATE INDEX load_records_company_date_idx ON load_records (company_id, business_date);
CREATE INDEX load_records_driver_date_idx ON load_records (driver_id, business_date);
CREATE INDEX load_records_tractor_date_idx ON load_records (tractor_id, business_date);
CREATE INDEX load_records_trailer_date_idx ON load_records (trailer_id, business_date);
CREATE INDEX load_records_creator_date_idx ON load_records (created_by, business_date);

CREATE TABLE load_record_snapshots (
    load_record_id uuid NOT NULL REFERENCES load_records (id),
    version integer NOT NULL CHECK (version > 0),
    fields jsonb NOT NULL CHECK (jsonb_typeof(fields) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (load_record_id, version)
);

CREATE TABLE field_provenance (
    load_record_id uuid NOT NULL,
    version integer NOT NULL,
    field_name text NOT NULL CHECK (btrim(field_name) <> ''),
    source text NOT NULL CHECK (source IN ('MANUAL', 'HISTORY', 'EXTERNAL_API')),
    verification_status text NOT NULL DEFAULT 'UNVERIFIED' CHECK (verification_status IN ('UNVERIFIED', 'VERIFIED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (load_record_id, version, field_name),
    FOREIGN KEY (load_record_id, version) REFERENCES load_record_snapshots (load_record_id, version)
);

CREATE TABLE audit_logs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid REFERENCES users (id),
    entity_type text NOT NULL,
    entity_id uuid NOT NULL,
    action text NOT NULL,
    request_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_entity_date_idx ON audit_logs (entity_type, entity_id, created_at DESC);
CREATE INDEX audit_logs_date_idx ON audit_logs (created_at DESC, id);

CREATE TABLE external_api_queries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    requested_by uuid REFERENCES users (id),
    provider text NOT NULL,
    lookup_type text NOT NULL CHECK (lookup_type IN ('DNI', 'RUC', 'LICENSE', 'PLATE')),
    outcome text NOT NULL,
    status_code integer CHECK (status_code BETWEEN 100 AND 599),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
    user_id uuid NOT NULL REFERENCES users (id),
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    load_record_id uuid NOT NULL REFERENCES load_records (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, idempotency_key)
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE external_api_queries;
DROP TABLE audit_logs;
DROP TABLE field_provenance;
DROP TABLE load_record_snapshots;
DROP TABLE load_records;
DROP TABLE daily_order_counters;
