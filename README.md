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
