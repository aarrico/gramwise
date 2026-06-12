# Milestone 2 — USDA Ingest Pipeline: Design

Date: 2026-06-12
Status: approved
Parent plan: `docs/plan.md` (milestone 2)

## Goal

`cmd/ingest` loads USDA FoodData Central data (Foundation Foods + SR Legacy) into Postgres: streamed CSV parsing, staging table via `pgx.CopyFrom`, one set-based idempotent upsert keyed on FDC ID, an `ingest_runs` audit row per execution. Golden-file tests prove exact DB state and idempotency in CI. A scheduled GitHub Action runs the binary against Neon, which gets set up this milestone.

## Decisions made during brainstorming

- **Neon:** in scope — create the project, `DATABASE_URL` becomes a repo secret, scheduled Action targets it. Neon Auth stays off (auth is hand-rolled in M5 by design).
- **Schema depth:** macros only — `foods` carries `protein_g`, `carbs_g`, `fat_g`, `kcal` per 100g as columns. No EAV nutrient tables; raw CSVs remain the source if a future feature needs micronutrients.
- **Source input:** `--source` accepts a local zip/directory or an https URL (the Action downloads the official FDC zip through the same code path).
- **Pipeline shape (Approach A):** staging table + set-based upsert, not row-by-row batches — preserves the COPY performance story for the future Branded-Foods 250x milestone.
- **Plan deviation, accepted:** testcontainers-go arrives now (golden-file DB-state tests need a real disposable Postgres); M3 extends this harness rather than introducing it.

## Schema (goose migration 0001)

```sql
CREATE TABLE foods (
    fdc_id         bigint PRIMARY KEY,
    description    text NOT NULL,
    dataset_source text NOT NULL,          -- 'foundation_food' | 'sr_legacy_food' (later 'branded_food')
    protein_g      numeric(7,2) NOT NULL,
    carbs_g        numeric(7,2) NOT NULL,
    fat_g          numeric(7,2) NOT NULL,
    kcal           numeric(7,1) NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE staging_foods (
    LIKE foods INCLUDING ALL EXCLUDING INDEXES
);  -- unlogged, truncated at the start of every run

CREATE TABLE ingest_runs (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    started_at      timestamptz NOT NULL,
    finished_at     timestamptz NOT NULL,
    source          text NOT NULL,          -- URL or path the run consumed
    datasets        text[] NOT NULL,
    rows_staged     int NOT NULL,
    rows_upserted   int NOT NULL,           -- rows actually inserted or changed
    duration_ms     bigint NOT NULL
);
```

Notes: `staging_foods` is `UNLOGGED` (created explicitly, not via LIKE, in the actual migration — LIKE shown for intent). All amounts are per 100g, USDA's basis for Foundation/SR Legacy. Migrations run via goose with `embed.FS` from both binaries at startup (`goose.Up`), so compose, CI, and the Action need no separate migration step.

## CSV → macros mapping

From the FDC zip the pipeline reads:

- `food.csv` → `fdc_id`, `data_type` (filter to `foundation_food`, `sr_legacy_food`), `description`
- `food_nutrient.csv` → `fdc_id`, `nutrient_id`, `amount`
- Macro nutrient numbers (fixed USDA nutrient IDs): protein **1003**, total fat **1004**, carbohydrate by difference **1005**, energy kcal **1008**

Foods missing any of the three macronutrients are skipped and counted (logged in the run summary); a missing 1008 is computed as `4p + 4c + 9f`. Parsing is streaming (`encoding/csv` over the zip entries) — no full-file slurps; `food_nutrient.csv` rows for non-macro nutrients are discarded as they stream by.

## Pipeline flow (one run)

1. Resolve `--source` (https URL → download to temp file; path → open zip or directory).
2. Stream-parse, assemble per-food macro rows in a map keyed by fdc_id (Foundation + SR Legacy ≈ 8k foods — trivially fits memory; Branded later may revisit).
3. Begin tx: `TRUNCATE staging_foods`; `pgx.CopyFrom` all rows into staging.
4. Set-based upsert in the same tx:
   `INSERT INTO foods SELECT ... FROM staging_foods ON CONFLICT (fdc_id) DO UPDATE SET ... WHERE (foods.description, foods.dataset_source, foods.protein_g, foods.carbs_g, foods.fat_g, foods.kcal) IS DISTINCT FROM (excluded.description, excluded.dataset_source, excluded.protein_g, excluded.carbs_g, excluded.fat_g, excluded.kcal)` — the guard compares **data columns only**, never `updated_at`, so re-running identical data touches zero rows (what the idempotency test asserts via the statement's row count). `updated_at` is set to `now()` only inside the `DO UPDATE SET`, i.e. only when data actually changed.
5. Insert `ingest_runs` row; commit. Failure anywhere rolls back the whole run — `foods` is never partially updated.
6. Log JSON summary via slog: datasets, staged, upserted, skipped, duration.

## Package layout

```
cmd/ingest/main.go          # flags, config, pool, calls internal/ingest.Run
internal/ingest/source.go   # --source resolution: URL download / zip / dir → io.Readers per CSV
internal/ingest/parse.go    # streaming CSV → []FoodRow (macro assembly, skip counting)
internal/ingest/load.go     # staging COPY + upsert + ingest_runs (the only DB-aware file)
internal/db/                # goose embed.FS migrations + sqlc-generated queries
```

sqlc handles `ingest_runs` insert and simple lookups; `CopyFrom` and the upsert statement stay hand-written pgx in `load.go` (sqlc doesn't model COPY).

## Testing

- **Parse unit tests:** table-driven over small in-repo fixture CSVs (handful of foods incl. a missing-macro food and a missing-kcal food).
- **Golden-file integration tests (testcontainers-go):** run the full pipeline against a disposable Postgres from a fixture zip; assert exact `foods` rows against a golden snapshot and the `ingest_runs` row shape. Then **re-run the same fixture** and assert `rows_upserted = 0` and `foods` unchanged — idempotency proven in CI.
- CI gains the integration job (docker available on ubuntu-latest runners).

## Scheduled GitHub Action

`.github/workflows/ingest.yml`: `workflow_dispatch` + monthly cron. Steps: checkout, setup-go, `go run ./cmd/ingest --source <official FDC CSV zip URL> --datasets foundation,sr_legacy` with `DATABASE_URL` from repo secret (Neon). USDA publishes versioned zips; the URL lives in the workflow as a pinned, updatable input with the latest release as default. A new USDA release = bump the default or dispatch with the new URL; `ingest_runs` records each as a distinct run.

## Neon setup (one-time, manual, this milestone)

Create Neon project (Postgres 18, region near Cloud Run's eventual region), no Neon Auth. Save the pooled connection string as repo secret `DATABASE_URL`. First ingest run via `workflow_dispatch` populates it; verified by querying `ingest_runs` and `count(*) from foods`.

## Out of scope

Branded Foods, Open Food Facts, search endpoints (M3), any API changes, full nutrient storage, Cloud Run deploy.
