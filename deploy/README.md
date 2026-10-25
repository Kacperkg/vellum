# Vellum on an existing EC2 server

This stack runs on Ubuntu 24.04 with Docker Engine and Docker Compose v2
(2.24 or newer). It uses the existing Caddy container and external network `web`.
It creates no Caddy service, changes no server configuration, and publishes no
ports. Run all commands below on the EC2 server unless stated otherwise.

The registry/OIDC/SSM pipeline and its manual
activation prerequisites are documented in [CI-CD.md](CI-CD.md). This page
retains the local SHA-tagged manual deployment and recovery procedure.

## 1. Prepare the checkout

Use this layout:

```text
/opt/hosting/projects/vellum/        Git checkout
  backend/
  frontend/
  deploy/
    backups/                       existing ignored, sensitive database dumps
/etc/vellum-cd/                     root-owned 700, outside the checkout
  production.env                   root-owned 600
  staging.env                      root-owned 600
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

For an existing installation, use [ENVIRONMENT-MIGRATION.md](ENVIRONMENT-MIGRATION.md)
to copy the existing files byte-for-byte into `/etc/vellum-cd/`. Do not regenerate
credentials, change project names or install templates over initialized settings.
The checkout and its parent directories remain writable by ubuntu.

For a brand-new environment only, have the administrator create root-owned
mode-600 files from the corresponding `.env.example` under root-owned
`/etc/vellum-cd/` (700), refusing existing destination files. Fill every required
value privately with `sudoedit`. New environments need four independent random
values: database password and JWT secret for each. Existing environments retain
their exact values. Never source dotenv files as shell scripts, commit them,
pass secrets as build arguments or print resolved Compose output.

All manual Compose examples below use this shell helper. It removes exported
application/Compose overrides, runs as root to read the managed file, and retains
the explicit project name. Use only reviewed checkout code for local source
builds; sudo-running an unreviewed writable-checkout script/Compose file is unsafe.

```sh
vellum_compose() {
  sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root DOCKER_CONFIG=/etc/vellum-cd/docker docker compose "$@"
}
```

For the installed CD controller, retain `TRUSTED_PROXY_CIDRS=172.18.255.254/32`
in both managed files. The more general manual-network guidance below does not
relax that controller preflight. Set Caddy's exact `/32` or
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
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml config --quiet
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml build --pull
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml up -d --no-build --wait --wait-timeout 180
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml ps
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

After staging passes, put the tested SHA in `/etc/vellum-cd/production.env` using sudoedit. The frontend uses
`VITE_API_BASE_URL=/` at build time, so the same images work in both environments.
If that SHA was already built, skip rebuilding and use those exact local images.

```sh
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml config --quiet
# Only necessary when these SHA-tagged images are not already present:
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml build --pull
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml up -d --no-build --wait --wait-timeout 180
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml ps
curl --fail https://vellum.kacpergajdarski.com/ping
```

Never rebuild an already promoted SHA tag; promote the tested image artifact.
Both environments may run different SHA tags. No application or database `ports`
are published; Dockerfile `EXPOSE` is metadata and does not open an EC2 port.

## 6. Updates, rollback and operations

For controller receipts predating the `/etc` migration, read the rollback
compatibility section in ENVIRONMENT-MIGRATION.md first; do not replay old
controllers or rewrite manifest hashes to bypass the bundle check.

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
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml logs --tail=100 backend
vellum_compose --project-name vellum-production --env-file /etc/vellum-cd/production.env -f deploy/compose.yml stop
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

The proposed daily S3 workflow is documented in
[backup/README.md](backup/README.md), with an
[isolated restore runbook](backup/RESTORE.md). It uses root-installed
`backup.sh`/`backup.py`, the existing `/etc/vellum-cd/` environment files and
trusted installed Compose configuration. Never sudo-run checkout scripts.

The policy is daily production/staging dumps, mandatory verified S3 upload,
SSE-S3 in `eu-west-2`, and two-day lifecycle expiration. No AWS resources,
root-installed files or schedules are configured by a repository change. Follow
the operator inventory, approval, installation, S3 round-trip restore and timer
enablement checklist before declaring backups operational. Existing dumps and
PostgreSQL volumes remain untouched.

Daily snapshots can lose nearly 24 hours of writes plus backup/upload delay;
failed jobs extend that exposure. Two-day retention cannot recover corruption
discovered after retained copies expire. RTO must be measured in a drill.
`pg_restore --list` is only an archive-list check. Never test a production dump
in the live staging database or a publicly routed application. Live recovery,
write loss and schema-incompatible rollback require separate operator approval;
image rollback does not undo AutoMigrate or database writes.

## 8. Future registry / GitHub Actions workflow

The staging Actions workflow and production promotion design are documented in CI-CD.md. Both services already use
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
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml pull
vellum_compose --project-name vellum-staging --env-file /etc/vellum-cd/staging.env -f deploy/compose.yml up -d --no-build --pull never --wait --wait-timeout 180
```

The explicit `--no-build` prevents rebuilding registry artifacts on EC2.

## Reference behavior

- [Compose readiness dependencies](https://docs.docker.com/reference/compose-file/services/#depends_on)
- [Compose interpolation and precedence](https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/)
- [Unprivileged Nginx runtime](https://github.com/nginx/docker-nginx-unprivileged)
- [Caddy method/path matchers](https://caddyserver.com/docs/caddyfile/matchers)
- [PostgreSQL 17 dumps and restore](https://www.postgresql.org/docs/17/backup-dump.html)
