# API Marketplace

One key and one bill for thousands of third-party APIs, for developers and businesses in India.

The platform is a proxy/gateway in front of provider APIs. It resells them at a margin, meters every call, and gives customers near-real-time logs, usage stats, audit and alerts. Billing is in INR: prepaid credits for individuals, postpaid for institutions.

**Status:** Phase 1 (MVP), Stage 0: local development on a Mac with Docker.

## Design docs
- [docs/mvp-requirements.md](docs/mvp-requirements.md): scope, roles, billing rules, non-functional requirements
- [docs/mvp-architecture.md](docs/mvp-architecture.md): components, gateway hot path, data model, staged deployment, testing expectations
- [docs/aws-cost-estimate.md](docs/aws-cost-estimate.md): costs for the full AWS deployment

## Quick start (Mac)
Needs Docker Desktop (give it ~8 GB RAM). Go 1.25+ is only needed to run tests outside Docker.

```bash
git clone https://github.com/shivakumarfk/api-marketplace && cd api-marketplace
make up      # builds and starts everything with seed data
make demo    # walks through the main gateway behaviours
make logs    # follow gateway / control plane / metering logs
make reset   # wipe local data and start fresh
make migrate     # apply new schema migrations to an existing stack (after git pull)
make seed-real   # add real public APIs (Open-Meteo, IFSC, JSONPlaceholder, httpbin) to a running stack
make demo-real   # call them through the gateway (needs internet)
```

| What | Where |
|---|---|
| Web console | http://localhost:3000 (catalog); http://localhost:3000/admin (log in with `dev-admin-token`) |
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
All HTTP is served with Gin. Every layer talks to the next through interfaces (see [Code structure](#code-structure)).

- **Gateway:** key auth (hashed keys, expiry, revoke, IP allowlist), routing by API slug, subscription check, upstream credential injection (the customer's key never reaches the provider), sandbox keys routed to a mock server for free.
- **Limits in one atomic Redis call:** plan rate limit, monthly quota (blocks when used up), the provider's shared upstream limit, and the prepaid balance reservation. If the balance is unknown, the call is refused rather than let through free.
- **Billing rules:** 2xx–4xx billed; provider 5xx/timeouts not billed and refunded. Retries only for idempotent requests (never a plain POST). A per-provider circuit breaker.
- **Metering:** usage events go to a Redis Stream, with a disk spool if the stream is down. A worker writes metadata-only call logs to ClickHouse (60-day TTL, de-duplicated on request ID) and acks only after the write succeeds.
- **Control plane (admin API, Stage 0 token):**
  - create orgs (format check on the GSTIN for institutions)
  - create and revoke keys
  - subscribe to plans
  - prepaid top-ups through a balanced double-entry ledger
  - list APIs: onboard providers, add APIs as drafts (provider credential encrypted before storage, never returned), price them with plans, publish and suspend
  - all actions written to an audit log
  - Redis balances rebuilt from the ledger on startup
- **Database guards:** the ledger must balance and is append-only. Individuals must be prepaid; institutions must be postpaid and have a well-formed GSTIN. Price can't be below provider cost.

## Not built yet (next steps)
- Posting usage to the ledger, Redis-vs-ledger reconciliation, postpaid invoices with GST, provider statements, and the payment gateway
- Google sign-in, the customer console (keys, usage, balance), org management UI, public API detail pages, provider self-service onboarding and the review/approval workflow, OpenAPI ingestion, snippets
- Alert engine and email/in-app notifications; OpenTelemetry instrumentation feeding SigNoz

## Web console (`web/`)
Next.js 16 (App Router, Tailwind). Everything talks to the control plane **from the server**: the browser only sees the web app, so there is no CORS and the admin token never reaches client JavaScript.

- **Public catalog** at `/`: published APIs and their plans. Each API has a public page at `/apis/{path}` with its contract: base URL, authentication, plans and billing terms, and every documented endpoint (parameters, headers, request and response examples, a ready `curl`) with **Try it**, a free mock that returns the documented example and never calls the provider.
- **Admin** at `/admin`: providers (create, sign the resale agreement), APIs (create drafts, edit, rotate the provider secret, publish, suspend), plans (add, retire) and endpoint documentation (add, edit, delete). Upstream auth can be none, an API key in a header, a bearer token, basic auth or an API key in a query parameter, plus fixed headers sent on every upstream call. Errors from the control plane (`AGREEMENT_REQUIRED`, `LAST_ACTIVE_PLAN`…) are shown next to the action.
- **Login (Stage 0):** the admin token, checked against the control plane and kept in an HttpOnly, SameSite=Strict cookie. Google sign-in replaces it.

```bash
make web-dev     # hot reload on :3000 against the local control plane (docker compose stop web first)
make web-check   # lint, typecheck, unit tests (Vitest), production build
make e2e         # Playwright end-to-end tests against the running stack (make up first)
make e2e-ui      # the same in Playwright's UI mode
```

The Playwright specs (`web/e2e/`) drive the real stack with nothing mocked. Test APIs point at the in-stack `mockprovider`, which only answers when it receives the right provider credential, so a 200 through the gateway proves the secret typed into the UI was sealed, decrypted and injected. Every spec uses unique names, so it can be re-run without `make reset`.

## Admin API (Stage 0)
All admin calls need `Authorization: Bearer dev-admin-token`.

| Method | Path | Body |
|---|---|---|
| GET | `/v1/catalog/apis` | public, no token |
| POST | `/v1/admin/orgs` | `{"name","type":"individual\|institution","gstin"}` |
| POST | `/v1/admin/orgs/{org}/keys` | `{"env":"production\|sandbox","name"}`, returns the key once |
| POST | `/v1/admin/keys/{key}/revoke` | |
| POST | `/v1/admin/orgs/{org}/subscriptions` | `{"api_slug","plan_id"}` |
| POST | `/v1/admin/orgs/{org}/topups` | `{"amount_rupees","payment_ref"}`, prepaid only; the same `payment_ref` can't be credited twice |
| GET | `/v1/admin/orgs/{org}/balance` | |

**Listing APIs.** An API starts as a draft. To publish it, its provider must have signed the resale agreement and it needs at least one active plan. Changes reach the gateway within its catalog refresh (5 s). Amounts are exact rupee strings such as `"0.02"`, never JSON numbers.

| Method | Path | Body |
|---|---|---|
| POST | `/v1/admin/providers` | `{"name","contact_email","agreement_signed":bool}` |
| GET | `/v1/admin/providers` | |
| POST | `/v1/admin/providers/{id}/agreement` | records the agreement as signed now (repeat calls keep the first date) |
| POST | `/v1/admin/apis` | `{"provider_id","slug","name","description","category","base_url","mock_url","auth":{"type":"none\|api_key","header","secret"},"upstream_rate_limit_rps"}` |
| GET | `/v1/admin/apis` | every API in any status, with all plans; credentials show only `has_secret` |
| GET / PATCH | `/v1/admin/apis/{slug}` | PATCH takes any of the create fields except `provider_id` and `slug`; with `auth` but no `secret`, an `api_key` API keeps its stored secret |
| POST | `/v1/admin/apis/{slug}/publish` · `/suspend` | suspending stops calls (404) but keeps subscriptions |
| POST | `/v1/admin/apis/{slug}/plans` | `{"name","kind":"free\|tier\|payg","monthly_fee","included_calls","price_per_call","provider_cost_per_call","rate_limit_rps"}` |
| POST | `/v1/admin/plans/{id}/retire` | stops new subscriptions; existing subscribers keep the plan |
| POST | `/v1/admin/apis/{slug}/endpoints` | `{"method","path":"/items/{id}","summary","description","params":[{"name","in":"path\|query","required","description","example"}],"headers":[{"name","required","description","example"}],"request_example","response_status","response_content_type","response_example"}` |
| PUT / DELETE | `/v1/admin/endpoints/{id}` | PUT replaces the documentation |
| GET | `/v1/catalog/apis/{slug}` | public contract of one published API: plans and endpoints, nothing internal |

`auth.type` is `none`, `api_key` (`header` = header name), `bearer`, `basic` (`secret` = `user:password`) or `query` (`header` = query parameter name; the gateway replaces any value the customer sends). `upstream_headers` is a list of fixed `{"name","value"}` headers sent to the provider; they override the customer's.

```bash
A="Authorization: Bearer dev-admin-token"
curl -s -H "$A" -d '{"name":"Postman","agreement_signed":true}' localhost:8081/v1/admin/providers     # → id
curl -s -H "$A" -d '{"provider_id":"<id>","slug":"postman-echo","name":"Postman Echo","base_url":"https://postman-echo.com"}' localhost:8081/v1/admin/apis
curl -s -H "$A" -d '{"name":"PAYG","kind":"payg","price_per_call":"0.03"}' localhost:8081/v1/admin/apis/postman-echo/plans
curl -s -H "$A" -X POST localhost:8081/v1/admin/apis/postman-echo/publish
```

## Code structure
Go 1.25, **Gin** for HTTP, **pgx** for PostgreSQL, **go-redis** for Redis. Interfaces sit at every boundary, and dependencies point inward:

```
cmd/                         wiring only: build adapters, inject them (no DI framework)
  gateway/ controlplane/ metering/ mockprovider/
internal/
  domain/                    entities + business rules, no I/O
    apikey/ billing/ gstin/ money/   key format, billable/retry rules, GSTIN check, micro-rupees
    catalog/ limits/ usage/          gateway read model, limiter contract, usage event
  service/                   control-plane use cases (orgs, keys, subscriptions, billing)
    ports.go                 the interfaces services depend on: OrgRepo, KeyRepo, LedgerRepo,
                             BalanceStore, TxManager, Clock, KeyGenerator, ...
  adapter/                   implementations of those interfaces
    postgres/                repositories, TxManager, gateway catalog snapshot
    redis/                   atomic limiter (Lua), live balances, usage stream
    clickhouse/ secrets/ spool/
  gateway/                   the proxy (Gin); depends only on its own ports.go
  metering/                  usage → call logs worker; ports: Consumer, CallLogWriter
  transport/http/
    controlplane/            Gin router + handlers (decode → service interface → encode)
    middleware/              request id, recovery, access log, admin bearer auth, error shape
  platform/                  config (typed, validated), breaker, run helpers
migrations/  deploy/  scripts/  docs/
```

How to add a feature:
1. Put the rule in `domain`.
2. Add the use case in `service`, declaring any new port it needs in `ports.go`.
3. Implement the port in `adapter/...`, with a compile-time `var _ service.X = ...` check.
4. Expose it in `transport/http`.
5. Wire it in `cmd/`.

## Tests
```bash
make up     # Postgres + Redis for the integration tests
make test   # go test -race ./...
```
| Layer | How it's tested |
|---|---|
| domain | pure unit tests, including property tests for money rounding and ledger balance |
| service | unit tests with in-memory fakes of every port, including rollback when the audit write fails |
| adapter/postgres | integration tests against a real PostgreSQL. Each run gets a fresh schema with the real migration applied, and the DB-level guards are tested too. |
| adapter/redis | integration tests against a real Redis, e.g. 200 concurrent calls on a nearly empty balance never overspend |
| transport/http, gateway | Gin handler tests with fake services, a fake provider and a fake limiter |

Integration tests skip when `TEST_DATABASE_URL` or Redis aren't available. CI (GitHub Actions) runs everything with real Postgres and Redis, plus these checks:
- formatting and vet
- that the migrations and seed apply cleanly
- a Docker image build for every service

**One-time step:** `go.sum` isn't committed yet. The first scaffold was built somewhere that couldn't reach the Go checksum database. Run `make tidy` on your Mac, then commit `go.sum`. Docker builds and CI generate it automatically until then.
