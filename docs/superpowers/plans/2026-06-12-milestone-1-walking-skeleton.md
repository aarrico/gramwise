# Milestone 1 — Walking Skeleton Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deployable Go skeleton for gramwise — scaffold, green CI, multi-stage Dockerfile, compose dev environment, hello-world huma endpoint, and a DB-pinging health check.

**Architecture:** Single binary `cmd/api`; `main.go` owns process concerns (env config, slog JSON logger, pgxpool, graceful shutdown), `internal/api` owns HTTP concerns (huma on stdlib `http.ServeMux` via humago, hand-rolled request-ID/logging/recovery middleware, `/v1/hello` and `/healthz` operations). Spec: `docs/superpowers/specs/2026-06-12-milestone-1-walking-skeleton-design.md`.

**Tech Stack:** Go 1.25+, huma v2 (humago adapter), pgx v5 (pgxpool), Postgres 17, distroless Docker image, GitHub Actions + golangci-lint.

> **Git override (user rule):** Alex runs all git mutations himself. At every "Commit checkpoint" step: `git add` the listed files, show `git status`, suggest the commit message, then STOP and wait for Alex to commit. Never run `git commit` or `git push`.

---

### Task 1: Module scaffold

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `web/README.md`
- Create: `.gitignore`

- [ ] **Step 1: Init module**

Run: `go mod init github.com/aarrico/gramwise`
Expected: creates `go.mod` with `module github.com/aarrico/gramwise`.

- [ ] **Step 2: Create web placeholder**

`web/README.md`:

```markdown
# web

Reserved for the milestone-6 frontend: Vite + React SPA with a TypeScript client
generated from the API's OpenAPI spec. Intentionally empty until then.
```

- [ ] **Step 3: Create .gitignore**

`.gitignore`:

```
/bin/
*.env
```

- [ ] **Step 4: Commit checkpoint**

`git add go.mod web/README.md .gitignore docs/superpowers/` — suggest message `chore: init go module and scaffold`, stop for Alex.

---

### Task 2: API package — hello + healthz (TDD)

**Files:**
- Create: `internal/api/api.go`
- Test: `internal/api/api_test.go`

- [ ] **Step 1: Add dependencies**

Run:
```bash
go get github.com/danielgtaylor/huma/v2@latest
go get github.com/jackc/pgx/v5@latest
```
Expected: `go.mod` gains both; `go.sum` created. (pgx is unused until Task 4 but fetched once here.)

- [ ] **Step 2: Write the failing tests**

`internal/api/api_test.go`:

```go
package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aarrico/gramwise/internal/api"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func newTestHandler(t *testing.T, db api.Pinger) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return api.New(api.Config{Logger: logger, DB: db})
}

func TestHello(t *testing.T) {
	h := newTestHandler(t, fakePinger{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/hello", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if want := "hello, world"; body.Message != want {
		t.Errorf("message = %q, want %q", body.Message, want)
	}
}

func TestHealthz(t *testing.T) {
	tests := []struct {
		name string
		db   api.Pinger
		want int
	}{
		{"db up", fakePinger{}, http.StatusOK},
		{"db down", fakePinger{err: errors.New("connection refused")}, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t, tt.db)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/api/`
Expected: FAIL (compile error — `undefined: api.New`, `api.Pinger`, `api.Config`).

- [ ] **Step 4: Implement the API package**

`internal/api/api.go`:

```go
// Package api wires the gramwise HTTP API: huma operations on a stdlib mux.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// Pinger reports whether the backing database is reachable.
// *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

type Config struct {
	Logger *slog.Logger
	DB     Pinger
}

func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	humaAPI := humago.New(mux, huma.DefaultConfig("Gramwise API", "0.1.0"))

	registerHello(humaAPI)
	registerHealthz(humaAPI, cfg.DB)

	var h http.Handler = mux
	h = logging(cfg.Logger, h)
	h = requestID(h)
	h = recoverPanics(cfg.Logger, h)
	return h
}

type helloOutput struct {
	Body struct {
		Message string `json:"message" example:"hello, world" doc:"A friendly greeting"`
	}
}

func registerHello(humaAPI huma.API) {
	huma.Register(humaAPI, huma.Operation{
		OperationID: "hello",
		Method:      http.MethodGet,
		Path:        "/v1/hello",
		Summary:     "Hello world",
	}, func(_ context.Context, _ *struct{}) (*helloOutput, error) {
		out := &helloOutput{}
		out.Body.Message = "hello, world"
		return out, nil
	})
}

type healthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok"`
	}
}

func registerHealthz(humaAPI huma.API, db Pinger) {
	huma.Register(humaAPI, huma.Operation{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Summary:     "Health check, pings the database",
	}, func(ctx context.Context, _ *struct{}) (*healthOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			return nil, huma.Error503ServiceUnavailable("database unreachable", err)
		}
		out := &healthOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}
```

Note: this references `logging`, `requestID`, `recoverPanics` from Task 3. To keep Task 2 green on its own, create `internal/api/middleware.go` with the Task 3 implementation now if executing tasks out of order — otherwise proceed to Task 3 before running tests. (Recommended execution: write both files, Tasks 2 and 3 are one commit checkpoint.)

- [ ] **Step 5: Continue to Task 3 (same checkpoint)**

---

### Task 3: Middleware — request ID, logging, recovery

**Files:**
- Create: `internal/api/middleware.go`
- Test: extend `internal/api/api_test.go`

- [ ] **Step 1: Add failing middleware assertions**

Append to `TestHello` in `internal/api/api_test.go` (after the message assertion):

```go
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("missing X-Request-Id response header")
	}
```

And add a new test:

```go
func TestRequestIDPassthrough(t *testing.T) {
	h := newTestHandler(t, fakePinger{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/hello", nil)
	req.Header.Set("X-Request-Id", "abc123")
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-Id"); got != "abc123" {
		t.Errorf("X-Request-Id = %q, want %q", got, "abc123")
	}
}
```

- [ ] **Step 2: Implement middleware**

`internal/api/middleware.go`:

```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey string

const requestIDKey ctxKey = "request_id"

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", w.Header().Get("X-Request-Id"),
		)
	})
}

func recoverPanics(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic recovered", "value", v, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

Middleware order in `New` (innermost first): `logging` → `requestID` → `recoverPanics`, i.e. requests flow recover → request-ID → logging → mux, so the logger always sees the `X-Request-Id` header already set.

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./internal/api/ -v`
Expected: PASS — `TestHello`, `TestHealthz/db_up`, `TestHealthz/db_down`, `TestRequestIDPassthrough`.

- [ ] **Step 4: Vet and format**

Run: `gofmt -l . && go vet ./...`
Expected: no output from gofmt, vet clean.

- [ ] **Step 5: Commit checkpoint**

`git add go.mod go.sum internal/` — suggest message `feat: huma API with hello, healthz, and request middleware`, stop for Alex.

---

### Task 4: `cmd/api` entrypoint

**Files:**
- Create: `cmd/api/main.go`

- [ ] **Step 1: Write main.go**

`cmd/api/main.go`:

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aarrico/gramwise/internal/api"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api exited", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           api.New(api.Config{Logger: logger, DB: pool}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "port", port)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
```

(`pgxpool.New` does not dial eagerly — startup succeeds without a reachable DB; `/healthz` is what proves connectivity.)

- [ ] **Step 2: Build and verify config validation**

Run: `go build ./... && go run ./cmd/api`
Expected: build succeeds; run exits 1 with JSON log `"DATABASE_URL is required"`. Live verification happens in Task 5 via compose.

- [ ] **Step 3: Commit checkpoint**

`git add cmd/` — suggest message `feat: api entrypoint with env config and graceful shutdown`, stop for Alex.

---

### Task 5: Dockerfile + compose

**Files:**
- Create: `Dockerfile`
- Create: `compose.yaml`

- [ ] **Step 1: Write Dockerfile**

`Dockerfile`:

```dockerfile
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
```

(`:nonroot` tag runs as the distroless nonroot user; no `USER` line needed.)

- [ ] **Step 2: Write compose.yaml**

`compose.yaml`:

```yaml
services:
  postgres:
    image: postgres:17
    environment:
      POSTGRES_USER: gramwise
      POSTGRES_PASSWORD: gramwise
      POSTGRES_DB: gramwise
    ports:
      - "5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U gramwise"]
      interval: 2s
      timeout: 2s
      retries: 15
    volumes:
      - pgdata:/var/lib/postgresql/data

  api:
    build: .
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://gramwise:gramwise@postgres:5432/gramwise
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  pgdata:
```

- [ ] **Step 3: Build and run the stack**

Run: `docker compose up --build -d`
Expected: postgres becomes healthy, api starts. If `docker` is unavailable in this shell, ask Alex to run it via `! docker compose up --build -d`.

- [ ] **Step 4: Verify endpoints**

Run:
```bash
curl -s -i http://localhost:8080/healthz
curl -s http://localhost:8080/v1/hello
curl -s -o /dev/null -w '%{http_code}' http://localhost:8080/docs
```
Expected: healthz `200` with `{"status":"ok"}` (note the `X-Request-Id` header); hello returns `{"message":"hello, world"}` (huma wraps with `$schema` — fine); `/docs` returns `200`.

- [ ] **Step 5: Verify image size and teardown**

Run: `docker image ls | head -5`, then `docker compose down`
Expected: api image in the tens of MB (distroless + static binary).

- [ ] **Step 6: Commit checkpoint**

`git add Dockerfile compose.yaml` — suggest message `feat: distroless Dockerfile and compose dev stack`, stop for Alex.

---

### Task 6: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write workflow**

`.github/workflows/ci.yml`:

```yaml
name: ci

on:
  push:
    branches: [main]
  pull_request:

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        run: test -z "$(gofmt -l .)"
      - name: vet
        run: go vet ./...
      - name: lint
        uses: golangci/golangci-lint-action@v8
        with:
          version: latest
      - name: test
        run: go test ./...
      - name: build
        run: go build ./...
      - name: docker build
        run: docker build -t gramwise-api .
```

- [ ] **Step 2: Run the same checks locally**

Run: `test -z "$(gofmt -l .)" && go vet ./... && go test ./... && go build ./... && echo LOCAL-CI-OK`
Expected: `LOCAL-CI-OK`. If `golangci-lint` is installed locally, also run `golangci-lint run ./...`; otherwise CI is the first lint run — check it after Alex pushes.

- [ ] **Step 3: Commit checkpoint**

`git add .github/` — suggest message `ci: fmt, vet, lint, test, build, docker build`, stop for Alex.

---

### Task 7: ADRs + README

**Files:**
- Create: `docs/adr/0001-modular-monolith.md`
- Create: `docs/adr/0002-openapi-first-rest-with-huma.md`
- Create: `README.md`

- [ ] **Step 1: Write ADR 0001**

`docs/adr/0001-modular-monolith.md`:

```markdown
# 0001 — Modular monolith over microservices

Status: accepted · Date: 2026-06-12

## Context

Gramwise is a solo-built portfolio backend with a handful of domains (foods,
solver, auth) and a $0/month hosting budget. Microservices would add network
boundaries, deployment units, and operational cost without a team or scale
requirement to justify them.

## Decision

One Go module, one repository, multiple binaries (`cmd/api`, later
`cmd/ingest`) sharing `internal/` packages with enforced boundaries. Domains
are separated by package, not by network.

## Consequences

- Single deploy target (Cloud Run service) fits the free tier.
- Refactoring across domains is a compile-time operation, not an API
  migration.
- The `cmd/` split keeps batch ingest out of the API's runtime without
  inventing a second service.
- If a domain ever needs independent scaling, package boundaries are the
  extraction seams.
```

- [ ] **Step 2: Write ADR 0002**

`docs/adr/0002-openapi-first-rest-with-huma.md`:

```markdown
# 0002 — OpenAPI-first REST via huma

Status: accepted · Date: 2026-06-12

## Context

The API needs machine-readable contracts: milestone 6 generates the
frontend's TypeScript client from a spec, and a documented API is part of the
portfolio pitch. Options: hand-written OpenAPI (drifts), ConnectRPC/protobuf
(binary contracts, weaker browser/curl ergonomics for a public demo API), or
generating the spec from Go code.

## Decision

huma v2 mounted on the stdlib `http.ServeMux` (humago adapter). Go types are
the source of truth; huma derives the OpenAPI spec, serves `/docs`, and
validates requests. No third-party router: stdlib routing is sufficient and
keeps the dependency surface minimal.

## Consequences

- Spec can never drift from the implementation.
- `/docs` is a free, always-current demo surface for reviewers.
- Request/response validation comes from the same type definitions.
- Tied to huma's operation model; acceptable for a single-team API.
```

- [ ] **Step 3: Write README**

`README.md`:

````markdown
# gramwise

Inverse macro calculator: instead of "what's in 150g of chicken?", gramwise
answers **"how many grams of chicken to hit 40g of protein?"** — generalized
to multiple foods at once via goal programming.

Go backend showcase project. Plan and architecture: [`docs/plan.md`](docs/plan.md) ·
decisions: [`docs/adr/`](docs/adr/).

## Quickstart

```sh
docker compose up --build
```

- API: <http://localhost:8080/v1/hello>
- Docs (OpenAPI): <http://localhost:8080/docs>
- Health: <http://localhost:8080/healthz>

## Development

```sh
docker compose up -d postgres
DATABASE_URL=postgres://gramwise:gramwise@localhost:5432/gramwise go run ./cmd/api
go test ./...
```

## Status

Milestone 1 (walking skeleton) — see [`docs/plan.md`](docs/plan.md) for the
roadmap.
````

- [ ] **Step 4: Commit checkpoint**

`git add docs/adr/ README.md` — suggest message `docs: ADRs 0001-0002 and README`, stop for Alex.

---

### Task 8: Final verification

- [ ] **Step 1: Full local gate**

Run: `test -z "$(gofmt -l .)" && go vet ./... && go test ./... && go build ./... && echo ALL-GREEN`
Expected: `ALL-GREEN`.

- [ ] **Step 2: Compose smoke test once more**

Run: `docker compose up --build -d && sleep 3 && curl -sf http://localhost:8080/healthz && docker compose down`
Expected: `{"status":"ok",...}` printed, clean teardown.

- [ ] **Step 3: Hand off for push**

Stop: Alex pushes to GitHub, confirms the `ci` workflow is green on `main`. Milestone 1 is done (deploy to Cloud Run + Neon is a separate follow-up session per the spec).
