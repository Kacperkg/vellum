# Vellum

Vellum is a personal finance application for people who want to track their money
manually across accounts, spending categories, budgets, and savings goals. It
brings recorded balances and activity into one place, with summaries that help
explain where money goes and how much remains available.

[Production application](https://vellum.kacpergajdarski.com) ·
[Staging environment](https://staging.vellum.kacpergajdarski.com)

![Vellum overview with synthetic demo data](docs/screenshots/overview.png)

## Features

- **Accounts and activity:** create and manage accounts, record income and
  expenses, and search, filter, sort, edit, or export visible activity as CSV.
- **Transfers:** move recorded money between accounts without inflating income,
  expenses, or budget spending. Editing or deleting a transfer updates both sides.
- **Budgets:** set category limits and compare them with current-month spending.
- **Savings goals:** enter progress manually and optionally link goals to an
  account. Linked targets reserve an allocation and show remaining availability
  or a shortfall; they do not move money.
- **Summaries:** review account balances, six-month income/expense charts, and an
  expense-category breakdown.
- **Recurring activity:** view entries marked daily, weekly, monthly, or yearly
  together on the subscriptions page.
- **User access and preferences:** register, log in, refresh sessions, log out,
  and save display preferences across desktop and mobile layouts.

Vellum is a manual tracker. Frequency labels do not schedule payments or generate
future ledger entries. Bank synchronization, opening balances, and currency
conversion are not implemented. Budgets are current category limits rather than
historical budget periods.

## Technology and architecture

| Area                 | Technologies                                                                                             |
| -------------------- | -------------------------------------------------------------------------------------------------------- |
| Frontend             | React 19, TypeScript 6, Vite 8, TanStack Router, Tailwind CSS 4, Axios                                   |
| Backend              | Go 1.26.10, Gin, GORM                                                                                    |
| Database             | PostgreSQL 17; money stored in integer minor units                                                       |
| Development          | Docker Compose, Air, Node.js 24.22.1, pnpm 10.34.8                                                       |
| Hosting and delivery | AWS EC2, Caddy, Nginx, GHCR, GitHub Actions, AWS OIDC and Systems Manager                                |
| Verification         | Go tests and race detector, PostgreSQL integration tests, Node test runner, Playwright, ESLint, Prettier |

The React client calls the Go API through typed Axios modules. Gin routes pass
requests through authentication middleware to handlers, services, and GORM
repositories. PostgreSQL stores user-owned resources; account balances and goal
allocations are calculated on the server.

Local Compose runs Vite, the API with Air reload, and PostgreSQL. Deployment uses
separate production Dockerfiles: Nginx serves the built frontend, and a compiled
Go binary serves the API. An existing Caddy proxy terminates HTTPS and routes
browser/API traffic on EC2. Staging and production have separate Compose projects,
databases, credentials, and volumes, while sharing the host and Caddy proxy.
Deployment services do not publish application or database ports to the host.

## Getting started

### Prerequisites

For the container workflow, install Git, Docker Engine or Docker Desktop, and
Docker Compose v2. For host development, also use **Go 1.26.10**, **Node.js
24.22.1**, and **pnpm 10.34.8**, matching the repository's CI and toolchain files.

```sh
git clone https://github.com/Kacperkg/Vellum.git
cd Vellum
cp backend/.env.example backend/.env
```

Replace `JWT_SECRET` in `backend/.env` with a long random development secret.
The remaining example values match the local Compose database. Keep this file
private and untracked.

### Run with Docker Compose

From the repository root, start PostgreSQL first and wait until it is ready:

```sh
docker compose up -d postgres
docker compose exec postgres pg_isready -U vellum -d vellum
```

If the readiness check reports no response, repeat it before starting the app:

```sh
docker compose up --build
```

| Service          | Local address                |
| ---------------- | ---------------------------- |
| Web client       | <http://localhost:5173>      |
| REST API         | <http://localhost:8080>      |
| API health check | <http://localhost:8080/ping> |
| PostgreSQL       | `localhost:5432`             |

Register a user in the browser. API startup runs GORM migrations and seeds the
canonical categories; no manual schema import is needed. These Compose settings
are for development on a trusted machine: they use local database defaults and
publish ports on host interfaces. Use the deployment configuration for hosting.

Stop the local stack with `docker compose down`. Its named PostgreSQL volume is
retained; avoid volume removal if you want to preserve local records.

### Run the API and frontend on the host

Start PostgreSQL with the same commands above, then set `DB_HOST=localhost` in
`backend/.env` (the container workflow uses `DB_HOST=postgres`). In separate
terminals, starting from the repository root:

```sh
cd backend
go run ./cmd/server
```

```sh
cd frontend
pnpm install --frozen-lockfile
pnpm dev
```

The addresses are the same as in the table above. The client defaults to
`http://localhost:8080`; set `VITE_API_BASE_URL` before starting Vite to override
it. The API's `CORS_ALLOWED_ORIGINS` defaults to `http://localhost:5173`.
Stop each host process with Ctrl+C and stop the database with
`docker compose down` from the repository root.

## Testing and quality checks

Run frontend checks from `frontend/` after installing dependencies:

```sh
pnpm test                 # finance regressions and architecture boundaries
pnpm exec tsc -b           # application TypeScript checks
pnpm test:e2e:check        # browser-test TypeScript checks
pnpm lint
pnpm format:check
pnpm build                # application TypeScript checks and production bundle
```

Run backend checks from `backend/`:

```sh
go build ./...
go vet ./...
go test ./...
```

Without `TEST_DATABASE_DSN`, PostgreSQL integration tests skip. For the complete
suite, point that variable at a **disposable test database**, then run:

```sh
go test -count=1 -p 1 ./...
go test -race -count=1 -p 1 ./...
```

Integration tests run migrations and create/drop test schemas. Never point them
at a personal or deployed database. See the [backend testing guide](backend/README.md#testing)
for DSN configuration. CI provides PostgreSQL and rejects unexpected test skips
when the backend checks are selected.

Browser tests require Docker, Go, frontend dependencies, and Chromium. From
`frontend/`:

```sh
pnpm exec playwright install chromium
pnpm test:e2e
```

On Linux, Playwright may also require system browser dependencies; CI uses
`pnpm exec playwright install --with-deps chromium`. The runner creates its own
throwaway PostgreSQL container and dedicated API/frontend servers on ports
**18080/15173**, refuses occupied ports, and cleans up afterward. It covers
authentication, transfers, goal allocations, budgets, activity, preferences, and
mobile navigation using synthetic records. See the
[frontend testing guide](frontend/README.md#browser-tests-and-screenshots) for
failure artifacts and screenshot generation.

## Development and delivery

Use `feature/*`, `fix/*`, or `chore/*` branches and open focused pull requests
against `staging`. Discuss larger changes before implementation and include
relevant validation. `main` is the production branch; promotion and merging
require explicit maintainer approval.

[CI](.github/workflows/ci.yml) runs on pull requests to `staging` and `main` and is
reused for staging pushes by the release workflow. Checks cover frontend quality,
Go/PostgreSQL tests including race detection, browser workflows, Docker secret
exclusions, deployment safety, and production image builds. A change plan selects
relevant work while keeping check contexts present; documentation-only root
README changes do not request an application release. See
[CI check selection](deploy/CI-OPTIMISATION.md) for the details.

The [staging release workflow](.github/workflows/release-staging.yml) validates
eligible pushes to protected `staging`. When delivery is enabled and changes
require a release, it builds and publishes images to GHCR and deploys through
AWS OIDC and Systems Manager. Release manifests record
immutable image digests and staging verification evidence. The
[delivery guide](deploy/CI-CD.md) records a successful end-to-end staging release.

**Production deployment automation remains disabled:** its workflow is stored as
[`release-production.yml.disabled`](.github/workflows/release-production.yml.disabled).
The prepared promotion design reuses verified staging image digests through a
reviewed release manifest on `main`, with separate approval and backup/schema
gates. It is not an active production pipeline. Consult the
[deployment runbook](deploy/README.md) for manual operations and recovery.

## Security and operational limits

- Passwords use bcrypt. Access tokens use JWTs; random refresh tokens are stored
  hashed, rotated transactionally, and revoked on logout.
- Protected API routes validate access tokens, with server-side ownership checks
  for user resources. Authentication endpoints have in-memory rate limiting;
  application restart resets limiter state.
- Browser tokens currently live in `localStorage`. Browser script compromise
  remains a session risk; session storage and CSP policy need continued review.
- Production API/frontend containers run without root, with read-only filesystems
  and dropped capabilities. Docker ignore rules exclude runtime environment files
  and credential patterns from builds.
- Staging and production share an EC2 host and proxy network. Separate database
  volumes do not provide independent infrastructure failure boundaries.
- Schema changes run through startup AutoMigrate. Rolling back an image does not
  reverse a database migration; backup and compatibility review remain necessary.

## Project status

The features above are implemented in `staging`. The public environments can run
different revisions; this README does not establish that every current feature
has been promoted to production. Staging deployment is recorded in the delivery
guide; production promotion is controlled separately.

| Area                                             | Current repository status                                                                 |
| ------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| Production runtime images and staging delivery   | Implemented; staging release verification recorded                                        |
| Production deployment automation                 | Prepared workflow remains disabled                                                        |
| AWS SES password recovery and email verification | Not implemented                                                                           |
| Automated PostgreSQL backups to S3               | Not configured; manual backup tooling exists, with migration prerequisites in the runbook |
| Terraform and SOPS migration                     | Not implemented; future infrastructure work                                               |

## Further reading

- [Frontend structure and authentication](frontend/README.md)
- [Backend API contracts, configuration, and testing](backend/README.md)
- [Deployment and recovery](deploy/README.md)
- [CI/CD and promotion design](deploy/CI-CD.md)

## Licence

No licence file is currently included. Public visibility does not grant an
open-source licence; contact the maintainer about reuse or licensing.
