DATABASE_URL ?= postgres://gramwise:gramwise@localhost:5432/gramwise
export DATABASE_URL

DS ?= foundation

.DEFAULT_GOAL := help
.PHONY: help db-up db-down db-reset psql run build docker-build ingest-fixture ingest test test-integration lint fmt generate

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop containers (keep data)
	docker compose down

db-reset: ## Recreate Postgres from scratch (wipes data + schema)
	docker compose down -v
	docker compose up -d --wait postgres

psql: db-up ## Open a psql shell in the Postgres container
	docker compose exec postgres psql -U gramwise gramwise

run: db-up ## Run the API locally against local Postgres
	go run ./cmd/api

build: ## Compile all packages
	go build ./...

docker-build: ## Build the production API image (mirrors CI/deploy)
	docker build -t gramwise-api .

ingest-fixture: db-up ## Ingest the bundled CSV fixture (fast smoke test)
	go run ./cmd/ingest --source internal/ingest/testdata/fdc_small --datasets foundation,sr_legacy

ingest: db-up ## Ingest real data: make ingest SRC=/tmp/foundation.zip [DS=foundation]
	@test -n "$(SRC)" || { echo "SRC is required, e.g. make ingest SRC=/tmp/foundation.zip DS=foundation"; exit 1; }
	go run ./cmd/ingest --source "$(SRC)" --datasets "$(DS)"

test: ## Run unit tests
	go test ./...

test-integration: ## Run all tests incl. integration (needs Docker for testcontainers)
	go test -race -tags=integration ./...

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format all Go code
	gofmt -w .

generate: ## Regenerate sqlc
	sqlc generate