#!/usr/bin/env bash
# Walk through the main gateway behaviours against the local stack (make up).
set -euo pipefail

GW=${GW:-http://localhost:8080}
CP=${CP:-http://localhost:8081}
ADMIN=${ADMIN_TOKEN:-dev-admin-token}
IND_LIVE=mk_live_DemoIndividualLive0000000000000000000000001
IND_TEST=mk_test_DemoIndividualTest0000000000000000000000001
INST_LIVE=mk_live_DemoInstitutionLive000000000000000000000001
IND_ORG=00000000-0000-0000-0000-0000000000d1

step() { printf "\n\033[1;36m▶ %s\033[0m\n" "$*"; }
call() { curl -s -w "\n  → HTTP %{http_code}\n" "$@"; }

step "Public catalog"
call "$CP/v1/catalog/apis"

step "Individual (prepaid): balance before"
call -H "Authorization: Bearer $ADMIN" "$CP/v1/admin/orgs/$IND_ORG/balance"

step "Call Weather with a production key (billed ₹0.05; provider credential injected by the gateway)"
call -i -H "X-API-Key: $IND_LIVE" "$GW/weather/today?city=Bengaluru" | grep -E '^(HTTP|X-Request-Id|X-Quota)|temp|→' || true

step "Same call with a sandbox key (goes to the mock server, free)"
call -H "X-API-Key: $IND_TEST" "$GW/weather/today?city=Mysuru"

step "Provider returns 503 → retried for GET, and never billed"
call -H "X-API-Key: $IND_LIVE" "$GW/weather/status/503"

step "Not subscribed (individual has no Echo subscription)"
call -H "X-API-Key: $IND_LIVE" "$GW/echo/echo"

step "Institution (postpaid) calls Echo with a POST body"
call -X POST -H "X-API-Key: $INST_LIVE" -H 'Content-Type: application/json' -d '{"hello":"world"}' "$GW/echo/echo"

step "Bad key"
call -H "X-API-Key: mk_live_wrong" "$GW/weather/today"

step "Individual: balance after (only the successful production call was charged)"
call -H "Authorization: Bearer $ADMIN" "$CP/v1/admin/orgs/$IND_ORG/balance"

step "Call logs in ClickHouse (written by the metering worker within a few seconds)"
sleep 3
curl -s -u default:dev "http://localhost:8123/" --data-binary \
  "SELECT ts, env, method, path, status, billable, cost_micro, retry_count FROM call_logs FINAL ORDER BY ts DESC LIMIT 10 FORMAT PrettyCompact"
