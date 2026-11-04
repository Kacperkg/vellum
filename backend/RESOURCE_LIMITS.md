# Authenticated API resource policy

Issue #24 applies the following per-process limits to registered `/api` routes.
Public login, registration, refresh, logout and password recovery retain their
existing independent protections. `/ping` and CORS preflights remain available.

| Resource | Limit | Failure |
| --- | --- | --- |
| Total request body, including trailing bytes | 64 KiB | 413 `request body too large` |
| Non-empty POST and PUT bodies | One JSON document with `application/json` (parameters allowed) | 400 invalid JSON / 415 unsupported content type |
| Account, goal, transaction, transfer name | 200 Unicode code points, before trimming | 400 validation error |
| Raw query string | 2,048 bytes | 400 `bad request` |
| Decoded query | 16 keys; 64 bytes/key; one value/key; 256 bytes/value | 400 `bad request` |
| Explicit transaction date range | `YYYY-MM-DD` or RFC3339 endpoints, ordered and at most 366 calendar days apart (both endpoints inclusive) | 400 validation error |
| Frequency filter | Existing one-off/daily/weekly/monthly/yearly enum | 400 validation error |
| Requests per verified user | 240 per fixed one-minute window | 429 |
| Requests per source | 2,400 per fixed one-minute window | 429 |
| Concurrent requests per verified user | 16 | 429 |
| Concurrent requests per source | 24 | 429 |
| Concurrent resource requests globally | 32, including session validation | 429 |
| Limiter state | 10,000 entries shared across source/user keys, sharded | 429 when a shard is full |
| Request database work deadline | 10 seconds, or earlier client deadline/cancellation | 504 timeout / 408 cancellation |
| Session validation (version and family lookups) | 3 seconds within the request deadline | 504 timeout / 408 cancellation / 503 lookup failure |
| PostgreSQL statement / lock / idle transaction | 5 seconds / 2 seconds / 10 seconds | 500 resource query failure / 503 session lookup failure (408 or 504 on request cancellation or expiry) |
| Runtime SQL connection pool | 16 open, 8 idle, 5-minute idle, 30-minute lifetime | Wait bounded by request context |
| Database connection establishment | 5 seconds | Connection failure |

Existing HTTP limits remain: 5-second header read, 15-second request read,
30-second write, 60-second idle and 16 KiB headers. A context deadline does not
interrupt a blocked body read; the HTTP read timeout bounds uploads. After the
read finishes, an expired context prevents handler execution. No worker runs a
timed-out handler in the background. Cancellation propagates through request-scoped
service/repository clones to GORM queries and transactions; shared instances are
never mutated. Slots are held until synchronous work and transaction cleanup end.
Successful financial writes are not relabeled as failures merely because the
deadline expires after commit. As with any networked write, a disconnect at commit
can leave the client uncertain: reload before manually repeating a mutation.

Source and global admission run before JWT verification and bound database
checks for the authentication version and active session family. A deadline or
cancellation during either lookup returns 504 or 408. Invalid or revoked
credentials continue to receive 401. A database failure during validation returns
a generic 503 to avoid initiating a refresh for an unverified session. User
admission follows successful
validation and uses the verified user UUID, so refresh rotation and changing
sources do not reset it. Source identity follows the trusted proxy configuration
(IPv4 address or IPv6 /64). A single user across multiple sources can still occupy
the globally bounded validation slots. Limits are process-local and reset when
the process restarts.

429 responses retain the JSON `error` envelope and include `Retry-After` (rounded
up to seconds, one second for concurrency) and `Cache-Control: no-store`. Active
rate counters are never evicted to admit new keys. Expiry is incremental and
bounded; concurrency keys are removed as the last active request finishes. No
Redis, background cleanup worker, unbounded queue or per-request worker is added.

## Compatibility and pagination (#25)

Successful response shapes and complete collection arrays are preserved. Exact
minor-unit arithmetic, ownership checks and account-summary guards remain in
place. Date filters retain the written calendar day of RFC3339 input, including
its original offset, and include the whole last day. This
change adds no `LIMIT`, default page size, cursor or pagination envelope. Query
keys within the input bounds that were previously ignored remain ignored.
Repeated query keys and malformed encodings now receive 400. Invalid JSON/binding
errors on authenticated resources return safe generic messages instead of parser
internals. Overlong names and oversized date filters are rejected, never clipped.
Existing stored names remain readable and are not rewritten.

FinanceDataContext currently requests seven resources concurrently at mount,
focus and after mutations. The 16/user limit admits two overlapping seven-request
hydrations with two slots to spare. The 240/minute user budget admits 34 complete
seven-request hydrations per minute, before other operations; excessive tab/focus
bursts can receive 429 and the existing UI error state permits manual retry.
The client displays resource errors and does not automatically retry a write
after 400, 408, 413, 415, 429, 500, 503 or 504. The existing 401 refresh path can
resend a request that authentication rejected before the financial handler ran.

Complete list memory/serialization cost still grows with stored data. Admission,
query deadlines and the pool contain concurrent work, but do not replace #25's
coordinated pagination and complete-total/export design. Large users may receive
an explicit database failure instead of partial finance data. #25 must preserve
these input bounds or explicitly revise them when defining a cursor contract.

## Database review and startup

Account summaries and transactional money guards correlate transactions, transfers and goals with owner
IDs. Transaction list variants constrain user ID plus account/category/date/
frequency. Transfer lists constrain user ID and sort by date/created time. Several
of these columns lack supporting compound indexes; statement deadlines bound the cost of individual SQL statements. No schema/index migration is added:
index selection and deterministic pagination ordering should be measured against
#25's final query shape. Financial totals are never computed from capped lists.

Runtime timeouts are PostgreSQL connection startup parameters, so every new or
replacement connection receives them. Startup migrations use a separate one-open-
connection pool with a two-minute statement timeout, two-second lock timeout and
three-minute overall context deadline including the initial connection. It closes
before the runtime pool opens. The admission and database budgets are backend
constants. Trusted proxy addresses come from the existing `TRUSTED_PROXY_CIDRS`
configuration. Deployment health checks retain their existing deadlines.

## Validation

Use a disposable PostgreSQL database; integration tests run migrations and seed
test records. Never point `TEST_DATABASE_DSN` at an application database.

```sh
go build ./...
go vet ./...
TEST_DATABASE_DSN='host=127.0.0.1 port=55424 user=vellum_test dbname=vellum_issue24 sslmode=disable' go test -race -p 1 ./...
go test ./internal/middleware -run '^$' -bench ResourceAdmission -benchmem
```

Tests cover body/query/name/date boundaries, malformed/chunked/trailing bodies,
safe error responses, hydration, separate auth/reset protections, source trust,
concurrency/rate rejection and slot/state cleanup. PostgreSQL tests exercise
statement/lock deadlines, pool exhaustion, cancellation of all finance repository
types, rollback after canceled mutations and concurrent financial operations.
