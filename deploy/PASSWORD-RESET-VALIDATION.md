# Password reset implementation and validation

Verdict: **Ready with caveats** for draft-PR review. Live SES delivery and production rollout require the approvals in [the operator runbook](PASSWORD-RESET.md); this is not a deployment approval.

Implementation started from staging `0e5af08f69a0efef0c72775d9d1b654c103cef39` on isolated branch `feature/aws-ses-password-reset`. Other worktrees and their uncommitted changes were preserved. `AGENTS.md` remains ignored and untracked.

## Changes

| Area | Entry points / behavior |
| --- | --- |
| Backend API | `backend/internal/handlers/password_reset.go`, `internal/routes/{router,routes}.go`: public POST `/forgot-password` and `/reset-password`; generic recovery responses, bounded request bodies, no-store/no-referrer, rate limits |
| Recovery service | `backend/internal/services/password_reset.go`: 32-byte random bearer material, SHA-256 persistence, default 20-minute expiry, bounded queue and one delivery worker, timeout, no retries, database cooldown, bounded cleanup |
| SES | `backend/internal/email/ses.go`: mockable AWS SDK for Go v2 SES API sender, default AWS credential chain, UTF-8 text and HTML email, HTTPS fragment link |
| Schema | `backend/internal/models/{user,refresh_token,password_reset_token}.go`, `internal/database/migrate.go`: default-zero authentication versions, unique reset hash, indexed user/expiry/consumption, reset-user FK |
| Session integrity | `backend/internal/repository/{password_reset,refresh_token}.go`, auth service/JWT/middleware: same user-row lock for login/refresh persistence and reset; atomic password/version/reset consumption/refresh revocation; database version check on protected requests |
| Sensitive logs | `backend/internal/database/database.go` and router: suppress SQL values and request/panic payloads; log fixed outcomes and registered route/status only |
| Frontend | Public `/forgot-password` and `/reset-password`, visible login recovery link, accessible validation/status/loading, confirmation password, memory-only token, early URL replacement, clear local auth after reset, no automatic login |
| Proxy/runtime | `deploy/Caddyfile.example`, Compose/environment examples, `frontend/nginx.prod.conf`: POST/OPTIONS auth routing, GET SPA links, privacy headers, disabled-default email configuration |
| Operator procedure | `deploy/PASSWORD-RESET.md`: migration compatibility, SES/DNS/IAM setup, monitoring, explicit approval checkpoints, deployment and rollback |
| Routing regression | `scripts/check-recovery-routing.py`, called by existing configuration check: disposable Caddy fixture verifies both host blocks, auth POST/OPTIONS, GET deep links and headers |

Successful reset invalidates all outstanding reset links and existing refresh sessions in the same transaction as the bcrypt password and version update. Previously issued access JWTs fail the next version check. Existing JWTs without a version remain compatible as version zero until a reset. Token validity is rechecked after lock acquisition, so concurrent redemption and expiry while waiting are safe. Requests authenticated before reset may finish.

## Executed checks

All checks below passed against the final implementation. No real SES sends or credentials were used. PostgreSQL tests used an ephemeral PostgreSQL 17 container with synthetic data and no persistent volume.

| Check | Result |
| --- | --- |
| `go build -p 1 ./...`, `go vet -p 1 ./...` | Pass |
| `go test -count=1 -p 1 -json ./...` with `TEST_DATABASE_DSN` | 257 tests/subtests passed, zero skipped tests/failures |
| `go test -race -count=1 -p 1 -json ./...` with PostgreSQL | Same 257 passed, zero skipped tests/failures |
| Reset PostgreSQL concurrency stress, `-race -run TestPasswordReset -count=10` | Pass; 11 dedicated integration cases per repetition |
| `pnpm test` | 18 passed |
| `pnpm test:e2e:check`, production build/TypeScript, lint, format check | Pass |
| `pnpm test:e2e` | 21 browser tests passed, including existing protected workflows and mobile flow |
| Backend/frontend production Dockerfiles, `--platform linux/amd64` | Both built successfully locally; no push/deploy |
| `bash scripts/check-deployment-config.sh` | Both Compose variants, environment-file relocation, Caddy syntax and live local routing/header fixture passed |
| `bash scripts/check-docker-context.sh` | Both synthetic secret-exclusion contexts passed |
| `python3 -m unittest discover -s deploy/tests -v` | 79 passed: deployment safety, release/provenance, coordination, backup and environment controls |
| `python3 -m unittest discover -s scripts/ci/tests -v` | 28 passed |
| `go test -count=1 ./deploy/tests/ssm_regex_test.go` | Pass |
| Pinned actionlint 1.7.7 over active workflows and inactive templates | Pass |
| Go formatting and `git diff --check` | Pass |

Coverage includes existing/unknown accounts, invalid/expired/consumed tokens, multiple outstanding tokens, concurrent redemption/issuance/login/refresh/logout, legacy migration/JWT behavior, bcrypt verification, refresh revocation, rollback after the final token write fails, expiry after lock waiting, FK and cleanup behavior, mock SES failure/timeout, worker processing/queue capacity, recipient restrictions, account-enumeration response parity, rate quotas, request bounds and redacted normal/panic logs. Frontend tests exercise input/mismatch validation, loading, confirmation, API payloads, missing/invalid links, navigation, protected flows, token storage/history/referrer behavior and local session clearing.

The first browser run passed 20/21; its history assertion assumed Back always mounts a fresh invalid-link screen. TanStack can restore the completed confirmation. The final assertion verifies no password form/token can return, no extra redemption occurs, and reloading the cleaned URL shows an invalid link. Independent security review accepted that behavior. No failing tests were disabled.

## Subagents and resolved findings

Six dedicated subagents performed work:

- Backend: implemented and reviewed Go auth/recovery/SES/configuration and ran full backend checks.
- Database/concurrency: independently challenged lock ordering and stale authentication snapshots, added 11 PostgreSQL integration cases, stressed races and verified rollback/migration compatibility.
- Frontend: inspected existing auth/router/provider architecture, implemented recovery UI and added unit/browser coverage.
- AWS security: independently reviewed IAM/sender/credential/environment boundaries and operator procedure. Required exact environment senders, monitored sender-mailbox forwarding and cleanup capacity matching maximum issuance; resolved.
- Application security: independently reviewed the full recovery/session/frontend flow. Required panic-log redaction and bounded database waits; resolved. Focused tests passed.
- Testing/reliability: independently reviewed Go architecture, PostgreSQL failure coverage, frontend history assertions, Docker runtime and CI compatibility. Added JWT algorithm/expiration, redemption/network/body limits and actual mock worker regressions; focused race tests passed.

Normal/SQL log exposure was also corrected during review. No confirmed implementation blockers remain in the completed reviews. Passing tests do not establish live delivery or infrastructure isolation.

## Remaining caveats and manual steps

- Sending defaults to disabled. Operators must verify SES identities/DKIM, Namecheap DNS/SPF/DMARC, sandbox recipients/production access, least-privilege IAM, IMDSv2 container credential reachability, feedback forwarding and alarms. Real email delivery is deliberately untested.
- Shared EC2-role credentials do not isolate staging from production. Application sender/recipient restrictions do not establish an IAM boundary. Accept the documented residual risk or provision enforceable workload isolation under separate approval.
- Stop old backend writers before migration/new startup. Do not perform a mixed-version rolling deployment. Returning to version-unaware code can resurrect revoked access JWTs; prefer a version-aware rollback and follow the explicit session invalidation precautions.
- GORM migration can acquire table/index locks. Take a fresh protected backup, measure migration in staging, and verify restore in isolation before production. The existing two-day retention and previously unverified restore are material rollout constraints. Production pipeline readiness is a separate decision.
- In-memory queue/IP limits are per process. Database cooldown works across processes; aggregate throughput scales with replicas. Queue-full requests and restart-lost jobs are intentionally best effort with generic responses; users can request again after the cooldown. There is no durable outbox or retry guarantee.
- Recovery preserves existing case-sensitive email identity. Changing canonical email semantics requires separate collision/migration analysis.
- Merge/install reviewed Caddy and runtime examples through the existing operator procedure; no live Caddy reload occurred. Coordinate shared-host locks without clearing recovery markers or altering backup timers.

No live AWS resources, IAM, DNS, PostgreSQL data, GitHub repository settings, backup timers, staging/production deployment, or production workflow activation were changed. Only isolated test databases and local containers were used. The feature branch and draft PR are the authorized GitHub changes; merging remains a separate approval.
