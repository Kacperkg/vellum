# Vellum

Vellum is a personal-finance application for managing accounts, transactions,
budgets, savings goals, categories, and user preferences. The repository pairs
a substantially implemented Go/PostgreSQL API with an early-stage React client.

The backend is the strongest part of the project: it uses explicit handler,
service, and repository boundaries; enforces ownership in the service and data
layers; and supports short-lived JWT access tokens with rotating, revocable
refresh tokens. The frontend currently provides authentication, protected
routing, token refresh, navigation shells, reusable UI primitives, and component
prototypes. The finance dashboard and most user workflows are not yet connected.

## Project status

### Implemented

- Registration and login with bcrypt password hashing.
- JWT access tokens plus high-entropy, hashed, single-use refresh tokens.
- Refresh-token rotation and logout revocation.
- Authenticated CRUD APIs for accounts, budgets, goals, and transactions.
- Transaction filtering by date range, recurrence, account, and category.
- Shared category reads and per-user settings reads/updates.
- Per-user ownership checks that avoid revealing foreign resources.
- PostgreSQL persistence through GORM and automatic schema migration.
- React registration/login forms, route guards, logout, and automatic retry
  after one coordinated token refresh.
- Reusable buttons, cards, inputs, selects, navigation, and dialog prototypes.
- Docker Compose development environment for the client, API, and database.
- Backend unit, handler, routing, middleware, and repository integration tests.

### In progress

- The authenticated home route is a placeholder rather than a finance dashboard.
- The frontend exposes typed account requests, but account, transaction, budget,
  goal, category, and settings screens are not wired to the API.
- Several finance dialogs are UI demonstrations and do not persist data.
- Subscription and analytics features appear in older design notes but have no
  backend model, route, or working frontend flow.

## Technology stack

| Area | Technologies |
| --- | --- |
| Web client | React 19, TypeScript, Vite, TanStack Router, Tailwind CSS 4, Axios |
| API | Go, Gin, GORM |
| Authentication | bcrypt, HS256 JWT access tokens, opaque refresh tokens |
| Database | PostgreSQL 17 |
| Development | Docker, Docker Compose, Air, pnpm |
| Testing | Go `testing`, Gin test utilities, GORM/PostgreSQL integration tests |

## Architecture

```text
React client
  ├─ file-based routes and route guards
  ├─ local component/form state
  └─ Axios client and auth interceptors
                │ REST/JSON
                ▼
Gin routes → handlers → services → repository interfaces → GORM → PostgreSQL
              auth middleware      ownership and validation
```

The API is assembled explicitly in `backend/cmd/server/main.go`. HTTP handlers
translate requests and responses, services own validation and authorization,
and repositories isolate persistence. User-scoped repositories include the user
identifier in mutation and query conditions as an additional tenant boundary.

Authentication uses short-lived JWTs for protected requests. Refresh tokens are
generated from cryptographically secure random bytes, returned once to the
client, stored as SHA-256 hashes in PostgreSQL, and replaced transactionally on
refresh. The browser client serializes simultaneous refresh attempts through one
shared promise before retrying a failed protected request.

Current browser tokens are stored in `localStorage`. This is convenient during
development but increases exposure to cross-site scripting; an HTTP-only cookie
or another reviewed production session design should be considered before a
public deployment.

## Repository layout

| Path | Purpose |
| --- | --- |
| `backend/cmd/server` | API composition and process entry point |
| `backend/internal/handlers` | HTTP parsing and response mapping |
| `backend/internal/services` | validation, business rules, and ownership checks |
| `backend/internal/repository` | persistence interfaces and GORM implementations |
| `backend/internal/models` | PostgreSQL/GORM entities and relationships |
| `frontend/src/api` | typed API client, token storage, and refresh behavior |
| `frontend/src/components` | reusable UI primitives and dialog prototypes |
| `frontend/src/routes` | TanStack Router file-based routes and guards |
| `docs` | architecture notes and diagrams |

## Screenshots

No application screenshots are currently committed. Useful captures to add once
the UI is stable are:

1. Registration and sign-in at desktop and mobile widths.
2. The reusable component/dialog catalogue at `/components`.
3. The finance dashboard after it is connected to real API data.
4. Account, transaction, budget, and goal workflows after implementation.

The existing architecture diagram is available at
[`docs/architecture-spec.png`](docs/architecture-spec.png); it is a system
diagram, not an application screenshot.

## Getting started

### Docker Compose

Requirements:

- Docker Engine or Docker Desktop
- Docker Compose v2

Create `backend/.env` (there is currently no committed example file):

```dotenv
PORT=8080
DB_HOST=postgres
DB_PORT=5432
DB_USER=vellum
DB_PASSWORD=password
DB_NAME=vellum
DB_SSLMODE=disable
JWT_SECRET=replace-with-a-long-random-development-secret
JWT_EXPIRY=15m
JWT_REFRESH_EXPIRY=720h
```

Then start the development stack:

```bash
docker compose up --build
```

| Service | Local address |
| --- | --- |
| Web client | `http://localhost:5173` |
| REST API | `http://localhost:8080` |
| Health check | `http://localhost:8080/ping` |
| PostgreSQL | `localhost:5432` |

Stop the stack with `docker compose down`. Running
`docker compose down -v` also deletes the local database volume.

### Run from source

Requirements: Go 1.26, Node.js, pnpm, and a reachable PostgreSQL database.
Use `DB_HOST=localhost` when the API runs on the host.

```bash
cd backend
go run ./cmd/server
```

In another terminal:

```bash
cd frontend
pnpm install
pnpm dev
```

The client API base URL is currently hard-coded to `http://localhost:8080` in
`frontend/src/api/client.ts`.

## Testing and quality checks

Backend:

```bash
cd backend
go test ./...
```

The suite covers token creation, authentication middleware, route protection,
handler behavior, service validation and ownership rules, transaction/goal
logic, tenant-scoped repositories, and single-use refresh-token rotation. The
repository integration test can use PostgreSQL when configured by the test.

Frontend:

```bash
cd frontend
pnpm lint
pnpm build
```

Both commands passed during the documentation audit on 16 October 2026. There
are no frontend automated tests and no committed CI workflow.

## Deployment

The repository contains a development-oriented Docker Compose setup and a
frontend development-server container. It does not contain a verified production
deployment workflow or live application URL. Before deployment, add production
container/runtime configuration, environment-specific API/CORS settings, HTTPS,
secret management, migration strategy, and CI.

## Technical decisions and trade-offs

- **Layered backend:** repository interfaces make service logic testable without
  HTTP or database concerns, at the cost of additional types and mapping code.
- **Integer money values:** transaction, budget, and goal amounts use `int64`,
  avoiding binary floating-point errors; API units should be documented before
  the UI formats currency.
- **Rotating refresh tokens:** server-side hashes enable revocation and replay
  prevention, but token storage and cleanup require database operations.
- **GORM AutoMigrate:** convenient while the schema is evolving, but explicit,
  versioned migrations would provide safer production change control.
- **Local component state:** appropriate for the current small client; server
  state and cache strategy should be selected when finance screens are wired.

## Further documentation

- [Backend API and development notes](backend/README.md)
- [Frontend structure and authentication flow](frontend/README.md)
- [Architecture specification](docs/architecture-specification.md)
