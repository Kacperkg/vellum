# Production recovery worker

The worker verifies source-bound version 2 backups and produces independent
backup-verification, restore-drill and optional candidate-rehearsal receipts.
Install and enable it only after separate approval. The production issuer still
decides admission and may require maintenance even with a rehearsal receipt.

## Worker boundary

Use a dedicated recovery VM with a local Docker Engine and a distinct instance
profile based on [worker-policy.example.json](worker-policy.example.json). The
worker needs no Vellum or shared application host mount, live database route,
SSH connection to production or production application secret. Keep it in a
private subnet with no inbound rules or public IP. Supply approved outbound
paths for S3, KMS, SNS and CloudWatch on the worker host. Pin IMDSv2 with a
response hop limit of one and disable the IPv6 metadata endpoint. The worker
host needs its instance profile. Both sandbox containers use a network
namespace with only loopback, regardless of the host's metadata settings.

Start with a reviewed existing isolated recovery VM if one is available. The
timer units below assume that VM stays available. For a disposable VM launched
by a separate scheduler, run the same CLI on demand after boot, retain the
redacted job record on encrypted persistent state, enforce a hard maximum
instance lifetime in the launcher, and wait for durable receipts before issuer
dispatch. A deployment request for a disposable VM must trigger fresh
verification before dispatch. Provisioning and validating that launcher and
its failure routing are separate activation work.

- Reserve at least 6 GiB RAM for the VM, disable swap, encrypt its root and
  state volumes and disable core dumps. The worker mount caps downloads at
  768 MiB. PostgreSQL's 2 GiB tmpfs holds the logical restore, its physical
  standby copy and WAL. Docker enforces a 3 GiB memory and two-vCPU limit for
  PostgreSQL and 256 MiB/one vCPU for the backend. A large compressed dump may
  pass verification and exceed the restore cap. That fails the drill and
  requires operator sizing and review. Do not move scratch to a persistent
  unencrypted mount.
- Install reviewed files under the same relative paths beneath
  `/usr/local/lib/vellum-recovery`. Include `deploy/recovery/worker.py`,
  `deploy/recovery/restore-checks.sql`,
  `deploy/recovery/runtime/prepare-roles.sql` and
  `backend/internal/database/runtime_policy.sql`. The installation and all
  ancestors must be root-owned and non-writable by other users. Run Python 3.11
  or later with OpenSSL 3, AWS CLI v2, and a Docker Engine with memory and PID
  limits and its default seccomp profile enabled. Require working clock
  synchronisation and a kernel supporting the tmpfs `noswap` mount option.
- Install the producer public key and the [worker profile](worker.example.json)
  as root-owned private files in `/etc/vellum-recovery`, then fill in the
  reviewed account, source identity and baseline. Replace the entire sample
  `backend_runtime` map with the resolved production backend settings excluding
  `DB_PASSWORD` and `JWT_SECRET`. Hash it using the release canonical JSON
  encoding. Keep the map private, including recovery-recipient settings.
  The sandbox substitutes the local standby endpoint, disposable password and
  JWT secret and disables SES. Record the configured worker profile SHA-256
  with the issuer separately. Set the harness digest from
  `python3 -I deploy/recovery/worker.py harness-fingerprint` against the
  reviewed files. Confirm the source's database locale, encoding,
  role names and exact PostgreSQL 17 Alpine image match the profile.
- Preload both image digests on the trusted VM before running the job. The
  worker itself never pulls an image and verifies the PostgreSQL image ID and
  backend execution fingerprint before use. Configure the receipt bucket with
  versioning, S3 Block Public Access, server-side encryption, conditional
  creation enforcement, and retention of drill and rehearsal evidence for at
  least 30 days. Give the worker no delete or retention-bypass permission.
  The worker checks account and region on the two buckets and publishes
  privately with a separate Ed25519 KMS key. Keep that key distinct from the
  backup producer and the release issuer. Record its public key in the
  independent issuer configuration.
- Install the `systemd/` units after review. Enable the bounded tmpfs mount
  and the verification, drill and health timers separately. The drill runs
  every three to four days. The verifier downloads each new exact archive
  once, revalidates its pinned SHA-256 metadata when the same version recurs,
  and continues to download and verify the signed marker and manifest each
  time. It refreshes evidence every five minutes while the machine is
  available. A drill always downloads and restores the full archive. The
  worker serialises jobs and records started, failed or completed states under
  `/var/lib/vellum-recovery`. Failed jobs leave a durable cleanup pointer.
  `ExecStopPost` removes only those precisely labelled disposable containers
  and scratch, then verifies cleanup. Monitor a stuck pointer or failed
  cleanup. Retain state for incident review and rotate old redacted job files
  under a separately reviewed retention policy.
- Provision two external CloudWatch alarms on `Vellum/Recovery` metrics
  `FreshBackupVerification` and `RestoreDrillBeforeExpiry`, without dimensions.
  Use five-minute periods, minimum statistic, a threshold below 1 and
  missing-data treatment `breaching`. Three consecutive periods allow
  verification to resume after a short drill. Send both alarms to the
  reviewed SNS topic and independently confirm a subscribed destination
  receives test alarms. The drill metric goes to zero at five days, two
  days before the issuer stops accepting the proof. Loss of the VM or its
  CloudWatch route results in a missing heartbeat. The direct worker SNS
  acknowledgement proves acceptance by SNS. Test downstream delivery
  separately.

## CLI and receipt boundaries

```sh
python3 -I /usr/local/lib/vellum-recovery/deploy/recovery/worker.py verify-backup
python3 -I /usr/local/lib/vellum-recovery/deploy/recovery/worker.py drill
python3 -I /usr/local/lib/vellum-recovery/deploy/recovery/worker.py rehearse \
  --candidate-profile /etc/vellum-recovery/candidate.json
```

Use both `--completion-key` and `--completion-version` to select an exact
completion. Without them the worker selects the newest visible production
completion in its bounded 24-hour listing and independently resolves its
version. Missing, stale or malformed completion stops the job. Backup
freshness uses the producer's signed snapshot time. Drill freshness uses the
worker's completion time and never extends the backup recovery point.

The worker accepts a candidate profile only from a separate root-protected
file. It binds the current worker profile digest, exact backend image digest,
image ID, execution digest and migration contract. Fields are
`profile_sha256`, `image`, `image_id`, `execution_sha256` and
`migration_contract`. The candidate runs `serve` with the restored,
restricted application role against the exact read-only physical standby.
It receives no archive path, network egress, signing credentials or live
database secrets. The trusted harness checks the migration ledger, compares
schema/data digests before and after the process and checks application
readiness. A changed contract or baseline blocks this method. A successful
read-only smoke and unchanged hashes cannot establish the safety of arbitrary
schema changes or data transformations.

A successful receipt is immutable and scoped to one of the three paths below.
The envelope contains `evidence` and a lowercase hexadecimal Ed25519
`signature`. Sign the matching domain plus LF and canonical evidence.
The exact bucket object references include their version, size and digest.
The issuer pins the worker profile and verifies signatures independently.

| Path under `production/recovery/` | Signature domain |
| --- | --- |
| `verified-backup/` | `vellum-production-verified-backup-v1` |
| `restore-drill/` | `vellum-production-restore-drill-v1` |
| `candidate-rehearsal/` | `vellum-production-candidate-rehearsal-v1` |

## Disposable verification

Run `python3 -m unittest discover -s deploy/tests -p test_recovery_worker.py`
for version-pinning, source, signature and bounded-command failures. Run
`python3 deploy/tests/recovery_restore_integration.py` with local Docker, a cached
PostgreSQL 17 Alpine digest and a local backend image built from
`backend/Dockerfile.prod` to exercise the actual PostgreSQL restore,
post-snapshot write exclusion, read-only baseline and candidate startup and
truncated archive rejection. This fixture creates only synthetic records
and performs no AWS calls.
