-- 0001_init.sql — Phase 1 core schema (architecture §5).
-- Money is integer micro-rupees (BIGINT, *_micro). Never floats.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

-- ---------- identity ----------
CREATE TABLE organizations (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    type          TEXT NOT NULL CHECK (type IN ('individual','institution')),
    billing_mode  TEXT NOT NULL CHECK (billing_mode IN ('prepaid','postpaid')),
    gstin         TEXT,
    legal_name    TEXT,
    credit_limit_micro BIGINT,                       -- [OPEN] postpaid credit limit
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','closed')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Individuals are always prepaid; institutions are postpaid and need a GSTIN.
    CONSTRAINT individual_prepaid  CHECK (type <> 'individual'  OR billing_mode = 'prepaid'),
    CONSTRAINT institution_postpaid CHECK (type <> 'institution' OR (billing_mode = 'postpaid' AND gstin IS NOT NULL)),
    CONSTRAINT gstin_shape CHECK (gstin IS NULL OR gstin ~ '^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$')
);

CREATE TABLE users (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    google_sub  TEXT UNIQUE,
    email       TEXT NOT NULL UNIQUE,
    name        TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE memberships (
    org_id   UUID NOT NULL REFERENCES organizations(id),
    user_id  UUID NOT NULL REFERENCES users(id),
    role     TEXT NOT NULL CHECK (role IN ('admin','developer')),
    PRIMARY KEY (org_id, user_id),
    UNIQUE (user_id) -- Phase 1: a user belongs to one org
);

-- ---------- providers and catalog ----------
CREATE TABLE providers (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 TEXT NOT NULL,
    contact_email        TEXT,
    gstin                TEXT,
    payout_ref           TEXT,
    agreement_signed_at  TIMESTAMPTZ,  -- resale agreement; required before publishing
    status               TEXT NOT NULL DEFAULT 'active',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE apis (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id              UUID NOT NULL REFERENCES providers(id),
    slug                     TEXT NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,62}$'),
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    category                 TEXT NOT NULL DEFAULT 'other',
    protocol                 TEXT NOT NULL DEFAULT 'rest' CHECK (protocol IN ('rest')), -- grpc/webhook in Phase 2
    base_url                 TEXT NOT NULL,
    mock_url                 TEXT,
    upstream_auth_type       TEXT NOT NULL DEFAULT 'api_key' CHECK (upstream_auth_type IN ('none','api_key','oauth_cc')),
    upstream_auth_header     TEXT,           -- e.g. X-Api-Key
    upstream_secret_sealed   TEXT,           -- AES-GCM sealed by internal/secrets; never plaintext
    upstream_rate_limit_rps  NUMERIC,
    status                   TEXT NOT NULL DEFAULT 'draft' CHECK (status IN
                               ('draft','submitted','in_review','changes_requested','approved','published','suspended','retired')),
    spec_object_key          TEXT,
    health_status            TEXT NOT NULL DEFAULT 'unknown',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_endpoints (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_id         UUID NOT NULL REFERENCES apis(id),
    method         TEXT NOT NULL,
    path_template  TEXT NOT NULL,
    operation_id   TEXT,
    idempotent     BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (api_id, method, path_template)
);

-- Approval workflow history: Draft → Submitted → In review → … → Published.
CREATE TABLE onboarding_reviews (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_id        UUID NOT NULL REFERENCES apis(id),
    source        TEXT NOT NULL CHECK (source IN ('self','auto')),
    from_state    TEXT,
    to_state      TEXT NOT NULL,
    submitted_by  TEXT,
    reviewer_id   TEXT,
    notes         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE plans (
    id                            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_id                        UUID NOT NULL REFERENCES apis(id),
    name                          TEXT NOT NULL,
    kind                          TEXT NOT NULL CHECK (kind IN ('free','tier','payg')),
    monthly_fee_micro             BIGINT NOT NULL DEFAULT 0 CHECK (monthly_fee_micro >= 0),
    included_calls                BIGINT NOT NULL DEFAULT 0 CHECK (included_calls >= 0), -- 0 = no quota
    price_per_call_micro          BIGINT NOT NULL DEFAULT 0 CHECK (price_per_call_micro >= 0),
    provider_cost_per_call_micro  BIGINT NOT NULL DEFAULT 0 CHECK (provider_cost_per_call_micro >= 0),
    rate_limit_rps                DOUBLE PRECISION NOT NULL DEFAULT 0,                  -- 0 = unlimited
    period                        TEXT NOT NULL DEFAULT 'month',
    status                        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','retired')),
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT margin_not_negative CHECK (price_per_call_micro >= provider_cost_per_call_micro OR kind = 'free')
);

CREATE TABLE subscriptions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organizations(id),
    api_id        UUID NOT NULL REFERENCES apis(id),
    plan_id       UUID NOT NULL REFERENCES plans(id),
    status        TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','cancelled','blocked')),
    period_start  TIMESTAMPTZ NOT NULL,
    period_end    TIMESTAMPTZ NOT NULL,
    created_by    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_subscription ON subscriptions (org_id, api_id) WHERE status = 'active';

-- ---------- keys ----------
CREATE TABLE api_keys (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id           UUID NOT NULL REFERENCES organizations(id),
    env              TEXT NOT NULL CHECK (env IN ('production','sandbox')),
    name             TEXT NOT NULL DEFAULT '',
    prefix           TEXT NOT NULL,
    secret_hash      TEXT NOT NULL UNIQUE,  -- SHA-256 hex; the plain key is never stored
    ip_allowlist     CIDR[],
    expires_at       TIMESTAMPTZ,
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','rotating','revoked')),
    rotated_from_id  UUID REFERENCES api_keys(id),
    created_by       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at       TIMESTAMPTZ,
    last_used_at     TIMESTAMPTZ
);
CREATE INDEX api_keys_org ON api_keys (org_id);

-- ---------- ledger (double entry, append-only) ----------
CREATE TABLE ledger_accounts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_type  TEXT NOT NULL CHECK (owner_type IN ('org','provider','platform')),
    owner_id    TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('wallet','receivable','payable','revenue','tax','cash')),
    UNIQUE (owner_type, owner_id, kind)
);

CREATE TABLE ledger_txns (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ref_type    TEXT NOT NULL,
    ref_id      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (ref_type, ref_id)  -- idempotency: the same source event posts once
);

CREATE TABLE ledger_entries (
    id            BIGSERIAL PRIMARY KEY,
    txn_id        UUID NOT NULL REFERENCES ledger_txns(id),
    account_id    UUID NOT NULL REFERENCES ledger_accounts(id),
    amount_micro  BIGINT NOT NULL CHECK (amount_micro > 0),
    direction     TEXT NOT NULL CHECK (direction IN ('debit','credit')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ledger_entries_account ON ledger_entries (account_id);

-- Every transaction must balance (checked at commit).
CREATE FUNCTION ledger_txn_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE diff BIGINT;
BEGIN
    SELECT COALESCE(SUM(CASE direction WHEN 'debit' THEN amount_micro ELSE -amount_micro END), 0)
      INTO diff FROM ledger_entries WHERE txn_id = NEW.txn_id;
    IF diff <> 0 THEN
        RAISE EXCEPTION 'ledger transaction % does not balance (diff %)', NEW.txn_id, diff;
    END IF;
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER ledger_balanced AFTER INSERT ON ledger_entries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ledger_txn_balanced();

-- Append-only: no updates or deletes on ledger rows.
CREATE FUNCTION ledger_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'ledger is append-only; post a reversing transaction instead'; END $$;
CREATE TRIGGER ledger_entries_no_change BEFORE UPDATE OR DELETE ON ledger_entries
    FOR EACH ROW EXECUTE FUNCTION ledger_append_only();

-- ---------- payments and invoices ----------
CREATE TABLE payments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        UUID NOT NULL REFERENCES organizations(id),
    psp_ref       TEXT UNIQUE,
    amount_micro  BIGINT NOT NULL,
    status        TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE invoices (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID NOT NULL REFERENCES organizations(id),
    period          DATE NOT NULL,
    subtotal_micro  BIGINT NOT NULL,
    gst_micro       BIGINT NOT NULL,
    total_micro     BIGINT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'draft',
    pdf_key         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, period)
);

CREATE TABLE invoice_lines (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id    UUID NOT NULL REFERENCES invoices(id),
    api_id        UUID REFERENCES apis(id),
    plan_id       UUID REFERENCES plans(id),
    description   TEXT NOT NULL,
    qty           BIGINT NOT NULL,
    amount_micro  BIGINT NOT NULL
);

CREATE TABLE provider_statements (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID NOT NULL REFERENCES providers(id),
    period        DATE NOT NULL,
    calls         BIGINT NOT NULL,
    amount_micro  BIGINT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'draft',
    UNIQUE (provider_id, period)
);

-- ---------- alerts, notifications, audit ----------
CREATE TABLE alert_rules (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     UUID NOT NULL REFERENCES organizations(id),
    kind       TEXT NOT NULL CHECK (kind IN ('quota','budget','balance','error_rate','status')),
    threshold  NUMERIC NOT NULL,
    channels   TEXT[] NOT NULL DEFAULT '{email,in_app}',
    CONSTRAINT phase1_channels CHECK (channels <@ ARRAY['email','in_app'])
);

CREATE TABLE notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(id),
    user_id     UUID REFERENCES users(id),
    kind        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Account/admin actions. Copied to ClickHouse audit_events (60-day TTL) by a
-- later job; kept here as the transactional record.
CREATE TABLE audit_log (
    id           BIGSERIAL PRIMARY KEY,
    ts           TIMESTAMPTZ NOT NULL DEFAULT now(),
    org_id       UUID,
    actor_type   TEXT NOT NULL,
    actor_id     TEXT NOT NULL,
    action       TEXT NOT NULL,
    target_type  TEXT,
    target_id    TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_org_ts ON audit_log (org_id, ts DESC);
