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
- Health: <http://localhost:8080/health>

## Development

Common tasks live in the `Makefile` (`make help` lists them):

```sh
make run                                            # API against local Postgres (auto-starts the DB)
make test                                           # unit tests
make ingest-fixture                                 # load the bundled CSV fixture
make ingest SRC=/tmp/foundation.zip DS=foundation   # load real USDA data
```

`make` runs recipes in `sh`, so they behave the same under fish, bash, or zsh. To
invoke a binary directly under fish, set the env with `env`:

```fish
env DATABASE_URL=postgres://gramwise:gramwise@localhost:5432/gramwise go run ./cmd/api
```

## Status

Ingestion pipeline complete and deployed to GCP (API) and Neon (Postgres DB).
