# Vellum architecture

## Scope

Vellum is a manual personal-finance tracker. React screens consume persisted
records through a Go REST API; PostgreSQL stores those records. Bank feeds,
automatic recurring occurrences, exchange rates, and payment execution are not
implemented. This document describes the current implementation rather than a
target deployment.

## Request and data flow

```text
TanStack file route → React feature page / form
                                ↓
                     typed Axios endpoint
                                ↓
                  Gin route → JWT middleware
                                ↓
                    handler → service → repository
                                ↓
                         GORM → PostgreSQL
```

Handlers bind JSON and map HTTP errors. Services own validation and ownership
checks. Repository interfaces isolate SQL and persistence transactions. The API's
composition root is `backend/cmd/server/main.go`.

## Frontend

React 19 and TypeScript 6 run through Vite 8. Tailwind CSS 4 provides utility
classes and theme tokens; feature CSS supplies the dashboard layouts. TanStack
Router generates the route tree from `frontend/src/routes/` and splits routes.
The application uses React Context/local state and Axios; it does not use React
Router or an external state-management/chart/form framework.

`features/` owns domain pages, forms, utilities, styles, and colocated finance
tests. `components/ui/` contains reusable primitives; `components/shared/` holds
common presentation. `layout/` owns navigation and the shell. `api/` retains
central resource endpoints and contracts. `contexts/FinanceDataContext.tsx`
hydrates seven resources and refreshes them after mutations, retaining previous
values on individual resource failures. Architecture tests check import
resolution, feature entry points, dependency direction, and runtime cycles.

Pages include Overview, Accounts, Transactions, Budgets, Analytics, Goals,
Subscriptions, and Settings. Protected routes check access-token presence; the
API validates authentication and ownership. Transactions provide responsive
sort/filter/edit selection and CSV export. Analytics derives six-month totals
and category shares in the browser from loaded transactions. Account balance and
goal allocation summaries are calculated by the backend.

## Database entities

| Entity       | Responsibility                                                                 |
| ------------ | ------------------------------------------------------------------------------ |
| User         | Identity, names, unique email, bcrypt password hash                            |
| UserSettings | Currency/date/number/week preferences                                          |
| RefreshToken | User, unique token hash, expiry, revocation                                    |
| Account      | Owned name; summaries are query results, not stored balance fields             |
| Category     | Shared canonical category names                                                |
| Transaction  | Owned account/category entry, positive amount, date, income/expense, frequency |
| Transfer     | Owned source/destination movement, positive amount, date, frequency            |
| Budget       | Owned category allocation, unique per user/category                            |
| Goal         | Target/progress/contribution and optional owned account link                   |

There is no standalone Subscription table. Subscriptions displays transactions
and transfers with non-one-off frequency. Each recorded entry affects balances
once; frequency does not generate future occurrences.

## Financial invariants

API/backend money values are integer minor units. A transfer is one row with two
derived account effects and no global income/expense effect. Source and
destination must differ and belong to the authenticated user. Database checks
and restrictive account foreign keys enforce key transfer constraints.

Account queries independently aggregate transaction net, incoming/outgoing
transfers, linked goal targets, and goal counts. Separate correlated aggregates
avoid multiplying totals through multi-way joins. Unallocated money equals
recorded balance minus full goal targets; negatives are preserved as shortfalls.
Saved goal progress is independent and does not move funds.

Budgets are current category limits, not historical monthly records. A composite
unique index enforces one budget per user/category. Startup checks existing
records for duplicates before migration and stops without deleting or merging
allocations if any exist.

## Authentication and authorization

Passwords use bcrypt. Access tokens are HS256 JWTs. Refresh tokens use random
opaque values; only SHA-256 hashes are stored. Registration prepares credentials
in the service, then one repository transaction commits the user, default
settings, and initial refresh hash. Failed persistence rolls back the whole
registration; no credentials are returned. Refresh rotation conditionally revokes
the old token and inserts its replacement in a transaction.

The client stores tokens in localStorage and coordinates simultaneous 401
responses through one refresh promise. Refresh failure clears the session and
returns to login. Logout clears local state and attempts server revocation.
Already issued access tokens expire according to configuration.

Services validate ownership and related accounts. Repository lists and mutations
include user conditions. Foreign resources generally return not-found responses.
These are application-layer tenancy controls, not PostgreSQL row-level security.

## Development and configuration

Compose starts the API with Air, frontend with Vite, and PostgreSQL 17 with a
persistent development volume. The local web/API URLs use HTTP. HTTPS is a
production deployment requirement, not something the current Compose file
provides. PostgreSQL is published to the host for development.

`backend/.env.example` documents local settings. Compose injects the local `.env`
at runtime; Go can also load it when run from the backend directory. Backend and
frontend build contexts have their own `.dockerignore` rules excluding environment
files, credentials, and generated output. `CORS_ALLOWED_ORIGINS` can configure
allowed frontend origins; the default remains `http://localhost:5173`.

Startup uses GORM AutoMigrate and idempotent canonical-category seeding. There is
no versioned migration/rollback framework or verified production deployment.

## Verification

GitHub Actions provides frontend checks, Go/PostgreSQL integration tests,
synthetic Docker-context exclusion checks, and Playwright browser workflows.
Go integration tests need an isolated `TEST_DATABASE_DSN`; CI rejects skips and
failures. Backend tests serialize packages to avoid competing schema migrations.

`pnpm test:e2e` starts a disposable PostgreSQL container, temporary Go binary, and
dedicated frontend/API servers. Occupied ports are rejected. Every test uses
synthetic records and an isolated browser context. The runner stops its processes
and container afterward. Desktop and mobile tests exercise auth, accounting,
allocations, duplicate budgets, export, settings, and refresh paths. Screenshot
capture is a separate opt-in fixture against the same isolated stack.

## Remaining deployment work

Production serving/images, HTTPS, request limits/authentication throttling,
backup/restore procedures, controlled migrations, and a reviewed browser token/CSP
policy remain necessary. Bank integrations, recurrence scheduling, opening
balances, FX, and password recovery would extend the current product scope.
