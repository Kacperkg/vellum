#!/usr/bin/env bash
# Start a fresh disposable PostgreSQL cluster for one hosted Go test pass.
set -euo pipefail

case "${1:-}" in
  integration|race) ;;
  *) echo "Specify integration or race for the disposable PostgreSQL fixture." >&2; exit 1 ;;
esac

name="ci-vellum-runtime-$1"
docker run -d --rm --name "$name" -p "127.0.0.1::5432" \
  -e POSTGRES_USER=ci_admin -e POSTGRES_PASSWORD=synthetic-admin-only \
  -e POSTGRES_DB=vellum_runtime_test -e POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256 \
  postgres:17 >/dev/null

ready=false
for _ in {1..60}; do
  if docker exec "$name" pg_isready -h 127.0.0.1 -U ci_admin -d vellum_runtime_test >/dev/null; then
    ready=true
    break
  fi
  sleep 1
done
test "$ready" = true

docker exec -i "$name" psql -X -q -v ON_ERROR_STOP=1 -v expected_database=vellum_runtime_test \
  -U ci_admin -d vellum_runtime_test < deploy/recovery/runtime/prepare-roles.sql
docker exec "$name" psql -X -q -v ON_ERROR_STOP=1 -U ci_admin -d vellum_runtime_test \
  -c "ALTER ROLE vellum_migrator PASSWORD 'synthetic-migrator-only'; ALTER ROLE vellum_runtime PASSWORD 'synthetic-runtime-only';"

binding=$(docker port "$name" 5432/tcp)
[[ "$binding" =~ ^127\.0\.0\.1:[0-9]+$ ]]
dsn="host=127.0.0.1 port=${binding##*:} dbname=vellum_runtime_test sslmode=disable"
{
  printf 'TEST_RUNTIME_ADMIN_DSN=%s user=ci_admin password=synthetic-admin-only\n' "$dsn"
  printf 'TEST_RUNTIME_MIGRATOR_DSN=%s user=vellum_migrator password=synthetic-migrator-only\n' "$dsn"
  printf 'TEST_RUNTIME_APP_DSN=%s user=vellum_runtime password=synthetic-runtime-only\n' "$dsn"
} >> "$GITHUB_ENV"
