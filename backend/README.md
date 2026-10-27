# Vellum Backend

## Money amounts

All integer money fields use minor currency units. For the currently supported
GBP, USD, and EUR currencies, `100` represents one major currency unit (for
example, £1.00, $1.00, or €1.00). Clients convert decimal user input to minor
units before sending requests and convert back for display.

Central REST API for Vellum. It owns PostgreSQL data, authentication,
authorization, validation, and business rules for the browser client.

## Commands

Run these from `backend/`:

```bash
go run ./cmd/server                         # run locally
go build ./...                              # compile every package
go test ./...                               # run unit tests
go test -race ./...                         # run tests with race detection
go vet ./...                                # static analysis
go fmt ./...                                # format Go source
```

The server listens on the configured `PORT` and runs GORM migrations during
startup.

Copy `.env.example` to `.env` for local development and replace its placeholder
JWT secret. Compose supplies that file through `env_file` at runtime; it is
excluded from backend image builds. Go still loads local dotenv configuration
when run directly from this directory. Neither image should contain real
environment files, private keys, or credentials.

## Architecture

```text
routes and middleware
          ↓
       handlers
          ↓
       services
          ↓
      repository
          ↓
   GORM/PostgreSQL
```

- `handlers` own HTTP request binding and response status codes.
- `services` own validation, business rules, ownership checks, and API contracts.
- `repository` owns database access and translates persistence failures.
- `apperrors` provides shared, dependency-free error categories.
- `models` contains GORM persistence models.

HTTP-specific request structs stay private to handlers. Services accept their
own input contracts and return response types that do not expose persistence
internals such as password hashes.

## Authentication

Registration and login return a short-lived HS256 JWT access token, an opaque
refresh token, and the authenticated user.

- Access tokens authenticate protected `/api` routes through the Bearer header.
- Refresh tokens are random values; only SHA-256 hashes are stored.
- `POST /refresh` atomically revokes the submitted refresh token and creates its
  replacement.
- `POST /logout` revokes the submitted refresh token.
- Reused, expired, or revoked refresh tokens are rejected.
- Registration creates its user, default settings, and initial refresh-token
  hash in one repository transaction. A persistence failure rolls everything
  back and returns no credentials; concurrent duplicate emails return 409.

```http
Authorization: Bearer <access-token>
```

## Authenticated resource limits

Protected APIs enforce bounded request bodies, names, filters, date ranges,
rate/concurrency admission, request cancellation and PostgreSQL work. See
[the resource policy](RESOURCE_LIMITS.md) for exact limits, retry behavior,
performance measurements and the Issue #25 pagination compatibility boundary.
Complete transaction/transfer arrays and financial totals remain unchanged.

## Environment variables

Every variable is required. Expiry values use Go duration syntax.

| Variable             | Example               | Purpose                                            |
| -------------------- | --------------------- | -------------------------------------------------- |
| `PORT`               | `8080`                | HTTP server port.                                  |
| `DB_HOST`            | `localhost`           | PostgreSQL host. Use `postgres` in Docker Compose. |
| `DB_PORT`            | `5432`                | PostgreSQL port.                                   |
| `DB_USER`            | `vellum`              | PostgreSQL user.                                   |
| `DB_PASSWORD`        | `password`            | PostgreSQL password.                               |
| `DB_NAME`            | `vellum`              | PostgreSQL database.                               |
| `DB_SSLMODE`         | `disable`             | PostgreSQL SSL mode.                               |
| `JWT_SECRET`         | `<long-random-value>` | Access-token signing secret.                       |
| `JWT_EXPIRY`         | `15m`                 | Access-token lifetime.                             |
| `JWT_REFRESH_EXPIRY` | `720h`                | Refresh-token lifetime.                            |

Local values may be placed in `backend/.env`; do not commit that file.

Optional `CORS_ALLOWED_ORIGINS` accepts a comma-separated list of frontend
origins. It defaults to `http://localhost:5173`. Configure this explicitly for
other origins, including isolated browser tests.
Optional `BIND_HOST` selects a listen interface; leaving it unset preserves the
existing all-interface development/Compose behavior. Isolated browser tests bind
their API to `127.0.0.1`.

### Authentication rate limiting

The four public authentication POST endpoints use a bounded, in-memory fixed
window limiter. A rejected request returns status `429`, JSON
`{"error":"rate limit exceeded"}`, and a `Retry-After` header containing the
number of seconds until the rejecting bucket expires, rounded up. Concurrency
rejections use an advisory one-second backoff. Responses include `Cache-Control:
no-store`, and CORS exposes `Retry-After` to frontend JavaScript.

The initial per-identity limits are deliberately stricter than the per-IP
safeguard:

| Endpoint    | Default per network + identity | Aggregate network limit | Identity |
| ----------- | ------------------------------ | ----------------------- | -------- |
| `/login`    | 10 per minute                  | 200 per minute          | Normalized email |
| `/register` | 5 per hour                     | 100 per hour            | Normalized email |
| `/refresh`  | 30 per minute                  | 600 per minute          | Refresh token |
| `/logout`   | 30 per minute                  | 600 per minute          | Refresh token |

Every attempt first consumes a network bucket with 20 times the endpoint limit,
including invalid bodies and identity-limit rejections. Different users behind
one NAT have independent identity buckets within that shared safeguard. Identity
limits are network-scoped: someone on another network cannot exhaust a known
email's allowance. Missing/malformed identities share a smaller fallback bucket.
Rate-counter keys are SHA-256 digests, not raw emails, tokens, or addresses.

IPv4 addresses (including mapped IPv6 representations) are canonicalized. IPv6
addresses share a bucket per `/64` to prevent privacy-address rotation from
resetting limits. This can combine several users on the same subnet, like NAT.
Successful refresh rotation creates a new token identity, but never resets the
aggregate network quota. Successful and unsuccessful attempts both count;
successful login does not reset counters. The limits allow normal retries and
refreshes with the default 15-minute access-token lifetime.

| Optional variable                         | Default | Purpose                                      |
| ----------------------------------------- | ------- | -------------------------------------------- |
| `RATE_LIMIT_LOGIN_REQUESTS`               | `10`    | Login requests per identity/window.          |
| `RATE_LIMIT_LOGIN_WINDOW`                 | `1m`    | Login window.                                |
| `RATE_LIMIT_REGISTER_REQUESTS`            | `5`     | Registration requests per identity/window.   |
| `RATE_LIMIT_REGISTER_WINDOW`              | `1h`    | Registration window.                         |
| `RATE_LIMIT_REFRESH_REQUESTS`             | `30`    | Refresh requests per identity/window.        |
| `RATE_LIMIT_REFRESH_WINDOW`               | `1m`    | Refresh window.                              |
| `RATE_LIMIT_LOGOUT_REQUESTS`              | `30`    | Logout requests per identity/window.         |
| `RATE_LIMIT_LOGOUT_WINDOW`                | `1m`    | Logout window.                               |
| `RATE_LIMIT_SHARED_IP_MULTIPLIER`          | `20`    | Multiplier for the secondary per-IP bucket.  |
| `RATE_LIMIT_MAX_ENTRIES`                  | `10000` | Hard cap across all in-memory limiter keys.  |
| `TRUSTED_PROXY_CIDRS`                     | empty (none) | Proxies allowed to supply client IP headers. |

Durations use Go duration syntax and all numeric values must be positive. The
store uses up to 32 independent locks and expiry min-heaps. Both counters and
heap nodes are hard-capped by `RATE_LIMIT_MAX_ENTRIES`. Only expired entries are
removed; a full shard rejects new keys until its earliest expiry, preserving
existing active quotas under unique-client floods. Each auth attempt also sweeps
one other shard; all shards are visited within 32 attempts. During complete
inactivity, expired allocations remain bounded until traffic resumes. There are
no background goroutines or per-key timers.

Auth bodies are limited to 64 KiB (413 above this). At most eight auth requests
may read bodies or run handlers concurrently, with at most two per network and
immediate 429 when busy. A single network cannot monopolize all eight slots.
The concurrency guard briefly retains at most eight canonical network keys and
deletes each when its last active request finishes.
The HTTP server has a 5-second header deadline, 15-second total read deadline,
30-second write deadline, 60-second idle timeout, and a 16-KiB header budget
(subject to Go's parser allowance). These guard resources rather than promise
protection against volumetric denial of service.

Gin accepts only `X-Forwarded-For` from explicitly trusted peers. `X-Real-IP`,
`Forwarded` and platform headers are ignored. Repeated or overlong XFF fields
fall back to the socket peer; malformed/missing IPs share an `unknown` bucket.
No proxy is trusted in direct local development, including loopback. For a
local reverse proxy, explicitly configure its loopback address if needed.

For Docker, prefer Caddy's exact `/32` or `/128` address when stable. Otherwise
use the exact external `web` subnet, obtained with
`docker network inspect web --format '{{(index .IPAM.Config 0).Subnet}}'`.
Trusting that subnet means trusting **every container on it** to assert client
addresses. Keep network membership controlled and do not publish backend ports.
Universal CIDRs are rejected; unnecessarily broad private ranges are unsafe.
Caddy normally replaces public incoming XFF; retain that behavior. If adding a
CDN or load balancer, reassess trust at both proxy layers before deploying.

State is local to one process: container restart, recreation, application crash,
or scaling to new replicas resets counters. Clients cannot directly request a
reset; inducing crashes could nevertheless weaken protection. Fixed windows
allow boundary bursts. Distributed networks can still guess passwords or create
accounts, and a full store temporarily denies new clients. This lightweight
single-instance limiter is not an account lockout, bot filter or durable abuse
history. Persistent edge controls or a shared store would be warranted when
scaling or responding to sustained distributed abuse; Redis is not required for
the current single-instance deployment.

The existing frontend clears local authentication on any failed refresh,
including 429, and its interceptor redirects to login. A transient throttle can
therefore require another sign-in; automatic `Retry-After` handling would be a
separate client change. Authentication handler and token-rotation behavior are
unchanged.

## Budget uniqueness and existing records

PostgreSQL enforces one budget for each `(user_id, category_id)` through
`idx_budgets_user_category`. Create/update constraint conflicts map to the same
409 response as the friendly service pre-check. Concurrent requests cannot
persist duplicate allocations.

Before AutoMigrate, a read-only preflight checks existing budgets for duplicate
groups. If any exist, startup stops with count-only resolution guidance. It does
not delete, merge, or choose an allocation automatically. Review those records
before retrying migration. Atomic registration likewise does not rewrite older
partial registrations; any existing missing-settings accounts require deliberate
recovery.

## API overview

Public routes:

| Method | Path        | Purpose                                       |
| ------ | ----------- | --------------------------------------------- |
| `GET`  | `/ping`     | Health check.                                 |
| `POST` | `/register` | Create a user and token pair.                 |
| `POST` | `/login`    | Authenticate and create a token pair.         |
| `POST` | `/refresh`  | Rotate a refresh token and create a new pair. |
| `POST` | `/logout`   | Revoke a refresh token.                       |

Protected resource groups:

| Prefix              | Operations                                                         |
| ------------------- | ------------------------------------------------------------------ |
| `/api/accounts`     | Create, list, read, update, and delete accounts.                   |
| `/api/categories`   | List and read categories.                                          |
| `/api/transactions` | CRUD plus account, category, date-range, and frequency filters.    |
| `/api/transfers`    | Create, list, read, update, and delete internal account transfers. |
| `/api/budgets`      | Create, list, read, update, and delete budgets.                    |
| `/api/goals`        | Create, list, read, update, and delete goals.                      |
| `/api/settings`     | Read and update the authenticated user's settings.                 |

All protected resources are scoped or ownership-checked against the user ID from
the validated access token.

## Account balances and goal allocations

Account responses include `balance`, `allocated_amount`, `unallocated_amount`,
and `goal_count`. Money values are integer minor units. These are computed by
the backend from the user's recorded income minus expenses and both sides of
transfers. Each record contributes once, regardless of frequency. They are
recorded balances; opening balances and bank reconciliation are not modeled.

Goals accept and return nullable `account_id`. Omit it or send `null` for an
unlinked goal; sending `null` on update removes a link. A non-null account must
belong to the authenticated user (missing or foreign accounts return 404).
Existing goals migrate with no link. An account referenced by a goal cannot be
deleted until that goal is reassigned, unlinked, or deleted (409).

`allocated_amount` sums the full target amounts of linked goals, independently
of `saved_amount`; assigning a goal reserves a target but never moves money.
`unallocated_amount = balance - allocated_amount` is signed so a negative value
represents a shortfall. For example, balance `300000` and two linked targets of
`120000` produce allocated `240000`, unallocated `60000`, and goal count `2`.
Reassigning, unlinking, editing, or deleting goals changes the next account
summary without changing income, expenses, or transfers.

## Account transfers

`POST /api/transfers` and `PUT /api/transfers/:id` accept:

```json
{
  "name": "Subscription account funding",
  "amount": 12599,
  "from_account_id": "<source-account-uuid>",
  "to_account_id": "<destination-account-uuid>",
  "date": "2026-10-24T12:00:00Z",
  "frequency": "monthly"
}
```

Responses contain those fields plus `id`. `GET /api/transfers` returns the
authenticated user's array (including `[]` when empty); `GET`, `PUT`, and
`DELETE /api/transfers/:id` enforce ownership. Create returns 201, reads/updates
200, and deletion 204. Both accounts must exist, be different, and belong to
the authenticated user. Names must be nonempty, amounts positive integer minor
units, and dates valid RFC3339 timestamps. Missing/foreign accounts or transfer
records return 404; invalid requests return 400. `frequency` accepts `one-off`,
`daily`, `weekly`, `monthly`, or `yearly`. Omitted/null frequency defaults to
`one-off` for compatibility; invalid explicit values return 400. The migration
assigns `one-off` to existing transfers when adding the required frequency column.

A transfer is stored as one row with restrictive foreign keys to both accounts.
Changing or deleting that row updates both sides together. It subtracts the
amount from the source account's recorded balance and adds it to the
destination's recorded balance. It does not create income or expense rows and
therefore does not change the overall balance, budget spending, savings, or
subscription totals. Account balances are derived from recorded transactions
and transfers, not reconciled bank balances. Frequency records a transfer's
cadence using the same semantics as recurring income/expense transactions. Each
record affects balances once; no background scheduler generates occurrences.
Transfers have no category. An account referenced by transactions, transfers,
or goals cannot be deleted; the API returns 409 until those records are moved,
unlinked, or deleted.

## Project layout

```text
cmd/server/            application entry point and dependency wiring
internal/auth/         JWT generation and validation
internal/apperrors/    shared application error categories
internal/config/       environment configuration
internal/database/     PostgreSQL connection and migrations
internal/handlers/     HTTP request and response handling
internal/middleware/   authentication middleware
internal/models/       GORM models and enums
internal/repository/   repository interfaces and GORM implementations
internal/routes/       route registration
internal/services/     business logic and API contracts
```

## Testing

Tests are colocated with the packages they exercise. Package-local
`fakes_test.go` files provide repository fakes for service, handler, and route
tests.

Repository integration tests require an isolated PostgreSQL database and skip
when `TEST_DATABASE_DSN` is absent:

```bash
TEST_DATABASE_DSN='host=localhost port=5432 user=vellum password=password dbname=vellum_test sslmode=disable' \
  go test -count=1 -p 1 ./...
```

Use a disposable test database, never a personal or deployed database: these
tests migrate tables and create/drop isolated test schemas. Serial package
execution prevents competing migration setup. The suite includes actual
registration rollback/retry, concurrent email registration, concurrent budget
creation, update conflicts, legacy duplicate preservation, tenant ownership,
transfers, and goal allocation. CI provides PostgreSQL and rejects skipped tests.

`bash scripts/check-docker-context.sh` from the repository root verifies the real
ignore rules using harmless synthetic files. It does not read local `.env`
contents. The frontend's `pnpm test:e2e` owns a separate disposable database and
servers for complete browser workflows.

## Data model

![Vellum entity relationship diagram](./doc/ERD.png)

![Vellum backend layers](./doc/layered-uml-component-diagram.png)
