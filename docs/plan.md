# Gramwise — Founding Plan

Portfolio project for senior backend roles. Successor to `macro_measure` (single-file Flutter calculator, being retired). Public repo from day one; the repo itself is a showcased artifact alongside the running app.

## The product

Every nutrition app answers "what's in 150g of chicken?" Almost none answer the inverse: **"how many grams of chicken to hit 40g of protein?"** Gramwise answers the inverse — and generalizes it to multiple foods at once: given macro targets (protein/carbs/fat) and a set of chosen foods, solve for how much of *each* food to eat.

Exact-hit is usually infeasible, so the real formulation is goal programming: minimize weighted deviation from targets (linear program with slack variables, non-negativity, optional per-food portion caps, weighted priorities like "protein matters most, over-fat worse than under").

## Goals and constraints

- **Audience:** hiring managers and interviewers reviewing a senior backend candidate. Backend is the showcase; frontend is polished but secondary.
- **Primary learning target:** Go. Everything else minimizes novelty (SQL, React, Postgres are known ground).
- **Budget: $0/month.** Hard constraint, no income right now. Documented below; also a README talking point.
- **Running theme:** no tool without a requirement. Every "boring beats shiny" call gets an ADR.

## Stack

| Layer | Choice | Rejected |
|---|---|---|
| Backend | Go, modular monolith, multi-binary (`cmd/api`, `cmd/ingest`) | microservices |
| API | OpenAPI-first REST via huma (Go types as source of truth) | ConnectRPC/protobuf, GraphQL |
| DB | Postgres (Neon free tier) | — |
| Queries | sqlc + pgx; goose migrations | GORM, ent, raw scanning |
| Search | Postgres FTS (tsvector/GIN) + pg_trgm fuzzy | Elasticsearch/Meilisearch |
| Solver | `Solver` interface; gonum `lp.Simplex` v1; hand-rolled simplex stretch | — |
| Auth | Hand-rolled: argon2id, opaque server-side sessions in Postgres, HttpOnly/Secure/SameSite cookies, login rate limiting | JWTs, hosted IdP (Clerk/Auth0), OAuth-only |
| Frontend | Vite + React SPA, TanStack Query + Router, Tailwind + shadcn/ui, TS client generated from OpenAPI spec | Next.js (no SSR/SEO requirement behind auth) |
| Observability | slog (JSON, request IDs) + OpenTelemetry traces (router + pgx spans) + RED metrics → Grafana Cloud free tier | — |
| Hosting | Cloud Run (API, scale-to-zero) + Neon (DB) + Vercel (SPA) + GH Actions (CI/CD + scheduled ingest) + GHCR | Railway/Fly ($), VPS/k8s |
| IaC | None — PaaS config + compose for local dev | Terraform |

Monorepo. PR-based workflow even solo, conventional commits, green CI per merge.

## Data: USDA FoodData Central

Bulk CSV ingestion (`cmd/ingest`), never live API proxying (rate limits, availability coupling).

- **v1 datasets:** Foundation Foods (~hundreds) + SR Legacy (~7,800). Clean generic foods; fast dev-loop ingests.
- **Designed-for-later:** Branded Foods (~1.9M rows) as a documented 250x scale milestone — schema carries dataset-source column from day one. Benchmark against local Docker Postgres (Neon free tier is 0.5GB); write up query plans and FTS behavior at scale.
- **Pipeline shape:** download dump → stream-parse CSV → staging table via `pgx.CopyFrom` → idempotent upserts keyed on FDC IDs → `ingest_runs` row per execution (dataset version, rows upserted, duration). Re-running is always safe; a new USDA release is a new versioned run.
- **Schedule:** GitHub Actions cron workflow runs the ingest binary against Neon — batch ETL doesn't deserve an always-on machine.
- Non-US sources (Open Food Facts) out of scope; nutrition reference books from Alex may inform data modeling (units, serving conversions).

## Auth design notes

Server-side sessions over JWT is a deliberate ADR: single backend, instant revocation, no signing-key ceremony, the session lookup hits a DB we're querying anyway. Public-repo auditability means auth gets adversarial tests: session expiry, rotation on login, constant-time comparisons, rate-limit behavior. Frontend gets a demo-account button so reviewers skip signup.

## Testing

- **Unit:** table-driven Go tests for domain logic.
- **Integration:** testcontainers-go — real disposable Postgres per run; sqlc queries and goose migrations tested for real, identical in CI.
- **API:** httptest through the real router + middleware against containerized DB; doubles as the auth adversarial suite.
- **Solver:** property-based — randomized foods/targets, hand-rolled solver vs gonum as oracle; invariants (non-negative weights, deviation ≥ oracle optimum).
- **Ingest:** golden-file fixtures; assert exact DB state, then re-run and assert zero changes (idempotency proven in CI).
- **E2E:** none in v1 (cut deliberately; Playwright already demonstrated elsewhere in portfolio).
- Coverage reported, no percentage gate.

## $0 deployment notes

- Cloud Run free tier ≈ 2M req/month; scale-to-zero cold start ~1s for a small Go container — note it on the demo page.
- Neon free tier: 0.5GB, scales to zero, no expiry. Fits v1 datasets easily; Branded milestone benchmarks run locally instead.
- Vercel free for static SPA. GH Actions + GHCR free for public repos.
- Domains: `macros.arrico.me` / `api.macros.arrico.me` (already-owned domain) over bare `*.run.app`/`*.vercel.app`.

## Repo presentation

- **README as landing page:** live demo + demo account, Mermaid architecture diagram, Grafana dashboard + trace screenshots, "runs on $0/month" section, one-command quickstart (`docker compose up`).
- **`docs/adr/`** — each written in the milestone that decides it: modular monolith over microservices; REST/huma over ConnectRPC; sqlc over ORM; Postgres FTS over a search engine; sessions over JWT; no Terraform; Vite SPA over Next.js.
- **Skip:** CONTRIBUTING, issue templates, code of conduct (open-source cosplay).
- Multi-stage Dockerfile: distroless final image, non-root, MB-scale Go binary.

## Milestones (tracer-bullet: deployed and demoable after every one)

1. **Walking skeleton** — scaffold (`cmd/`, `internal/`, `web/`), CI green, Dockerfile, compose, hello-world huma endpoint live on Cloud Run + Neon. Deploy-first front-loads all infra risk. Basic slog from day one.
2. **Ingest pipeline** — staging COPY loads, idempotent upserts, `ingest_runs`, golden-file tests, scheduled GH Action; real USDA data in Neon.
3. **Food search API** — FTS + trigram, ranked `/v1/foods?q=`, testcontainers suite. Demo is now publicly curlable.
4. **Solve v1** — single-food inverse lookup, then multi-food via gonum behind the `Solver` interface; property tests. Differentiator live.
5. **Auth** — argon2id, sessions, rate limiting, adversarial tests; saved foods/targets.
6. **Frontend** — Vite SPA, generated client, search → pick foods → solve flow, demo-account button.
7. **Observability + polish** — OTel/Grafana wired, dashboards, trace screenshot, README finished, ADRs complete.
8. **Stretch: hand-rolled simplex** — swapped in behind the interface; gonum demoted to test oracle. Project is complete without it; headline if it lands.

## Future (explicitly out of v1 scope)

- Meal logging / tracking over time (schema designed not to fight it).
- Branded Foods at 2M rows; possibly Open Food Facts.
- OAuth providers behind the existing session layer.
- Mobile: Expo/React Native client in the monorepo consuming the same generated OpenAPI client — the backend is client-agnostic by design, so mobile is additive, not a rewrite.

## Naming

"Gramwise" chosen over "Macrofit" (crowded: multiple live Macrofit apps + MacroFactor in the nutrition space) and "MacroSolver" (technical, not marketable). Only collision is an abandoned 2015 unit-converter iOS app; gramwise.com/.app had no DNS as of June 2026 — register before announcing.
