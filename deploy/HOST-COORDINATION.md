# Shared host operation coordination

Backups, controller deployments and guarded operator schema work share the
existing `/var/lib/vellum-cd/backup.lock` as a **host-wide exclusive mutex**.
Staging and production cannot perform these managed operations concurrently.
The name is retained so an already installed backup uses the same inode. No
AWS, SSM document, application, database credential or volume change is needed.

## Lock protocol and failure recovery

`cd/coordination.py` is the shared root-installed implementation. Every operation
acquires, nonblocking, in this order:

1. `/var/lib/vellum-cd/backup.lock`
2. `/var/lib/vellum-cd/staging.lock`
3. `/var/lib/vellum-cd/production.lock`

Both environment locks are retained, even for an operation targeting only one
environment. This detects an older environment-only controller in either project.
Contention releases any already acquired locks in reverse order and fails before
the dump or application update. There is no wait queue, nested acquisition,
lock-order inversion or automatic retry. Never delete, replace or truncate a lock
file: replacing the inode could split exclusion across old and new processes.
Existing root-owned regular single-link 0644 controller locks are accepted;
symlinks, hardlinks, FIFOs, non-root owners and writable locks are rejected.
The state directory must be root-owned 0700 under trusted root-owned parents.

Before any managed Docker/database work, the operation exclusively creates
`/var/lib/vellum-cd/operation-pending.json` at 0600 and fsyncs the file and directory.
The record contains only operation kind, environment, PID and UTC start time.
A successful operation removes it and syncs the directory while still locked.
Every incomplete operation retains it: errors, graceful cancellation, SIGKILL,
SSM timeout, reboot and loss of the Docker client all require operator recovery.
This intentionally includes harmless early failures and failed uploads; safety
is preferred over automatic resumption after uncertain work.

The file lock alone cannot prove quiescence: Docker's daemon can continue an
update, a newly started backend can continue AutoMigrate, and an in-container
`pg_dump` can outlive the host client. Subsequent updated backups, deployments and
maintenance fail closed on **any** existing pending entry, even a dangling symlink.
A dead PID, old timestamp, reboot or free flock never authorizes automatic recovery.
Freshness checks remain read-only and bypass this gate so alarms still operate.

Controller manifest/bundle verification and production release-specific backup,
schema and proxy approvals remain mandatory. The gate surrounds `Commands.apply`,
including image work, application startup/AutoMigrate, smoke checks and durable
release receipts. Failure retains the gate and does not automatically roll back.
The backup gate covers dump, upload verification and cleanup. Backup cancellation
still attempts in-container cancellation and retains the existing hard-timeout
wait; the pending record remains even after a handled failure.

## Operator installation requirement

This repository change does not update EC2. Before resuming either controller or
backup jobs, an operator must separately approve a **paired installation**:

1. Freeze manual/SSM/Actions deployments and migrations in both environments;
   pause backup timers and other jobs through their approved operational process.
   Drain active controller, Compose, migration and dump work. Inspect backend
   health/startup, database sessions and no orphan dumps privately. Do not kill
   PostgreSQL or remove volumes. Copy recovery evidence/configuration securely.
2. Review the exact immutable revision and independent SHA-256 values. Copy files
   as inert data into a new root-owned private staging directory, verify the copy,
   reject symlink/untrusted destinations/ancestors, and preserve previous installed
   code/units for recovery. Never execute a writable-checkout script as root.
3. Install the **six-file** reviewed deployment bundle at the existing paths:
   `compose.yml`, `compose.registry.yml`, `cd/controller.py`, `cd/release.py`,
   `cd/smoke.py`, and **new `cd/coordination.py`**. Scripts/libraries are root-owned
   0644; directories root-owned 0755. Do not replace `/etc/vellum-cd/*.env`.
   Example destinations after staging and independent hash verification:

   ```sh
   sudo install -o root -g root -m 644 /root/APPROVED-STAGE/cd/coordination.py /usr/local/lib/vellum-cd/cd/coordination.py
   sudo install -o root -g root -m 644 /root/APPROVED-STAGE/cd/controller.py /root/APPROVED-STAGE/cd/release.py /root/APPROVED-STAGE/cd/smoke.py /usr/local/lib/vellum-cd/cd/
   ```

   Verify/retain the matching Compose files using the existing installation
   procedure in [CI-CD.md](CI-CD.md); do not overwrite unreviewed local changes.
   Install verified `backup.py` (0644) and `backup.sh` (0755) in
   `/usr/local/lib/vellum-cd/` during the same freeze. The backup checks the shared
   module and its directory before import; the controller validates it before
   import and includes it in release provenance hashes. Preserve root-private
   state and runtime configuration, database credentials and PostgreSQL volumes.
4. Compare all installed bytes with approved hashes, validate existing systemd
   services/timers with `systemd-analyze verify`, then `systemctl daemon-reload`
   only if required by a separately approved unit update. This change alters no
   unit/schedule. Confirm no pending operation from previous work and no legacy
   entry points/jobs before resuming. No Docker/PostgreSQL restart is required.
5. Do not re-enable backup timers until the original staging/production S3 restore,
   alert-delivery, capacity and independent freshness-monitor gates also pass.
   Resume release operations only through the updated installed controller.

An older installed controller fails closed against the new candidate's six-file
manifest, and a new controller rejects old five-file manifests. Do not edit
receipts/hashes to bypass this check. Both versions share the lock paths for
in-flight exclusion, but **mixed installations are not supported for active use**:
old code does not understand the durable pending record. Upgrade both together
under the drained freeze. A previous-image rollback still needs compatible schema,
reviewed current tooling and a new valid approval; downgrading the controller to
replay a receipt removes these protections and is not an operational rollback.
No change to the restricted SSM command/document, role or deployment flags is needed.

## Manual schema work and raw Compose

There is no separate migration-only application command. Controlled backend
startup/AutoMigrate is inside the controller gate. Use that controller for normal
deployments and digest rollback; **do not wrap it in another acquisition**.

An operator-approved manual migration, restore, credential/config edit or manual
Compose replacement must use the same installed `operation(state, 'maintenance',
environment)` context for its **entire foreground operation and verification**.
Use a reviewed root-owned maintenance script importing the verified installed
module under isolated Python, like the controller. For example, after verifying
installed ownership/hashes (this illustrates placement, not approval to execute
SQL or deploy):

```python
# In an individually reviewed root-owned script, invoked with /usr/bin/python3 -I.
import sys
from pathlib import Path
sys.path.insert(0, '/usr/local/lib/vellum-cd/cd')  # verified trusted installation
from coordination import operation
with operation(Path('/var/lib/vellum-cd'), 'maintenance', 'staging'):
    # Run only the exact separately approved foreground migration/restore/update.
    # Check its exit status, then verify database/backend quiescence and health.
    # Exceptions or cancellation must propagate, leaving the recovery gate.
    ...
```

Do not run raw mutating Docker/SQL/manual Compose commands from the general
README examples concurrently with managed operations. Do not catch an error and
return success, fork detached migrations, invoke a second controller/backup from
inside the gate, or start a backend and exit before startup/health is verified.

## Recover an interrupted operation

An operator must explicitly approve recovery after investigating the recorded
operation and affected deployment state. Keep a freeze across both environments.

1. Inspect the pending file privately without following links; require a regular
   root-owned 0600 single-link file. If it is malformed/untrusted, investigate
   instead of treating it as absent. Inspect both service results, Docker update
   state, backend restarts/health and database sessions/migrations. Retain the
   pending record, previous/current receipts, digests and known-good recovery data.
2. For an interrupted backup, verify no orphan `pg_dump`; if cancellation cannot
   be confirmed, wait beyond the documented approximately 16-minute in-container
   bound from interruption **and** verify quiescence. Do not infer deployment
   quiescence from this timeout: backend AutoMigrate has no equivalent bound.
   For interrupted deployments, resolve incomplete startup/schema work explicitly;
   never assume old-image rollback reverses migrations or later writes.
3. Once quiescence is independently established, use a separately reviewed
   root-owned recovery script to acquire `host_locks(STATE)` in the same order.
   Recheck the exact pending entry under the locks, archive its bytes as protected
   incident evidence, unlink **only that pending record**, and `sync_state(STATE)`.
   Never unlink any `*.lock`, auto-expire records, or clear a gate from a timer.
   This recovery-only use of `host_locks` intentionally does not start an operation.
4. Release the recovery locks and separately retry one approved operation through
   its normal entry point. Confirm receipts, application health and backup/monitor
   results. No automatic rollback/retry or cleanup is added by this change.

## Timers and scope limits

Daily production 02:15 UTC/staging 03:15 UTC schedules and `Persistent=true` are
unchanged. Boot/enable can trigger immediate catch-up. One operation can occupy
45 minutes plus 20 minutes stopping, exceeding the one-hour gap; the next job
fails/alerts instead of overlapping. Investigate and retry explicitly. No service
is ordered after the other service (which could start an unwanted job); the
shared gate enforces exclusion for timers, SSM and manual managed calls alike.
SNS remains conditional; independently monitor failures, missing fresh snapshots
and the monitor runner itself. A pending gate can stop all future backups until
recovery, so daily two-day-retention coverage depends on prompt operator response.

These are advisory host locks, not a PostgreSQL lock or a root security boundary.
They do not intercept arbitrary root/Docker/SQL commands, another host's migrations,
or autonomous backend restarts (`restart: unless-stopped`) that rerun AutoMigrate.
Application writes continue during logical backups by design. Before enabling or
resuming timers after reboot/crash, verify backend startup/migrations are complete;
application-level migration coordination for autonomous restart paths would be a
separate change. This fix guarantees exclusion for the managed paths above and
guarded operator work; it does not claim to serialize every possible schema writer.

## Staging authentication replacement and recovery

For PR #28, follow [STAGING-AUTH-ROLLOUT.md](STAGING-AUTH-ROLLOUT.md). The entire
stop/termination/removal/session-drain/start/health/smoke/receipt sequence stays
inside the existing operation context. The lock order, lock inodes, pending
format and recovery rules are unchanged. Only a successful operation clears the
pending record. Even if no new container started, a failed stop/removal/drain
keeps the record and requires review.

After cancellation, do not infer quiescence from a dead controller or unchanged
receipts. A Docker daemon request or new backend restart can still be executing
AutoMigrate. Privately inspect all staging backend instances (including one-offs),
actual digests, startup health and staging DB client sessions/transactions. Drain
all relevant Docker/SSM work before authorized recovery; stop and remove failed
candidate writers through a reviewed maintenance operation if needed. Confirm
that no old restartable container remains before clearing a pending marker under
the existing approved recovery procedure. Do not clear a marker automatically,
delete lock files, change PostgreSQL/volumes, restart a version-unaware image,
downgrade installed tooling or rewrite receipt hashes. Prefer a version-aware
roll-forward release using the installed reviewed bundle.
