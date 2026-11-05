# Guarded production activation

**Feature PR → staging → reviewed PR into main → Run Promote production.**
A release-needed staging push runs full CI, publishes and deploys both images
when CD is enabled, and records the verified release. Production finds the single
eligible release for the reviewed `main` tree and promotes those same digests.
The existing standard production backup, isolated restore, schema and proxy
approval still applies by default. [Signed readiness](PRODUCTION-READINESS.md)
documents an opt-in verifier and the prerequisites that block its activation.
The [pre-launch exception](PRELAUNCH-PRODUCTION.md)
expired on 4 November 2026 UTC and is no longer exposed by this workflow.

The opt-in automation implementation remains blocked for live activation.
See the [independent recovery runbook](recovery/README.md) for its trust
boundaries, repository test coverage and separately approved activation steps.
The standard procedure below remains the current supported approval route.

## 1. GitHub configuration and verified current state

Read-only inspection on 2026-10-27 found default branch `staging`, repository CD
flag `VELLUM_CD_ENABLED=false`, and `PRODUCTION_APPROVAL_MODE=manual`. Active
main/staging rulesets require PRs and seven CI checks, prohibit force pushes and
deletion, and have no bypass actors. Neither requires approving/code-owner
reviews; main does not require an up-to-date branch, staging does. Production
Environment permits only branch `main`, staging only `staging`; neither has
reviewers, and administrator bypass is enabled. Recheck before operation:

```sh
gh api repos/Kacperkg/vellum --jq '{default_branch,visibility}'
gh api repos/Kacperkg/vellum/rulesets --paginate
gh api repos/Kacperkg/vellum/rulesets/REPLACE_RULESET_ID
gh api repos/Kacperkg/vellum/environments/production
gh api repos/Kacperkg/vellum/environments/production/deployment-branch-policies
gh api repos/Kacperkg/vellum/environments/staging/deployment-branch-policies
gh variable list --repo Kacperkg/vellum
gh variable list --repo Kacperkg/vellum --env production
```

Keep manual approval mode. Dispatch permission is an operator gate, without
enforced two-person approval. Recommend at least one independent approving
review, code-owner approval for workflows/controller, stale approval
invalidation, and strict required checks on main. Any settings changes need
separate approval. Environment reviewers, prevent-self-review and disabled admin
bypass would strengthen the gate, but adopting another approval mode requires a
separately reviewed implementation/configuration. This workflow rejects
`environment` mode and never deploys on push.

Production Environment variables must identify only the production account,
role, region, instance and document ARN: `AWS_ACCOUNT_ID`, `AWS_REGION`,
`AWS_DEPLOY_ROLE_ARN`, `SSM_INSTANCE_ID`, `SSM_DOCUMENT_ARN`. The production workflow pins version
`2` and AWS metadata SHA-256
`8d43e90f07c95456d7f781b591cc187eaf448e0c80b3a87d0433b9665297f83a`, carried
over from the reviewed main workflow in PR #39. The production job gets `contents: read`,
`actions: read`, `id-token: write`; no package writes. PR CI receives no AWS
credentials. The exact case-sensitive repository/workflow identity remains
`Kacperkg/vellum`, independently of case-insensitive GitHub URLs.

## 2. Workflow availability

The same workflow path must exist on default `staging` for discovery and on
`main` for execution. Its only trigger is `workflow_dispatch`. Dispatch to
staging, a feature branch, a tag, another repository or an unprotected main
cannot deploy. Keep the production concurrency group and Environment protections.

## 3. Verify current AWS and controller compatibility

Historical AWS evidence (not current live verification): region `eu-west-2`,
instance `i-073a8f8cf2abd86c5`, role `vellum-github-production-deploy`, document
`VellumDeploy-production`, version `1`, metadata SHA-256
`63e591e39c255a88d9a947474062242124816434eacf0c9c4f6ad8db1b000626`.
This task did not contact AWS. With an authorised read-only operator identity:

```sh
aws sts get-caller-identity --query Account --output text
aws iam get-role --role-name vellum-github-production-deploy
aws iam list-role-policies --role-name vellum-github-production-deploy
aws iam list-attached-role-policies --role-name vellum-github-production-deploy
aws ssm describe-document --region eu-west-2 --name VellumDeploy-production \
  --document-version 2 --query 'Document.{Name:Name,Version:DocumentVersion,Hash:Hash,HashType:HashType,Owner:Owner,Status:Status}'
aws ssm get-document --region eu-west-2 --name VellumDeploy-production \
  --document-version 2 --document-format JSON --query Content --output text
aws ssm describe-instance-information --region eu-west-2 \
  --filters Key=InstanceIds,Values=i-073a8f8cf2abd86c5 \
  --query 'InstanceInformationList[].{Id:InstanceId,Agent:AgentVersion,Ping:PingStatus}'
```

Retrieve every listed inline policy (`iam get-role-policy`) and attached policy's
default version (`iam get-policy`, `iam get-policy-version`). Verify trust has
exact audience `sts.amazonaws.com` and subject
`repo:Kacperkg/vellum:environment:production`, no wildcard or staging trust.
Verify SendCommand grants only the reviewed account's exact production document
and instance, with no generic shell-document, document-edit or alternate role
grants. Preserve the existing GetCommandInvocation read permission. Compare AWS
metadata hash/version to the independently reviewed pin; a local JSON formatting
hash is not a substitute. The document must execute only the root-installed
`python3 -I` controller for production using the Manifest ENV_VAR input. The
revised template also requires bounded `ApprovalMode` ENV_VAR data, defaulting to
`standard`; separately review a new numeric version and its AWS metadata hash
before updating production pins, as detailed in the pre-launch runbook. Verify
SSM Agent ENV_VAR support. Investigate drift; do not update pins blindly.

## 4. Exact six-file bundle verification

The pre-launch policy changes `cd/controller.py` and `cd/release.py` within the
six-file bundle. It does not certify the live installation. The existing
40587847468/1 manifest is incompatible with those changes; do not rewrite it.
Before creating a new staging release, verify the installed bundle against the
reviewed source on the drained host under the existing authorised installation
procedure in [STAGING-AUTH-ROLLOUT.md](STAGING-AUTH-ROLLOUT.md). The exact list is:

```text
compose.yml
compose.registry.yml
cd/controller.py
cd/coordination.py
cd/release.py
cd/smoke.py
```

From a trusted checkout, print expected hashes locally (no root execution):

```sh
python3 - <<'PY'
import sys
sys.path.insert(0, 'deploy/cd')
from release import bundle_hashes
for name, digest in bundle_hashes('deploy').items():
    print(digest, name)
PY
```

On the host, read-only:

```sh
cd /usr/local/lib/vellum-cd
sha256sum compose.yml compose.registry.yml cd/controller.py cd/coordination.py cd/release.py cd/smoke.py
stat -c '%U %a %n' . cd compose.yml compose.registry.yml cd/controller.py cd/coordination.py cd/release.py cd/smoke.py
sudo test ! -e /var/lib/vellum-cd/operation-pending.json
```

Compare every hash to reviewed source and the selected manifest. Files must be
root-owned 0644, trusted directories 0755; state/config remain root-private 0700.
If mismatched, stop and obtain separate installation approval; retain original
lock inodes, credentials, volumes and recovery information. Do not downgrade a
controller or rewrite receipt hashes. Runner helper stays in the Actions
checkout; never install/execute it as root.

## 5. Obtain an eligible staging release

Merge a reviewed feature PR to protected `staging` after verifying the existing
staging delivery prerequisites. Confirm **Release staging** succeeded, including
`publish-and-deploy`. When delivery is enabled, any release-needed change runs
full CI and produces the evidence required for production. Docs-only pushes
can reuse a release with an equivalent tree. A skipped deployment, a manual
verification, an old-format artifact or an unsuccessful rerun is ineligible.
The staging job summary gives the canonical manifest SHA-256 needed below.
There is no need to download the manifest or calculate the hash locally.

## 6. Review and merge staging into main

Open a normal staging-to-main PR. The resulting tree must match the tested
staging commit across all tracked files, including both complete application
trees, workflows, CI helpers, Compose, SSM and IAM examples, file types and
modes. Only root `README.md`, `docs/`, top-level `deploy/*.md` runbooks and the
unused historical `deploy/production-release.json` are excluded. Keep any
production-only workflow update in the reviewed staging source as well so it
is covered by release CI. The production SSM version and hash are already
present there; preserve those exact pins in the merge.

Squash, rebase and merge commits are all compatible with discovery if branch
rules allow the merge strategy and the resulting files are equivalent. The
branches already diverged; the first PR has conflicts in application,
workflow and documentation files. Resolve them in review so the resulting
application, deployment and workflow files match the tested staging tree.
Retain the previous selector in Git history as release evidence. GitHub's
current linear-history rules allow squash/rebase rather than a merge commit;
these do not bring staging ancestry into main, so future PRs can also need
conflict resolution. If a proposed resolution changes release-relevant
files, land and test it on staging first. A different main tree cannot be
promoted, regardless of how the PR was merged.

## 7–8. How discovery verifies the release

The read-only job searches ordinary staging-push runs created in the previous
90 days (the artifact retention period). An incomplete search blocks promotion.
It requires a source in current staging history, tree equivalence, successful
CI and publication/deployment for the exact latest attempt, and one matching
unexpired artifact. A rerun that is running, failed or cancelled invalidates
earlier attempts of that run. The verified artifact name binds the run, attempt,
canonical manifest SHA-256, and both staging build output digests. The workflow
verifies the ZIP checksum and GitHub run metadata, compares the manifest
checksum and image digests, and checks all six deployment-bundle hashes.

If no release matches, inspect the intended staging run and check the main
PR's resulting tree. Fix any differences on staging or obtain a fresh ordinary
release for the intended source. A full rerun is usable only if the exact new
attempt runs CI and deploys successfully and remains within the search window.
If two releases match, the workflow refuses to choose. Review the evidence and
use a new reviewed staging revision to establish a unique match; do not edit
historical manifests or remove evidence to force selection.

Historical context: manual run 40560489451/1 was ineligible. PR #36 selected
ordinary staging run 40587847468/1 using the legacy selector. That release
predates this artifact contract; its historical evidence remains in Git.

## 9. Backup, isolated restore and release-specific approval

This section covers standard approval. Use [signed readiness](PRODUCTION-READINESS.md)
for the separately gated automation design and activation blockers.

Two-day S3 retention is accepted, but is not restore proof. **Never set
`restore_verified=true` without an actual isolated verified restore.** Missing
restore evidence intentionally blocks production, even after workflow activation.

Minimum procedure for standard production approval:

1. On an authorised private disposable host, follow
   [backup/RESTORE.md](backup/RESTORE.md) using an actual recent **production**
   completion marker/archive/manifest triple, replacing that guide's staging key
   examples with exact production keys. Use a read-only recovery role, private
   files and checksum verification of every selected S3 object.
2. Restore with matching PostgreSQL 17 tools into a uniquely named disposable
   database/container, no live volumes, no host/public ports, no `web` network,
   no production environment file/secret and no IMDS exposure. `pg_restore --list`
   alone is insufficient. Run restore with exit-on-error and inspect private
   table/index/constraint/relationship and financial aggregate evidence.
3. On separate restored copies, verify the matching retained application and
   selected version-aware candidate, with fresh drill secrets, controlled test
   accounts and SES disabled. Inspect schema before/after AutoMigrate/seeding,
   budget uniqueness preflight, auth/session revocation and financial behavior.
   Migration can leave partial DDL/data. Record durations, digests, recovery
   point, aggregate results and private evidence reference. No live DB writes.
4. Clean up only recorded disposable resources after retaining private evidence.
   Take a new protected production backup if the selected backup/verification
   would exceed 24 hours at dispatch; repeat/refresh evidence as appropriate.

Through the existing separately approved host configuration process, prepare
root-private `/var/lib/vellum-cd/production-approval.json` from
`production-approval.example.json`. Require the canonical selected manifest's
exact hash, timezone-aware backup creation and verification timestamps within
24 hours (creation no later than verification), private nonempty
`backup_reference`, and strictly true `off_host_copy_verified`, `restore_verified`,
`schema_compatible`, `trusted_proxy_reviewed`. Do not publish evidence, dumps,
credentials or user data in a PR. The installed controller enforces these before
downtime; it also rejects a wrong release, missing or stale approval.

## 10–11. Runtime and production Caddy routing

Privately verify `/etc/vellum-cd/production.env` ownership/mode and the existing
production project, PostgreSQL volume/name/user/password, JWT signing secret,
origin `https://vellum.kacpergajdarski.com`, private backend/database ports and
reviewed `TRUSTED_PROXY_CIDRS`. Retain staging/production isolation. SES may stay
`disabled`; migrations and version revocation still apply. Follow
[PASSWORD-RESET.md](PASSWORD-RESET.md) for any separately approved runtime changes.
Do not print resolved Compose or secrets. Caddy must already route auth/recovery
POST and OPTIONS to backend, GET deep links to SPA, with privacy headers and
correct proxy trust. Validate any merged Caddy configuration separately before
an authorised reload; preserve unrelated sites and shared `web`. The deployment
workflow/controller never modifies Caddy. Disconnect maintenance clients and
freeze external production writers before the backend drain gate.

## 12. Explicit production dispatch — mutating operator action

After the reviewed merge and installation of the standard host approval, open
**Actions → Promote production → Run workflow**, select `main`, and tick
`confirm_production_deployment`. The checkbox acknowledges backend replacement,
brief API downtime, AutoMigrate/seeding and smoke requests. The host record
separately attests to backup, restore and schema evidence. With no GitHub
Environment reviewer configured, dispatch does not pause for that record;
prepare it first from the staging summary hash.

The read-only job reports the exact staging run, artifact, image digests and
manifest checksum. The production job requires the same unique release before
obtaining credentials and again before sending the pinned SSM command. It checks
live `main` just before dispatch and rejects an observed change. GitHub and SSM
do not provide a shared transaction; avoid concurrent merges or staging reruns
while production is being dispatched. The controller verifies the same standard,
release-specific approval before mutation and retains the existing recovery gates.

## 13–14. Monitoring and post-deployment verification

Watch the exact workflow run (`gh run watch RUN_ID --repo Kacperkg/vellum`).
Inspect SSM status privately without dumping payloads/logs. Through approved host
access, compare exact runtime image digests, one healthy production backend and
current/per-release receipts to the selected manifest. Verify `/ping`, frontend,
login/refresh/logout, protected authorization and password recovery routing with
approved accounts; verify version revocation and SES behavior only through
separately approved tests. Ensure backup freshness and no pending marker before
resuming coordinated work. Historical receipts alone do not prove current state.

**Expect a brief API outage/502s**: old production backends stop, are identity
verified/removed and the production database's client sessions drain before one
candidate starts. Immutable digest/singleton health and Caddy smoke gates remain
mandatory. No rolling overlap of old/new authentication writers is allowed.

## 15. Failure investigation and version-aware recovery

Do not retry, auto-cancel SSM, clear markers or roll back automatically. Workflow
cancellation/timeouts can leave SSM/Docker/migration work running. If no command
ID appears, note the exact run/time window and find the matching production
command privately (success output emits command_id only after completion):

```sh
aws ssm list-commands --region eu-west-2 --instance-id i-073a8f8cf2abd86c5 \
  --query 'Commands[].{Id:CommandId,Document:DocumentName,Time:RequestedDateTime,Status:Status}'
aws ssm get-command-invocation --region eu-west-2 \
  --instance-id i-073a8f8cf2abd86c5 --command-id REPLACE_EXACT_ID \
  --query '{Status:Status,Code:ResponseCode}'
```

Match the pinned production document and dispatch window; never infer success
from a stopped workflow. Follow [HOST-COORDINATION.md](HOST-COORDINATION.md) for
private investigation and separately approved recovery under original lock
inodes. A retained marker blocks both environments and backups; respond promptly.
Quiesce candidate restart/migration work, verify target sessions, container state
and all receipts (receipt writes can partially succeed after smoke), then obtain
explicit recovery approval. Never delete PostgreSQL volumes, rewrite historical
hashes, downgrade the controller or assume image rollback reverses migration.
Use a reviewed version-aware roll-forward/recovery release with current bundle
and fresh release-specific approval. Pre-PR28 authentication code can resurrect
revoked sessions and is not an automatic rollback option. There is no second
production deployment path or unreviewed manual Compose fallback in this workflow.

## Historical activation validation and independent review (2026-10-27)

Local results for this activation change: 50 CI helper regression tests and 128
deployment safety tests passed. These cover production approval, provenance,
backend-writer replacement, recovery markers, SSM timeout/failure/cancellation,
backup safety and staging/manual compatibility. Actionlint v1.7.7, Go SSM regex
tests, synthetic Compose relocation/model validation, Caddy validation and both
hosts' loopback recovery routing, Docker secret exclusions, Python syntax and
`git diff --check` passed. Docker checks used only local disposable synthetic
fixtures; no live services/databases were used. Docker context checks emitted a
non-fatal build metadata warning about unavailable Git commit information.

Five independent reviewer roles completed: Actions security, release provenance,
AWS deployment security, deployment reliability and final diff review. Findings
resolved include the template's automatic push path, missing dispatch identity
and acknowledgement gates, skipped publication/latest-attempt validation, and a
regression test that would have incorrectly prohibited later valid selector
configuration. No confirmed blocking implementation finding remains. Live GitHub
protections were inspected read-only; live AWS/controller installation/restore
and deployment behavior were not verified. The existing six-file bundle,
selector, application code, runtime config, SSM/IAM and CI performance settings
remain unchanged. AGENTS.md stays local and ignored.

Verdict: **Ready with caveats** for the reviewed implementation draft PR;
**Not ready** for production dispatch until this runbook's release, restore,
configuration and host/AWS verification gates are satisfied. No staging or
production deployment, image publication, merge, AWS mutation, GitHub settings
change, live database change, Caddy reload or backup-timer change was performed.
Hosted PR CI is separate from the local validation above; inspect the draft PR's
current checks before any merge approval.
