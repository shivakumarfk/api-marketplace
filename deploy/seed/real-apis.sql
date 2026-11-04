-- Real, free, public APIs for testing the gateway end to end (Stage 0 only).
-- Requires seed.sql first (uses its provider and demo orgs). Safe to re-run.
--
-- Chosen because they need no sign-up, return harmless public data, and are
-- stable enough for testing. Be polite: they are free community services.
--
--   slug             upstream                                  auth injected by the gateway
--   open-meteo       https://api.open-meteo.com/v1             none
--   ifsc             https://ifsc.razorpay.com                 none
--   jsonplaceholder  https://jsonplaceholder.typicode.com      none (fake writes; nothing is stored)
--   httpbin          https://httpbin.org                       X-Provider-Key (dev value; shows injection in /headers)
--
-- Load into a running stack:  make seed-real
-- Fresh stacks (make reset) load it automatically.

BEGIN;

INSERT INTO providers (id, name, contact_email, agreement_signed_at) VALUES
  ('00000000-0000-0000-0000-0000000000a2', 'Public Internet APIs (testing)', 'ops@public-apis.example', now())
ON CONFLICT (id) DO NOTHING;

-- No mock_url: sandbox (mk_test_) keys get NO_MOCK for these APIs.
-- httpbin's upstream_secret_sealed is "dev-provider-secret" sealed with the dev MASTER_KEY (same as seed.sql).
INSERT INTO apis (id, provider_id, slug, name, description, category, base_url, mock_url,
                  upstream_auth_type, upstream_auth_header, upstream_secret_sealed, upstream_rate_limit_rps, status) VALUES
  ('00000000-0000-0000-0000-0000000000b3', '00000000-0000-0000-0000-0000000000a2', 'open-meteo',
   'Open-Meteo Forecast', 'Weather forecast by latitude/longitude (open-meteo.com)', 'weather',
   'https://api.open-meteo.com/v1', NULL, 'none', NULL, NULL, 5, 'published'),
  ('00000000-0000-0000-0000-0000000000b4', '00000000-0000-0000-0000-0000000000a2', 'ifsc',
   'IFSC Lookup', 'Indian bank branch details by IFSC code (Razorpay)', 'finance',
   'https://ifsc.razorpay.com', NULL, 'none', NULL, NULL, 5, 'published'),
  ('00000000-0000-0000-0000-0000000000b5', '00000000-0000-0000-0000-0000000000a2', 'jsonplaceholder',
   'JSONPlaceholder', 'Fake REST API for posts/users/todos; writes are simulated', 'developer-tools',
   'https://jsonplaceholder.typicode.com', NULL, 'none', NULL, NULL, 5, 'published'),
  ('00000000-0000-0000-0000-0000000000b6', '00000000-0000-0000-0000-0000000000a2', 'httpbin',
   'httpbin', 'HTTP testing: headers, status codes, delays', 'developer-tools',
   'https://httpbin.org', NULL, 'api_key', 'X-Provider-Key',
   'ssUrKzzk4h7kh+PJFks2GTULCHNIMmpCQiHIN5Y/PWQf+4My2FvViUOFVrx/l9I=', 5, 'published')
ON CONFLICT (id) DO NOTHING;

-- One pay-per-call plan each: ₹0.02/call, provider cost ₹0 (the upstreams are free), 2 rps.
INSERT INTO plans (id, api_id, name, kind, monthly_fee_micro, included_calls,
                   price_per_call_micro, provider_cost_per_call_micro, rate_limit_rps) VALUES
  ('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-0000000000b3', 'Pay per call', 'payg', 0, 0, 20000, 0, 2),
  ('00000000-0000-0000-0000-0000000000c5', '00000000-0000-0000-0000-0000000000b4', 'Pay per call', 'payg', 0, 0, 20000, 0, 2),
  ('00000000-0000-0000-0000-0000000000c6', '00000000-0000-0000-0000-0000000000b5', 'Pay per call', 'payg', 0, 0, 20000, 0, 2),
  ('00000000-0000-0000-0000-0000000000c7', '00000000-0000-0000-0000-0000000000b6', 'Pay per call', 'payg', 0, 0, 20000, 0, 2)
ON CONFLICT (id) DO NOTHING;

-- Subscribe both demo orgs (individual prepaid, institution postpaid) to every real API.
INSERT INTO subscriptions (org_id, api_id, plan_id, period_start, period_end, created_by)
SELECT o.id, p.api_id, p.id, date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 'seed-real'
FROM plans p
CROSS JOIN (VALUES ('00000000-0000-0000-0000-0000000000d1'::uuid), ('00000000-0000-0000-0000-0000000000d2'::uuid)) AS o(id)
WHERE p.id IN ('00000000-0000-0000-0000-0000000000c4', '00000000-0000-0000-0000-0000000000c5',
               '00000000-0000-0000-0000-0000000000c6', '00000000-0000-0000-0000-0000000000c7')
ON CONFLICT DO NOTHING;

COMMIT;
