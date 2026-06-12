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
