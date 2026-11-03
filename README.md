# API Marketplace

One key and one bill for thousands of third-party APIs, for developers and businesses in India.

The platform is a proxy/gateway in front of provider APIs. It resells them at a margin, meters every call, and gives customers near-real-time logs, usage stats, audit and alerts. Billing is in INR: prepaid credits for individuals, postpaid for institutions.

**Status:** Phase 1 (MVP), Stage 0: local development on a Mac with Docker.

## Design docs
- [docs/mvp-requirements.md](docs/mvp-requirements.md): scope, roles, billing rules, non-functional requirements
- [docs/mvp-architecture.md](docs/mvp-architecture.md): components, gateway hot path, data model, staged deployment, testing expectations
- [docs/aws-cost-estimate.md](docs/aws-cost-estimate.md): costs for the full AWS deployment

## Quick start (Mac)
Needs Docker Desktop (give it ~8 GB RAM). Go 1.24+ is only needed to run tests outside Docker.

```bash
git clone https://github.com/shivakumarfk/api-marketplace && cd api-marketplace
make up      # builds and starts everything with seed data
make demo    # walks through the main gateway behaviours
make logs    # follow gateway / control plane / metering logs
make reset   # wipe local data and start fresh
```

| What | Where |
|---|---|
| Gateway | http://localhost:8080/{api}/{path} with header `X-API-Key` |
| Control plane | http://localhost:8081/v1/catalog/apis |
| ClickHouse (call logs) | http://localhost:8123/play (user `default`, password `dev`) |
| PostgreSQL | localhost:5432 (user/password/db `apimarket`) |

**Demo keys (local only, they're public):**
- Individual, prepaid, ₹100 opening balance: `mk_live_DemoIndividualLive0000000000000000000000001`. Sandbox version: `mk_test_DemoIndividualTest0000000000000000000000001`
- Institution, postpaid: `mk_live_DemoInstitutionLive000000000000000000000001`

```bash
curl -H "X-API-Key: mk_live_DemoIndividualLive0000000000000000000000001" \
  "http://localhost:8080/weather/today?city=Bengaluru"
```

## What works today
- **Gateway:** key auth (hashed keys, expiry, revoke, IP allowlist), routing by API slug, subscription check, upstream credential injection (the customer's key never reaches the provider), sandbox keys routed to a mock server for free.
- **Limits in one atomic Redis call:** plan rate limit, monthly quota (blocks when used up), the provider's shared upstream limit, and the prepaid balance reservation. If the balance is unknown, the call is refused rather than let through free.
- **Billing rules:** 2xx–4xx billed; provider 5xx/timeouts not billed and refunded. Retries only for idempotent requests (never a plain POST). A per-provider circuit breaker.
- **Metering:** usage events go to a Redis Stream, with a disk spool if the stream is down. A worker writes metadata-only call logs to ClickHouse (60-day TTL, de-duplicated on request ID) and acks only after the write succeeds.
- **Control plane (admin API, Stage 0 token):**
  - create orgs (format check on the GSTIN for institutions)
  - create and revoke keys
  - subscribe to plans
  - prepaid top-ups through a balanced double-entry ledger
  - all actions written to an audit log
  - Redis balances rebuilt from the ledger on startup
- **Database guards:** the ledger must balance and is append-only. Individuals must be prepaid; institutions must be postpaid and have a well-formed GSTIN. Price can't be below provider cost.

## Not built yet (next steps)
- Posting usage to the ledger, Redis-vs-ledger reconciliation, postpaid invoices with GST, provider statements, and the payment gateway
- Google sign-in, the customer console and catalog UI (`web/`), provider onboarding and approval workflow, OpenAPI ingestion, snippets
- Alert engine and email/in-app notifications; OpenTelemetry instrumentation feeding SigNoz

## Admin API (Stage 0)
All admin calls need `Authorization: Bearer dev-admin-token`.

| Method | Path | Body |
|---|---|---|
| GET | `/v1/catalog/apis` | public, no token |
| POST | `/v1/admin/orgs` | `{"name","type":"individual\|institution","gstin"}` |
| POST | `/v1/admin/orgs/{org}/keys` | `{"env":"production\|sandbox","name"}`, returns the key once |
| POST | `/v1/admin/keys/{key}/revoke` | |
| POST | `/v1/admin/orgs/{org}/subscriptions` | `{"api_slug","plan_id"}` |
| POST | `/v1/admin/orgs/{org}/topups` | `{"amount_rupees"}`, prepaid only |
| GET | `/v1/admin/orgs/{org}/balance` | |

## Layout
```
cmd/          gateway, controlplane, metering worker, mockprovider
internal/     apikey, billing rules, breaker, catalog, config, controlplane,
              events (bus + spool), gateway, gstin, limiter (+ Lua), metering,
              money (micro-rupees), secrets (credential encryption)
migrations/   PostgreSQL and ClickHouse schema
deploy/       docker-compose.yml, seed data, OpenTelemetry Collector config
scripts/      demo.sh
docs/         design docs
```

## Tests
```bash
make test   # go test -race ./...   (limiter tests use a real Redis on localhost:6379, else they skip)
```
CI (GitHub Actions) runs these checks with real Redis and PostgreSQL:
- formatting and vet
- that the migrations and seed apply cleanly
- all tests with the race detector
- a Docker image build for every service

**One-time step:** `go.sum` isn't committed yet. The first scaffold was built somewhere that couldn't reach the Go checksum database. Run `make tidy` on your Mac, then commit `go.sum`. Docker builds and CI generate it automatically until then.
