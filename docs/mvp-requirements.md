# API Marketplace — MVP Requirements (v0.7)

_Consolidated from the original "API marketplace" doc plus Q&A and review comments on 2026-11-03. Items marked **[OPEN]** still need a decision. Architecture is in `mvp-architecture.md`; costs in `aws-cost-estimate.md`._

## 1. Product summary
A marketplace where Indian developers and businesses find third-party APIs, subscribe to them, and call **all** of them with **one platform key**, paying **one INR bill**. The platform sits in the request path as a **proxy/gateway**: it resells provider APIs at a markup, meters every call, and gives near-real-time logs, stats, audit and alerts.

- **Business model:** resale. The platform pays providers monthly and charges customers with its own margin.
- **Market:** India only. INR only. Credit cards only.
- **Hosting (decided 2026-11-03):** start with **Stage 0 — local development on the founder's Mac** (Docker), per architecture §11. Move to Stage 1 (lean servers in India) for the first paying customers, and to Stage 2 (full HA, AWS Mumbai preferred) when traffic or revenue justifies it.
- **Stage:** real product, starting with an MVP.
- **Size:** ~500 APIs at launch, growing to ~2,000.

## 2. Scope by phase

| Phase 1 (MVP) | Phase 2+ |
|---|---|
| Public catalog (browse without login) | Provider self-serve dashboard (usage, revenue, subscribers) |
| Open sign-up (Google login) | Email login, GitHub, enterprise SSO |
| Provider onboarding: self sign-up **or** auto-onboarding from a docs/spec endpoint, **with an approval workflow run by the platform's own admins** | API versioning and deprecation |
| Protocols: **REST (OpenAPI) only** | **gRPC**; **webhooks** (provider event relay; platform events by webhook) |
| Customer types: **Individual** (prepaid only) and **Institution** (postpaid) | Developer can request a subscription → Admin approves |
| Org roles: Admin (everything), Developer (keys only) | SMS alerts |
| **Only Admin can subscribe** | Downtime / SLA credits |
| One key across all subscribed APIs; separate keys per environment | Audit retention 1 year |
| Key rotation, expiry, IP allowlisting | OAuth for developers' own apps |
| Plans per API: freemium, monthly tiers with quotas, pay per call | Overage billing past quota |
| **Quota exhausted → requests blocked** | Multi-currency, other payment methods |
| Mock responses in the browser (no live test calls) | Full SDK generation |
| Code snippets: Go, Node.js, Java | Stronger GSTIN verification (if the implementation team proposes it) |
| Metadata-only call logs, seconds-level freshness, 60-day retention | |
| Audit trail (calls + account actions), **60-day retention** | |
| Alerts and platform notifications by **email + in-app only** | |
| Retries and provider status notifications (no credits) | |

## 3. Personas and roles
- **Visitor:** browses the catalog, docs, pricing and mock responses. No login needed.
- **Individual developer:** a one-person org. Always **prepaid**. Is the Admin of their own org.
- **Institution customer:** a multi-member org. **Postpaid**. Roles:
  - **Admin:** billing, payment method, invoices, all keys, subscriptions, members, audit.
  - **Developer:** keys only (create/rotate/use), plus logs and stats for those keys. Cannot subscribe in Phase 1.
- **Provider:** signs up or is auto-onboarded; in Phase 1 has no dashboard.
- **Platform admin (internal):** reviews and approves APIs, sets retail price and margin, handles payouts, monitors provider health and upstream quotas.

**Becoming an Institution:**
- The org picks *Institution* at sign-up and enters a **GSTIN**.
- Phase 1 checks the **format only**: 15 characters in the standard GSTIN pattern (2-digit state code, 10-character PAN, entity number, `Z`, check character). There is no lookup against the GST portal. Valid format → the org is an Institution and postpaid is enabled.
- The implementation team may propose stronger verification (e.g. checksum validation, a GST lookup API, or a platform-admin review step) as part of building this flow.
- **[OPEN]** Is there a credit limit for postpaid institutions?

## 4. Functional requirements

### 4.1 Catalog
- Public listing: name, description, category, provider, plans and prices, rate limits, status, docs, mock examples, snippets.
- Search and filter by category and price.
- Live status badge per API from gateway health data.
- Pages server-rendered for SEO.

### 4.2 Provider onboarding and approval workflow
- **Self sign-up:** provider account, OpenAPI spec (upload or URL), upstream auth (API key or OAuth client credentials), base URL, upstream rate limits, provider cost per call.
- **Auto-onboarding:** given one docs/spec URL, ingest it and generate a draft listing (endpoints, schemas, examples, auth type).
- **Workflow:** `Draft → Submitted → In review → (Changes requested ↔ Submitted) → Approved → Published → Suspended / Retired`.
  - A platform admin must approve. In review, the admin sets or confirms **retail price, margin, plans**, checks the upstream credentials with a test call, and checks the provider's resale agreement.
  - Every state change is audited (who, when, notes).
- Upstream credentials are stored encrypted (envelope encryption with a key kept outside the database). The platform refreshes OAuth tokens itself.

### 4.3 Webhooks — moved to Phase 2
- Not in Phase 1. Phase 2 will cover: relaying provider events to customers through the platform (signature checks, re-signing, retries, metering), whether those events are billed per event or included in the plan, and delivering platform events (quota, status, invoice) to customers by webhook.
- In Phase 1, all platform notifications go by **email and in-app** only (see 4.10).

### 4.4 Identity and access
- Open sign-up with Google. A user belongs to one org in Phase 1.

### 4.5 Keys
- One platform key works for every API the org has subscribed to.
- Separate keys per environment: `sandbox` (mock only, free) and `production` (live, billed).
- Rotation with an overlap window, expiry date, IP allowlist per key, immediate revoke.
- Keys stored hashed; the full key is shown once at creation.

### 4.6 Plans, subscriptions, quotas
- Each API has its own plans: free tier (quota), monthly tiers (fee + included calls + rate limit), pay per call.
- Only the org Admin subscribes in Phase 1.
- **Quota exhausted → requests blocked** (HTTP 429, code `QUOTA_EXCEEDED`) until the next period or a plan upgrade.
- Prepaid balance exhausted → requests blocked (HTTP 402, code `INSUFFICIENT_BALANCE`).

### 4.7 Gateway
- REST/HTTP only in Phase 1.
- Authenticate platform key → check subscription → plan rate limit, quota and balance → swap in the upstream credentials → forward → meter.
- **Two layers of limits:** the customer's plan limit and the provider's upstream limit. The platform tracks each provider's upstream consumption globally; at a threshold, ops and heavy users are flagged; above the upstream limit requests get 429 `UPSTREAM_LIMIT`.
- **Retries:** only for idempotent requests (GET/HEAD/PUT/DELETE, or calls with an idempotency key), on connection errors, 502/503/504 or timeouts, with backoff. Never auto-retry a POST.
- Circuit breaker per provider. When it opens, the API status changes and subscribers are notified (email + in-app).
- Calls that fail because of the provider or platform (5xx, timeouts) are **not billed**.

### 4.8 Mock and snippets
- A mock server returns responses generated from spec examples/schemas. Free; no quota.
- Snippets for Go, Node.js and Java, generated from the spec.

### 4.9 Billing
- INR only, credit card only.
- **Individuals — prepaid:** top up credits; calls draw them down in real time; low-balance alert; blocked at zero. Monthly tier fees are taken from the balance at the start of each period.
- **Institutions — postpaid:** card charged monthly for tier fees + metered usage.
- GST-compliant invoices (with the institution's GSTIN for B2B).
- **Provider payouts:** monthly statement at provider cost per call; paid monthly.
- No downtime credits in Phase 1.

### 4.10 Logs, stats, audit, alerts
- **Call logs (metadata only):** timestamp, key, env, API, endpoint, method, status, latency (total and upstream), sizes, cost, billable flag, error code, request ID, client IP. **No bodies.** Visible within seconds. 60-day retention.
- **Stats:** calls, errors, p50/p95 latency, spend by API/key/env/day; spend against budget.
- **Audit:** account and admin actions plus call logs. 60-day retention in Phase 1 (1 year later).
- **Alerts and notifications — email + in-app only in Phase 1:** quota 80/100%, budget threshold, low prepaid balance, error-rate spike, provider down/recovered, invoices and payment events. SMS and webhook delivery later.

## 5. Non-functional requirements
- **Availability by stage:**
  - Stage 0 (local Mac): development and private demos only — no uptime commitment, no real customers or payments.
  - Stage 1 (lean servers): publish ~99.5–99.9%.
  - Stage 2 (full HA): 99.99% target for the gateway.
  - **[OPEN]** Exact SLA to publish at Stage 1 launch.
- **Latency (confirmed):** gateway overhead **< 100 ms**, excluding the provider's own response time; design target p99 < 30 ms in-region.
- **Data:** India only once live (Stage 1+): servers in an Indian region; DPDP Act 2023. Stage 0 uses test data only.
- **Security:** hashed keys, encrypted provider credentials, firewall/WAF on the console, per-org isolation, card data handled only by the PSP.
- **Scale (confirmed for Phase 1):** launch peak of ~2,000 RPS through the gateway (≈ 40M calls/day), with the design able to scale horizontally to ~50,000 RPS without rework. Stage 1 handles a few hundred RPS; move to Stage 2 before sustained traffic exceeds that. Revisit once real traffic data exists.

## 6. Risks and notes
- **Prepaid credits and RBI rules:** keep credits closed-loop (platform-only, non-transferable, no cash-out) to stay clear of prepaid-payment-instrument licensing. Confirm with a lawyer/CA. _(Not legal advice.)_
- **Recurring card charges:** RBI e-mandate rules need extra authentication above certain amounts; some postpaid charges may need the customer's approval.
- **Format-only GSTIN check:** a made-up but well-formed GSTIN would unlock postpaid, and invoices could carry an invalid GSTIN. A postpaid credit limit (see §3) and a later verification step reduce the exposure.
- **Shared upstream quotas:** one heavy customer can use up a provider's limit for everyone.
- **APIs that depend on webhooks:** some provider APIs only work fully with callbacks (e.g. async jobs, payment status). Until Phase 2, either exclude them at onboarding review or list them with a clear "webhooks not supported" note.
- **Local-only stage:** nothing on the laptop is backed up or highly available — keep code in a remote Git repository and treat local data as disposable.
- **Auto-onboarding from HTML docs (non-OpenAPI)** is unreliable; the approval workflow covers it.
- **Provider terms of service:** each provider must agree in writing to resale/proxying.
