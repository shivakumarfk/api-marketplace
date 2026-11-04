#!/usr/bin/env bash
# Call real public APIs through the gateway. Needs internet and `make seed-real`.
set -euo pipefail

GW=${GW:-http://localhost:8080}
CP=${CP:-http://localhost:8081}
ADMIN=${ADMIN_TOKEN:-dev-admin-token}
IND_LIVE=mk_live_DemoIndividualLive0000000000000000000000001
IND_TEST=mk_test_DemoIndividualTest0000000000000000000000001
INST_LIVE=mk_live_DemoInstitutionLive000000000000000000000001
IND_ORG=00000000-0000-0000-0000-0000000000d1

step() { printf "\n\033[1;36m▶ %s\033[0m\n" "$*"; }
call() { curl -s --max-time 40 -w "\n  → HTTP %{http_code} in %{time_total}s\n" "$@"; }
pause() { sleep 0.6; } # plans allow 2 rps per key

step "Balance before"
call -H "Authorization: Bearer $ADMIN" "$CP/v1/admin/orgs/$IND_ORG/balance"

step "Open-Meteo: current weather in Bengaluru"
call -H "X-API-Key: $IND_LIVE" "$GW/open-meteo/forecast?latitude=12.97&longitude=77.59&current=temperature_2m,wind_speed_10m&timezone=Asia/Kolkata"; pause

step "IFSC: look up a bank branch"
call -H "X-API-Key: $IND_LIVE" "$GW/ifsc/HDFC0CAGSBK"; pause

step "IFSC: unknown code → provider 404 (a 4xx is billed)"
call -H "X-API-Key: $IND_LIVE" "$GW/ifsc/NOTAREAL000"; pause

step "JSONPlaceholder: GET a post"
call -H "X-API-Key: $IND_LIVE" "$GW/jsonplaceholder/posts/1"; pause

step "JSONPlaceholder: POST (institution, postpaid; the write is simulated upstream)"
call -X POST -H "X-API-Key: $INST_LIVE" -H 'Content-Type: application/json' \
  -d '{"title":"hello","body":"from the gateway","userId":1}' "$GW/jsonplaceholder/posts"; pause

step "httpbin /headers: provider key injected, customer X-API-Key stripped"
call -H "X-API-Key: $IND_LIVE" "$GW/httpbin/headers"; pause

step "httpbin /status/503: retried for GET, not billed"
call -H "X-API-Key: $IND_LIVE" "$GW/httpbin/status/503"; pause

step "Sandbox key on a real API → NO_MOCK (no mock server configured)"
call -H "X-API-Key: $IND_TEST" "$GW/ifsc/HDFC0CAGSBK"; pause

step "Rate limit: 5 quick calls at 2 rps → some 429s"
for _ in 1 2 3 4 5; do
  curl -s -o /dev/null -w "%{http_code} " -H "X-API-Key: $IND_LIVE" "$GW/ifsc/HDFC0CAGSBK"
done
echo

step "Balance after (₹0.02 per billed call)"
call -H "Authorization: Bearer $ADMIN" "$CP/v1/admin/orgs/$IND_ORG/balance"
