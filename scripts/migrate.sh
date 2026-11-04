#!/usr/bin/env bash
# Apply migrations after 0001 to the running local stack. 0001 is applied by
# initdb on a fresh volume; later migrations are written to be idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."
for f in $(ls migrations/postgres/*.sql | tail -n +2); do
  echo "applying $f"
  docker compose -f deploy/docker-compose.yml exec -T postgres \
    psql -q -v ON_ERROR_STOP=1 -U apimarket -d apimarket < "$f"
done
