# Milestone 3 — Food Search API: Design

Date: 2026-06-20
Status: approved
Parent plan: `docs/plan.md` (milestone 3)

## Goal

A ranked, public `GET /v1/foods?q=` endpoint over the ~8k Foundation + SR Legacy
foods already in Postgres. Search is typo-tolerant relevance search: Postgres
full-text search (tsvector/GIN) drives relevance, pg_trgm fuzzy matching rescues
misspellings, combined into a single ranked result set. A testcontainers
integration suite proves ranking, fuzzy matching, and pagination against a real
Postgres. After this milestone the demo is publicly curlable.

## Decisions made during brainstorming

- **Search UX:** typo-tolerant relevance search (not autocomplete/prefix, not
  exact-keyword). FTS for relevance + trigram for fuzzy — the best showcase of
  both Postgres tools.
- **Ranking (Approach B):** one unified SQL statement. A row matches if the FTS
  predicate hits *or* trigram word-similarity clears a threshold; results are
  ordered by a blended rank (FTS matches first, `ts_rank` within them,
  `word_similarity` for the fuzzy remainder). One round-trip, one code path,
  both indexes exercised on every query. Rejected: two-tier FTS-then-trigram
  fallback (two paths, arbitrary "enough hits" knob); trigram-primary (under-
  sells FTS, worse multi-word relevance).
- **Pagination:** `limit`/`offset`. Default `limit` 20, max 50; `offset` >= 0.
  Standard for relevance-ranked search; offset-depth degradation doesn't bite
  real search usage. Rejected: cursor/keyset (awkward over a relevance score,
  more than this milestone needs) and top-N-only (weaker API-design showcase).
- **Endpoint scope (YAGNI):** `q` + `limit` + `offset` only. No `dataset_source`
  filter, no macro-range filters — added when a real consumer (frontend/solver)
  needs them.
- **numeric → float64 (sqlc override):** add a global `numeric → float64`
  override to `sqlc.yaml`, parallel to the existing `timestamptz → time.Time`.
  Every `numeric` column in the schema is a `NOT NULL` macro we want as a plain
  JSON number, so a global override hits exactly the right columns. Safe because
  ingest carries its own `float64` `FoodRow` and streams via `CopyFrom` — it
  never references the generated `pgtype.Numeric` model fields, which have zero
  consumers today. pgx v5's numeric codec scans `numeric → float64` natively.
  This removes the need for a conversion/service layer (see below). M4's gonum
  solver also wants `float64`, so the macro story gets cleaner everywhere.
  - Lossy and global, but acceptable: every numeric here is a small, bounded,
    `NOT NULL` value; exact decimals still live in Postgres (`numeric(7,2)`),
    only the Go-side read is float64; NaN/overflow are non-issues given the
    `>= 0` CHECK constraints and finite macros.
  - Use bare `numeric` (matching the `timestamptz` convention). If sqlc doesn't
    resolve it, fall back to `pg_catalog.numeric`.
- **No service package:** with sqlc returning clean `float64` rows, there is no
  conversion to isolate, so `internal/foods` is not introduced. The handler
  calls the query and maps `db.SearchFoodsRow` → response DTO directly. The
  testable seam is a narrow `FoodSearcher` interface over `*db.Queries`.
- **Validation status = 422:** huma v2 returns 422 for all input validation
  failures by default (verified in huma v2.38.0). Kept deliberately rather than
  overriding to 400 — uniform status and structured `problem+json` body across
  every validation error (missing `q`, bad `limit`, negative `offset`) beats
  fighting the framework for a debatable REST-semantics win.
- **Score not exposed:** the relevance score stays internal — an implementation
  detail clients shouldn't depend on. Easy to add later if a UI wants it.

## Package layout

```
sqlc.yaml                               # + numeric -> float64 override
internal/db/queries.sql                 # + SearchFoods :many
internal/db/migrations/0004_foods_search.sql  # pg_trgm, tsvector column, GIN indexes
internal/api/foods.go                   # registerSearchFoods: huma op, validation, row -> DTO
internal/api/foods_test.go              # fake FoodSearcher, httptest, no DB
internal/api/foods_integration_test.go  # //go:build integration, real *db.Queries + testcontainers
cmd/api/main.go                         # pass db.New(pool) into api.Config
```

`api` defines the seam:

```go
type FoodSearcher interface {
    SearchFoods(ctx context.Context, arg db.SearchFoodsParams) ([]db.SearchFoodsRow, error)
}
```

satisfied by `*db.Queries`. `api.Config` gains a `Foods FoodSearcher` field. Unit
tests fake it (trivial now: `db.SearchFoodsRow{ProteinG: 22.5, ...}` is plain
floats); the integration test wires the real querier over a testcontainer.

## Schema (goose migration 0004)

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;

ALTER TABLE foods
    ADD COLUMN description_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', description)) STORED;

CREATE INDEX foods_description_tsv_gin  ON foods USING gin (description_tsv);
CREATE INDEX foods_description_trgm_gin ON foods USING gin (description gin_trgm_ops);

-- Lower the word-similarity threshold database-wide so short misspelled queries
-- (e.g. 'chiken' -> 'chicken') clear the `@query <% description` bar; the 0.6
-- default is too strict for short queries. ALTER DATABASE SET applies to every
-- new connection, so API and test pools both pick it up with no per-pool hook.
-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER DATABASE %I SET pg_trgm.word_similarity_threshold = 0.3', current_database());
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    EXECUTE format('ALTER DATABASE %I RESET pg_trgm.word_similarity_threshold', current_database());
END $$;
-- +goose StatementEnd
DROP INDEX foods_description_trgm_gin;
DROP INDEX foods_description_tsv_gin;
ALTER TABLE foods DROP COLUMN description_tsv;
DROP EXTENSION IF EXISTS pg_trgm;
```

Notes:
- The **generated STORED** `description_tsv` column stays in sync automatically;
  ingest's upsert never mentions it, so M2 code is untouched and the column
  backfills for all existing rows on migration.
- Two GIN indexes: `description_tsv` for FTS, `gin_trgm_ops` on raw
  `description` for fuzzy word-similarity.
- sqlc adds `DescriptionTsv` to the generated `Food` model. `tsvector` isn't in
  sqlc's default type map, so it comes out as `interface{}` — cosmetically inert
  since no query selects it. Not worth an override.
- Migration auto-applies at API startup via the existing `db.Migrate`
  (`goose.Up` from `embed.FS`), so compose, CI, and Cloud Run need no separate
  step.

## The query

`SearchFoods :many` in `queries.sql` — Approach B as one statement:

```sql
-- name: SearchFoods :many
SELECT
    fdc_id, description, dataset_source,
    protein_g, carbs_g, fat_g, kcal,
    count(*) OVER() AS total
FROM foods
WHERE description_tsv @@ websearch_to_tsquery('english', @query)
   OR @query <% description
ORDER BY
    (description_tsv @@ websearch_to_tsquery('english', @query)) DESC,  -- FTS matches first
    ts_rank(description_tsv, websearch_to_tsquery('english', @query)) DESC,
    word_similarity(@query, description) DESC,                          -- fuzzy rank within the rest
    fdc_id ASC                                                          -- stable tiebreak
LIMIT @result_limit OFFSET @result_offset;
```

- **`websearch_to_tsquery`** accepts user-friendly input (quotes, `or`, `-term`)
  and never errors on weird input — important for a public endpoint.
- **`@query <% description`** is pg_trgm's `word_similarity(query, description)
  >= word_similarity_threshold` — "does the query appear as a fuzzy word inside
  the description". Index-accelerated by the trigram GIN index; rescues
  misspellings (`chiken` → `chicken`). The database default
  `pg_trgm.word_similarity_threshold` is lowered to 0.3 in migration 0004
  (`ALTER DATABASE`), since the 0.6 default is too strict for short queries.
- **Blended `ORDER BY`:** real FTS hits sort above fuzzy-only hits (boolean
  DESC); `ts_rank` orders within the FTS group; `word_similarity` orders the
  fuzzy remainder; `fdc_id` makes paging deterministic.
- **`count(*) OVER() AS total`** rides along in the same query — no second count
  round-trip. The handler reads it off the first row (0 when there are no
  matches).
- sqlc collapses the repeated `@query` into a single param:
  `SearchFoodsParams{ Query string; ResultLimit, ResultOffset int32 }`.

## API shape

Registered in `api.New` alongside `registerHello`/`registerHealth`, behind the
same logging / request-ID / recover middleware. Operation: `GET /v1/foods`,
`OperationID: "searchFoods"`, `Summary: "Search foods"`, tag `Foods`.

**Request:**

```go
type searchFoodsInput struct {
    Query  string `query:"q" required:"true" minLength:"1" maxLength:"100" example:"chiken breast" doc:"Search text"`
    Limit  int    `query:"limit"  default:"20" minimum:"1" maximum:"50" doc:"Max results per page"`
    Offset int    `query:"offset" default:"0"  minimum:"0" doc:"Results to skip"`
}
```

**Response:**

```go
type foodResult struct {
    FdcID         int64   `json:"fdc_id"         example:"171077"`
    Description   string  `json:"description"    example:"Chicken, broilers or fryers, breast, meat only, raw"`
    DatasetSource string  `json:"dataset_source" example:"sr_legacy_food"`
    ProteinG      float64 `json:"protein_g"      example:"22.5"`
    CarbsG        float64 `json:"carbs_g"        example:"0"`
    FatG          float64 `json:"fat_g"          example:"2.62"`
    Kcal          float64 `json:"kcal"           example:"120"`
}
type searchFoodsOutput struct {
    Body struct {
        Foods  []foodResult `json:"foods"`
        Total  int          `json:"total"  doc:"Total matching foods across all pages"`
        Limit  int          `json:"limit"`
        Offset int          `json:"offset"`
    }
}
```

**Edge cases:**
- Missing/empty `q` → **422** (huma `required` + `minLength:1`).
- `limit > 50`, `limit < 1`, `offset < 0` → **422** (huma validation).
- Valid query, zero matches → **200**, `{"foods": [], "total": 0, "limit": ..., "offset": ...}`.
- DB/query error → **500** (`huma.Error500InternalServerError`), logged.
- `Foods` always serializes as `[]`, never `null` (initialize the slice).

## Testing

**Unit (`internal/api/foods_test.go`, no DB):** fake `FoodSearcher`, table-driven
through the real huma router via `httptest`:
- valid `q` → 200; rows mapped correctly; `total`/`limit`/`offset` echoed; empty
  result serializes as `[]`
- missing `q` → 422; `limit=0`, `limit=99`, `offset=-1` → 422
- searcher returns error → 500
- defaults applied when `limit`/`offset` omitted (assert the params the fake
  received)

**Integration (`internal/api/foods_integration_test.go`, `//go:build integration`,
testcontainers):** reuse the M2 harness (`postgres:18`, `db.Migrate`), seed a
handful of foods via INSERT, drive the real query through the real huma router
against the container:
- exact match ranks the obvious food first ("chicken breast" → breast above thigh)
- **misspelling works:** `chiken` returns chicken rows (proves the trigram path,
  the new extension, and the index)
- multi-word relevance ordering
- pagination: `limit`/`offset` slice correctly; `total` stays the full match
  count across pages
- no-match query → empty, `total: 0`

CI already runs the `integration`-tagged job (added in M2), so these are picked
up automatically.

## Demo / deploy

- `0004` auto-applies on the next Cloud Run deploy via the existing startup
  `db.Migrate`; Neon gets the column, indexes, and `pg_trgm` extension.
- README: add a curl example
  (`curl 'https://api.macros.arrico.me/v1/foods?q=chiken+breast'`) and update the
  status line to note search is live.

## Out of scope

Filters (`dataset_source`, macro ranges), exposing the relevance score, cursor
pagination, autocomplete/prefix search, the solver (M4), auth (M5), Branded
Foods at scale.
