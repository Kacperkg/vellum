# First automated staging deployment

Historical activation handoff: the first staging deployment has since succeeded.
The current change-selection rules are in [CI-OPTIMISATION.md](CI-OPTIMISATION.md).
Documentation-only pushes no longer trigger publication or deployment.
For the current explicit manual entry point, use
[MANUAL-STAGING-VERIFICATION.md](MANUAL-STAGING-VERIFICATION.md). Its merge and
execution prerequisites supersede the historical first-trigger instructions here.

This PR removes GHCR bootstrap and enables the staging workflow in source only.
It does not set `VELLUM_CD_ENABLED`, dispatch a deployment, change GitHub settings
or touch AWS. Production was disabled at that historical activation; the current
dispatch-only workflow and remaining prerequisites are in
[PRODUCTION-ACTIVATION.md](PRODUCTION-ACTIVATION.md).

## Effect of merging

Merge only after review and all seven required CI checks pass. With the current
`VELLUM_CD_ENABLED=false`, the merge push runs reusable CI inside Release
staging; publication/deployment skip. No release images are published, AWS role assumed or SSM command
sent. The bootstrap disappears. Changing a repository variable alone does not
emit a push or start a deployment.

Once CD is enabled, every eligible non-documentation push to protected `staging` in
`Kacperkg/vellum` runs reusable CI, then builds both production images from that
push's immutable checkout SHA for `linux/amd64`. It publishes unique
`sha-<source-sha>-run-<run-id>-<attempt>` tags, creates a candidate with the build
action's two digest outputs, assumes the staging role and sends that data to
the fixed staging SSM document. No tag lookup or `latest` resolution is used.
Successful health/proxy smoke checks produce the verified release artifact.
The current staging workflow also has an explicitly acknowledged manual
verification trigger, described in the runbook above. It has no PR, tag, main
or workflow-call trigger.

## Review findings and safety boundaries

- Both jobs require the push event, canonical repository, exact staging ref
  and `github.ref_protected`. Publication additionally requires the CD flag,
  successful reusable CI and a release-needed plan. Checkout explicitly
  uses the event SHA. The reusable checks have only `contents: read`; only the
  publish/deploy job gets `packages: write` and `id-token: write` and uses the
  staging GitHub Environment.
- GitHub read-only checks during preparation confirmed default staging, all
  seven required staging CI checks, exact staging environment branch policy,
  CD false and production mode manual. No approving reviews or code-owner
  approval are currently required by staging's ruleset. Human review of this
  PR and trusted workflow/controller changes remains an operator prerequisite;
  CI checks alone do not establish review. Enforce the review requirements in
  CI-CD.md before enabling CD, or treat their absence as an activation blocker.
- Staging variables pass format/consistency checks for `eu-west-2`, its AWS
  account/role, instance ID, exact `VellumDeploy-staging` ARN, numeric document
  version and SHA-256. This does not verify their live AWS resources or policies.
- The dispatcher supplies both `--document-version` and
  `--document-hash-type Sha256 --document-hash`. The SSM document accepts bounded
  base64 JSON through `ENV_VAR` and invokes only the installed isolated Python
  controller with literal `staging`. No manifest command/path is executed.
- Manifest validation restricts package names and SHA-256 digests, source SHA,
  run/attempt IDs, fixed bundle files and verification fields. The controller
  checks its installed bundle hashes, image architecture and revision labels.
  Labels are a consistency check, not cryptographic provenance: trust depends
  on reviewed protected-branch code, pinned actions and GHCR write permissions.
- GitHub concurrency does not cancel an active staging release. A host file lock
  rejects concurrent staging controller invocations. Pending GitHub runs can be
  superseded; not every queued commit is guaranteed a rollout. Manual commands
  must use the same lock/controller and must not overlap the first run.
- Docker update commands select project `vellum-staging` and only `backend`
  and `frontend`, with `--no-deps --no-build --pull never`. Both images pull and
  pass inspection before either service updates. There is no postgres update,
  volume deletion, `down`, prune, production service command or Caddy reload.
  Existing PostgreSQL readiness is checked; volumes and images are retained.
- This does **not** preserve staging database contents unchanged: backend startup
  runs AutoMigrate and seeding. Review migrations and back up meaningful staging
  data. Production databases are outside the targeted Compose project, but the
  t3.small and external web network are shared: memory, disk or CPU exhaustion
  can affect production availability. There is no infrastructure isolation or
  absolute guarantee of zero production impact.
- Two-service replacement is not atomic. Failure can leave mixed application
  versions; no automatic rollback runs, and image rollback cannot undo database
  migration/writes. Retain known-good image digests and coordinate manual recovery.
- New preflight checks reject non-root-owned/non-private environment files,
  a proxy CIDR other than fixed Caddy `172.18.255.254/32`, or staging smoke login
  limits outside 1..100 **before** image pulls or service updates. Previously,
  unsupported smoke limits were discovered only after service replacement.

## Manual prerequisites before enabling the flag

1. Confirm the merged revision has passed review/CI and production remains
   disabled. Confirm no older staging workflow run or SSM command is pending or
   running. Record the current staging app image references and database volume
   identity privately; retain all rollback images. Verify adequate EC2 disk,
   memory and CPU headroom, with no conflicting maintenance/deployments.
2. **Update the installed controller bundle to this exact reviewed revision.**
   The shared [host-coordination gate](HOST-COORDINATION.md) also requires
   installing `cd/coordination.py` and matching backup scripts in a drained
   host-wide maintenance freeze. This PR changes `deploy/cd/controller.py`.
   An already-installed older bundle
   fails closed against the new candidate hashes. The operator must install the
   reviewed six files using the root-owned installation procedure in CI-CD.md;
   compare SHA-256 of `compose.yml`, `compose.registry.yml`, `cd/controller.py`,
   `cd/coordination.py`, `cd/release.py` and `cd/smoke.py` between `deploy/` at the reviewed revision and
   `/usr/local/lib/vellum-cd/`. Keep directories root-owned 755, files 644 and
   state directory root-owned 700. Retain the previous reviewed bundle revision
   with rollback evidence. Older controllers expecting checkout env paths cannot
   simply be replayed; use the migration guide's reviewed recovery procedure.
   This shared library also serves any manual production controller invocation;
   coordinate its update. Future manual production calls inherit the private
   `/etc/vellum-cd/production.env` and exact-proxy preflight checks too. No container
   restart or production deployment is needed to install it. This task does not perform
   that EC2 update.
3. Migrate existing secrets using [ENVIRONMENT-MIGRATION.md](ENVIRONMENT-MIGRATION.md).
   Verify `/etc/vellum-cd/staging.env` is root-owned, regular, singly linked and
   mode 600, with root-owned, non-writable, non-symlink parents. The checkout
   remains ubuntu-writable; the controller has no checkout fallback. Never source
   or print the env file. Preserve project name
   `vellum-staging`, database credentials/volume, exact staging APP_ORIGIN and
   JWT secret. Set `TRUSTED_PROXY_CIDRS=172.18.255.254/32`, and review login limit
   (1..100) and window (normally 1m) for the smoke sequence.
4. Verify existing Caddy still has fixed address `172.18.255.254` on external
   network `web`, correct staging aliases, HTTPS, API and POST/OPTIONS auth
   routes. Review any Caddy global proxy trust. Check two different real external
   clients retain different IPs; forged-header smoke alone cannot establish this.
   No Caddy reload/recreation is part of this release.
5. Verify one existing running staging PostgreSQL instance, Compose >=2.24,
   Python >=3.12 and SSM Agent >=3.3.2746.0. Review AutoMigrate compatibility and
   obtain a recoverable backup if staging contains meaningful data. Verify both
   published GHCR packages remain anonymously pullable using the controller's
   root-managed Docker config and EC2 egress/DNS, without changing running apps.
6. Independently inspect live AWS policies: the staging role trust must require
   audience `sts.amazonaws.com` and the exact staging environment subject
   `repo:Kacperkg/vellum:environment:staging` for the currently confirmed legacy
   GitHub OIDC format. Keep production a distinct role/subject. Environment
   subjects omit branch identity, so retain selected-branch policy **staging
   only**, with no tags/wildcards. IAM must allow SendCommand only for the exact
   staging instance and `VellumDeploy-staging` document, not production or
   AWS-RunShellScript, and must not grant document edits or other admin actions.
   GetCommandInvocation requires `Resource: *` and exposes other invocation
   results in the account; review this existing limitation.
7. Compare the actual immutable numeric SSM document version and AWS-reported
   SHA-256 against GitHub staging variables. Inspect that version's content
   against `deploy/ssm/staging.json` (literal staging controller invocation,
   ENV_VAR and size bounds). Do not rely on `$LATEST` or `$DEFAULT`; verify the
   node has the intended matching controller. GitHub format checks cannot prove
   any of these remote properties. This task did not call AWS or inspect EC2.

## Safest first trigger (operator action, not performed here)

1. Merge this PR with `VELLUM_CD_ENABLED=false` and wait for the merge CI to pass.
   Complete every prerequisite above. Ensure production `.yml` is absent from
   both main and staging; the repository flag is shared with future production
   workflows, so its protection depends on production remaining disabled.
2. Have the operator set repository variable `VELLUM_CD_ENABLED=true` only when
   ready to observe the whole rollout. Do not rerun a historical release job.
3. Create a fresh `feature/first-staging-release` branch from current staging,
   make a reviewed application change, push it and open a PR into staging.
   A documentation-only change will validate CI but will not trigger a release.
   Review it, require all seven checks and merge via normal branch protections.
   Do not direct-push to protected staging or weaken checks. For verification
   without an application change, use the reviewed manual verification runbook.
4. Observe **Release staging** for that merge SHA: reusable CI must succeed
   before publication/deployment. Check role/account, pinned document references,
   successful SSM result and the verified artifact's source SHA and both digests.
   Confirm staging `/ping`, `/`, `/login` and auth routing/429 proxy smoke passed.
   Privately confirm the original postgres/volume, production service container
   identities and Caddy were retained, and production remains healthy. Do not
   print secrets, resolved Compose configuration or application data into CI.
5. If anything fails, set the flag false to prevent future starts and inspect the
   exact SSM command, host locks and [pending operation recovery](HOST-COORDINATION.md)
   before retrying. Disabling the flag/cancelling
   Actions does not stop an already-running remote command. Do not prune images,
   replace databases or automatically roll back migrations. Recover deliberately
   using the previous digests and the schema/backup procedure in CI-CD.md.

Afterward, normal reviewed feature PR merges into staging automatically follow
this pipeline. Main and production activation remain separate approval work.
