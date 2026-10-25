# Vellum on an existing EC2 server

This stack runs on Ubuntu 24.04 with Docker Engine and Docker Compose v2
(2.24 or newer). It uses the existing Caddy container and external network `web`.
It creates no Caddy service, changes no server configuration, and publishes no
ports. Run all commands below on the EC2 server unless stated otherwise.

The optional, initially inactive registry/OIDC/SSM pipeline and its manual
activation prerequisites are documented in [CI-CD.md](CI-CD.md). This page
retains the local SHA-tagged manual deployment and recovery procedure.

## 1. Prepare the checkout

Use this layout:

```text
/opt/hosting/projects/vellum/        Git checkout
  backend/
  frontend/
  deploy/
    production.env                 ignored, mode 600
    staging.env                    ignored, mode 600
    backups/                       ignored, sensitive database dumps
```

Have the server administrator create `/opt/hosting/projects/vellum/`, owned by
the deployment user, and clone this repository there using its existing Git
access. Do not copy a developer's `.env` files or database volume. Then:

```sh
cd /opt/hosting/projects/vellum
docker compose version
docker network inspect web
git status --short
```

Verify the existing Caddy container is attached to `web`. DNS A records for
`vellum.kacpergajdarski.com` and `staging.vellum.kacpergajdarski.com` must point
to this server; only publish AAAA records if IPv6 actually reaches it. Keep the
existing Caddy listeners on 80/443. EC2 security groups need no inbound rules
for 5432, 8080 or 5173; restrict SSH or use SSM for administration.

## 2. Configure each environment

```sh
cd /opt/hosting/projects/vellum
umask 077
cp -n deploy/production.env.example deploy/production.env
cp -n deploy/staging.env.example deploy/staging.env
chmod 600 deploy/production.env deploy/staging.env
openssl rand -hex 32
```

Edit both files with an editor. Generate four independent random values: a
database password and JWT secret for each environment. Hex values avoid the
current backend DSN's whitespace/quote escaping limitations. Leave no blank
required values. Never commit these files, pass secrets as build arguments, or
paste resolved `docker compose config` output into logs; it contains secrets.
Use `config --quiet` for validation.

Set `TRUSTED_PROXY_CIDRS` in both environment files to Caddy's exact `/32` or
`/128` address on `web` if stable. Otherwise use the exact subnet of the external
`web` network and restrict its membership: every container on a trusted subnet
can impersonate a proxy. Find that subnet with:

```sh
docker network inspect web --format '{{(index .IPAM.Config 0).Subnet}}'
```

Do not use an all-address CIDR or a broader private-network range. If Caddy's
trusted address or subnet changes, update this value before restarting the
backend. Local direct development trusts no proxies by default. The backend
only accepts XFF from trusted peers and ignores X-Real-IP. Keep Caddy's default
replacement of public incoming XFF; review its global trusted-proxy settings
before adding a CDN/load balancer. See `backend/README.md` for limiter defaults,
resource guards, and restart/saturation limitations. Before production rollout,
verify on staging that forged XFF/X-Real-IP do not obtain fresh allowances and
that distinct external clients are identified independently. No backend port
should be reachable from outside Docker.

Set `IMAGE_TAG` to the full output of `git rev-parse HEAD` for the revision being
built. Build only a clean checkout with the deployment files committed; otherwise
the tag would misrepresent its contents. Initially leave `IMAGE_PREFIX=vellum`.
Keep these environment identities stable:

| File | Project | Caddy aliases on `web` |
| --- | --- | --- |
| `production.env` | `vellum-production` | `vellum-production-backend`, `vellum-production-frontend` |
| `staging.env` | `vellum-staging` | `vellum-staging-backend`, `vellum-staging-frontend` |

Compose scopes `postgres_data` and the internal `database` network by project.
Never give volumes a global `name`, reuse project names, or change a project's
name during an upgrade. Each project has its own PostgreSQL 17 container,
database, credentials, JWT secret, volume and database network. Only the API
joins both its database network and `web`; the frontend joins `web` only.
The shared `web` network is trusted proxy infrastructure, not a security boundary
between application containers. Caddy must use the unique aliases above, not
the ambiguous automatic service names `backend` or `frontend`.

Compose shell variables override values in `--env-file`. Use a deployment shell
without exported `COMPOSE_PROJECT_NAME`, `DB_*`, `JWT_*`, `APP_ORIGIN`, `IMAGE_TAG`,
`IMAGE_PREFIX`, `TRUSTED_PROXY_CIDRS`, or `RATE_LIMIT_*` values. Always pass the
matching explicit project and env file.

## 3. Build and start staging first

```sh
cd /opt/hosting/projects/vellum
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml config --quiet
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml build --pull
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml up -d --no-build --wait --wait-timeout 180
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml ps
```

The API waits for PostgreSQL's `pg_isready` health check before its first start.
It then runs the existing GORM migrations and category seeding before listening.
All three services have health checks and `unless-stopped` restart policies.
Docker restarts crashed processes; it does not automatically restart a merely
unhealthy container. Monitor health and disk space. A database restart after
host reboot may race the API; its restart policy retries failed API starts.
The API `/ping` check confirms HTTP startup, not ongoing database readiness.

API/frontend run without root, with read-only filesystems, writable temporary
directories, dropped capabilities and `no-new-privileges`. Log files rotate at
10 MB, retaining three files per container. PostgreSQL writes only to its data
volume and uses the image's normal initialization/privilege handling. Its
credentials are also the initial PostgreSQL administrator credentials; a future
versioned-migration workflow should use a separate least-privilege runtime role.

## 4. Integrate routing with the existing Caddy

`Caddyfile.example` is an integration example, not a replacement for the existing
configuration. Have the Caddy operator merge its two site blocks into the active
Caddyfile (or an existing imported snippet directory), preserving all unrelated
sites, global options, certificate volumes and the current deployment.
Do not start another Caddy container.

Validate the merged file using the existing container's name and actual config
path, then reload it through its normal operational procedure. For example,
if those are `caddy` and `/etc/caddy/Caddyfile`:

```sh
docker exec caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

The example sends `/api`, `/api/*` and `/ping` unchanged to Go. **POST** and **OPTIONS**
requests to `/register`, `/login`, `/refresh` and `/logout` go to Go; **GET**
`/login` goes to React. Do not use `handle_path`, which would strip `/api`.
Nginx supplies SPA fallback for browser deep links and returns 404 for missing
hashed assets. Normal production auth calls are same-origin; forwarding OPTIONS
also preserves configured cross-origin preflights. `APP_ORIGIN` configures the
backend's exact CORS origin.

```sh
curl --fail https://staging.vellum.kacpergajdarski.com/ping
curl --fail -I https://staging.vellum.kacpergajdarski.com/login
```

Expect JSON from `/ping` and HTML from GET `/login`. Exercise registration,
login, token refresh, CRUD, and a direct browser deep link in staging. Check that
POST `/login` with invalid credentials returns an API error rather than HTML.

## 5. Start production

After staging passes, put the tested SHA in `production.env`. The frontend uses
`VITE_API_BASE_URL=/` at build time, so the same images work in both environments.
If that SHA was already built, skip rebuilding and use those exact local images.

```sh
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml config --quiet
# Only necessary when these SHA-tagged images are not already present:
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml build --pull
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml up -d --no-build --wait --wait-timeout 180
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml ps
curl --fail https://vellum.kacpergajdarski.com/ping
```

Never rebuild an already promoted SHA tag; promote the tested image artifact.
Both environments may run different SHA tags. No application or database `ports`
are published; Dockerfile `EXPOSE` is metadata and does not open an EC2 port.

## 6. Updates, rollback and operations

Before upgrading, back up the target database and retain the previous image tag.
Fetch/check out the intended committed revision, build and test staging, then
promote the same images/tag to production. Re-run `up -d --no-build --wait` for
the relevant project. Expect a brief interruption when its API container is
replaced; this is a single-instance deployment.

The current server always runs GORM AutoMigrate, including on rollback. It has
no migration-only command, migration lock, down migration or schema history.
Run one backend replica per project. Review schema changes before release and
prefer backward-compatible changes. Reverting `IMAGE_TAG` is safe only if the
older binary supports the new schema; otherwise restore the pre-upgrade backup
and run the matching old image. Do not run a second backend as a migration job.

```sh
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml logs --tail=100 backend
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml stop
# Start it again with the up command above.
```

`down` retains the named database volume; **never use `down -v` for normal
operations**. Do not prune volumes. Updating `DB_PASSWORD`, `DB_USER` or `DB_NAME`
in an env file does not alter an initialized PostgreSQL cluster. Coordinate SQL
role changes with application credential changes. Rotating `JWT_SECRET`
invalidates existing access tokens; plan session handling accordingly. Runtime
environment variables are visible to users with Docker/root access, so restrict
that access and protect the environment files and backups.

The application still stores browser tokens in localStorage. Review CSP/session
policy and add authentication abuse protection in the existing edge deployment
before broad public access. These deployment files do not change application
authentication behavior or add rate-limiting plugins to Caddy.

## 7. Backups and restore

Backups contain financial data and token hashes. The supplied Ubuntu script
creates a consistent custom-format `pg_dump`, checks its archive table of
contents, and atomically renames a mode-600 file. It prevents overlapping backups
of the same environment. This quick archive check is not a restore test.

```sh
cd /opt/hosting/projects/vellum
bash deploy/backup.sh production
bash deploy/backup.sh staging
```

Files are stored in `deploy/backups/<environment>/`. The script uses local Unix
socket authentication inside the PostgreSQL container and never needs to expose
its port or put a password on the command line. Dumps omit ownership and ACLs;
restore creates objects owned by the destination database user. Cluster roles
are not included: retain the protected environment configuration separately.

First practice restoring into a **new disposable database** in staging; never
test a production dump in a publicly accessible staging app. A restore can be
checked with `pg_restore --exit-on-error --single-transaction --no-owner --no-acl`
against an empty test database, followed by application/schema/data checks. Use
synthetic backups for public staging, and a private isolated drill for real data.

To replace an existing production database during recovery, schedule downtime,
select the matching application SHA and take an additional backup of the current
database. Double-check the project, env file and archive before proceeding.
The following commands **delete and recreate that production database**:

```sh
cd /opt/hosting/projects/vellum
# Set this to an existing, verified archive; do not use a wildcard.
backup_file=/opt/hosting/projects/vellum/deploy/backups/production/REPLACE_WITH_VERIFIED_FILENAME.dump
test -s "$backup_file"
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml stop backend
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml exec -T postgres sh -ec 'dropdb --if-exists --force -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" -O "$POSTGRES_USER" "$POSTGRES_DB"'
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml exec -T postgres sh -ec 'exec pg_restore --exit-on-error --single-transaction --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < "$backup_file"
# Run only after successful restore; keep backend stopped if restore fails.
docker compose --project-name vellum-production --env-file deploy/production.env -f deploy/compose.yml up -d --no-build --wait --wait-timeout 180
```

Run recovery commands individually and stop on any error; do not blindly paste
the entire block into a shell that continues after failures. Restoring loses all
writes after the snapshot. Verify account totals, login and resource ownership
after recovery. Keep prior dumps until recovery has been verified.

For off-server backups, install AWS CLI and attach an EC2 instance role with
permission to upload only to the intended S3 prefixes; do not store AWS access
keys in these env files. Configure a private bucket with public access blocked,
default encryption (SSE-KMS if required), versioning and lifecycle retention.
For example, after selecting the actual bucket:

```sh
bash deploy/backup.sh production s3://YOUR-PRIVATE-BACKUP-BUCKET/vellum/production
```

Schedule this command with a systemd timer or cron under the deployment user
(Docker access is root-equivalent). For example, with the correct bucket name,
this user-crontab entry runs at 02:15 in the server's timezone:

```cron
15 2 * * * /bin/bash /opt/hosting/projects/vellum/deploy/backup.sh production s3://YOUR-PRIVATE-BACKUP-BUCKET/vellum/production
```

Schedule staging separately if needed. Capture failures and alert on a missing
fresh object, not just a successful timer invocation. Monitor local disk usage
and configure local retention only after verifying uploads. Protect production
backups from staging credentials, consider a separate account/Object Lock, and
perform regular private restore drills from S3. Daily dumps can lose up to a
day of writes; use WAL archiving/PITR or managed PostgreSQL if that is unacceptable.

## 8. Future registry / GitHub Actions workflow

No Actions workflow is included. Both services already use
`${IMAGE_PREFIX}-backend:${IMAGE_TAG}` / `${IMAGE_PREFIX}-frontend:${IMAGE_TAG}`
and carry an OCI revision label. A future pipeline should build once per full
commit SHA, scan/test both images, push them, deploy staging, and promote those
same immutable artifacts to production. Match the build platform to EC2
(`linux/amd64` for x86 instances or `linux/arm64` for Graviton), or publish a
multi-platform manifest. Never overwrite a SHA tag; retain a digest for audit.
Base images follow their declared version tags; pin reviewed image digests for
fully repeatable builds and refresh them deliberately for security updates.

Set `IMAGE_PREFIX=ghcr.io/<lowercase-owner>/vellum` in each env file when switching
to GHCR (or use an ECR repository prefix). Authenticate using the registry's
normal mechanism, then:

```sh
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml pull
docker compose --project-name vellum-staging --env-file deploy/staging.env -f deploy/compose.yml up -d --no-build --pull never --wait --wait-timeout 180
```

The explicit `--no-build` prevents rebuilding registry artifacts on EC2.

## Reference behavior

- [Compose readiness dependencies](https://docs.docker.com/reference/compose-file/services/#depends_on)
- [Compose interpolation and precedence](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)
- [Unprivileged Nginx runtime](https://github.com/nginx/docker-nginx-unprivileged)
- [Caddy method/path matchers](https://caddyserver.com/docs/caddyfile/matchers)
- [PostgreSQL 17 dumps and restore](https://www.postgresql.org/docs/17/backup-dump.html)
