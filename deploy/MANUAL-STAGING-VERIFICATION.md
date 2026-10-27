# Manual staging deployment verification

This extends **Release staging** (`release-staging.yml`); no additional workflow
or AWS capability is needed. It implements the controlled deployment fallback,
not a read-only host probe. Implementation and review do not dispatch it.

**Merge safety:** CD was `true` during read-only inspection on 25 October 2026.
This PR changes workflow/tooling files, so merging with CD enabled triggers an
ordinary staging release. An operator must disable CD before merging if the
first deployment is to be the deliberate manual verification below. This task
does not change that setting or merge the PR.

## Existing contract and selected mechanism

Previously Release staging accepted protected staging pushes only. Its reusable
CI already provides the required checks; neither the workflow nor the installed
controller exposes a non-mutating verification operation. The pinned restricted
SSM document accepts only bounded base64 `Manifest` data and unconditionally
executes the installed isolated Python controller with literal `staging`.
Every valid candidate enters the deployment operation, pulls images, updates
application services and runs smoke requests. Invalid manifests are failures,
not evidence of a successful host verification.

A read-only path verifying installed ownership, Compose/env security, locks,
pending state, current containers and release provenance would require a
separately reviewed controller/SSM contract. We do not add commands, manifest
fields or IAM permissions to simulate it. Public HTTP health alone cannot
establish those host properties or verify image replacement/migrations/rollback.

The manual fallback requires an exact 40-character commit SHA and an explicit
boolean acknowledgement of staging container replacement, AutoMigrate/seeding
and smoke traffic. A credential-free guard rejects the wrong repository,
workflow, event, ref, unprotected branch, disabled CD, missing acknowledgement,
invalid run identity or mismatched SHA. Approval must match `GITHUB_SHA`, checkout
HEAD and the fetched `origin/staging` head. It checks again before publication
and candidate creation. If staging advanced before the release checkout, submit
a fresh approved request. A later push can arrive during builds; the approved
immutable event SHA remains authoritative, not a continuously moving branch.

Manual CI explicitly selects every existing check, rather than relying on an
unsupported-event fallback or pretending an application file changed. After CI
succeeds, the existing publisher builds a fresh pair from that checkout, uses
build-action digest outputs and dispatches the existing staging SSM document.
Schema 1 binds both images to one source/run/bundle; fresh paired builds preserve
that contract without trusting tags, borrowing unverified artifacts or replaying
a failed release. Existing CI image preflight builds remain validation only.

The runner-only adapter is `scripts/ci/staging_verification.py`. None of the six
installed bundle files change: both Compose files and `cd/controller.py`,
`cd/coordination.py`, `cd/release.py`, `cd/smoke.py` keep identical bytes/hashes.
No controller reinstallation is required for this change. A future bundle
mismatch still requires separately approved installation and fails closed.

## Preserved boundaries and evidence

- Canonical `Kacperkg/vellum`, protected `refs/heads/staging`, existing CD flag,
  staging GitHub Environment and its exact branch restriction remain required.
  Only the deployment job has `packages: write` and `id-token: write`; checks
  have `contents: read`. No new permissions or production role are introduced.
- Existing pinned actions, staging OIDC subject/account/role, fixed instance,
  `VellumDeploy-staging` ARN and numeric document version/SHA-256 remain in use.
  No arbitrary shell input, document edits, mutable image selection or production
  execution path is added. Environment-scoped OIDC omits branch identity, so its
  server-side branch restriction remains essential.
- The installed controller verifies six hashes, root ownership/permissions,
  trusted Compose, private `/etc/vellum-cd/staging.env`, image platform/revision,
  existing PostgreSQL readiness, service health and existing Caddy/auth routes.
  Success records the exact candidate source/run/digests/bundle and SSM UUID.
  This is new release evidence; it does not audit an arbitrary pre-existing
  staging receipt or independently prove prior runtime provenance.
- GitHub uses the same non-cancelling `vellum-deploy-staging` group. The installed
  coordination module acquires `backup.lock`, `staging.lock`, `production.lock`
  nonblocking in order, retaining compatibility with older lock users and backup
  services. These are coordination locks; production services are not updated.
  Contention aborts work. Any pending recovery entry aborts work. The workflow
  never deletes markers or removes/replaces lock files.
- The pending record is durable before managed work, removed only after complete
  success under locks, and retained on uncertain failure. The 40-minute job and
  existing 900-second SSM/poll bounds do not prove remote quiescence on timeout.
  Cancellation, status lookup failure or a lost client can leave SSM/Docker or
  migrations running. No automatic SSM cancellation, retry or rollback is added.
- Manual run titles say **Manual staging deployment verification**. Successful
  evidence is named `vellum-staging-verification-<run-id>-<attempt>`, containing
  only `release.json`, retained immutably for 90 days. It is **not eligible for
  production promotion**: the existing resolver still requires a successful
  staging `push` and the `vellum-release-*` artifact namespace. Normal release
  artifacts, source comparisons and staging-to-production promotion are unchanged.

This verifies the managed staging deployment path, including actual replacement
and startup smoke. It does not test recovery/rollback against live data, all
migration outcomes, S3 backup execution, or production readiness. Those failure
paths are exercised only with mocks/synthetic process and lock fixtures here.

## CI and configuration compatibility

The seven required context names and `ci.yml` job definitions are unchanged.
PR CI still runs once; staging push and manual verification each call reusable
CI once. Invalid manual requests fail before that call or release credentials.
Documentation-only pushes still skip publication/deployment; frontend/backend
and dependency-aware detection is unchanged. Manual full validation is the only
explicit exception to normal diff selection. No new image publishing workflow,
trigger bridge or extra automatic release is introduced.

Read-only GitHub inspection on 25 October 2026 confirmed:

- Default branch `staging`; effective staging ruleset protects it, requires PRs,
  up-to-date checks and all seven existing required status names. The legacy
  branch-protection endpoint returns 404 because protection is through a ruleset.
- Staging Environment permits exactly branch `staging`, with no tag policy.
  It has no required reviewers and permits administrator bypass. Staging's
  ruleset requires zero approvals; trusted human review/operator access remains
  an existing security assumption. This change adds no two-person approval.
- Repository CD flag `true`, production approval mode `manual`; staging variables
  select a staging role and instance in `eu-west-2`, numeric SSM version `2`, and
  SHA-256 `942b135e630847280822a9b7aefd7f9c4c153a377da5fd3d2973148840deab3b`.

These are GitHub configuration observations, not live AWS/EC2 verification.
No AWS API, host inspection, database operation, backup timer activation or
deployment was performed. The supplied installed-bundle/no-pending-marker state
is operator context, not independently re-observed here. No new permanent GitHub
configuration is required. Preserve existing restrictions and verify actual OIDC
trust, IAM scope and the immutable SSM version/hash before execution using the
existing [activation prerequisites](STAGING-ACTIVATION.md).

## Operator execution (not performed during implementation)

1. Have an operator set `VELLUM_CD_ENABLED=false` **before merging**, review the
   draft and require all seven PR checks. Merge only with separate authorization.
   Wait for merge CI; its publish/deploy job must skip. Variable changes alone
   do not emit a push. Do not test dispatch from a feature branch or `main`.
2. Verify matching installed six-file bundle and backup scripts, root-owned
   installation/state/env permissions, trusted Compose and staging release
   evidence privately through the established operator procedure. Confirm no
   active/pending SSM, Docker, dump or maintenance operation and no recovery
   marker. Follow [host recovery](HOST-COORDINATION.md) for any uncertainty;
   do not clear markers as part of verification. Keep S3 timers and production
   automation disabled. Verify adequate shared-host CPU, memory and disk capacity.
3. Review staging startup AutoMigrate and seed behavior for the exact selected
   source. Even unchanged application code can write the staging database on
   restart. Obtain a recoverable backup for meaningful staging data using the
   approved process; preserve PostgreSQL credentials/volumes and previous images.
   No backup or database operation is part of this implementation task.
4. Record the current protected staging full SHA and known-good staging release
   evidence. Set CD true only when ready to observe deployment. In **Actions →
   Release staging → Run workflow**, select branch **staging**, paste that SHA in
   `expected_sha`, and check `confirm_staging_deployment` only after approving the
   actual deployment/database effects. Submit once. The workflow must first be
   merged onto the default branch for dispatch discovery; see
   [GitHub dispatch behavior](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#workflow_dispatch).
5. Observe guard, explicit full CI, publication, pinned SSM result and successful
   evidence upload. Compare source SHA, run/attempt, both digests, six hashes,
   SSM UUID and manifest SHA-256. Privately confirm staging health and receipt,
   unchanged PostgreSQL/volume, production container identities and Caddy, and
   no pending record. Never publish env files, resolved Compose or app data.
6. On failure/cancellation/timeout, prevent further releases through the approved
   operator process and inspect the exact remote SSM operation and host recovery
   state. Disabling CD or cancelling Actions does not stop a remote command.
   Do not rerun a failed attempt, overlap recovery, clear a marker or prune
   known-good images. Recover deliberately; image rollback cannot undo migrations.

Two-service replacement is not atomic and failure can leave mixed versions.
Target commands select only staging backend/frontend with `--no-deps`; they do
not update PostgreSQL, production containers or Caddy. Shared EC2 resource
pressure can still affect production availability. Advisory locks cannot
intercept arbitrary root/Docker commands or independent backend restart work.

Readiness: **Ready with caveats** for reviewed operator execution after the
prerequisites above. Live OIDC/SSM/host behavior remains unverified until that
separately approved run. Production now has a dispatch-only workflow; its
release/restore gates remain blocked until the separate
[production activation procedure](PRODUCTION-ACTIVATION.md) is completed.

## Local implementation validation

Executed successfully with synthetic deployment fixtures and no AWS calls:

- 28 CI/planner/manual guard and workflow-boundary tests, including normal
  documentation selection, full manual selection, rejected SHA/repository/ref/
  permissions paths, immutable candidate hashes and promotion rejection.
- 79 deployment security, SSM manifest, provenance, rollback-safety, environment,
  backup and coordination tests. These include real local process/flock
  contention and SIGTERM/SIGKILL fixtures, durable pending-state failure paths,
  and mocked SSM timeout/cancellation/status lookup behavior. No live rollback,
  backup, migration, container replacement or database tests are claimed.
- actionlint v1.7.7 for both active workflows and inactive production template;
  Go SSM regex-engine tests; Compose base/registry and synthetic env relocation;
  local isolated Caddy validation; synthetic backend/frontend Docker context
  secret exclusions; `git diff --check`.

Application files are unchanged, so frontend/backend application suites and
release image builds are left to the ordinary required PR checks; no images
were published. Hosted check reporting and the manual dispatch itself require
GitHub execution. The implementation has independent Actions, AWS security,
deployment reliability and complete-diff reviews; review findings and any final
updates are recorded in the draft PR.
