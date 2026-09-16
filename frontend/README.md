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
pnpm format           # format source with Prettier
pnpm format:check     # verify formatting without changing files
pnpm preview          # serve the production bundle locally
```

The development server runs at `http://localhost:5173`. API requests currently
target `http://localhost:8080` through the Axios instance in
`src/api/client.ts`.

## Authentication flow

1. Register or login returns an access token, rotating refresh token, and user.
2. `tokenStorage.ts` stores both tokens in browser `localStorage`.
3. The request interceptor attaches the access token as a Bearer token.
4. A `401` from a protected `/api/...` request starts one shared refresh request.
5. Successful refresh stores the replacement pair and retries the original
   request once.
6. Failed refresh clears local authentication.
7. Logout clears local tokens and asks the backend to revoke the refresh token.

Refresh requests are shared because backend refresh tokens are single-use; this
prevents simultaneous `401` responses from trying to rotate the same token more
than once.

The local storage keys are:

```text
vellum:access_token
vellum:refresh_token
```

## Key pieces

- **`src/api/client.ts`** — shared Axios instance and access-token request
  interceptor.
- **`src/api/interceptors.ts`** — `401` handling, refresh coordination, and
  one-time request retry.
- **`src/api/tokenStorage.ts`** — access and refresh token persistence.
- **`src/api/endpoints/`** — endpoint functions grouped by backend resource.
- **`src/api/types/`** — request and response contracts grouped by domain.
- **`src/routes/`** — TanStack file-based route definitions and guards.
- **`src/pages/`** — page-level components.
- **`src/components/`** — reusable UI and form components.

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
│   └── auth.ts
└── types/
    ├── accounts.ts
    ├── auth.ts
    ├── budgets.ts
    ├── categories.ts
    ├── common.ts
    ├── goals.ts
    ├── settings.ts
    └── transactions.ts
```

## Routing

- `/login` contains the sign-in and registration forms.
- `/` is the authenticated application entry page.
- Route guards redirect unauthenticated users to `/login` and prevent an
  authenticated user from reopening the login page.

As more authenticated pages are added, their guards should move under a shared
authenticated parent route rather than being repeated on every page.

## Production build

```bash
pnpm build
```

Vite writes the static production bundle to `dist/`. The deployment must serve
those assets and make the Go API reachable at the base URL configured in
`src/api/client.ts`.
