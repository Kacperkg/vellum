# One-use pre-launch production recovery exception

This is a proposed, separately approved operator procedure. Preparing this change
does not merge, install, configure or deploy anything. Only isolated restore
verification is waived. Production backups, verified off-host copies, schema and
proxy review, immutable release evidence, locks and authentication protections
remain mandatory. **Recovery has not been demonstrated; pre-launch data can be
lost.** Production SES sending must remain disabled.

## Scope and exact approval behaviour

The exception is for the **first managed production deployment**, before public
launch and while there are no public users. Two independent inputs must agree:

- Manual workflow: `recovery_approval=prelaunch` **and**
  `confirm_prelaunch_data_loss=true`, in addition to the ordinary deployment
  confirmation and exact reviewed main SHA.
- Root-private host record: the exact pre-launch schema below, bound to the
  selected canonical manifest hash. It must explicitly say
  `restore_verified=false`, attest no public users and accept data loss.

Normal dispatch defaults to `standard` and requires the original eight-field
approval with genuine `restore_verified=true`. Neither a pre-launch record alone
nor a workflow acknowledgement alone enables the exception. Staging rejects it.
Unknown modes, fields, missing attestations and non-boolean truthy values fail.

Approval/backup timestamps remain timezone-aware and at most 24 hours old;
backup creation cannot follow verification. Exception expiry must be in the
future, at most 24 hours after verification, and **no later than
2026-11-04 00:00:00 UTC**. The fixed sunset is in reviewed controller source,
not an environment setting. It bounds a trusted host clock; root/clock
administration is outside this protection boundary.

Under all three original host locks, the controller rechecks approval, refuses
any existing `production-prelaunch-closed.json` entry (including a dangling
symlink) or `production-current.json`, then atomically writes/fsyncs a permanent
closure record containing the full truthful approval, manifest hash and time.
It fsyncs the state directory **before** any Docker command. The one attempt is
spent even if pulling, draining, migrating or smoke later fails. Standard
production deployment also creates a permanent retirement record if absent.
The closure is audit/retirement evidence, not a successful deployment receipt
or a recoverability claim. Never remove it to retry the exception.
Consumption precedes Compose/runtime validation, so configuration mistakes also
spend the attempt. A rejected repeated dispatch (closure/current receipt) or an
approval becoming stale during the locked recheck leaves a pending recovery
marker even when Docker never ran. This deliberately blocks all managed host
work until authorised investigation; do not repeatedly dispatch or clear it
automatically.

## 1. Approve the risk and establish prerequisites

Record the owner, independent reviewers, maintenance window, no-public-users
decision, accepted potential loss and private evidence location. Confirm public
launch has not occurred; do not use this mode for real user data. Recheck the
current GitHub review/branch/environment protections and AWS/OIDC/SSM boundaries
in [PRODUCTION-ACTIVATION.md](PRODUCTION-ACTIVATION.md). Existing review
enforcement discrepancies still need operator resolution; this exception waives
none of those prerequisites. PR #31 remains excluded from the release ancestry.

Freeze merges/dispatches and all overlapping SSM, manual Docker, migrations and
backup work across the shared host. Coordinate timers, drain outstanding work,
and check `operation-pending.json` without deleting it. Free locks, cancellation
or reboot do not establish quiescence. Follow
[HOST-COORDINATION.md](HOST-COORDINATION.md) for any separately approved recovery.

## 2. Release compatibility: a new staging release is required

PR #36 merged to main as `fea05f127289dd50d1871bf5be106394ab2746ab`. Its selector
identifies run `40587847468`, attempt `1`, source
`dce09881e6278cc746bd5660ea9347ecd3659413`, raw manifest SHA-256
`5410415dbaaadc7dab0a3c6b011c661ac62974251a3d28662b2994d0053ee709`.
The checked manifest pins controller hash
`9cd67f48dc1442fdd05c6c8f18c2e2d28f63d91a390c61a6037f94d3947eb53e` and release
library hash `0050df87c8ac8179ce5606294d37a381a0158bd80da20654d1614c5113e3e3b5`.
That controller unconditionally requires verified restore. This change modifies
both hashed files: **the already-promoted release cannot use the exception**.
The six-file bundle check and runtime/deployment Git-tree equality remain strict.

Keep that artifact/manifest/selector unchanged as historical evidence. Do not
patch installed files and replay its manifest, falsify approval, rewrite hashes,
or manufacture a new manifest around old images. The normal release pipeline
must publish a new immutable image pair and manifest even though application
source is unchanged by this policy change. No application/schema change is
introduced here; the pending authentication transition still applies.

## 3. Install the reviewed matching six-file bundle

Before a separately authorised staging merge with CD enabled, independently
review the final immutable PR revision. Stage its six files **as inert data**
under a new root-private `/root/APPROVED-STAGE` directory. Independently compare
each hash to that approved revision. Never execute writable-checkout scripts as
root. Preserve prior installed bytes privately as evidence, plus known-good
images/receipts; do not downgrade tooling to replay them.

Use the drained installation protocol in
[STAGING-AUTH-ROLLOUT.md](STAGING-AUTH-ROLLOUT.md) and
[HOST-COORDINATION.md](HOST-COORDINATION.md), retaining original lock inodes,
pending records and the compatible installed backup implementation. Under that
approved freeze, install the six reviewed staged files:

```sh
sudo install -o root -g root -m 644 /root/APPROVED-STAGE/compose.yml /root/APPROVED-STAGE/compose.registry.yml /usr/local/lib/vellum-cd/
sudo install -o root -g root -m 644 /root/APPROVED-STAGE/cd/controller.py /root/APPROVED-STAGE/cd/coordination.py /root/APPROVED-STAGE/cd/release.py /root/APPROVED-STAGE/cd/smoke.py /usr/local/lib/vellum-cd/cd/
sudo sh -c 'cd /usr/local/lib/vellum-cd && sha256sum compose.yml compose.registry.yml cd/controller.py cd/coordination.py cd/release.py cd/smoke.py'
```

Verify root-owned regular files 0644, directories 0755 and root-private state and
runtime directories. No controller daemon/database restart is needed. This is
one shared installed bundle, so both environments must resume with matching
manifests; their old releases will fail closed. Once a new staging artifact
exists, compare all six installed hashes to its `deployment_files` as well.

## 4. Publish, verify and promote the new immutable release

After separate approval, merge into staging with the matching bundle installed
and normal staging readiness confirmed. An ordinary protected staging **push**
must complete all required checks, publish both digests, deploy and smoke staging,
then produce `vellum-release-<run>-<attempt>`. Manual staging verification is
ineligible. Do not rerun old source expecting new controller hashes.

Follow activation runbook sections 5–8 to inspect the latest/exact successful
attempt, all required jobs, unique unexpired artifact, raw manifest checksum,
source, six hashes and registry digests. Download into a new local directory:

```sh
run_id=REPLACE_NEW_RUN_ID
attempt=REPLACE_NEW_ATTEMPT
artifact_dir=$(mktemp -d)
gh run download "$run_id" --repo Kacperkg/vellum --name "vellum-release-$run_id-$attempt" --dir "$artifact_dir"
shasum -a 256 "$artifact_dir/release.json"
```

Create a separate reviewed promotion PR to main selecting those actual values
in `deploy/production-release.json` and carrying the exact tested runtime/bundle
trees, workflow and runner helper. Verify PR #31 remains excluded. Require CI,
independent review and separate merge approval. Never fill placeholders with
guessed identifiers. This implementation PR targets staging and does not change
its null selector or main's existing selection.

## 5. Revise and pin the production SSM document

The production template adds bounded `ApprovalMode` (`standard` by default,
`prelaunch` allowed), passed through SSM `ENV_VAR`. Staging's document is
unchanged and accepts no exception parameter. The production script requires
both ENV_VAR inputs and still executes only the fixed installed controller.

Through separately approved AWS administration, create a new **numeric version**
of the existing `VellumDeploy-production` document from reviewed
`ssm/production.json`. Inspect its exact content and AWS metadata SHA-256, then
separately approve updating production Environment `SSM_DOCUMENT_VERSION` and
`SSM_DOCUMENT_SHA256` to those verified pins. Do not guess a version or reuse the
historical hash; do not use `$LATEST`/`$DEFAULT`. Keep the fixed document ARN,
instance, production-only OIDC audience/subject and least-privilege deployment
role. Grant no document-edit permission to Actions. Verify SSM Agent
>=3.3.2746.0 and actual ENV_VAR support. These are proposed operator changes,
not actions performed by this PR.

Keep revised workflow/helper available on default staging and main. New
production dispatches send `ApprovalMode` even in standard mode, so the old
document is incompatible and must be repinned before dispatch. Old documents
cannot silently activate the exception; a missing mode defaults to standard at
the controller and a pre-launch-shaped record then fails the standard schema.

## 6. Preserve data/configuration and prepare truthful approval

Privately verify the existing production PostgreSQL container, project
`vellum-production`, database name/user, passwords, JWT secret and named volume
identity. Preserve every credential and volume; accepted loss is **not** an
instruction to delete or reset data. Do not run Compose down, volume prune,
database replacement or a credential rotation. Preserve staging isolation and
shared Caddy/other sites. Existing root-owned runtime configuration stays under
`/etc/vellum-cd/`. Confirm `APP_ENV=production`, `AWS_REGION=eu-west-2` and
`PASSWORD_RESET_EMAIL_MODE=disabled`; the exception additionally enforces the
first and last in resolved backend configuration before touching services.

Obtain/verify a recent real production backup and off-host copy through the
installed coordinated [backup procedure](backup/README.md). Existing fresh
evidence may be reused within the 24-hour limits. Verify transfer/checksums and
retain private references. This is not restore proof. Independently review
AutoMigrate/schema compatibility and the Caddy/auth/proxy configuration. Drain
maintenance database clients and external writers before dispatch; do not kill
sessions to pass the gate.

Copy [production-prelaunch-approval.example.json](production-prelaunch-approval.example.json)
into a private operator workspace. Replace every placeholder and set only
truthfully verified fields. The exact schema is those thirteen fields:

- `manifest_sha256`: SHA-256 of `release.canonical(selected_manifest)`; normal
  pipeline artifacts are canonical and this equals their reviewed raw checksum.
- `verified_at`, `backup_created_at`: actual evidence timestamps, within 24 hours.
- `backup_reference`: private verified backup/off-host evidence reference, 1–256
  characters. Never use an empty or invented restore-drill reference.
- `off_host_copy_verified`, `schema_compatible`, `trusted_proxy_reviewed`: `true`
  only after the respective checks. All remain mandatory.
- `restore_verified`: **false**. No restore was performed.
- `approval_mode`: `prelaunch`; `no_public_users`: `true`; `accept_data_loss`:
  `true`, only after explicit operator risk approval.
- `acknowledgement`: exactly
  `I_ACCEPT_PRELAUNCH_DATA_LOSS_WITHOUT_RESTORE_VERIFICATION`.
- `expires_at`: actual chosen UTC expiry, preferably a short maintenance window,
  within verification +24 hours and by 2026-11-04 00:00:00 UTC.

Through the approved protected host-configuration process, privately validate
the completed JSON against the exact new manifest with the installed validator
and explicit `approval_mode='prelaunch'`. Install the reviewed record as
`/var/lib/vellum-cd/production-approval.json`, root-owned regular mode 0600,
without symlinks, serialised with all host operations. Retain its checksum and
private approval evidence. Do not publish the completed record or logs in a PR.
Confirm no current production success receipt or pre-launch closure exists;
never remove either to make the exception eligible.

For example, after reviewing/staging the completed approval and **new** selected
manifest as root-private inert files at the paths below, validate with the
already hash-verified installed library (no service or database action):

```sh
sudo /usr/bin/python3 -I - <<'PY'
import sys
sys.path.insert(0, '/usr/local/lib/vellum-cd/cd')
from controller import validate_installation, validate_approval
from release import read_json, validate_manifest, bundle_hashes, require
validate_installation()
manifest = read_json('/root/APPROVED-STAGE/release.json')
validate_manifest(manifest, 'kacperkg/vellum', verified=True)
require(bundle_hashes('/usr/local/lib/vellum-cd') == manifest['deployment_files'], 'Bundle mismatch')
validate_approval(read_json('/root/APPROVED-STAGE/production-approval.json'),
                  manifest, approval_mode='prelaunch')
print('Approval schema, freshness, release binding and installed bundle validated')
PY
# Only while the approved host-wide configuration freeze is still in effect:
sudo install -o root -g root -m 600 /root/APPROVED-STAGE/production-approval.json /var/lib/vellum-cd/production-approval.json
```

## 7. Dispatch only after separate deployment approval

Recheck exact current main, selected release, installed hashes, document pins,
fresh approval, no pending operation, maintenance window and trusted host time.
The following command **deploys production** and was not executed by this task:

```sh
reviewed_main_sha=REPLACE_EXACT_INDEPENDENTLY_REVIEWED_40_CHARACTER_MAIN_SHA
gh workflow run release-production.yml --repo Kacperkg/vellum --ref main \
  -f expected_sha="$reviewed_main_sha" -f confirm_production_deployment=true \
  -f recovery_approval=prelaunch -f confirm_prelaunch_data_loss=true
```

Watch the exact resulting run. The guard rechecks main/eligibility before pinned
SSM delivery. The host rechecks bundle, approval and recovery marker; records
consumption; pulls/verifies both images; stops/removes old managed backends;
drains production client sessions; starts one version-aware backend; then runs
health and Caddy smoke gates. A brief API outage is expected. PostgreSQL, volumes,
credentials and Caddy are not replaced. Authentication-version migration and
session revocation remain enforced even with SES disabled.

## 8. Verify or recover without automatic rollback

Compare current/per-release success receipts and actual backend/frontend digests
to the new manifest. Confirm one healthy backend, `/ping`, SPA and backend auth
POST/OPTIONS routing. Using approved test accounts, privately verify login,
refresh, logout, protected access and version-aware session revocation. Verify
recovery routes without enabling SES; do not send production email as a test.
Confirm the closure retains `restore_verified=false`, backup evidence remains
available and no pending marker exists before resuming managed jobs.

On any failure, timeout or cancellation, stop new operations. SSM/Docker and
migration work can outlive their client. Retain both pending and closure records;
inspect the exact command, runtime state, database sessions/migration effects and
all receipts privately under [HOST-COORDINATION.md](HOST-COORDINATION.md).
AutoMigrate may leave partial DDL; success receipts may be partly written after
smoke. Do not infer rollback or quiescence from an outage or an old receipt.
Only separately approved recovery after proven quiescence can reconcile a
pending operation. **The exception remains spent.** Subsequent deployment needs
standard genuine restore evidence, or a separately designed/reviewed recovery
policy and new compatible release; do not delete closure to retry. Never
automatically roll back to pre-PR28 auth code, downgrade the controller, rewrite
provenance, or restore stale financial data merely to undo code.

## 9. Retire before public launch

Consumption or any standard production attempt permanently closes the exception.
If unused, retire it **before admitting public users**, even before the fixed
sunset. Under separately approved maintenance, using only the verified installed
coordination library, create root-private
`/var/lib/vellum-cd/production-prelaunch-closed.json` with a private decision/time
reference. Hold the existing `operation(STATE, 'maintenance', 'production')`
context (which rejects pending work), create the record with exclusive/no-follow
semantics, fsync file and state directory, and preserve an existing closure
unchanged. Do not delete a pending record or replace lock inodes.

After approving and reviewing this exact maintenance action and verifying the
installed library hashes, an operator can retire an unused exception with:

```sh
sudo /usr/bin/python3 -I - <<'PY'
import json
import os
from datetime import datetime, timezone
from pathlib import Path
import sys
sys.path.insert(0, '/usr/local/lib/vellum-cd/cd')
from coordination import operation, sync_state
state = Path('/var/lib/vellum-cd')
with operation(state, 'maintenance', 'production'):
    path = state / 'production-prelaunch-closed.json'
    if not os.path.lexists(path):
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as output:
            json.dump({'schema': 1, 'reason': 'operator-retired-before-public-launch',
                       'closed_at': datetime.now(timezone.utc).isoformat()}, output)
            output.flush()
            os.fsync(output.fileno())
        sync_state(state)
print('Pre-launch exception closed; existing closure evidence preserved')
PY
```

Remove the live pre-launch approval through the approved configuration process,
retaining private audit evidence. Before public launch, complete a genuine
isolated production-backup restore under [backup/RESTORE.md](backup/RESTORE.md),
obtain a fresh standard eight-field approval and dispatch with standard mode
and the pre-launch checkbox false. Review removal of the exception branch,
workflow choice and SSM parameter in a later PR/new immutable release and pinned
document revision. Retain permanent closure evidence; do not extend the sunset
as routine configuration. No automated path removes or renews it.
