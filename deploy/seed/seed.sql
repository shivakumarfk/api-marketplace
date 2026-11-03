-- Local development seed data (Stage 0 only). NEVER load into a real environment:
-- the demo keys below are public.
--
-- Demo keys (use in the X-API-Key header):
--   individual, production : mk_live_DemoIndividualLive0000000000000000000000001
--   individual, sandbox    : mk_test_DemoIndividualTest0000000000000000000000001
--   institution, production: mk_live_DemoInstitutionLive000000000000000000000001

BEGIN;

INSERT INTO providers (id, name, contact_email, agreement_signed_at) VALUES
  ('00000000-0000-0000-0000-0000000000a1', 'Demo Weather Co', 'ops@demo-weather.example', now());

-- upstream_secret_sealed = "dev-provider-secret" sealed with the dev MASTER_KEY in .env.example
INSERT INTO apis (id, provider_id, slug, name, description, category, base_url, mock_url,
                  upstream_auth_type, upstream_auth_header, upstream_secret_sealed, upstream_rate_limit_rps, status) VALUES
  ('00000000-0000-0000-0000-0000000000b1', '00000000-0000-0000-0000-0000000000a1', 'weather',
   'Weather Now', 'Current weather for Indian cities', 'weather',
   'http://mockprovider:9000', 'http://apimock:9000', 'api_key', 'X-Provider-Key',
   'ssUrKzzk4h7kh+PJFks2GTULCHNIMmpCQiHIN5Y/PWQf+4My2FvViUOFVrx/l9I=', 50, 'published'),
  ('00000000-0000-0000-0000-0000000000b2', '00000000-0000-0000-0000-0000000000a1', 'echo',
   'Echo Test API', 'Echoes the request; handy for testing', 'developer-tools',
   'http://mockprovider:9000', 'http://apimock:9000', 'api_key', 'X-Provider-Key',
   'ssUrKzzk4h7kh+PJFks2GTULCHNIMmpCQiHIN5Y/PWQf+4My2FvViUOFVrx/l9I=', 0, 'published');

INSERT INTO plans (id, api_id, name, kind, monthly_fee_micro, included_calls,
                   price_per_call_micro, provider_cost_per_call_micro, rate_limit_rps) VALUES
  -- Weather: free (100 calls/month, 2 rps) and pay-per-call (₹0.05/call, provider gets ₹0.03)
  ('00000000-0000-0000-0000-0000000000c1', '00000000-0000-0000-0000-0000000000b1', 'Free',        'free', 0,          100,   0,      0,      2),
  ('00000000-0000-0000-0000-0000000000c2', '00000000-0000-0000-0000-0000000000b1', 'Pay per call','payg', 0,          0,     50000,  30000,  20),
  -- Echo: ₹499/month tier with 10,000 calls included, 10 rps
  ('00000000-0000-0000-0000-0000000000c3', '00000000-0000-0000-0000-0000000000b2', 'Starter',     'tier', 499000000,  10000, 0,      0,      10);

INSERT INTO organizations (id, name, type, billing_mode, gstin, legal_name) VALUES
  ('00000000-0000-0000-0000-0000000000d1', 'Asha (individual developer)', 'individual',  'prepaid',  NULL, NULL),
  ('00000000-0000-0000-0000-0000000000d2', 'Acme Co-operative Bank',      'institution', 'postpaid', '29ABCDE1234F1Z5', 'Acme Co-operative Bank Ltd');

INSERT INTO subscriptions (org_id, api_id, plan_id, period_start, period_end, created_by) VALUES
  ('00000000-0000-0000-0000-0000000000d1', '00000000-0000-0000-0000-0000000000b1', '00000000-0000-0000-0000-0000000000c2',
   date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 'seed'),
  ('00000000-0000-0000-0000-0000000000d2', '00000000-0000-0000-0000-0000000000b1', '00000000-0000-0000-0000-0000000000c2',
   date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 'seed'),
  ('00000000-0000-0000-0000-0000000000d2', '00000000-0000-0000-0000-0000000000b2', '00000000-0000-0000-0000-0000000000c3',
   date_trunc('month', now()), date_trunc('month', now()) + interval '1 month', 'seed');

INSERT INTO api_keys (org_id, env, name, prefix, secret_hash, created_by) VALUES
  ('00000000-0000-0000-0000-0000000000d1', 'production', 'demo live', 'mk_live_Demo',
   '0fad8a284c9b6f97632ce2a24be235c37c4ee0868cc3386fa8930d9ecc49425b', 'seed'),
  ('00000000-0000-0000-0000-0000000000d1', 'sandbox',    'demo test', 'mk_test_Demo',
   'eb76d67d7a3fc864a2885b8fe2c11a5487a58d63f3fcbe71c4120dacbea28ec0', 'seed'),
  ('00000000-0000-0000-0000-0000000000d2', 'production', 'demo live', 'mk_live_Demo',
   'cae785d514c6f8ec35972765a7d82c34edbe6875bf6167de2f0d1dc96907d0c6', 'seed');

-- ₹100 opening top-up for the individual (prepaid) org, as a balanced ledger transaction.
INSERT INTO ledger_accounts (id, owner_type, owner_id, kind) VALUES
  ('00000000-0000-0000-0000-0000000000e1', 'platform', '', 'cash'),
  ('00000000-0000-0000-0000-0000000000e2', 'org', '00000000-0000-0000-0000-0000000000d1', 'wallet');
INSERT INTO ledger_txns (id, ref_type, ref_id) VALUES
  ('00000000-0000-0000-0000-0000000000f1', 'topup', 'seed-opening-balance');
INSERT INTO ledger_entries (txn_id, account_id, amount_micro, direction) VALUES
  ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-0000000000e1', 100000000, 'debit'),
  ('00000000-0000-0000-0000-0000000000f1', '00000000-0000-0000-0000-0000000000e2', 100000000, 'credit');

COMMIT;
