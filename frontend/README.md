# Vellum Frontend

Browser client for Vellum. Built with React, TypeScript, Vite, Tailwind CSS,
TanStack Router, and Axios.

## Development

Run these from `frontend/`:

```bash
pnpm install          # install dependencies
pnpm dev              # start the Vite development server
pnpm build            # type-check and create the production bundle
pnpm lint             # run ESLint
pnpm test             # finance regressions and architecture boundaries
pnpm test:e2e:check    # type-check browser tests and configuration
pnpm test:e2e          # complete browser workflows in an isolated stack
pnpm format           # format source with Prettier
pnpm format:check     # verify formatting without changing files
pnpm preview          # serve the production bundle locally
```

The development server runs at `http://localhost:5173`. API requests default to
`http://localhost:8080` through the Axios instance in `src/api/client.ts`; set
`VITE_API_BASE_URL` to override the local API address.

## Authentication flow

1. Register or login returns an access token, rotating refresh token, and user.
2. `tokenStorage.ts` stores both tokens and the returned user in browser
   `localStorage`.
3. The request interceptor attaches the access token as a Bearer token.
4. A `401` from a protected `/api/...` request starts one shared refresh request.
5. Successful refresh stores the replacement pair and retries the original
   request once.
6. Failed refresh clears local authentication and returns to `/login`.
7. Logout clears local tokens and asks the backend to revoke the refresh token.

Refresh requests are shared because backend refresh tokens are single-use; this
prevents simultaneous `401` responses from trying to rotate the same token more
than once.

The local storage keys are:

```text
vellum:access_token
vellum:refresh_token
vellum:user
```

## Key pieces

- **`src/api/client.ts`** — shared Axios instance and access-token request
  interceptor.
- **`src/api/interceptors.ts`** — `401` handling, refresh coordination, and
  one-time request retry.
- **`src/api/tokenStorage.ts`** — access and refresh token persistence.
- **`src/api/endpoints/`** — endpoint functions grouped by backend resource.
- **`src/api/types/`** — request and response contracts grouped by domain.
- **`src/app/`** — application setup and router creation.
- **`src/routes/`** — thin TanStack file-based route definitions and guards.
- **`src/features/`** — domain pages, forms, styles, and feature utilities/tests.
- **`src/components/ui/`** — reusable UI primitives with no domain dependencies.
- **`src/components/shared/`** — shared page headers, states, branding, and confirmation UI.
- **`src/layout/`** — application shell, navigation, and layout styles.
- **`src/contexts/`** — shared finance resource state.
- **`src/utils/`** — shared money, date, and finance calculations.
- **`src/assets/styles/`** — theme, base styles, stylesheet entry, and shared responsive rules.

## Feature structure and imports

The frontend groups related implementation by feature, following the
[feature-oriented approach in the React/Vite guide](https://sandeshrathnayake.medium.com/mastering-modern-react-vite-folder-structure-a-production-ready-guide-for-scalable-applications-9ad8e233f8b9).
The existing TanStack routes, Axios modules, shared finance context, and backend
contracts remain in place. Directories are added when there is code to put in
them rather than creating empty scaffolding.

```text
src/
├── app/router.ts
├── api/                       # existing resource endpoints and contracts
├── assets/styles/globals.css  # ordered stylesheet entry point
├── components/
│   ├── ui/
│   └── shared/
├── contexts/FinanceDataContext.tsx
├── features/
│   ├── accounts/              # components, pages, styles, public exports
│   ├── analytics/
│   ├── auth/
│   ├── budgets/
│   ├── goals/
│   ├── overview/
│   ├── settings/
│   ├── subscriptions/
│   └── transactions/
│       ├── components/
│       ├── pages/Transactions.tsx
│       ├── utils/             # activity, CSV export, and colocated tests
│       ├── index.ts           # public cross-feature exports
│       └── styles.css
├── layout/
├── pages/ComponentsDemo.tsx    # development catalogue composition
├── routes/                   # filenames and URLs remain stable
└── utils/finance.ts
```

Use `@/` for imports across shared directories; it resolves to `src/` in both
TypeScript and Vite. Use relative imports within a feature. When one feature
needs another feature's functionality, import its named public exports from
`@/features/<name>`, rather than reaching into its internals. Only features with
cross-feature consumers need an `index.ts`.

Routes and application composition can import feature pages directly. Pages
are deliberately excluded from feature public barrels to preserve route loading
boundaries. UI primitives cannot depend on API, context, routing, or product
features. Shared API, contexts, utilities, and components cannot import product
features. Runtime imports must remain acyclic; `pnpm test:architecture` checks
these boundaries and resolves local imports.

Feature styles live beside their implementation. `globals.css` imports them in
the original cascade order, with shared responsive overrides last. Keep that
ordering when changing styles. The stylesheet extraction preserved the compiled
production CSS byte for byte.

## API organization

Endpoint functions use the shared Axios client rather than creating their own
clients. Domain types are defined in separate files and re-exported from
`src/api/types/index.ts` for convenient imports.

```text
src/api/
├── client.ts
├── interceptors.ts
├── tokenStorage.ts
├── endpoints/
│   ├── accounts.ts
│   ├── auth.ts
│   ├── budgets.ts
│   ├── categories.ts
│   ├── goals.ts
│   ├── settings.ts
│   ├── transfers.ts
│   └── transactions.ts
└── types/
    ├── accounts.ts
    ├── auth.ts
    ├── budgets.ts
    ├── categories.ts
    ├── common.ts
    ├── goals.ts
    ├── settings.ts
    ├── transfers.ts
    └── transactions.ts
```

## Routing

- `/login` contains the sign-in and registration forms.
- `/` is the authenticated application entry page.
- Route guards redirect unauthenticated users to `/login` and prevent an
  authenticated user from reopening the login page.

As more authenticated pages are added, their guards should move under a shared
authenticated parent route rather than being repeated on every page.

## Live finance data

`FinanceDataContext` loads accounts, categories, transactions, transfers, budgets,
goals, and settings through the typed endpoint modules. Screens select only the
resource errors relevant to them, and successful mutations refresh the shared
state. Overview and analytics values are derived from those live resources.

`/accounts` displays server-calculated `balance`, `allocated_amount`,
`unallocated_amount`, and `goal_count` from the existing accounts API. Each
linked goal reserves its full target; negative unallocated amounts display as
shortfalls. Add/edit goal forms persist nullable `account_id`, allow unlinking,
and preview the allocation after saving. Linking does not change saved progress
or create a transaction. Account deletion is blocked while transactions,
transfers, or goals still reference it. Accounts and Settings use server balance
values; the activity utility remains responsible for transaction-history views.

Subscription membership is determined by frequency. Every income, expense, or
transfer whose frequency is not `one-off` appears on `/subscriptions`, labeled
by its type. Editing or deleting it updates the same record in both views.

Account, budget, goal, transaction, and subscription forms support persisted
creation, editing, and deletion. The navbar New button opens the transaction
form. Transaction search and filters operate on the loaded activity, and Export
downloads the filtered records as CSV.

Transactions normally show clean activity rows without frequency badges or row
action buttons. Edit mode enables single-row selection with shared Edit/Delete
controls; filtered-out selections cannot be edited or deleted. Frequency remains
available in forms and filters. Analytics pairs the category donut with a visible
color legend, amounts, and percentages of the displayed top-category subtotal;
no recent expenses produces an empty state rather than a sample chart.

The icon-only edit toggle keeps an accessible label and pressed state. Transaction
headers sort merchant, category, account, date, and signed displayed amount in
either direction; mobile exposes the same sort state through a selector. Export
uses the visible sort order. A global transfer sorts by its neutral face amount;
account-filtered transfers sort by the signed movement shown for that account.
The income/expense plot flexes with its card and exposes exact monthly totals
on hover, keyboard focus, or tap, with Escape dismissing the tooltip.

Settings groups account management and display preferences into two sections.
The four display options share a compact panel with labeled controls, previews,
and a footer for autosave feedback and restoring defaults. The existing settings
API remains authoritative; controls are disabled while saving and success is
shown only after a completed update. Currency previews honor number formatting.

API money amounts are integer minor units. Forms convert decimal major-unit
input to minor units before requests, and formatting utilities convert values
back for display.

Transfers use `src/api/endpoints/transfers.ts` and the same server-state context.
The transaction form offers Income, Expense, and Transfer types. Transfer mode
shows source and destination accounts and the same frequency options as income
and expenses: one-off, daily, weekly, monthly, and yearly. Dedicated Transfer
buttons in Transactions and Settings reuse this form. Editing or deleting a
transfer changes its effect on both accounts together. Transactions, account
filters, overview activity, and CSV exports include the transfer record.

`src/features/transactions/utils/activity.ts` combines the display history and computes recorded account
balances. A transfer reduces its source and increases its destination by the
same integer amount. It has zero global net impact and is excluded from income,
spending, budgets, and analytics. Recurring transfers appear in the recurring
list without adding subscription expenses. Recorded balances do
not include an unmodeled bank opening balance.

Frequency stores cadence, consistent with the existing recurring transactions.
It does not generate future ledger occurrences automatically.

Run `pnpm test:finance` for the accounting regression tests, including the
salary → transfer → subscription scenario, edits, deletion, return transfers,
account-filter signs, and CSV net impact. These tests live beside the activity
utility and use the installed TypeScript compiler through a small native Node
test helper in `tests/helpers/`; no additional test framework is required.

## Production build

```bash
pnpm build
```

Vite writes the static production bundle to `dist/`. The deployment must serve
those assets and make the Go API reachable at the base URL configured in
`src/api/client.ts`.

## Browser tests and screenshots

Install frontend dependencies, Go, Docker, and Chromium before the first run:

```sh
pnpm exec playwright install chromium
pnpm test:e2e
```

`scripts/run-e2e.mjs` creates a throwaway PostgreSQL container, compiles a Go
binary into a temporary directory, and starts dedicated API/frontend processes
on ports 18080/15173. It rejects occupied ports instead of reusing existing
servers. The API runs outside the backend directory with explicit synthetic
configuration, so it does not load the development `.env`. Processes, temporary
files, and the database container are removed after the run.

The desktop Chromium suite covers browser authentication, transfer balances,
linked goal availability, duplicate budgets, activity editing/filter/sort/export,
settings persistence, and successful/rejected session refresh. A mobile project
checks navigation, sorting, account creation, and page overflow. Tests use fresh
synthetic users in separate browser contexts. Browser tests are separately
type-checked by `tsconfig.e2e.json`; the production build still checks application
source. The normal `pnpm test` suite remains fast and does not require Docker.

Failures retain traces, screenshots, and video under ignored `test-results/` and
`playwright-report/` directories. CI uploads failure diagnostics for review.

`pnpm screenshots` explicitly opts into the screenshot fixture, seeds synthetic
records through the real API, and updates `docs/screenshots/`. It does not
photograph the developer's database. The fixture is excluded from normal tests.

Playwright setup follows its official [fixtures](https://playwright.dev/docs/test-fixtures)
and [CI](https://playwright.dev/docs/ci-intro) documentation. The custom runner
owns the database and process lifecycle so tests cannot silently reuse a local
application server.
