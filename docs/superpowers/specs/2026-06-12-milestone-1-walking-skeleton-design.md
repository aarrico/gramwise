# Milestone 1 — Walking Skeleton: Design

Date: 2026-06-12
Status: approved
Parent plan: `docs/plan.md` (milestone 1)

## Goal

A deployable Go skeleton for gramwise: scaffold, green CI, multi-stage Dockerfile, compose-based local dev, and a hello-world huma endpoint plus a DB-pinging health check. **Deployment to Cloud Run + Neon is deferred to a follow-up session** — this milestone ends with the app running locally via `docker compose up` and CI green on GitHub.

## Decisions made during brainstorming

- **Deploy scope:** scaffold/CI/Docker/compose now; Cloud Run + Neon later (accounts not set up yet).
- **DB wiring:** yes — pgx arrives now. `/healthz` does `SELECT 1` against Postgres to prove the app→DB path from day one.
- **`web/`:** placeholder directory with a README reserving it for the milestone-6 Vite SPA. No node toolchain, no CI job.
- **Router:** huma mounted on `humago` (Go 1.22+ stdlib `http.ServeMux`). No chi — request-ID and recovery middleware are ~20 lines by hand, and stdlib `otelhttp` covers milestone-7 tracing.
- **Lint:** golangci-lint in CI (public-repo showcase quality justifies the tool).

## Architecture

Single binary `cmd/api`. `main.go` owns process concerns: read env config (`PORT`, default 8080; `DATABASE_URL`), build slog JSON logger, create pgxpool, construct the API, run `http.Server` with graceful shutdown (SIGINT/SIGTERM).

`internal/api` owns HTTP concerns: huma setup on `humago`, middleware (request ID generation + slog request logging, panic recovery), and two operations:

- `GET /v1/hello` — huma operation returning `{"message": "hello, world"}`; appears in the generated OpenAPI spec and the `/docs` UI huma serves automatically.
- `GET /healthz` — pings Postgres (`SELECT 1` via pgxpool with a short timeout); 200 on success, 503 on failure. Registered as a huma operation but excluded from concerns like auth later; infra-facing.

Module path: `github.com/aarrico/gramwise`.

## File layout

```
cmd/api/main.go
internal/api/            # huma setup, hello + healthz handlers, middleware
web/README.md            # placeholder for M6 SPA
docs/adr/0001-modular-monolith.md
docs/adr/0002-openapi-first-rest-with-huma.md
.github/workflows/ci.yml
Dockerfile
compose.yaml
README.md
go.mod / go.sum
```

## Dockerfile

Multi-stage: `golang` build stage (static binary, `CGO_ENABLED=0`) → distroless final image (`gcr.io/distroless/static`), non-root user, binary only. MB-scale image is a stated repo talking point.

## Compose

Two services: `postgres:17` (with healthcheck) and `api` built from the Dockerfile, depending on postgres healthy, `DATABASE_URL` pointed at the postgres service. `docker compose up` is the one-command quickstart. Dev loop alternative: `go run ./cmd/api` against the compose Postgres.

## CI (GitHub Actions)

Single workflow on push/PR to main: gofmt check, `go vet`, golangci-lint, `go test ./...`, `go build ./...`, and a docker build (no push — registry/deploy wiring comes with the Cloud Run session).

## Testing

Deliberately light for the skeleton:

- httptest through the real huma router for `GET /v1/hello` (status + body).
- `/healthz` verified manually via compose; DB-backed test infrastructure (testcontainers) arrives in milestones 2–3 with the first schema.

## ADRs written this milestone

1. **Modular monolith over microservices** (0001)
2. **OpenAPI-first REST via huma over ConnectRPC/GraphQL** (0002)

## Out of scope

Cloud Run, Neon, GHCR push, scheduled ingest, goose/sqlc, OTel, frontend tooling, README landing-page polish.
