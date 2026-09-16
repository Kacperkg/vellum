# Vellum Backend

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

```http
Authorization: Bearer <access-token>
```

## Environment variables

Every variable is required. Expiry values use Go duration syntax.

| Variable | Example | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP server port. |
| `DB_HOST` | `localhost` | PostgreSQL host. Use `postgres` in Docker Compose. |
| `DB_PORT` | `5432` | PostgreSQL port. |
| `DB_USER` | `vellum` | PostgreSQL user. |
| `DB_PASSWORD` | `password` | PostgreSQL password. |
| `DB_NAME` | `vellum` | PostgreSQL database. |
| `DB_SSLMODE` | `disable` | PostgreSQL SSL mode. |
| `JWT_SECRET` | `<long-random-value>` | Access-token signing secret. |
| `JWT_EXPIRY` | `15m` | Access-token lifetime. |
| `JWT_REFRESH_EXPIRY` | `720h` | Refresh-token lifetime. |

Local values may be placed in `backend/.env`; do not commit that file.

## API overview

Public routes:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/ping` | Health check. |
| `POST` | `/register` | Create a user and token pair. |
| `POST` | `/login` | Authenticate and create a token pair. |
| `POST` | `/refresh` | Rotate a refresh token and create a new pair. |
| `POST` | `/logout` | Revoke a refresh token. |

Protected resource groups:

| Prefix | Operations |
| --- | --- |
| `/api/accounts` | Create, list, read, update, and delete accounts. |
| `/api/categories` | List and read categories. |
| `/api/transactions` | CRUD plus account, category, date-range, and frequency filters. |
| `/api/budgets` | Create, list, read, update, and delete budgets. |
| `/api/goals` | Create, list, read, update, and delete goals. |
| `/api/settings` | Read and update the authenticated user's settings. |

All protected resources are scoped or ownership-checked against the user ID from
the validated access token.

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
  go test ./internal/repository -count=1
```

## Data model

![Vellum entity relationship diagram](./doc/ERD.png)

![Vellum backend layers](./doc/layered-uml-component-diagram.png)
