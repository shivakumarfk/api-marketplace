-- 0002: endpoint documentation (public contract, examples for "Try it") and
-- more ways to authenticate to providers. Idempotent: safe to re-apply
-- (make migrate), and applied by initdb on fresh volumes.

-- Upstream auth: bearer token, basic auth and a key in the query string, in
-- addition to a key in a header. upstream_auth_header holds the header name
-- (api_key) or the query parameter name (query).
ALTER TABLE apis DROP CONSTRAINT IF EXISTS apis_upstream_auth_type_check;
ALTER TABLE apis ADD CONSTRAINT apis_upstream_auth_type_check
    CHECK (upstream_auth_type IN ('none','api_key','bearer','basic','query','oauth_cc'));

-- Fixed, non-secret headers the gateway sends to the provider on every call,
-- e.g. [{"name":"X-Client-Id","value":"acme"}]. Secrets go in the sealed credential.
ALTER TABLE apis ADD COLUMN IF NOT EXISTS upstream_headers JSONB NOT NULL DEFAULT '[]'
    CHECK (jsonb_typeof(upstream_headers) = 'array');

-- Endpoint documentation, shown publicly on the API's page.
ALTER TABLE api_endpoints
    ADD COLUMN IF NOT EXISTS summary               TEXT  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS description           TEXT  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS params                JSONB NOT NULL DEFAULT '[]',  -- [{name,in,required,description,example}]
    ADD COLUMN IF NOT EXISTS headers               JSONB NOT NULL DEFAULT '[]',  -- headers the customer sends: [{name,required,description,example}]
    ADD COLUMN IF NOT EXISTS request_example       TEXT  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS response_status       INT   NOT NULL DEFAULT 200 CHECK (response_status BETWEEN 100 AND 599),
    ADD COLUMN IF NOT EXISTS response_content_type TEXT  NOT NULL DEFAULT 'application/json',
    ADD COLUMN IF NOT EXISTS response_example      TEXT  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_at            TIMESTAMPTZ NOT NULL DEFAULT now();
