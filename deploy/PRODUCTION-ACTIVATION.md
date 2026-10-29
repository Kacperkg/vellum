# Guarded production activation

For the specifically approved first-deployment data-loss exception, see
[PRELAUNCH-PRODUCTION.md](PRELAUNCH-PRODUCTION.md). It waives only isolated restore
verification, requires a new immutable staging release/matching controller and
revised pinned production SSM document, and expires by 4 November 2026 UTC. The
standard procedure below still requires genuine restore evidence. Historical
release/configuration observations below are dated; PR #36 subsequently promoted
run 40587847468/1, which cannot use the new exception's changed bundle.

This is the operator runbook for a later, separately approved promotion. The
workflow is active in source; this pin change does not deploy or change AWS
resources, GitHub settings, runtime configuration, databases, Caddy or timers.
**Production deployment is not ready:** current live prerequisites still need
operator verification. Standard mode requires actual isolated restore evidence;
the pre-launch exception needs a new compatible release and its separate gates.

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
review, code-owner approval for workflows/controller/selector, stale approval
invalidation, and strict required checks on main. Any settings changes need
separate approval. Environment reviewers, prevent-self-review and disabled admin
bypass would strengthen the gate, but adopting another approval mode requires a
separately reviewed implementation/configuration. This workflow rejects
`environment` mode and never deploys on push.

Production Environment variables must identify only the production account,
role, region, instance and document: `AWS_ACCOUNT_ID`, `AWS_REGION`,
`AWS_DEPLOY_ROLE_ARN`, `SSM_INSTANCE_ID`, `SSM_DOCUMENT_ARN`. The reviewed
version `2` and AWS metadata SHA-256 are pinned in the workflow. The job gets
`contents: read`,
`actions: read`, `id-token: write`; no package writes. PR CI receives no AWS
credentials. The exact case-sensitive repository/workflow identity remains
`Kacperkg/vellum`, independently of case-insensitive GitHub URLs.

## 2. Workflow availability

This pin change targets main through a reviewed PR and separate merge approval.
With staging as default, `.github/workflows/release-production.yml` must exist
at the same path on both branches for dispatch discovery and execution.
The only trigger is `workflow_dispatch`. A merge to either branch cannot trigger
this production workflow, even if the selector changes. A dispatch to staging,
a feature branch, a tag, another repository or an unprotected main cannot deploy.
Keep `cancel-in-progress: false` and the single `vellum-deploy-production` group.

## 3. Verify current AWS and controller compatibility

Historical AWS evidence (not current live verification): region `eu-west-2`,
instance `i-073a8f8cf2abd86c5`, role `vellum-github-production-deploy`, document
`VellumDeploy-production`, version `1`, metadata SHA-256
`63e591e39c255a88d9a947474062242124816434eacf0c9c4f6ad8db1b000626`.
The owner reports that version `2` is Active in `eu-west-2`, with AWS metadata
SHA-256 `8d43e90f07c95456d7f781b591cc187eaf448e0c80b3a87d0433b9665297f83a`.
Version `1` remains the default; the workflow selects version `2` explicitly.
This repository change did not contact AWS. With an authorised read-only
operator identity:

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
`standard`. For any later document change, separately review its numeric version
and AWS metadata hash before changing the workflow pin. Verify
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

## 5. Obtain a fresh ordinary staging-push release

Historical evidence before PR #36: PR #29 push `40565614034/1` succeeded overall but skipped
`publish-and-deploy` and has no artifact. PR #28 push `40558125941/1` has no
release artifact. Manual `40560489451/1` produced
`vellum-staging-verification-40560489451-1`, which is ineligible. The selector
was therefore exactly three null fields at that time. PR #36 subsequently
selected real release 40587847468/1 on main. This staging-based policy branch
retains staging's null selector; a new compatible release/promotion is required
for the exception. Older receipts cannot substitute for changed controller source.

After independently approving staging deployment, matching bundle/runtime/proxy
readiness and required CI, an operator may enable CD through the existing
staging activation procedure, then approve a genuine new ordinary push/merge to
protected staging. Its change plan must require a release. This PR changes
workflows/scripts and therefore selects full validation and publication when CD
is enabled. A docs-only push, empty commit or rerun of PR #29 can skip publishing;
none guarantees a usable release. Do not dispatch the manual staging verification
workflow as a workaround. This task does not enable CD, merge or publish images.

## 6. Inspect production eligibility

Record the exact new run and attempt, source SHA and artifact ID. Commands below
are read-only except downloading evidence into a new local directory:

```sh
run_id=REPLACE_REAL_RUN_ID
attempt=REPLACE_REAL_ATTEMPT
gh api "repos/Kacperkg/vellum/actions/runs/$run_id"
gh api "repos/Kacperkg/vellum/actions/runs/$run_id/attempts/$attempt"
gh api "repos/Kacperkg/vellum/actions/runs/$run_id/attempts/$attempt/jobs?per_page=100" --paginate
gh api "repos/Kacperkg/vellum/actions/runs/$run_id/artifacts?per_page=100" --paginate
artifact_dir=$(mktemp -d)
gh run download "$run_id" --repo Kacperkg/vellum \
  --name "vellum-release-$run_id-$attempt" --dir "$artifact_dir"
shasum -a 256 "$artifact_dir/release.json"
```

Require the latest attempt and the exact selected attempt to be completed,
successful ordinary `push` runs on staging in the canonical release-staging
workflow/repository. Require exactly one successful publication job, Change plan
and all seven named CI jobs for that attempt/source. Skipped, failed, cancelled,
missing or ambiguous jobs fail. A newer attempt invalidates an older attempt.
The artifact must be unique, unexpired and named exactly
`vellum-release-<run>-<attempt>`; its archive contains only `release.json`.
Manual verification artifacts, Docker build records and host receipts are not
promotion artifacts. Treat all artifact contents as data, never executable code.

Validate the manifest using `release.validate_manifest(..., 'kacperkg/vellum',
verified=True)` and compare run/attempt/source, immutable `ghcr.io/...@sha256:`
images and all six file hashes. The automated resolver repeats these checks,
checks the raw manifest SHA-256, requires source ancestry in fetched staging and
identical Git trees for `backend`, `frontend`, both deployment Compose files and
`deploy/cd` on main. Merge/squash commit IDs may differ; tree equality is required.
A historical source with identical runtime/deployment trees is acceptable only
with the exact reviewed successful latest-attempt push artifact; changed runtime
or bundle source is rejected. There is no arbitrary age-based substitution.

## 7–8. Configure selection and prepare the promotion PR

Create a descriptive isolated branch from reviewed staging for the promotion PR
to main. Include the workflow/helper and the selected release's exact runtime
and deployment trees. Configure only `deploy/production-release.json` with:

- `staging_run_id`: real positive JSON integer.
- `staging_run_attempt`: real positive JSON integer, latest successful attempt.
- `manifest_sha256`: actual lowercase 64-hex SHA-256 of the downloaded raw file.

No tags, SHA overrides, artifact names or approval evidence are dispatch inputs.
Never fill fields with sample numbers/hashes. Independent review must verify
artifact identity, digest accessibility, bundle compatibility and tree equality;
all seven PR checks must pass before a separately authorised merge. Freeze
unrelated promotions while preparing dispatch. A source/bundle change needs a
new eligible staging release, not a rewritten manifest.

## 9. Backup, isolated restore and release-specific approval

Two-day S3 retention is accepted, but is not restore proof. **Never set
`restore_verified=true` without an actual isolated verified restore.** Missing
restore evidence intentionally blocks production, even after workflow activation.

Minimum procedure for standard production approval (the specifically approved
pre-launch alternative is described in PRELAUNCH-PRODUCTION.md):

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

Only after all prerequisites, approval evidence and an explicitly authorised
promotion merge, recheck current main and the selected artifact. The following
command **deploys production** when enabled; it was not executed by this task:

```sh
main_sha=$(gh api repos/Kacperkg/vellum/git/ref/heads/main --jq .object.sha)
# Review this exact main commit and its selector before acknowledging:
gh workflow run release-production.yml --repo Kacperkg/vellum --ref main \
  -f expected_sha="$main_sha" -f confirm_production_deployment=true
```

The acknowledgement accepts backend replacement, brief API downtime,
AutoMigrate/seeding and smoke requests. It is not a restore attestation. The
helper checks canonical main/protected workflow identity, exact checked-out and
fetched main revision, CD flag, manual mode and acknowledgement; it rechecks
current main and release eligibility immediately before dispatch. A changed main
requires fresh review/dispatch. The reviewed selector stays the sole release
source. Environment-scoped OIDC assumes only the existing production role and
sends one immutable manifest through the pinned production SSM document.

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

## Implementation validation and independent review (2026-10-27)

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
