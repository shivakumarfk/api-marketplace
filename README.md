# API Marketplace

One key and one bill for thousands of third-party APIs, for developers and businesses in India.

The platform is a proxy/gateway in front of provider APIs. It resells them at a margin, meters every call, and gives customers near-real-time logs, usage stats, audit and alerts. Billing is in INR: prepaid credits for individuals, postpaid for institutions.

## Status
Phase 1 (MVP), Stage 0: local development on a Mac with Docker.

## Design docs
- [docs/mvp-requirements.md](docs/mvp-requirements.md): scope, roles, billing rules, non-functional requirements
- [docs/mvp-architecture.md](docs/mvp-architecture.md): components, gateway hot path, data model, staged deployment, testing expectations
- [docs/aws-cost-estimate.md](docs/aws-cost-estimate.md): costs for the full AWS deployment

## Planned layout
```
cmd/            gateway, control-plane API, metering & alert workers, mock provider
internal/       shared Go packages (keys, rate limiting, ledger, events, store)
migrations/     PostgreSQL and ClickHouse schema migrations
deploy/         Docker Compose (Stage 0/1), later Helm/Terraform (Stage 2)
web/            Next.js catalog, console and admin UI
docs/           design docs and ADRs
```

## Tech
Go (gateway and services), Next.js (web), PostgreSQL, Redis, ClickHouse, Redis Streams / NATS (event bus), OpenTelemetry and SigNoz (observability).
