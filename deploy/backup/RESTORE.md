# Isolated S3 restore drill

This procedure is for a future separately approved drill on a private disposable
host. It must not target either live Compose project or their PostgreSQL volumes.
A local synthetic-data restore tests dump/restore mechanics; it does not prove
S3 round-trip recovery. Declare operational readiness only after downloading and
restoring an actual selected S3 backup with the matching application checks.

Before starting, record the incident/drill owner, environment, exact completion
object key, archive/manifest keys, matching PostgreSQL image digest and retained
application digest. Use a recovery/read role restricted to the bucket's backup
prefixes. Ensure private disk capacity for the expanded database, indexes and
WAL, not just the compressed archive. Keep the disposable host off public routing;
block instance-metadata access from containers. Retain protected bootstrap/env
material separately; do not copy production env files into a drill container.

## Download a specific completed run

Use a root-private temporary directory (`umask 077`, `mktemp -d`) on the drill
host. Do not select a wildcard, overwrite an existing download or execute content
from a manifest. The commands below assume an authorized shell and configured
instance-profile recovery role. Replace every placeholder explicitly; the
examples do not create AWS resources or read the live database.

```sh
umask 077
work=$(mktemp -d)
cd "$work"
bucket=REPLACE_PRIVATE_BUCKET
account=REPLACE_12_DIGIT_ACCOUNT
marker_key=staging/completed/REPLACE_EXACT_RUN.json
archive_key=staging/runs/REPLACE_SAME_EXACT_RUN.dump
manifest_key=staging/runs/REPLACE_SAME_EXACT_RUN.json
aws_read() {
  env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root \
    AWS_CONFIG_FILE=/dev/null AWS_SHARED_CREDENTIALS_FILE=/dev/null AWS_PAGER= \
    aws --region eu-west-2 --no-cli-pager s3api get-object \
    --bucket "$bucket" --expected-bucket-owner "$account" --checksum-mode ENABLED "$@"
}
aws_read --key "$marker_key" complete.json >/dev/null
aws_read --key "$manifest_key" manifest.json >/dev/null
aws_read --key "$archive_key" database.dump >/dev/null
python3 - "$marker_key" "$archive_key" "$manifest_key" <<'PY'
import hashlib
import json
from pathlib import Path
import re
import sys

marker_key, archive_key, manifest_key = sys.argv[1:]
match = re.fullmatch(r'(staging|production)/completed/(\d{8}T\d{6}Z-[0-9a-f]{32})\.json', marker_key)
if not match:
    raise SystemExit('Invalid exact completion key')
environment, run = match.groups()
base = environment + '/runs/' + run
if (archive_key, manifest_key) != (base + '.dump', base + '.json'):
    raise SystemExit('Mismatched selected object keys')
marker = json.loads(Path('complete.json').read_text())
if marker['format_version'] != 1 or marker['environment'] != environment:
    raise SystemExit('Mismatched completion marker')
for field, key, filename in (
    ('archive', archive_key, 'database.dump'),
    ('manifest', manifest_key, 'manifest.json'),
):
    reference = marker[field]
    path = Path(filename)
    with path.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    if reference['key'] != key or reference['size'] != path.stat().st_size or reference['sha256'] != digest:
        raise SystemExit('Downloaded object integrity mismatch')
manifest = json.loads(Path('manifest.json').read_text())
if (manifest['environment'] != environment or manifest['archive_size'] != Path('database.dump').stat().st_size
        or manifest['archive_sha256'] != marker['archive']['sha256']):
    raise SystemExit('Manifest/archive mismatch')
print('Selected archive and manifest SHA-256/size verified')
PY
```

Run each step separately and stop on any failure. Do not proceed merely because
an archive table of contents can be listed. Record the backup's snapshot/start
time; upload completion is not the recovery point.

## Restore into a new disposable PostgreSQL 17 instance

Select the trusted PostgreSQL 17 image digest matching the source environment,
including required extensions and compatible locale. The example container uses
an isolated network namespace, no ports, no host mounts, no production credentials
and a capped temporary data directory. It must run on the disposable host, never
as `vellum-staging` or `vellum-production`. Increase its resource limits only after
checking the drill host's available capacity. The 512 MiB memory limit also
limits usable tmpfs storage; a 2 GiB tmpfs ceiling does not provide 2 GiB of
available RAM. For larger restored data, use a separately sized disposable
encrypted disk and adequate memory on the drill host.

```sh
PG_IMAGE=postgres@sha256:REPLACE_APPROVED_POSTGRES17_DIGEST
case "$PG_IMAGE" in postgres@sha256:*) ;; *) exit 2 ;; esac
drill=vellum-restore-drill-$(date -u +%Y%m%dT%H%M%SZ)-$(openssl rand -hex 4)
docker run -d --name "$drill" --network none --memory 512m --cpus 1 \
  --shm-size 128m --tmpfs /var/lib/postgresql/data:rw,nosuid,size=2g \
  -e POSTGRES_USER=drill -e POSTGRES_DB=restorecheck \
  -e POSTGRES_HOST_AUTH_METHOD=trust "$PG_IMAGE"
# Wait for readiness, with a bounded retry count; stop if readiness never succeeds.
for attempt in $(seq 1 30); do
  if docker exec "$drill" pg_isready -U drill -d restorecheck >/dev/null 2>&1; then break; fi
  sleep 2
done
docker exec "$drill" pg_isready -U drill -d restorecheck
docker exec -i "$drill" pg_restore --exit-on-error --single-transaction \
  --no-owner --no-acl -U drill -d restorecheck < database.dump
```

Trust authentication is confined to this disposable container with no networking
or production secrets. Restoring an archive executes SQL selected by its source
superuser, so only use trusted dumps and maintain isolation. The dump creates
objects owned by the disposable restore role. Do not use `--clean`, `dropdb`,
`down -v`, volume pruning or the live deployment helper for a verification drill.

Before launching Vellum, inspect expected tables, row counts, indexes, constraints,
foreign-key/orphan relationships, budget user/category uniqueness and transaction
and transfer totals. For Vellum dumps, a basic non-sensitive integrity check is:

```sh
docker exec -i "$drill" psql -X -v ON_ERROR_STOP=1 -U drill -d restorecheck <<'SQL'
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM budgets GROUP BY user_id, category_id HAVING count(*) > 1) THEN
    RAISE EXCEPTION 'duplicate budget allocation';
  END IF;
  IF EXISTS (SELECT 1 FROM transfers t
             LEFT JOIN accounts a ON a.id=t.from_account_id
             LEFT JOIN accounts b ON b.id=t.to_account_id
             LEFT JOIN users u ON u.id=t.user_id
             WHERE a.id IS NULL OR b.id IS NULL OR u.id IS NULL OR t.amount <= 0 OR t.from_account_id=t.to_account_id
                OR t.user_id<>a.user_id OR t.user_id<>b.user_id) THEN
    RAISE EXCEPTION 'invalid transfer ownership or amount';
  END IF;
  IF EXISTS (SELECT 1 FROM transactions t
             LEFT JOIN accounts a ON a.id=t.account_id
             LEFT JOIN users u ON u.id=t.user_id
             LEFT JOIN categories c ON c.id=t.category_id
             WHERE a.id IS NULL OR u.id IS NULL OR c.id IS NULL OR t.user_id<>a.user_id) THEN
    RAISE EXCEPTION 'invalid transaction relationships';
  END IF;
END $$;
SQL
```

This check is not exhaustive. Compare expected counts/financial aggregates against
private snapshot evidence when available, respecting writes made after the backup.
Account balances are calculated by application queries rather than persisted
balance columns. Retain aggregate comparisons privately; do not publish account
contents, passwords, token hashes or reset tokens as test evidence.

Then run the exact retained application digest against this disposable database
on a dedicated internal drill network, with a fresh drill JWT secret and no
production credentials or public routing. This requires explicitly adapting the
network isolation above; attach only the drill database and drill application,
never `web` or live services. Test controlled login/authorization, financial
calculations and representative API operations. Vellum runs AutoMigrate and
seeding at startup: record schema before startup, test the matching old image
first, and test an upgrade on a separate restored copy. Do not use a real user's
credentials or restore production sessions into a public application.

Record download/hash/restore/startup/check durations, image digests, selected keys,
backup recovery point and result. RTO remains unmeasured until this is done.
Delete only the uniquely recorded drill container after inspection:

```sh
docker inspect "$drill" --format '{{.Name}} {{.Config.Image}}'
# Only after confirming this is the disposable container created above:
docker rm -f "$drill"
```

Its tmpfs data disappears with the container. Remove only the recorded private
working directory after retaining appropriate non-secret evidence. Never run
broad Docker cleanup commands. Repeat the drill after significant backup/schema
changes. Live recovery, downtime, write loss and restored-session invalidation
are separate operator decisions; this runbook never replaces a live database.
