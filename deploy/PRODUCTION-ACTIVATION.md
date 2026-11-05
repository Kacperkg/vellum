# Promote a tested staging release to production

**Feature PR → staging → reviewed merge into main → Actions → Promote production.**
A release-needed push to protected `staging` runs full CI and, with CD enabled,
publishes and deploys both images. Once the owner chooses to merge the tested
changes into protected `main`, they choose when to dispatch production. The
workflow finds the one eligible staging release and deploys its existing image
digests. A merge or push to `main` does not deploy production.

Backups run independently. Their status is advisory for merges and deployments.
The confirmation checkbox authorises the deployment, including brief API
downtime, startup migrations and smoke requests. It carries no backup or restore
attestation. Keep historical approval and pre-launch closure records as evidence.
The [pre-launch procedure](PRELAUNCH-PRODUCTION.md) is retired.

## One-time rollout of this controller revision

These steps require operator approval and access to the shared host. They add
no recovery infrastructure or AWS monthly charges. The production SSM document
and its pins, deployment roles, backup jobs and schedules already have their
own installation and configuration.

1. **Review and prepare before the first staging delivery.** Use the exact six
   files from the reviewed revision, accounting for any changes included in
   the proposed staging merge. Merging a bundle change with CD enabled can
   immediately start staging CI and delivery. Install the matching protected
   bundle before that delivery can reach SSM. If installation cannot precede
   the merge, defer delivery through the existing operator process and obtain a
   new eligible ordinary staging push once the bundle is installed and CD is
   enabled. A manual staging verification does not supply production evidence.
2. **Drain the shared host.** Freeze managed and manual deployments and schema
   changes in both environments and coordinate backup timers for the maintenance
   window. Verify in-flight SSM, Docker, database migration and dump work has
   finished. Stop if `/var/lib/vellum-cd/operation-pending.json` exists and
   follow the [host recovery procedure](HOST-COORDINATION.md). An unlocked host
   or a cancelled Actions run is insufficient.
3. **Install and verify the exact bundle and updated backup runner.** Copy the
   six bundle files and `deploy/backup.py` as inert data into private, root-owned
   staging, verify independent SHA-256 hashes and install at the existing
   `/usr/local/lib/vellum-cd/` paths. Use root-owned 0644 files and trusted 0755
   directories. Preserve lock inodes, root-private state and runtime configuration,
   database credentials, volumes, earlier receipts and known-good images. Use
   the [installation procedure](CI-CD.md#3-prepare-ec2-and-ssm-manually) and
   [host coordination rules](HOST-COORDINATION.md). Resume the existing backup
   schedule after verification and before normal operation. The old releases
   cannot use the new bundle hashes, so keep production dispatch frozen until
   the new staging release verifies and reaches `main`.
4. **Verify protections and pins.** Keep `staging` as the default branch, both
   deployment branches protected, and the staging/production GitHub Environments
   restricted to their matching branches. Keep the exact production OIDC
   identity, restricted role, document name, instance and pins below. The
   existing `VELLUM_CD_ENABLED=true` and `PRODUCTION_APPROVAL_MODE=manual`
   settings enable CD and manual production dispatch respectively. They are
   one-time settings. Verify runtime and Caddy configuration privately.
5. **Wait for a verified staging release.** Check the successful ordinary
   **Release staging** run, including full CI and `publish-and-deploy`. Compare
   its release manifest hashes to the installed bundle. Then review and merge
   its release inputs into `main` before dispatching production.

Print expected bundle hashes from a trusted checkout as an unprivileged user:

```sh
python3 - <<'PY'
import sys
sys.path.insert(0, 'deploy/cd')
from release import bundle_hashes
for name, digest in bundle_hashes('deploy').items():
    print(digest, name)
PY
shasum -a 256 deploy/backup.py
```

During the same drained window, install the independently verified backup runner
from the root-owned staging directory alongside the controller bundle:

```sh
sudo install -o root -g root -m 644 /root/APPROVED-STAGE/backup.py /usr/local/lib/vellum-cd/backup.py
```

Compare the installed bytes and ownership on the host:

```sh
cd /usr/local/lib/vellum-cd
sha256sum compose.yml compose.registry.yml cd/controller.py cd/coordination.py cd/release.py cd/smoke.py backup.py
stat -c '%U %a %n' . cd compose.yml compose.registry.yml cd/controller.py cd/coordination.py cd/release.py cd/smoke.py backup.py
```

The bundle check deliberately rejects old receipts or manifests with other
hashes. Keep them for history. The runner helper belongs in the Actions
checkout. The SSM command invokes only the installed controller under isolated
Python for the fixed `production` environment.

## GitHub, AWS and runtime prerequisites

The same workflow path must exist on default `staging` for discovery and on
`main` for execution. Only an acknowledged `workflow_dispatch` of the canonical
workflow on protected `main` can enter the production job. The job uses
`contents: read`, `actions: read` and `id-token: write`. PR jobs do not get
the production role. Retain the production Environment branch restriction and
the workflow concurrency group.

Verify the actual protected branches, environment restrictions, role, target
instance and SSM Agent rather than inferring them from repository source.
Production Environment variables are `AWS_ACCOUNT_ID`, `AWS_REGION`,
`AWS_DEPLOY_ROLE_ARN`, `SSM_INSTANCE_ID` and `SSM_DOCUMENT_ARN`. The exact OIDC
audience is `sts.amazonaws.com` and the subject is
`repo:Kacperkg/vellum:environment:production`. Verify that `SendCommand` allows
only the reviewed production document and instance, with no generic shell
document, document-edit permissions or wildcard trust.

This policy keeps `VellumDeploy-production` at numeric version `2` and AWS
metadata SHA-256
`8d43e90f07c95456d7f781b591cc187eaf448e0c80b3a87d0433b9665297f83a`.
Check the actual AWS metadata and reviewed document with an authorised read-only
identity. A hash of the local JSON file is not an AWS document hash.

```sh
aws ssm describe-document --name VellumDeploy-production --document-version 2 \
  --query 'Document.{Name:Name,Version:DocumentVersion,Hash:Hash,HashType:HashType,Owner:Owner,Status:Status}'
aws ssm get-document --name VellumDeploy-production --document-version 2 \
  --document-format JSON --query Content --output text
```

The runner sends only the manifest. Version 2 still supplies its legacy
`ApprovalMode` default `standard` as an environment variable for compatibility.
The new controller rejects a missing or non-standard value, including the
retired pre-launch mode. This wire parameter carries no approval evidence. Investigate document
drift before any dispatch. Changing document versions or pins requires a
separate, reviewed change.

Privately verify ownership and mode of `/etc/vellum-cd/production.env`, the
existing production project, database, volume and credentials, the production
origin, private ports and `TRUSTED_PROXY_CIDRS`. The controller retains its
automated trusted proxy check. Caddy must route auth/recovery POST and OPTIONS to
the backend and GET deep links to the SPA. Keep runtime secrets, resolved Compose
output and detailed logs private. Disconnect maintenance clients and freeze
external writers for the backend drain. SES can stay disabled while migrations
and session revocation still run.

## Review the merge and dispatch

The reviewed `main` tree must match an eligible staging release across tracked
release inputs, including both application trees, workflows, CI helpers,
Compose, SSM and IAM examples, file types and modes. The comparison excludes
only root `README.md`, `docs/`, top-level `deploy/*.md` and the historical
`deploy/production-release.json`. Merge, squash and rebase can produce equivalent
trees when branch rules permit. Land and test any changes to release inputs on
staging before promoting.

After merging, open **Actions → Promote production → Run workflow**, select
**main** and tick **I approve production replacement, API downtime, migrations
and smoke requests**. The workflow reports the exact staging run, artifact,
manifest SHA-256 and image digests. The production job rechecks that same
release before obtaining AWS credentials and before sending the pinned SSM
command. Avoid concurrent merges or staging reruns while it is being dispatched.

Discovery requires a successful ordinary staging push from current staging
history within the 90-day search window, full required CI and staging delivery
for the exact latest attempt, and one unexpired matching artifact. It checks
archive provenance and checksum, manifest checksum, both build digests and all
six deployment-bundle hashes. A manual verification, skipped deploy or failed,
running or cancelled latest attempt cannot supply a release. If discovery finds
zero matches, inspect the intended run and compare the reviewed tree. If it
finds multiple matches, use a new reviewed staging revision to establish one
eligible match. Preserve all historical manifests and receipts.

## Verify the deployment or investigate a failure

Watch the exact Actions run (for example, `gh run watch RUN_ID --repo
Kacperkg/vellum`). The backend may return brief 502s while the controller stops
and removes old writers, drains the target database sessions, and starts one
version-aware backend. Startup migration precedes the API listener. Container
health, backend identity/digest and Caddy smoke checks must pass before the
controller writes successful release receipts. The production controller also
checks the new backend did not restart before or during smoke verification;
a healthy retry can mask a failed startup or migration.

With approved host access, compare the workflow summary with
`/var/lib/vellum-cd/production-current.json`,
`production-previous.json` and the per-release
`production-RUN_ID-RUN_ATTEMPT.json` in the same directory. Check actual
running container references and health in both production services. These
targeted read-only commands avoid printing container environment data:

```sh
sudo jq '{source_sha, run_id, run_attempt, images}' /var/lib/vellum-cd/production-current.json
sudo docker ps -a --filter label=com.docker.compose.project=vellum-production \
  --format '{{.ID}} {{.Label "com.docker.compose.service"}} {{.Image}} {{.Status}}'
sudo docker inspect --format '{{.Config.Image}} {{.Image}}' CONTAINER_ID
```

Check public `/ping` and frontend routing. Use approved test accounts for any
additional auth checks. Receipts identify successful history and can lag actual
runtime state after a failure.

If Actions fails, times out or is cancelled, inspect the exact SSM command
before attempting another release. Actions cancellation can leave remote work
running. If the workflow did not report a command ID, match the pinned document,
target instance and run time window in SSM:

```sh
aws ssm list-commands --instance-id INSTANCE_ID \
  --query 'Commands[].{Id:CommandId,Document:DocumentName,Time:RequestedDateTime,Status:Status}'
aws ssm get-command-invocation --instance-id INSTANCE_ID --command-id COMMAND_ID \
  --query '{Status:Status,Code:ResponseCode}'
```

If a pending marker exists, stop managed work and follow
[HOST-COORDINATION.md](HOST-COORDINATION.md#recover-an-interrupted-operation).
Inspect the running containers, database sessions, restarts and receipts before
authorising recovery. Never clear a pending marker automatically or replace a
lock file. A held lock or a marker from an unconfirmed database dump is a shared
host safety condition. Once the dump returns successfully, the backup clears
its own marker and keeps the locks through archive checks and S3 upload.
Backup freshness, archive and upload failures, alert results and restore
evidence are not dispatch gates.

The controller has no automatic image rollback after migration or health
failure. A reviewed image recovery can reuse known-good digests only when
the schema and authentication/session behaviour remain compatible and the
current bundle is verified. Reverting a Git commit or image does not reverse
schema or data changes, and a failed migration can leave partial changes.
Prefer a version-aware fix where safe. Retain receipts and images, and use the
separate [backup and restore runbook](backup/README.md) for optional recovery
planning or an authorised data restore. Never restore production data into
public staging.
