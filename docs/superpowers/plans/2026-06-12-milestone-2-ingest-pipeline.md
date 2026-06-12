# Milestone 2 — USDA Ingest Pipeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `cmd/ingest` loads USDA FoodData Central (Foundation + SR Legacy) into Postgres via staging COPY + idempotent set-based upsert, with an `ingest_runs` audit row, golden-file tests in CI, a scheduled GitHub Action, and Neon as the live target.

**Architecture:** `internal/ingest` splits into `source.go` (URL/zip/dir resolution), `parse.go` (streaming CSV → `[]FoodRow` macro assembly), and `run.go` (single-tx staging COPY + upsert + audit row). `internal/db` owns goose-embedded migrations and sqlc-generated queries. Spec: `docs/superpowers/specs/2026-06-12-milestone-2-ingest-pipeline-design.md`.

**Tech Stack:** goose v3 (embedded migrations), sqlc + pgx v5, testcontainers-go (postgres module), GitHub Actions cron, Neon.

> **Git override (user rule):** Alex runs all git mutations himself. At every "Commit checkpoint" step: `git add` the listed files, show `git status`, suggest the commit message, then STOP and wait for Alex to commit. Never run `git commit` or `git push`.

---

### Task 1: Migrations — goose + internal/db.Migrate

**Files:**
- Create: `internal/db/migrations/0001_foods.sql`
- Create: `internal/db/migrate.go`
- Modify: `cmd/api/main.go` (run migrations at startup)

- [ ] **Step 1: Add dependencies**

```bash
go get github.com/pressly/goose/v3@latest
go get github.com/testcontainers/testcontainers-go@latest
go get github.com/testcontainers/testcontainers-go/modules/postgres@latest
```

- [ ] **Step 2: Write migration 0001**

`internal/db/migrations/0001_foods.sql`:

```sql
-- +goose Up
CREATE TABLE foods (
    fdc_id         bigint PRIMARY KEY,
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
```

All amounts are per 100g (USDA's basis for these datasets). `staging_foods` is UNLOGGED: truncated every run, never needs crash safety.

- [ ] **Step 3: Write migrate.go**

`internal/db/migrate.go`:

```go
// Package db owns schema migrations and generated queries.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate brings the database at dsn up to the latest schema version.
// Both binaries call this at startup, so no environment needs a separate
// migration step.
func Migrate(ctx context.Context, dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open for migrate: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	return goose.UpContext(ctx, sqlDB, "migrations")
}
```

- [ ] **Step 4: Run migrations from cmd/api**

In `cmd/api/main.go`, inside `run()`, after the `DATABASE_URL` check and before `pgxpool.New`, add (plus `fmt` and `github.com/aarrico/gramwise/internal/db` imports):

```go
	if err := db.Migrate(ctx, dsn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
```

- [ ] **Step 5: Build and smoke-check against compose Postgres**

```bash
go build ./... && docker compose up -d postgres && \
DATABASE_URL=postgres://gramwise:gramwise@localhost:5432/gramwise timeout 5 go run ./cmd/api; \
docker compose exec postgres psql -U gramwise -c '\dt'
```

Expected: api starts (timeout kills it after 5s — that's the success path), `\dt` lists `foods`, `staging_foods`, `ingest_runs`, `goose_db_version`.

- [ ] **Step 6: Commit checkpoint**

`git add internal/db/ cmd/api/main.go go.mod go.sum` — suggest `feat: goose migrations with foods, staging, ingest_runs schema`, stop for Alex.

---

### Task 2: sqlc setup

**Files:**
- Create: `sqlc.yaml`
- Create: `internal/db/queries.sql`
- Generated: `internal/db/db.go`, `internal/db/models.go`, `internal/db/queries.sql.go` (committed)

- [ ] **Step 1: Write sqlc.yaml**

`sqlc.yaml` (repo root):

```yaml
version: "2"
sql:
  - engine: "postgresql"
    queries: "internal/db/queries.sql"
    schema: "internal/db/migrations"
    gen:
      go:
        package: "db"
        out: "internal/db"
        sql_package: "pgx/v5"
        overrides:
          - db_type: "pg_catalog.timestamptz"
            go_type: "time.Time"
```

sqlc understands goose migration files (it ignores `+goose Down` sections), so the migrations directory doubles as the schema source — no drift possible.

- [ ] **Step 2: Write queries.sql**

`internal/db/queries.sql`:

```sql
-- name: InsertIngestRun :exec
INSERT INTO ingest_runs (
    started_at, finished_at, source, datasets,
    rows_staged, rows_upserted, duration_ms
) VALUES ($1, $2, $3, $4, $5, $6, $7);
```

- [ ] **Step 3: Generate and verify**

```bash
go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate && go build ./...
```

Expected: `internal/db/{db,models,queries.sql}.go` appear; build passes. `InsertIngestRunParams` should use `time.Time`, `[]string`, `int32`, `int64`.

- [ ] **Step 4: Commit checkpoint**

`git add sqlc.yaml internal/db/` — suggest `feat: sqlc query layer for ingest_runs`, stop for Alex.

---

### Task 3: Fixtures + streaming CSV parser (TDD)

**Files:**
- Create: `internal/ingest/testdata/fdc_small/food.csv`
- Create: `internal/ingest/testdata/fdc_small/food_nutrient.csv`
- Create: `internal/ingest/parse.go`
- Create: `internal/ingest/source.go` (interface + dir source only; zip/URL in Task 4)
- Test: `internal/ingest/parse_test.go`

- [ ] **Step 1: Write fixture CSVs**

`internal/ingest/testdata/fdc_small/food.csv`:

```csv
"fdc_id","data_type","description","food_category_id","publication_date"
"100001","foundation_food","Butter, salted","1","2026-04-01"
"100002","foundation_food","Egg, whole, raw","1","2026-04-01"
"100003","sr_legacy_food","Chicken, breast, raw","5","2019-04-01"
"100004","sr_legacy_food","Rice, white, cooked","20","2019-04-01"
"100005","branded_food","Protein Bar, chocolate","3","2026-04-01"
"100006","foundation_food","Mystery food, incomplete","1","2026-04-01"
```

`internal/ingest/testdata/fdc_small/food_nutrient.csv`:

```csv
"id","fdc_id","nutrient_id","amount"
"1","100001","1003","0.85"
"2","100001","1004","81.11"
"3","100001","1005","0.06"
"4","100001","1008","717"
"5","100001","1003","0.99"
"6","100002","1003","12.56"
"7","100002","1004","9.51"
"8","100002","1005","0.72"
"9","100002","1008","143"
"10","100003","1003","22.5"
"11","100003","1004","2.5"
"12","100003","1005","0"
"13","100004","1003","2.69"
"14","100004","1004","0.28"
"15","100004","1005","28.17"
"16","100004","1008","130"
"17","100005","1003","20"
"18","100006","1004","5"
"19","100006","1005","10"
"20","100006","1008","100"
"21","100001","1087","24"
```

The fixture exercises every rule: row 5 is a duplicate protein for 100001 (first value wins — USDA repeats nutrients with different derivations); 100003 has no kcal row (computed as `4p + 4c + 9f` = 4·22.5 + 4·0 + 9·2.5 = **112.5** — values chosen to be float-exact); 100005 is `branded_food` (filtered out by dataset); 100006 lacks protein (skipped + counted); row 21 is calcium (non-macro, discarded).

- [ ] **Step 2: Write the failing parse test**

`internal/ingest/parse_test.go`:

```go
package ingest_test

import (
	"testing"

	"github.com/aarrico/gramwise/internal/ingest"
)

var fixtureDatasets = map[string]bool{
	"foundation_food": true,
	"sr_legacy_food":  true,
}

func TestParseFixture(t *testing.T) {
	src, err := ingest.NewSource("testdata/fdc_small")
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	res, err := ingest.Parse(src, fixtureDatasets)
	if err != nil {
		t.Fatal(err)
	}

	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", res.Skipped)
	}
	want := []ingest.FoodRow{
		{FDCID: 100001, Description: "Butter, salted", DatasetSource: "foundation_food", ProteinG: 0.85, CarbsG: 0.06, FatG: 81.11, Kcal: 717},
		{FDCID: 100002, Description: "Egg, whole, raw", DatasetSource: "foundation_food", ProteinG: 12.56, CarbsG: 0.72, FatG: 9.51, Kcal: 143},
		{FDCID: 100003, Description: "Chicken, breast, raw", DatasetSource: "sr_legacy_food", ProteinG: 22.5, CarbsG: 0, FatG: 2.5, Kcal: 112.5},
		{FDCID: 100004, Description: "Rice, white, cooked", DatasetSource: "sr_legacy_food", ProteinG: 2.69, CarbsG: 28.17, FatG: 0.28, Kcal: 130},
	}
	if len(res.Foods) != len(want) {
		t.Fatalf("got %d foods, want %d: %+v", len(res.Foods), len(want), res.Foods)
	}
	for i, w := range want {
		if res.Foods[i] != w {
			t.Errorf("food[%d] = %+v, want %+v", i, res.Foods[i], w)
		}
	}
}
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/ingest/`
Expected: FAIL (compile error — `undefined: ingest.NewSource`, `ingest.Parse`, `ingest.FoodRow`).

- [ ] **Step 4: Write the Source interface + dir source**

`internal/ingest/source.go`:

```go
// Package ingest loads USDA FoodData Central CSV data into Postgres.
package ingest

import (
	"io"
	"os"
	"path/filepath"
)

// Source yields named CSV files ("food.csv", "food_nutrient.csv") from
// wherever the dataset lives: a directory, a zip, or a downloaded URL.
type Source interface {
	Open(name string) (io.ReadCloser, error)
	Name() string // recorded in ingest_runs.source
	Close() error
}

// NewSource resolves arg into a Source. Task 4 extends this to zips and
// https URLs; for now: directory of CSVs.
func NewSource(arg string) (Source, error) {
	return dirSource{dir: arg}, nil
}

type dirSource struct {
	dir string
}

func (s dirSource) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(s.dir, name))
}

func (s dirSource) Name() string { return s.dir }
func (s dirSource) Close() error { return nil }
```

- [ ] **Step 5: Write the parser**

`internal/ingest/parse.go`:

```go
package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// USDA FDC nutrient IDs for the macros gramwise stores.
const (
	nutrientProtein = "1003"
	nutrientFat     = "1004"
	nutrientCarbs   = "1005"
	nutrientKcal    = "1008"
)

// FoodRow is one food's per-100g macro profile, ready for staging.
type FoodRow struct {
	FDCID         int64
	Description   string
	DatasetSource string
	ProteinG      float64
	CarbsG        float64
	FatG          float64
	Kcal          float64
}

type ParseResult struct {
	Foods   []FoodRow
	Skipped int // foods missing protein, fat, or carbs
}

type macros struct {
	protein, fat, carbs, kcal *float64
}

type foodMeta struct {
	description string
	dataset     string
	macros      macros
}

// Parse streams food.csv and food_nutrient.csv from src and assembles
// macro rows for foods whose data_type is in datasets. Missing kcal is
// computed via Atwater (4p + 4c + 9f); missing macronutrients skip the food.
func Parse(src Source, datasets map[string]bool) (*ParseResult, error) {
	foods := map[int64]*foodMeta{}
	if err := parseFoods(src, datasets, foods); err != nil {
		return nil, err
	}
	if err := parseNutrients(src, foods); err != nil {
		return nil, err
	}

	res := &ParseResult{}
	for id, f := range foods {
		m := f.macros
		if m.protein == nil || m.fat == nil || m.carbs == nil {
			res.Skipped++
			continue
		}
		kcal := 4**m.protein + 4**m.carbs + 9**m.fat
		if m.kcal != nil {
			kcal = *m.kcal
		}
		res.Foods = append(res.Foods, FoodRow{
			FDCID:         id,
			Description:   f.description,
			DatasetSource: f.dataset,
			ProteinG:      *m.protein,
			CarbsG:        *m.carbs,
			FatG:          *m.fat,
			Kcal:          kcal,
		})
	}
	sort.Slice(res.Foods, func(i, j int) bool { return res.Foods[i].FDCID < res.Foods[j].FDCID })
	return res, nil
}

func openCSV(src Source, name string) (io.ReadCloser, *csv.Reader, map[string]int, error) {
	rc, err := src.Open(name)
	if err != nil {
		return nil, nil, nil, err
	}
	r := csv.NewReader(rc)
	r.ReuseRecord = true
	header, err := r.Read()
	if err != nil {
		rc.Close()
		return nil, nil, nil, fmt.Errorf("%s: read header: %w", name, err)
	}
	cols := make(map[string]int, len(header))
	for i, h := range header {
		cols[h] = i
	}
	return rc, r, cols, nil
}

func parseFoods(src Source, datasets map[string]bool, foods map[int64]*foodMeta) error {
	rc, r, cols, err := openCSV(src, "food.csv")
	if err != nil {
		return err
	}
	defer rc.Close()

	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("food.csv: %w", err)
		}
		dataType := rec[cols["data_type"]]
		if !datasets[dataType] {
			continue
		}
		id, err := strconv.ParseInt(rec[cols["fdc_id"]], 10, 64)
		if err != nil {
			return fmt.Errorf("food.csv: fdc_id %q: %w", rec[cols["fdc_id"]], err)
		}
		foods[id] = &foodMeta{description: rec[cols["description"]], dataset: dataType}
	}
}

func parseNutrients(src Source, foods map[int64]*foodMeta) error {
	rc, r, cols, err := openCSV(src, "food_nutrient.csv")
	if err != nil {
		return err
	}
	defer rc.Close()

	for {
		rec, err := r.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("food_nutrient.csv: %w", err)
		}
		nutrientID := rec[cols["nutrient_id"]]
		if nutrientID != nutrientProtein && nutrientID != nutrientFat &&
			nutrientID != nutrientCarbs && nutrientID != nutrientKcal {
			continue
		}
		id, err := strconv.ParseInt(rec[cols["fdc_id"]], 10, 64)
		if err != nil {
			continue
		}
		food, ok := foods[id]
		if !ok {
			continue
		}
		raw := rec[cols["amount"]]
		if raw == "" {
			continue
		}
		amount, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("food_nutrient.csv: amount %q for fdc_id %d: %w", raw, id, err)
		}

		var dst **float64
		switch nutrientID {
		case nutrientProtein:
			dst = &food.macros.protein
		case nutrientFat:
			dst = &food.macros.fat
		case nutrientCarbs:
			dst = &food.macros.carbs
		case nutrientKcal:
			dst = &food.macros.kcal
		}
		if *dst == nil { // first value wins across duplicate derivations
			v := amount
			*dst = &v
		}
	}
}
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/ingest/ -v`
Expected: PASS — `TestParseFixture`.

- [ ] **Step 7: Commit checkpoint**

`git add internal/ingest/` — suggest `feat: streaming FDC CSV parser with macro assembly`, stop for Alex.

---

### Task 4: Zip + URL sources (TDD)

**Files:**
- Modify: `internal/ingest/source.go`
- Test: `internal/ingest/source_test.go`

- [ ] **Step 1: Write the failing zip test**

`internal/ingest/source_test.go`:

```go
package ingest_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/aarrico/gramwise/internal/ingest"
)

// Builds a zip mimicking USDA's layout (CSVs nested in a top-level folder)
// from the directory fixture, then parses through it.
func TestZipSource(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "fdc.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"food.csv", "food_nutrient.csv"} {
		data, err := os.ReadFile(filepath.Join("testdata/fdc_small", name))
		if err != nil {
			t.Fatal(err)
		}
		w, err := zw.Create("FoodData_Central_csv/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	src, err := ingest.NewSource(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()

	res, err := ingest.Parse(src, fixtureDatasets)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Foods) != 4 {
		t.Errorf("got %d foods from zip, want 4", len(res.Foods))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ingest/ -run TestZipSource`
Expected: FAIL — `NewSource` returns a dirSource for the .zip path, so `Open("food.csv")` fails with "not a directory"/ENOENT.

- [ ] **Step 3: Extend NewSource with zip and URL handling**

Replace `NewSource` in `internal/ingest/source.go` and append the zip/URL types (imports become: `archive/zip`, `fmt`, `io`, `net/http`, `os`, `path`, `path/filepath`, `strings`):

```go
// NewSource resolves arg: an http(s) URL (downloads the zip to a temp
// file), a .zip path, or a directory of CSVs.
func NewSource(arg string) (Source, error) {
	switch {
	case strings.HasPrefix(arg, "http://"), strings.HasPrefix(arg, "https://"):
		return newURLSource(arg)
	case strings.HasSuffix(arg, ".zip"):
		return newZipSource(arg, arg, false)
	default:
		return dirSource{dir: arg}, nil
	}
}

type zipSource struct {
	rc      *zip.ReadCloser
	name    string
	tmpPath string // non-empty for downloaded zips; removed on Close
}

func newZipSource(zipPath, name string, temp bool) (*zipSource, error) {
	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	s := &zipSource{rc: rc, name: name}
	if temp {
		s.tmpPath = zipPath
	}
	return s, nil
}

func (s *zipSource) Open(name string) (io.ReadCloser, error) {
	for _, f := range s.rc.File {
		if path.Base(f.Name) == name { // FDC zips nest CSVs in a top-level folder
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%s not found in %s", name, s.name)
}

func (s *zipSource) Name() string { return s.name }

func (s *zipSource) Close() error {
	err := s.rc.Close()
	if s.tmpPath != "" {
		os.Remove(s.tmpPath)
	}
	return err
}

func newURLSource(url string) (Source, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp("", "fdc-*.zip")
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return newZipSource(tmp.Name(), url, true)
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ingest/ -v`
Expected: PASS — `TestParseFixture`, `TestZipSource`. (URL path is exercised end-to-end by the Action in Task 8; no unit test — it's `http.Get` + the zip path already covered.)

- [ ] **Step 5: Commit checkpoint**

`git add internal/ingest/` — suggest `feat: zip and URL ingest sources`, stop for Alex.

---

### Task 5: Load + Run with golden-file integration test (TDD)

**Files:**
- Create: `internal/ingest/run.go`
- Create: `internal/ingest/testdata/golden/foods.json`
- Test: `internal/ingest/ingest_test.go`

- [ ] **Step 1: Write the failing golden test**

`internal/ingest/ingest_test.go`:

```go
package ingest_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/ingest"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestIngestGoldenAndIdempotency(t *testing.T) {
	ctx := context.Background()

	pgC, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("gramwise_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pgC)
	if err != nil {
		t.Fatal(err)
	}

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	datasets := []string{"foundation_food", "sr_legacy_food"}
	runOnce := func() *ingest.RunSummary {
		t.Helper()
		src, err := ingest.NewSource("testdata/fdc_small")
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()
		sum, err := ingest.Run(ctx, pool, src, datasets)
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}

	sum := runOnce()
	if sum.RowsStaged != 4 || sum.RowsUpserted != 4 || sum.Skipped != 1 {
		t.Fatalf("first run: staged=%d upserted=%d skipped=%d, want 4/4/1",
			sum.RowsStaged, sum.RowsUpserted, sum.Skipped)
	}

	got := dumpFoods(ctx, t, pool)
	const goldenPath = "testdata/golden/foods.json"
	if *update {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	var want [][]string
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("foods mismatch\n got: %v\nwant: %v", got, want)
	}

	// Idempotency: identical input must change nothing.
	sum2 := runOnce()
	if sum2.RowsUpserted != 0 {
		t.Errorf("second run upserted %d rows, want 0", sum2.RowsUpserted)
	}
	if got2 := dumpFoods(ctx, t, pool); !reflect.DeepEqual(got2, got) {
		t.Errorf("foods table changed on re-run")
	}

	var runs int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM ingest_runs").Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Errorf("ingest_runs count = %d, want 2", runs)
	}
}

func dumpFoods(ctx context.Context, t *testing.T, pool *pgxpool.Pool) [][]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT fdc_id::text, description, dataset_source,
		       protein_g::text, carbs_g::text, fat_g::text, kcal::text
		FROM foods ORDER BY fdc_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		row := make([]string, 7)
		ptrs := make([]any, 7)
		for i := range row {
			ptrs[i] = &row[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
```

- [ ] **Step 2: Write the golden file**

`internal/ingest/testdata/golden/foods.json` (`numeric(7,2)` renders two decimals, `numeric(7,1)` one — hence `"22.50"` and `"717.0"`):

```json
[
  ["100001", "Butter, salted", "foundation_food", "0.85", "0.06", "81.11", "717.0"],
  ["100002", "Egg, whole, raw", "foundation_food", "12.56", "0.72", "9.51", "143.0"],
  ["100003", "Chicken, breast, raw", "sr_legacy_food", "22.50", "0.00", "2.50", "112.5"],
  ["100004", "Rice, white, cooked", "sr_legacy_food", "2.69", "28.17", "0.28", "130.0"]
]
```

(If hand-derived values prove wrong, run once with `-update` and inspect the diff rather than hand-editing.)

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/ingest/ -run TestIngestGolden`
Expected: FAIL (compile error — `undefined: ingest.Run`, `ingest.RunSummary`).

- [ ] **Step 4: Implement Run**

`internal/ingest/run.go`:

```go
package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aarrico/gramwise/internal/db"
)

// The WHERE guard compares data columns only (never updated_at), so
// re-running identical data updates zero rows — that row count is the
// idempotency contract the golden test asserts.
const upsertSQL = `
INSERT INTO foods (fdc_id, description, dataset_source, protein_g, carbs_g, fat_g, kcal)
SELECT fdc_id, description, dataset_source, protein_g, carbs_g, fat_g, kcal
FROM staging_foods
ON CONFLICT (fdc_id) DO UPDATE SET
    description    = excluded.description,
    dataset_source = excluded.dataset_source,
    protein_g      = excluded.protein_g,
    carbs_g        = excluded.carbs_g,
    fat_g          = excluded.fat_g,
    kcal           = excluded.kcal,
    updated_at     = now()
WHERE (foods.description, foods.dataset_source, foods.protein_g,
       foods.carbs_g, foods.fat_g, foods.kcal)
   IS DISTINCT FROM
      (excluded.description, excluded.dataset_source, excluded.protein_g,
       excluded.carbs_g, excluded.fat_g, excluded.kcal)`

type RunSummary struct {
	Source       string
	Datasets     []string
	RowsStaged   int
	RowsUpserted int
	Skipped      int
	Duration     time.Duration
}

// Run executes one full ingest: parse, stage via COPY, set-based upsert,
// audit row. All DB work happens in a single transaction — a failure
// anywhere leaves foods untouched.
func Run(ctx context.Context, pool *pgxpool.Pool, src Source, datasets []string) (*RunSummary, error) {
	started := time.Now().UTC()

	set := make(map[string]bool, len(datasets))
	for _, d := range datasets {
		set[d] = true
	}
	parsed, err := Parse(src, set)
	if err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit

	if _, err := tx.Exec(ctx, "TRUNCATE staging_foods"); err != nil {
		return nil, fmt.Errorf("truncate staging: %w", err)
	}

	staged, err := tx.CopyFrom(ctx,
		pgx.Identifier{"staging_foods"},
		[]string{"fdc_id", "description", "dataset_source", "protein_g", "carbs_g", "fat_g", "kcal"},
		pgx.CopyFromSlice(len(parsed.Foods), func(i int) ([]any, error) {
			f := parsed.Foods[i]
			return []any{f.FDCID, f.Description, f.DatasetSource, f.ProteinG, f.CarbsG, f.FatG, f.Kcal}, nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("copy to staging: %w", err)
	}

	tag, err := tx.Exec(ctx, upsertSQL)
	if err != nil {
		return nil, fmt.Errorf("upsert: %w", err)
	}

	finished := time.Now().UTC()
	sum := &RunSummary{
		Source:       src.Name(),
		Datasets:     datasets,
		RowsStaged:   int(staged),
		RowsUpserted: int(tag.RowsAffected()),
		Skipped:      parsed.Skipped,
		Duration:     finished.Sub(started),
	}

	err = db.New(tx).InsertIngestRun(ctx, db.InsertIngestRunParams{
		StartedAt:    started,
		FinishedAt:   finished,
		Source:       sum.Source,
		Datasets:     sum.Datasets,
		RowsStaged:   int32(sum.RowsStaged),
		RowsUpserted: int32(sum.RowsUpserted),
		DurationMs:   sum.Duration.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("record ingest run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return sum, nil
}
```

(If sqlc generated different field names in `InsertIngestRunParams`, match those — check `internal/db/queries.sql.go`.)

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/ingest/ -v` (needs Docker running)
Expected: PASS — all three tests; golden test takes a few seconds for the container.

- [ ] **Step 6: Full local gate**

Run: `test -z "$(gofmt -l .)" && go vet ./... && go test ./... && go build ./... && echo ALL-GREEN`
Expected: `ALL-GREEN`.

- [ ] **Step 7: Commit checkpoint**

`git add internal/ingest/` — suggest `feat: staged COPY ingest with idempotent upsert and golden-file tests`, stop for Alex.

---

### Task 6: cmd/ingest entrypoint

**Files:**
- Create: `cmd/ingest/main.go`

- [ ] **Step 1: Write main.go**

`cmd/ingest/main.go`:

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aarrico/gramwise/internal/db"
	"github.com/aarrico/gramwise/internal/ingest"
)

var datasetNames = map[string]string{
	"foundation": "foundation_food",
	"sr_legacy":  "sr_legacy_food",
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("ingest failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	source := flag.String("source", "", "https URL, .zip path, or directory of FDC CSVs")
	datasetsFlag := flag.String("datasets", "foundation,sr_legacy", "comma-separated: foundation, sr_legacy")
	flag.Parse()

	if *source == "" {
		return errors.New("--source is required")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	var datasets []string
	for _, short := range strings.Split(*datasetsFlag, ",") {
		dt, ok := datasetNames[strings.TrimSpace(short)]
		if !ok {
			return fmt.Errorf("unknown dataset %q (want: foundation, sr_legacy)", short)
		}
		datasets = append(datasets, dt)
	}

	ctx := context.Background()

	if err := db.Migrate(ctx, dsn); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	src, err := ingest.NewSource(*source)
	if err != nil {
		return err
	}
	defer src.Close()

	logger.Info("ingest starting", "source", src.Name(), "datasets", datasets)
	sum, err := ingest.Run(ctx, pool, src, datasets)
	if err != nil {
		return err
	}
	logger.Info("ingest finished",
		"rows_staged", sum.RowsStaged,
		"rows_upserted", sum.RowsUpserted,
		"skipped", sum.Skipped,
		"duration_ms", sum.Duration.Milliseconds(),
	)
	return nil
}
```

- [ ] **Step 2: End-to-end against compose Postgres**

```bash
docker compose up -d postgres
DATABASE_URL=postgres://gramwise:gramwise@localhost:5432/gramwise \
  go run ./cmd/ingest --source internal/ingest/testdata/fdc_small
docker compose exec postgres psql -U gramwise -c \
  'SELECT count(*) FROM foods; SELECT rows_staged, rows_upserted FROM ingest_runs ORDER BY id'
```

Expected: JSON log lines `ingest starting` / `ingest finished` with `rows_staged=4`; psql shows 4 foods. Run the same `go run` command a second time — the new `ingest_runs` row must show `rows_upserted = 0`.

- [ ] **Step 3: Commit checkpoint**

`git add cmd/ingest/` — suggest `feat: ingest CLI with source and dataset flags`, stop for Alex.

---

### Task 7: Scheduled ingest workflow

**Files:**
- Create: `.github/workflows/ingest.yml`

- [ ] **Step 1: Confirm current USDA zip URLs**

Open <https://fdc.nal.usda.gov/download-datasets> and copy the current per-dataset CSV zip URLs for **Foundation Foods** and **SR Legacy** (SR Legacy's last release is April 2018; Foundation updates roughly twice a year). The URLs below were current at planning time — verify before writing the file. Per-dataset zips are used instead of the full FDC zip (which includes Branded Foods and is multi-GB).

- [ ] **Step 2: Write the workflow**

`.github/workflows/ingest.yml`:

```yaml
name: ingest

on:
  workflow_dispatch:
    inputs:
      foundation_url:
        description: Foundation Foods CSV zip URL (defaults to pinned)
        required: false
      sr_legacy_url:
        description: SR Legacy CSV zip URL (defaults to pinned)
        required: false
  schedule:
    - cron: "17 6 1 * *" # monthly; picks up Foundation refreshes

env:
  FOUNDATION_URL: ${{ inputs.foundation_url || 'https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_foundation_food_csv_2025-04-24.zip' }}
  SR_LEGACY_URL: ${{ inputs.sr_legacy_url || 'https://fdc.nal.usda.gov/fdc-datasets/FoodData_Central_sr_legacy_food_csv_2018-04.zip' }}

jobs:
  ingest:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: ingest foundation foods
        run: go run ./cmd/ingest --source "$FOUNDATION_URL" --datasets foundation
        env:
          DATABASE_URL: ${{ secrets.DATABASE_URL }}
      - name: ingest sr legacy
        run: go run ./cmd/ingest --source "$SR_LEGACY_URL" --datasets sr_legacy
        env:
          DATABASE_URL: ${{ secrets.DATABASE_URL }}
```

A new USDA release = dispatch with the new URL (or bump the pinned default). Each run lands its own `ingest_runs` row, so releases are auditable in the DB.

- [ ] **Step 3: Commit checkpoint**

`git add .github/workflows/ingest.yml` — suggest `ci: scheduled USDA ingest workflow`, stop for Alex.

---

### Task 8: Neon setup + first cloud ingest (user-driven)

No repo files — one-time infrastructure. Each step is a STOP for Alex.

- [ ] **Step 1: Create the Neon project** (Alex, in browser)

console.neon.tech → new project: name `gramwise`, Postgres **18**, region `aws-us-east-2` (near future Cloud Run region). **Neon Auth stays off.** Copy the **pooled** connection string (the one with `-pooler` in the host).

- [ ] **Step 2: Set the repo secret** (Alex, locally — keeps the secret out of the agent transcript)

```bash
gh secret set DATABASE_URL --repo aarrico/gramwise
```

Paste the pooled connection string at the prompt (ensure it includes `?sslmode=require`).

- [ ] **Step 3: Push everything, then dispatch**

After all commits are pushed and `ci` is green:

```bash
gh workflow run ingest --repo aarrico/gramwise
gh run watch --repo aarrico/gramwise
```

Expected: both ingest steps log `ingest finished` with `rows_staged` ≈ a few hundred (foundation) and ≈ 7,800 (sr_legacy).

- [ ] **Step 4: Verify in Neon** (Alex, with psql against the Neon string)

```sql
SELECT dataset_source, count(*) FROM foods GROUP BY 1;
SELECT source, rows_staged, rows_upserted, duration_ms FROM ingest_runs ORDER BY id;
```

Expected: two dataset_source groups; two ingest_runs rows. Optionally dispatch the workflow once more and confirm the new rows show `rows_upserted = 0` — idempotency against real data in the cloud.

---

### Task 9: Final verification

- [ ] **Step 1: Full local gate**

Run: `test -z "$(gofmt -l .)" && go vet ./... && go test ./... && go build ./... && echo ALL-GREEN`
Expected: `ALL-GREEN` (includes the testcontainers golden test; Docker must be running).

- [ ] **Step 2: CI green**

After Alex pushes: `gh run list --repo aarrico/gramwise --limit 2`
Expected: latest `ci` run `completed success` — the golden-file integration test runs in CI too (ubuntu runners have Docker).

- [ ] **Step 3: Milestone close**

Confirm with Alex: Neon populated, scheduled workflow live, `docs/plan.md` milestone 2 deliverables all present. Search API (M3) inherits the testcontainers harness and real data.
