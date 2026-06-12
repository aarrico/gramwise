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
