# Backend tests

Tests are grouped by the production boundary they exercise:

- `services/` contains black-box unit tests for business rules, ownership, and DTO mapping.
- `handlers/` contains HTTP tests using Gin and `httptest`.
- `security/` contains JWT and authentication middleware tests.
- `testutil/` contains reusable in-memory repository fakes.

Run the complete suite from `backend/`:

```bash
go test ./...
go test ./... -race
```

Measure coverage across production packages:

```bash
go test ./tests/... -coverpkg=./internal/... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

Database repository tests should go in `tests/integration/`. They must use an isolated PostgreSQL database and own its setup and teardown; unit tests must not depend on Docker or shared developer data.
