#!/usr/bin/env bash
set -euo pipefail
umask 077

# Usage: bash deploy/backup.sh production|staging [s3://bucket/prefix]
# Requires flock (util-linux on Ubuntu); credentials stay inside PostgreSQL's container.
environment=${1:-}
case "$environment" in
  production|staging) ;;
  *) printf 'Usage: %s production|staging [s3://bucket/prefix]\n' "$0" >&2; exit 2 ;;
esac
backup_s3_uri=${2:-}
if [[ -n "$backup_s3_uri" && "$backup_s3_uri" != s3://* ]]; then
  printf 'The optional upload destination must start with s3://\n' >&2
  exit 2
fi

deploy_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
backup_directory="$deploy_directory/backups/$environment"
mkdir -p "$backup_directory"
exec 9>"$backup_directory/.backup.lock"
flock -n 9 || { printf 'Another %s backup is running.\n' "$environment" >&2; exit 1; }

compose=(docker compose --project-name "vellum-$environment"
  --env-file "$deploy_directory/$environment.env" -f "$deploy_directory/compose.yml")
"${compose[@]}" config --quiet
temporary_file=$(mktemp "$backup_directory/.backup.XXXXXXXX.partial")
trap 'rm -f -- "$temporary_file"' EXIT

"${compose[@]}" exec -T postgres sh -ec \
  'exec pg_dump --format=custom --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  > "$temporary_file"
# Check that the archive can at least be read; a real restore drill is still required.
"${compose[@]}" exec -T postgres pg_restore --list < "$temporary_file" > /dev/null

backup_file="$backup_directory/vellum-$environment-$(date -u +%Y%m%dT%H%M%SZ).dump"
mv -- "$temporary_file" "$backup_file"
printf 'Backup created: %s\n' "$backup_file"
if [[ -n "$backup_s3_uri" ]]; then
  aws s3 cp "$backup_file" "${backup_s3_uri%/}/$(basename "$backup_file")" --only-show-errors
  printf 'Off-server upload completed.\n'
fi
