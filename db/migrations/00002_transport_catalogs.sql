-- +goose Up
CREATE TABLE companies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ruc text NOT NULL UNIQUE CHECK (ruc ~ '^[0-9]{11}$'),
    legal_name text NOT NULL CHECK (btrim(legal_name) <> ''),
    tax_status text,
    tax_condition text,
    fiscal_address text,
    is_active boolean NOT NULL DEFAULT true,
    last_external_lookup_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE drivers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    document_type text NOT NULL CHECK (document_type <> '' AND document_type = upper(btrim(document_type))),
    document_number text NOT NULL CHECK (document_number <> '' AND document_number = upper(btrim(document_number))),
    first_names text NOT NULL CHECK (btrim(first_names) <> ''),
    last_names text NOT NULL CHECK (btrim(last_names) <> ''),
    phone text,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (document_type, document_number)
);

CREATE TABLE driver_licenses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    driver_id uuid NOT NULL REFERENCES drivers (id),
    license_number text NOT NULL CHECK (license_number <> '' AND license_number = upper(btrim(license_number))),
    license_class text,
    category text,
    expires_on date,
    issuing_country text CHECK (issuing_country ~ '^[A-Z]{2}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (driver_id, id),
    UNIQUE NULLS NOT DISTINCT (driver_id, license_number, issuing_country)
);

CREATE TABLE vehicles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    plate text NOT NULL UNIQUE CHECK (plate ~ '^[A-Z0-9]+$'),
    vehicle_type text NOT NULL CHECK (vehicle_type IN ('TRACTOR', 'TRAILER', 'SEMITRAILER')),
    make text,
    model text,
    is_active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE vehicles;
DROP TABLE driver_licenses;
DROP TABLE drivers;
DROP TABLE companies;
