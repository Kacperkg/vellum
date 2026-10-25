# Daily PostgreSQL backups to S3

This is an operator-reviewed installation proposal. Adding or merging these files
does not configure AWS, install scripts, enable timers or run a backup. A merge to
`staging` can trigger the existing application release workflow; approve that
separately. No application, PostgreSQL volume, credential, Caddy or CD-controller
change is needed for backup setup.

The accepted policy is one custom-format PostgreSQL 17 dump per environment per
day, SSE-S3, `eu-west-2`, and S3 expiration after two days. There is no versioning,
PITR, cross-region/account copy, Object Lock or long-term archive. Corruption or
compromise discovered after the retained snapshots expire may be unrecoverable.

## Execution and paths

| Item | Legacy script | Proposed installed workflow |
| --- | --- | --- |
| Entry point | Checkout `deploy/backup.sh` | Root-owned `/usr/local/lib/vellum-cd/backup.sh` and `backup.py` |
| Env files | Checkout `deploy/<environment>.env` | Existing `/etc/vellum-cd/<environment>.env` |
| Compose | Checkout `deploy/compose.yml` | Existing trusted `/usr/local/lib/vellum-cd/compose.yml` |
| Destination | Caller-supplied optional S3 URI | Required bucket/account in root-private `/etc/vellum-cd/backup.json` |
| Local artifacts | Unbounded checkout backups | Private run directories under `/var/lib/vellum-cd/backups/` |
| Locks | Checkout lock per environment | `/var/lib/vellum-cd/backup.lock`, then `staging.lock`, then `production.lock` |
| Success | Local dump, optionally uploaded | Archive, manifest and completion marker uploaded and verified |

Only `backup.sh staging` and `backup.sh production` create backups. The optional
`--check-freshness` argument performs S3 reads only and needs no database/env-file
access. The entry point refuses checkout execution and non-root use. Configuration
is parsed as data; dotenv files are never sourced. Paths, owners, modes, links and
parents are checked. Child processes receive a clean environment and fixed region,
local Docker socket and AWS configuration; EC2 instance-profile credentials are
used, not user profiles or static keys.

Each run records conservative snapshot/start time, dump completion, PostgreSQL
version, archive size and SHA-256. Unique keys under `<environment>/runs/` prevent
overwrite. Single-object PUTs include SHA-256 and explicit `AES256`; HEAD verifies
size, checksum and encryption. A marker under `<environment>/completed/` is created
only after archive and manifest verification. This proves transfer integrity,
not that the database can be restored. Do not set CD `restore_verified` based on
`pg_restore --list` or an upload.

The shared host gate serializes backups, both environments' controller deployments
and guarded operator schema work. It preserves both environment locks and retains
a durable pending record after incomplete work, because Docker/database processes
can outlive their client. See [host coordination](../HOST-COORDINATION.md) for
lock order, interrupted-operation recovery, the required paired root-owned
controller/backup installation and manual maintenance requirements. Never invoke
a controller recursively while holding its locks. A scheduled backup is not an
automatically atomic pre-release backup gate; keep release-specific backup/schema
approval. Autonomous backend restarts and raw SQL/Docker require the operational
controls described there.

The archive limit is 512 MiB, aggregate spool cap is 1 GiB, and free-space reserve
is the greater of 5 GiB or 20% of the containing filesystem. Failure to satisfy
these limits stops the run. Incomplete dump files are removed on handled failures.
Complete dumps with failed uploads are retained privately; **there is no automatic
failed-dump deletion**. Inspect and resolve those failures promptly. If retained
copies consume the cap, further backups fail and alert until an operator uploads
and verifies them or explicitly approves removal. Never delete the last usable
copy merely to clear an alarm. Crashes can leave partial files: inspect their
specific run directories under the locks before approving cleanup.

Any incomplete run retains the host pending record and blocks later managed work
until operator recovery. SIGTERM/SIGINT follow the handled cancellation path.
A forced SIGKILL or host
failure cannot guarantee remote-process cleanup: before any deployment, verify
that no orphaned dump remains or wait at least the in-container hard timeout
(approximately 16 minutes from interruption). Do not bypass a held lock.

Successful runs delete only their own temporary files after verified off-host
success. Existing checkout dumps remain untouched. The PostgreSQL container has a
bounded dump timeout, and uploads use bounded retries/timeouts. The root systemd
service has a 45-minute start limit and a 20-minute stop grace period for
rare cancellation paths that must await the in-container dump timeout. No parallel dump or restore is appropriate
on the shared 2 GiB instance. Watch memory, disk/inodes, CPU credits and application
latency; scheduling process niceness does not constrain PostgreSQL's server work.

## Pre-install inventory and approval

These checks have **not** been performed on EC2/AWS by this implementation.
Before rollout, an authorized operator must:

1. Inventory existing root/user cron, `/etc/cron.*`, systemd timers/services,
   SSM maintenance/association schedules and any other backup jobs. Inspect S3
   buckets, lifecycle, encryption, ownership, versioning, notifications and role
   policies read-only. Do not infer absence from cron alone.
2. Privately confirm the running projects are exactly `vellum-production` and
   `vellum-staging`, identify PostgreSQL image digests/versions and volume IDs,
   measure each `pg_database_size`, filesystem free bytes/inodes and actual dump
   size after the first approved run. Do not print resolved Compose or env files.
3. Review the exact implementation commit, tests and hashes. Confirm current AWS
   CLI v2 (at least 2.17.34, supporting `s3api put-object --if-none-match`), Python 3.12+,
   Docker Compose v2 and `timeout` in the running PostgreSQL 17 Alpine image.
4. Retain bootstrap configuration, `/etc/vellum-cd/` recovery material and matching
   application/PostgreSQL image references securely outside this instance.
5. Select the bucket/account and confirmed SNS email recipient. The user has
   accepted the daily RPO exposure and two-day retention; still record operator
   responsibility for alerts, capacity and recovery testing.

## AWS templates — operator application only

Replace every `REPLACE_*` value privately; do not apply an unedited example.
Use a **dedicated** general-purpose bucket in `eu-west-2` with all four Block
Public Access settings enabled, bucket-owner-enforced ownership/ACLs disabled,
default encryption `AES256`, and versioning **never enabled** for this minimal
policy. These creation settings are operator tasks; no template creates a bucket.

- [Bucket policy](bucket-policy.example.json): deny non-TLS requests and uploads
  that do not explicitly request SSE-S3. It grants no access itself.
- [Lifecycle](lifecycle.example.json): expire both prefixes after two days and
  abort incomplete multipart uploads after one day. The uploader itself uses
  single PUTs. Defensive noncurrent-version and expired-delete-marker rules are
  included if versioning was previously enabled; inspect retained versions before
  applying to an existing bucket, because those rules delete them.
- [Writer IAM](writer-policy.example.json): exact bucket/prefix `PutObject` and
  `GetObject` (required for HEAD), plus optional exact-topic `sns:Publish`. No
  deletion, listing, ACL, lifecycle, bucket-policy or other administration grant.
- [Independent verifier IAM](verifier-policy.example.json): completion-prefix
  listing, exact backup-prefix reads and optional exact-topic notification. It
  cannot upload/delete backups. Remove SNS statements if no topic is used.

Both environments share one instance profile; prefix separation does not protect
production backups from root compromise of that host. Restrict Docker/root access
and block application-container access to EC2 metadata. SSE-S3 protects stored
objects, not against an already authorized reader. Local artifacts rely on root
permissions and the operator's EBS encryption configuration.

S3 computes `Days: 2` expiration by adding two days then rounding up to the next
midnight UTC. Thus eligibility is approximately two to three days after upload,
with asynchronous deletion. This is not an exact 48-hour retention guarantee.

## Install reviewed bytes, without enabling timers

The following is for a **future separately approved rollout**. Do not sudo-run
scripts from the ubuntu-writable checkout. Copy files as inert data into a new
root-owned staging directory, verify that copy against SHA-256 values from the
approved immutable commit/review channel, and only then install/execute it. A
hash manifest generated from the same untrusted checkout is not independent
verification. Reject existing symlinks/untrusted ancestors at all destinations.

For example, after selecting the approved committed revision and separately
recording its expected SHA-256 values:

```sh
sudo install -d -o root -g root -m 700 /root/vellum-backup-install
sudo install -o root -g root -m 600 deploy/backup.sh deploy/backup.py /root/vellum-backup-install/
sudo install -o root -g root -m 600 deploy/backup/systemd/*.service deploy/backup/systemd/*.timer /root/vellum-backup-install/
# Operator creates root-private SHA256SUMS here from approved review evidence.
sudo sh -c 'cd /root/vellum-backup-install && sha256sum -c SHA256SUMS'
# Continue only if every expected code/unit file is listed and verifies.
sudo install -d -o root -g root -m 755 /usr/local/lib/vellum-cd
sudo install -d -o root -g root -m 700 /var/lib/vellum-cd /var/lib/vellum-cd/backups
sudo install -o root -g root -m 755 /root/vellum-backup-install/backup.sh /usr/local/lib/vellum-cd/backup.sh
sudo install -o root -g root -m 644 /root/vellum-backup-install/backup.py /usr/local/lib/vellum-cd/backup.py
```

Before resuming either code path, install the matching six-file controller bundle
including root-owned `/usr/local/lib/vellum-cd/cd/coordination.py` during the same
drained maintenance freeze as these backup files; see [host coordination](../HOST-COORDINATION.md).
Do not replace env files or bypass bundle provenance checks. Older installed
controllers require the reviewed operator upgrade; active mixed versions are unsupported.
Have root create `/etc/vellum-cd/backup.json` at mode `600` from
[backup.example.json](backup.example.json), filling the exact bucket, 12-digit
account and optional SNS topic ARN privately. The existing `/etc/vellum-cd/`
should remain root-owned `700`. Topic configuration `null` disables script SNS
notifications; journal-only failures are insufficient unattended monitoring.

Install only `vellum-backup@.service`, `vellum-backup-production.timer` and
`vellum-backup-staging.timer` from the verified root staging directory to
`/etc/systemd/system/` as root-owned `644`. Check their hashes after install.
Validate units before reloading systemd:

```sh
sudo systemd-analyze verify /etc/systemd/system/vellum-backup@.service /etc/systemd/system/vellum-backup-production.timer /etc/systemd/system/vellum-backup-staging.timer
sudo systemctl daemon-reload
```

Do not enable timers yet. Disable superseded jobs only after understanding their
purpose and approving the replacement. Existing application/CD services remain
unchanged.

## Approve drills, then scheduling

1. Approve a single staging backup using the installed entry point or
   `systemctl start vellum-backup@staging.service`. Inspect sanitized journal and
   exact archive/manifest/completion objects. Perform the [isolated S3 restore
   drill](RESTORE.md). Record timings, checksum, selected keys and private evidence.
2. Separately approve a single production backup and isolated drill, with no
   alteration of the live database, application or volume. Confirm capacity and
   latency were acceptable. Do not connect the restored copy to public staging.
3. Confirm SNS subscription delivery and test a controlled failing checker, such
   as a read-only checker pointed at an intentionally empty test bucket through
   its root-managed configuration. Restore the approved configuration afterward.
   Permission/config-load failures can prevent script alerts; verify the external
   monitor detects missing data too.
4. On EC2, approve and enable only the two backup timers. They use explicit UTC:
   production 02:15, staging 03:15. `Persistent=true` catches up one missed run
   after boot; lock contention is a failed run, not a queued job. Investigate and
   retry under the normal approval policy rather than overlapping deployments.
5. Independently confirm the timer execution, fresh completion marker and sample
   object integrity. Observe expiration behavior after two days and review disk,
   CPU and memory. Backup readiness remains unproven until the S3 round trip and
   application validation succeed.

## Failure and independent freshness checks

The script sends a sanitized failure message to the configured SNS topic when
configuration has loaded and SNS is reachable. The service failure remains in
journald. An SNS/API outage or failure before configuration load can prevent
notification, so an EC2-only timer cannot be the only monitor.

On a separate already managed Ubuntu host with an instance profile, install the
same verified `backup.sh`/`backup.py`, private state/config and the
`vellum-backup-freshness@.service` plus its two timer files. Do **not** install or
enable backup timers there; no Docker, Compose or database env files are needed.
Apply the verifier policy, not writer permissions. Set the same bucket/account
and an independently tested alert topic. Its daily checks run at 04:45/05:45 UTC
and fail if the newest completed snapshot is older than 26 hours, if its object
references do not match, or if sizes/checksums/encryption fail. The host's own
monitoring must also alert on failed or missed freshness service runs. This
catches an EC2 host outage or missed daily backup the following morning, more
than 26 hours after the prior expected snapshot and before two-day expiration.

If no independent host/scheduler exists, use an operator's existing external
monitor to call an equivalent read-only S3 check daily. Record that dependency
explicitly; do not claim independent stale detection until it is configured and
tested. No Lambda, EventBridge or other AWS resources are created by this change.

## Cost model, eu-west-2

Current public AWS offers checked for this implementation: S3 publication
2026-10-15, SNS publication 2026-09-15. USD, before tax, account-level free tiers
and discounts. Actual database/dump sizes have not been measured; an exact total
cannot be stated yet.

| Component | Public rate / estimated increment |
| --- | --- |
| S3 Standard, first 50 TB | $0.024 per GB-month |
| S3 PUT/COPY/POST/LIST | $0.0053 per 1,000 requests |
| S3 GET/HEAD and other requests | $0.00042 per 1,000 requests |
| Two daily backups, three PUT + three HEAD each, 30 days | $0.000954 PUT + $0.0000756 HEAD |
| Two daily freshness checks, one LIST + one GET + two HEAD each | $0.000318 LIST + $0.0000756 GET/HEAD per 30 days |
| SNS Standard requests | First 1 million/month free, then $0.50/million |
| SNS email/email-JSON delivery | First 1,000/month free, then $2/100,000 |

The request examples exclude retries, operator downloads and drill checks. SNS
free allowances are shared at account level; failure notifications are not
unconditionally free. No KMS charge applies. There is no additional charge for
same-region EC2/S3 transfer; private restore-drill compute/storage, internet or
cross-region downloads and an external monitoring host are additional costs.

For compressed dump sizes `Dp` and `Ds` GB, budget approximately
`3 × (Dp + Ds) × $0.024/month` for steady-state storage, including lifecycle
rounding. For example: 0.1 GB each ≈ $0.0144/month; 0.25 GB each ≈ $0.036/month;
0.5 GB each ≈ $0.072/month. Larger archives need a separately reviewed capacity
change once they approach the 512 MiB limit. Tiny
manifests/markers add negligible storage; failed partial uploads and growth add
requests or temporary storage. Lifecycle eligibility is not a maximum bound on
physical deletion latency.

Sources: [S3 regional offer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonS3/current/eu-west-2/index.json),
[SNS regional offer](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonSNS/current/eu-west-2/index.json),
[S3 pricing](https://aws.amazon.com/s3/pricing/),
[SNS pricing](https://aws.amazon.com/sns/pricing/),
[lifecycle date calculation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/intro-lifecycle-rules.html),
[lifecycle expiration](https://docs.aws.amazon.com/AmazonS3/latest/userguide/lifecycle-expire-general-considerations.html).

## Recovery expectations

Daily snapshots can lose nearly 24 hours of data plus dump/upload delay; failures
extend that exposure. Two-day retention can leave no usable copy after multiple
missed runs. RTO is not guaranteed: measure download, restore, application checks,
operator response and reconstruction during the first drill. The dump excludes
cluster-wide roles/settings; retain bootstrap/configuration material separately.

Vellum starts AutoMigrate and category seeding on startup. An image rollback
cannot undo schema changes or later writes. Validate a restored database before
starting the matching retained application digest, then test upgrades on another
copy. A destructive live restore or incompatible rollback requires a separate
maintenance, write-freeze/data-loss and recovery approval. Restoring old auth data
may revive revoked refresh sessions; decide session invalidation separately
(rotating JWT signing state alone does not revoke opaque refresh tokens).

## Local validation evidence

On 2026-10-25 the isolated local Docker Desktop drill
`python3 deploy/tests/backup_restore_drill.py` completed against PostgreSQL 17.11.
It executed the real dump and transactional restore, checked restored counts,
relationships and constraints before application startup, then tested login and
account API equality after starting the restored backend. A truncated archive
was rejected by a real restore. The synthetic archive was 19,004 bytes, SHA-256
`da1ccf1a0a5108db617aac171a96de19d84c6d01851008b8b527fd3ba7915512`.
Elapsed time was 21.1 seconds including a cached backend image build and two
application startups; this is synthetic local evidence, not a production RTO.

The backup failure-path suite had 28 passing tests and the existing deployment
suite had 35 passing tests (63 total). Deployment Compose/Caddy validation,
the Go SSM regex test, Python compilation, shell syntax/ShellCheck and systemd
unit/calendar validation in a disposable Ubuntu 24.04 container also passed. No S3 upload/download was performed: **off-host
recovery, EC2 capacity, notifications and actual two-day expiry remain unproven**
until the approved rollout checks above run.
