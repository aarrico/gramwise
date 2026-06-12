-- +goose Up
CREATE TABLE foods (
    fdc_id bigint PRIMARY KEY,
    description    text NOT NULL,
    dataset_source text NOT NULL,
    protein_g      numeric(7,2) NOT NULL,
    carbs_g        numeric(7,2) NOT NULL,
    fat_g          numeric(7,2) NOT NULL,
    kcal           numeric(7,1) NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE UNLOGGED TABLE staging_foods (
    fdc_id         bigint NOT NULL,
    description    text NOT NULL,
    dataset_source text NOT NULL,
    protein_g      numeric(7,2) NOT NULL,
    carbs_g        numeric(7,2) NOT NULL,
    fat_g          numeric(7,2) NOT NULL,
    kcal           numeric(7,1) NOT NULL
);

CREATE TABLE ingest_runs (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    started_at    timestamptz NOT NULL,
    finished_at   timestamptz NOT NULL,
    source        text NOT NULL,
    datasets      text[] NOT NULL,
    rows_staged   int NOT NULL,
    rows_upserted int NOT NULL,
    duration_ms   bigint NOT NULL
);

-- +goose Down
DROP TABLE ingest_runs;
DROP TABLE staging_foods;
DROP TABLE foods;