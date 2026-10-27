# Password recovery with AWS SES

This feature is disabled by default. This runbook is an operator procedure, not permission to change AWS, DNS, runtime configuration, or deploy. Never enable sending during CI. Use mocks for automated tests.

## Implementation and contracts

`POST /forgot-password` accepts `{"email":"user@example.com"}` and returns the same confirmation for an existing, unknown, suppressed, disabled, queue-full, or delivery-failed request. Invalid request syntax receives validation errors independent of account existence. The request enters a bounded, single-worker memory queue; no SES request or user lookup is on the HTTP response path. Queued requests are lost on restart; there is no durable outbox or automatic recovery guarantee. Ask the user to try again after the cooldown. No unlimited retries or background goroutines are used.

`POST /reset-password` accepts `{"token":"…","new_password":"…"}`. Invalid, expired, and previously used links share an error. Success changes the bcrypt password, increments the user authentication version, revokes all refresh sessions, and consumes outstanding recovery links in one transaction. It never signs the user in. Request a new link after success or expiry.

Tokens contain 32 random bytes, encoded as unpadded URL-safe base64. Only SHA-256 hashes are stored in PostgreSQL. The email link uses `/reset-password#token=…`: fragments are not sent in HTTP requests. The frontend captures the token in memory and replaces the URL before router initialization. Reloading the cleaned page requires reopening the email link. Neither token storage nor automatic login is used. Recovery responses and pages use no-store/no-referrer headers. Do not add analytics, third-party scripts, query logging, or request-body capture to these routes.

Email identity follows the existing case-sensitive registration identity; recovery must not silently lowercase legacy addresses and accidentally choose another user's account. Users should enter the address exactly as registered. A future canonical-email migration requires duplicate/collision analysis and separate review.

## Migration and session transition

Startup GORM AutoMigrate adds `users.auth_version` and `refresh_tokens.auth_version` with default zero plus the password reset table and indexes. Existing users and refresh sessions retain version zero. Legacy access JWTs without the claim are treated as zero: they remain valid only until expiry or a reset increments the user's version. Every protected request checks the current database version and fails closed on a lookup failure. A reset invalidates previously issued access JWTs and refresh sessions. Requests already authenticated and in progress at reset time may finish; reset is not a cancellation mechanism for already-running financial transactions.

Login and refresh persistence serialize with reset by locking the user row first and verifying the authenticated password snapshot and version. Refresh rotation rechecks the current session after acquiring the user lock. Reset locks the same user before consuming token rows; concurrent redemptions cannot both succeed. SES network calls occur after database transactions commit.

**Do not run old and new backend writers concurrently.** The old application does not enforce versions or participate in this locking protocol. Use the staging controller barrier in [STAGING-AUTH-ROLLOUT.md](STAGING-AUTH-ROLLOUT.md): it stops, verifies and removes only old staging backend containers, waits for staging database client sessions to drain, then starts the candidate; expect a brief API interruption. The matching reviewed controller bundle must be installed before the release. Production replacement behavior is unchanged and requires its own approved stop-first procedure. This requirement applies even when email delivery remains disabled. Do not implement a rolling rollout. Older images can ignore the added columns, but reverting to an old image would remove version validation and could resurrect access JWTs until they expire. The supported recovery path is a reviewed version-aware roll-forward release. Any exceptional old-code rollback requires a separately designed and approved security/data migration, including a traffic freeze and complete session invalidation; secret rotation alone is not sufficient approval, and neither installed-controller downgrade nor rewriting receipt hashes is permitted. Do not drop columns or restore a stale financial backup simply to roll back application code.

AutoMigrate may acquire PostgreSQL table/index locks; measure duration with a representative staging snapshot and review the resulting DDL first. Adding columns with defaults and index creation are additive but are not a zero-downtime guarantee. Take a fresh backup before production migration and verify restore in an isolated environment. Existing two-day S3 retention and an unverified restore are insufficient evidence of recoverability. Coordinate the shared-host deployment lock and recovery state with the separate production-readiness work; do not clear markers or change backup timers.

## Configuration

Set these in the root-owned environment file under `/etc/vellum-cd/` and install the reviewed Compose file so values reach the container. Keep permissions and existing DB/JWT values intact. Example files intentionally use `disabled`.

| Variable | Default / use |
| --- | --- |
| `APP_ENV` | `development`; set `staging` or `production` before SES mode |
| `PASSWORD_RESET_EMAIL_MODE` | `disabled`; opt in with `ses` after approval |
| `AWS_REGION` | `eu-west-2` |
| `SES_FROM_ADDRESS` | Exact environment sender: staging `no-reply-staging@kacpergajdarski.com`, production `no-reply@kacpergajdarski.com` |
| `PASSWORD_RESET_FRONTEND_URL` | Exact HTTPS origin plus `/reset-password`, no query or fragment |
| `PASSWORD_RESET_TOKEN_TTL` | `20m`, maximum `1h` |
| `PASSWORD_RESET_COOLDOWN` | `1m`, minimum `1m`, database-enforced per user |
| `PASSWORD_RESET_ALLOWED_RECIPIENTS` | Comma-separated exact mailboxes; required in staging SES mode |
| `PASSWORD_RESET_QUEUE_SIZE` | `32`, maximum `256` |
| `PASSWORD_RESET_SEND_TIMEOUT` | `5s`, maximum `10s` |
| `RATE_LIMIT_FORGOT_PASSWORD_REQUESTS/WINDOW` | `3` / `1h` |
| `RATE_LIMIT_RESET_PASSWORD_REQUESTS/WINDOW` | `10` / `1m` |

Keep `TRUSTED_PROXY_CIDRS` restricted to Caddy's stable /32 or /128, or the controlled Docker web subnet. The existing limiter handles trusted client identity, shared-network capacity and IPv6 grouping. In-memory IP and queue limits apply per backend instance; the database cooldown serializes per-account sends across instances. Horizontal scaling multiplies aggregate throughput. SES account quotas and operator alarms are additional controls, not a replacement for application limits. Do not configure an indefinite account lockout. Protect container stdout and avoid logging email addresses, links, raw tokens, passwords, AWS error payloads, or SQL parameter values.

## Operator setup and approval checkpoints

1. **Approval: AWS/DNS preparation.** In SES **eu-west-2**, create and verify the domain identity for `kacpergajdarski.com`. Obtain Easy DKIM records from SES. At Namecheap Advanced DNS, add the exact CNAME names/values supplied by SES (Namecheap host fields are relative to the managed domain; avoid duplicating the domain suffix). Do not copy invented selectors from documentation. Wait for SES to show verification and DKIM success. Use separate staging sender `no-reply-staging@kacpergajdarski.com` and production sender `no-reply@kacpergajdarski.com`.
2. Configure aligned DKIM and a DMARC record at `_dmarc` with a monitored reporting mailbox. Start a reviewed monitoring policy and move to enforcement after confirming all legitimate senders. For aligned SPF, configure a custom MAIL FROM subdomain with the exact SES MX and TXT values shown in this region. SES provides the `feedback-smtp.eu-west-2.amazonses.com` MX target and SPF include when configuring custom MAIL FROM. Preserve existing mail-provider SPF: one SPF record per hostname; do not create conflicting TXT policies or overwrite working mail records. DKIM alignment can satisfy DMARC; the default SES MAIL FROM does not provide aligned SPF. Check actual headers in approved delivery testing.
3. While SES is in the sandbox, verify each permitted test recipient (or use the SES mailbox simulator only after explicit sending approval). Sandbox and sending quotas are regional. Keep staging recipient restrictions populated even after production access is granted. Record the tested identities and region without secrets.
4. **Approval: IAM and credential path.** Grant only `ses:SendEmail` on the verified sender identity ARN, with an exact `ses:FromAddress` condition. See the illustrative policy below. Add no administrator, identity-management, SMTP, or `SendRawEmail` permissions. Use the SDK default credential chain with the EC2 instance role. Do not place AWS keys in environment files or images. Confirm container access to IMDSv2 without printing credentials; container networking may require a reviewed metadata hop-limit change. This is an infrastructure change requiring separate approval.
5. **Shared-role limitation:** staging and production share an EC2 role. Sender conditions on that role constrain the role as a whole; if the role allows both senders, either compromised container can use both. Recipient allowlists and application mode checks are application safeguards, not IAM isolation. For strict separation, use separately scoped roles with enforceable workload credential boundaries or separate hosts/accounts. Do not claim role-level isolation on the current host or silently widen IAM during deployment. Operator must explicitly accept this residual risk before enabling production sending.
6. Enable SES bounce/complaint feedback forwarding on the identity. This sender does not override `FeedbackForwardingEmailAddress`: feedback goes to the From mailbox by default. Both `no-reply` and `no-reply-staging` must therefore be deliverable mailboxes or aliases that forward to an operator-monitored inbox; verify this path with an approved simulator test before enabling user delivery. Establish alarms for complaints, bounces, rejects, send volume, and quota exhaustion. Review suppression-list behavior and escalation ownership. If adding SNS/configuration-set event destinations, do so as separately approved infrastructure; this implementation does not provision them or require them to send. Never place recovery links or recipient identities in public dashboards or issue comments.
7. **Approval: staging runtime and Caddy installation.** Back up and merge `deploy/Caddyfile.example` into the installed Caddy configuration; preserve other virtual hosts and the shared `web` network. Both site blocks forward **POST and OPTIONS** for `/forgot-password` and `/reset-password` to Go, while GET deep links reach the SPA. Validate the merged file inside the Caddy container before a separately approved reload. Install the reviewed Compose/configuration changes under the existing deployment-controller procedure; preserve ownership, locking and immutable image digests. Set staging mode and exact verified recipients, keep sending disabled initially.
8. **Approval: staging deployment.** Follow the exact installation/rollout sequence in [STAGING-AUTH-ROLLOUT.md](STAGING-AUTH-ROLLOUT.md). Take and verify a staging backup; stop the old backend before starting the new version. Deploy only using the approved staging procedure and host coordination. Check migration, login, refresh, logout, protected requests, both recovery deep links, POST routing, same-origin behavior, allowed-origin OPTIONS preflights and headers. Unexpected origins must not get permissive CORS. This task does not activate or change deployment workflows.
9. **Approval: real SES test.** Enable staging `ses` mode only after identity, IAM, IMDS and recipient checks. Send one approved request to an allowed test account. Confirm text/HTML, HTTPS staging link, DKIM/DMARC, expiry, single use, generic unknown-account response, cooldown, old-password rejection, old access/refresh rejection and successful new-password login. Verify timeout/failure metrics contain no tokens or recipient data. Do not attach raw emails or links to the PR. Disable sending on unexpected volume or delivery failures.
10. **Approval: production access request.** Request SES production access in eu-west-2 with an accurate transactional password-recovery use case, abuse controls, bounce/complaint process and expected volumes. Wait for approval; the request itself is a live AWS action. Review quotas and alarms. Sandbox exit does not prove application readiness.
11. **Approval: production rollout.** The production pipeline is being assessed separately; do not assume it is ready. Require independent production-readiness approval, verified restore capability, a fresh protected backup, shared-role risk acceptance and a tested migration/rollback plan. Stop old writers, deploy digest-pinned version-aware images, start with sending disabled, smoke test, then separately approve SES mode. Preserve separate production/staging DBs, secrets and frontend origins.
12. **Monitoring/rollback.** Monitor generic delivery outcomes, latency, 429s, queue pressure, DB failures and SES health. A failed send cannot expose account existence through the public response. A process restart loses unsent memory jobs, so recovery is best effort. Disable `PASSWORD_RESET_EMAIL_MODE` first to stop new sends, retain the version-aware backend and investigate. A new runtime value requires the approved container recreation procedure. Use a tested version-aware image for rollback; never assume image rollback undoes migration or password changes. Follow the session precautions above for any old-image rollback.

## IAM policy example (review only)

Replace the illustrative account ID and verify the actual identity ARN before use. This is a role identity policy, not an SES cross-account sending-authorization policy. Staging recipient constraints may be added to a separately scoped staging role, but a shared role with a production allow statement cannot enforce staging-only recipients.

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": "ses:SendEmail",
    "Resource": "arn:aws:ses:eu-west-2:123456789012:identity/kacpergajdarski.com",
    "Condition": {
      "StringEquals": {"ses:FromAddress": "no-reply@kacpergajdarski.com"}
    }
  }]
}
```

## Retention and verification

Recovery rows contain hashes and timestamps, never plaintext. The worker removes at most 1,000 rows every 15 minutes once their expiry is more than 24 hours old; this bounded cleanup exceeds one process's maximum hourly issuance capacity. Any manual cleanup must be bounded, parameterized, operate only on expired/consumed recovery records, and run under the usual database-change approval. Do not remove users or financial records.

Before approval, rerun backend build/vet/tests/race tests with PostgreSQL 17, frontend tests/typechecking/lint/format/build/browser flows, both Docker builds, Compose/Caddy validation, deployment safety/provenance tests, and Docker secret exclusions. No automated test may use real SES credentials or send email. Test evidence and independent reviewer findings are recorded with the implementation PR.

AWS references: [SES IAM](https://docs.aws.amazon.com/ses/latest/dg/control-user-access.html), [DMARC alignment](https://docs.aws.amazon.com/ses/latest/dg/send-email-authentication-dmarc.html), [custom MAIL FROM](https://docs.aws.amazon.com/ses/latest/dg/mail-from.html), [sandbox exit](https://docs.aws.amazon.com/ses/latest/dg/request-production-access.html), [DKIM](https://docs.aws.amazon.com/ses/latest/dg/send-email-authentication-dkim.html).
