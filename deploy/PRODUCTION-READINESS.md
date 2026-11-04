# Signed production readiness

**Status: blocked for activation.** This change supplies the host verifier, the
production SSM template and the workflow opt-in. It does not supply the private
broker, recovery worker, source-bound backup producer or scheduled drill. The
standard production procedure remains in force until those components are
implemented, independently reviewed and verified together. Changed backend
execution or controller bundle requires the standard reviewed maintenance path.

## Operator quickstart

After separate activation approval, obtain a successful ordinary staging release,
review and merge the equivalent tree to `main`, then run **Actions → Promote
production** on `main` and tick the confirmation. GitHub Environment review, when
configured, is a separate gate. The production job revalidates the staging
artefact after that gate and immediately before dispatch. The controller obtains
fresh evidence after the SSM command starts. Wait for the exact run to finish and
verify the production receipt, expected images, application health and auth
routing. See [guarded production activation](PRODUCTION-ACTIVATION.md).

## Decision and trust boundary

GitHub performs release discovery and submits only the manifest and fixed mode
to a version/hash-pinned SSM document. Its production job can see SSM status.
It receives neither backup access nor evidence signing authority.

The installed controller checks the six release bundle hashes, synchronised host
time, healthy containers against the current production receipt, effective
database configuration, PostgreSQL source and schema, and the active Caddy
configuration. It pulls both exact images and checks platform and revision. These
checks run with the existing host locks and without stopping a backend writer.
It sends an invocation-specific challenge to a root-protected Unix socket. The
locks are released while the separate recovery worker checks evidence. The
controller then reacquires them in the existing order, checks for unfinished
work, compares the same baseline and validates the signature, binding, policy
and expiry before it creates the durable pending marker. It records the signed
evidence privately, drains writers, checks the PostgreSQL baseline again and
continues the existing replacement and smoke gates. A rejected preflight leaves
backups able to run. An interruption after the pending record is durable requires
the existing recovery procedure.

The broker and recovery worker are a separate trust boundary. The worker must
independently resolve the current reviewed `main` and the eligible ordinary
staging artefact, compare the exact manifest, and prove that the requested
images are eligible before it can sign. It must obtain recovery material via a
dedicated read-only identity. A compromised GitHub runner could request a
deployment or cause denial of service. It cannot mint evidence for a substituted
release unless the recovery trust boundary is also compromised. Keep signer,
recovery credentials and the release verifier outside the candidate's guest and
container trust boundary. Root, Docker daemon or signing-key compromise breaks
this model. An intercepted or previously signed response cannot answer a new
host nonce. The controller consumes evidence within one invocation and never
reloads an evidence receipt as approval. Replacing the root-installed key revokes
old signatures. Rotate it only under the drained installation freeze.

## Policy

| Gate | Required evidence and expiry |
| --- | --- |
| Every attempt | Production snapshot strictly less than 24 hours old. Validate owner account, bucket, immutable object versions or equivalent write-once controls, marker and manifest linkage, object sizes, checksums and completion before use. Reverify within 10 minutes before signing. |
| Every attempt | Successful isolated PostgreSQL restore drill strictly less than seven days old, bound to the same backup contract, source database, PostgreSQL image and server version. A weekly cadence establishes a recovery signal without a database restore on every equivalent release. Schedule every six days to allow time to retry before day seven. |
| Equivalent release | Exact ordered backend root filesystem layer digests and image config match the running image, ignoring only the OCI source revision label. All six deployment bundle hashes match the running release receipt. The drill must have exercised the same backend execution fingerprint against the same schema and equivalent runtime policy. |
| Changed or uncertain backend/bundle | Restore the same fresh production archive and run the exact candidate image. Schema and data fingerprints must agree before and after. The host validates the evidence contract and blocks automatic replacement even when those end states match. |
| Per invocation | Signed reply expires at most 10 minutes after issue. The host rejects future times, revalidates after waiting for the broker and reserves 420 seconds of the 900-second SSM execution budget for mutation. |

Backup age is measured from the snapshot, including the time it takes to
upload and verify. The current daily schedule can briefly exceed 24 hours while
the next backup completes. Retry after a verified completion. Keep the weekly
signed record separately from the two-day archive lifecycle. An aged-out drill
archive can provide historical restore evidence while a different, recent backup
provides the current recovery point.

Matching end states cannot rule out transient DDL, disruptive locks or
conditional writes on rows newer than the snapshot. Startup runs GORM
AutoMigrate and category seeding without one enclosing transaction. A later
migration automation change must implement an independently reviewed admission
policy for exact schema transitions, audit destructive and partial DDL, and
check relevant invariants at live cutover. Use the separately approved standard
backup, rehearsal and maintenance procedure in the meantime. Image rollback
cannot undo committed schema or data changes.

## Version 1 protocol

The host policy is a root-owned, singly linked 0400 or 0600 file at
`/var/lib/vellum-cd/production-readiness-policy.json`. It contains
`backup_contract` (a SHA-256 digest of the approved canonical backup profile),
`caddy_container`, `caddy_image` (Docker image ID with the `sha256:` prefix) and
`caddy_config_sha256`. Review Caddy's active config, proxy headers and routes
privately. Hash canonical JSON from its loopback-only admin API inside the
reviewed container. A disabled or unreachable admin API blocks this version.
Keep both the config and evidence out of CI logs. The Ed25519 PEM public key
lives at `production-readiness-key.pem` with the same file protections. The
corresponding private key stays with the independent signer.

The local relay listens on `production-readiness.sock` in that state directory
with root ownership and mode 0600. The host sends one canonical JSON object and
half-closes its write side. The relay returns one JSON object and closes the
connection, with a 32 KiB response limit and 420-second wait limit. The relay
authenticates its own host to the private broker and cannot sign evidence.
Use authenticated private transport and dedicated host identity. Do not publish
an internet-reachable broker endpoint or put the key in GitHub or on EC2.

The request has `schema=1`, a fresh 32-byte hex `nonce`, the full selected
`manifest` and the measured `live` object. The signed payload has `schema=1`,
`issuer=vellum-production-recovery-v1`, `request_sha256`, `issued_at`,
`expires_at`, `backup`, `drill` and `rehearsal`. The response wraps the payload
as `evidence` and the lower-case hex detached signature as `signature`. Sign
the ASCII prefix `vellum-production-readiness-v1` plus a newline followed by
the canonical payload. Use the repository's `release.canonical` for JSON
(sorted keys, two-space indentation, ASCII escaping and final newline).
Timestamps use UTC `YYYY-MM-DDTHH:MM:SSZ`. Digests use lower-case SHA-256 hex.

- `live` binds the current receipt hash, PostgreSQL image ID and version, source
  and schema digests, approved proxy hash, backup contract, backend execution and
  runtime digests, and the host-derived risk. The database source digest hashes
  canonical JSON containing PostgreSQL `system_id` and `database_oid` as decimal
  strings. The schema digest hashes the bytes of the matching PostgreSQL
  `pg_dump --schema-only --create --lock-wait-timeout=5s --restrict-key=VellumReadinessSchemaFingerprintV1`
  output. The fixed key serves only reproducible hashing. This dump is never
  executed. Use the same PostgreSQL image, database name, locale, extensions,
  role names, grants and database/role settings in the disposable restore. Use
  fresh secrets and verify global role settings separately. Runtime fingerprinting
  excludes the production DB password and JWT secret.
- `backup` includes `archive_sha256`, `completion_sha256`, `manifest_sha256`,
  `archive_bytes`, `contract`, `source_sha256`, `created_at` and `verified_at`.
  The 512 MiB existing maximum applies. Source evidence must come from the backup
  producer, linked to the actual archive by the immutable manifest. The current
  v1 backup manifest lacks source identity. Extend the producer and verifier
  compatibly before issuing readiness evidence. Do not fill in source identity
  from the request or infer it from the `production/` prefix.
- `drill` includes `archive_sha256`, `contract`, `source_sha256`, `postgres`,
  `schema_sha256`, `execution_sha256`, `runtime_sha256` and `finished_at`.
  The worker verifies an independently retained signed drill record before it
  copies these fields into the attempt envelope.
- `rehearsal` is null for low risk. For changed execution it binds
  `archive_sha256`, `postgres`, `candidate_backend`, `schema_before`,
  `schema_after`, `data_before`, `data_after` and `finished_at`. Compare
  deterministic content, including sequences and financial/authentication
  invariants. Startup health alone is insufficient. Record timings and results
  privately and keep personal data out of evidence.

The controller verifies the signature and independently checks the release,
freshness, source and live fingerprints. The recovery worker attests to the
private AWS object checks, restore and candidate execution. There is no automatic
fallback from a rejected signed request to legacy approval. Installing the host
policy also blocks legacy mode regardless of what the runner sends.

## Activation prerequisites and rollback

1. Implement and review the private relay, issuer, backup source evidence,
   scheduler, restore and rehearsal worker. Prefer an existing approved private
   recovery host with an ephemeral worker VM, encrypted scratch storage, CPU,
   memory and time limits, and a separate signer. Start PostgreSQL with no network
   and join the candidate to that namespace for loopback access only. No public
   port, host or shared web network, credential/host mounts, production secrets
   or access to either IP version of IMDS. Disable SES. Exercise a real
   `pg_restore --exit-on-error --single-transaction`, constraints, budget
   uniqueness, auth/session families and financial invariants before attesting.
   Verify egress denial and securely dispose of the guest and scratch data.
2. Provision only the worker's backup-read identity. Scope `s3:ListBucket` to
   the exact `production/completed/` prefix and `s3:GetObject` to the dedicated
   bucket's `production/completed/*` and `production/runs/*` objects. Add
   version-specific read only if the reviewed evidence format uses it. No writes
   or deletes against backups, and no new S3 permissions on the application
   instance role or GitHub deployment role. Use the expected bucket owner on
   each request. Grant the scheduler only the ability to invoke the recovery
   job and publish to its one alert target. Retain signed, encrypted drill
   receipts for at least 30 days in private storage with protected retention and
   no delete permission for the producer. Alert on failures, missing backup and
   impending drill expiry. Verify the alert path independently.
3. Use the six-file drained installation process and keep the original locks,
   pending marker, approval and prelaunch closure. Verify a new ordinary staging
   release after the matching files are installed. If the current production
   receipt is missing or its bundle differs, use an approved standard promotion
   to establish a real receipt before installing the automated host policy.
   Collect a qualifying drill against that baseline. Validate both paths in a
   disposable environment, including cancellation, failure after writer drain
   and no-op rehearsal rejection when evidence is incomplete.
4. Separately publish the reviewed production SSM document under the existing
   name and independently read its new version and AWS metadata hash. Check the
   IAM and SSM Agent boundaries and production Environment's protected-`main`
   restriction. Set production Environment variables
   `SSM_READINESS_DOCUMENT_VERSION`, `SSM_READINESS_DOCUMENT_SHA256` and
   `PRODUCTION_READINESS_MODE=automated` only after approval. Keep
   `PRODUCTION_APPROVAL_MODE=manual` for the GitHub dispatch gate. A local hash
   of the document JSON is insufficient for the AWS metadata pin.
5. For approval-system rollback, freeze dispatches, quiesce all related work
   and inspect the existing locks and pending record. Under reviewed operator
   maintenance, retire the automated policy, preserve the signed receipts and
   historical approval/closure, and restore the reviewed workflow's v2/manual
   policy with freshly gathered standard evidence. Keep a compatible installed
   controller and verify bundle hashes. Never reuse expired approval, restore an
   older database or clear the recovery marker as part of policy rollback.

The reported instance-role `s3:ListBucket` denial and container access to an
IMDSv2 token are historical operator observations. The source writer policy
grants `GetObject` for known keys. Independently verify deployed IAM and
metadata isolation. This change does not address the shared production host's
instance-role exposure.

For sizing, start with 2 vCPU, 4 GiB memory and 20 GiB encrypted scratch,
conditional on archive expansion limits. Budget 5–10 minutes for a weekly drill
and up to 7 minutes for a promotion-time recovery worker. These are design
targets. The local synthetic exercise does not predict production restore
runtime. On existing approved capacity, the marginal compute cost depends on
available headroom. A separately approved ephemeral VM at an illustrative
USD 0.06/hour for 5 drill runs and 4 rehearsals of 10 minutes each is USD 0.09
per month in compute, before storage, networking, alerts and tax. A continuously
retained 20 GiB volume at an illustrative USD 0.10/GiB-month is USD 2/month.
Price the approved region and network design separately. No Terraform resources
are introduced. [AWS EC2 pricing](https://aws.amazon.com/ec2/pricing/on-demand/)
and [AWS EBS pricing](https://aws.amazon.com/ebs/pricing/).

| Failure | Operator action |
| --- | --- |
| Missing, stale or incompatible backup/drill | Check the private recovery alert and object/restore evidence. Wait for a real qualifying run, then dispatch again. |
| Running baseline or proxy differs | Investigate deployment, schema or proxy drift privately. Re-review the policy and re-run the drill if the baseline legitimately changed. |
| Backend or bundle changed | Use reviewed maintenance with a real candidate rehearsal, fresh standard approval and a coordinated policy transition. |
| Invalid signature, broker unavailable or wrong SSM pin | Check the protected relay, trusted key and reviewed AWS metadata. Preserve the evidence and treat unexplained mismatches as an incident. |
| Unfinished host operation | Follow [host recovery](HOST-COORDINATION.md). Verify quiescence, database state and matching receipts before any authorised recovery. |
