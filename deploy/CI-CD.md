# CI/CD implementation and activation handoff

Status: **implemented locally, deployment workflows inactive**. No AWS, GitHub
settings, DNS, secrets or live server have been changed. The two release files
end in `.yml.disabled`; GitHub does not load them as workflows. Both also require
repository variable `VELLUM_CD_ENABLED=true`. The production selection is empty
and fails closed. Activation is a separate, reviewed operator action.

## Release and branch model

- Feature PRs target `staging`. CI checks frontend unit/architecture tests,
  E2E typechecking, build/typecheck, lint, formatting and browser workflows;
  Go build/vet/tests/race tests with PostgreSQL 17; production amd64 Docker
  builds; Docker secret exclusions; Compose/Caddy and deployment safety tests.
- CI runs on pushes for diagnostics, PRs to `staging`/`main`, and via
  `workflow_call`. PR jobs have only `contents: read`, no AWS identity or secrets,
  and no write access to release caches. `pull_request_target` is not used.
- Once activated, a push to `staging` first runs the same reusable CI, then
  builds/publishes both release images, deploys their digests to staging and
  verifies them through the existing Caddy. Each release image is built once
  for publication. CI validation builds are not promoted artifacts.
- Staging versions are `sha-<full-sha>-run-<run-id>-<attempt>` in
  `ghcr.io/kacperkg/vellum-backend` and `ghcr.io/kacperkg/vellum-frontend`.
  Tags are identifiers, not a security boundary: deployment uses only digests.
- After a successful staging deployment, the workflow stores immutable artifact
  `vellum-release-<run-id>-<attempt>` containing only `release.json`, for 90 days.
  It records source SHA, image digests, deployment-file hashes, workflow identity
  and the successful staging SSM invocation. The summary prints its SHA-256.
- The reviewer selects that run/attempt and manifest SHA-256 in
  `deploy/production-release.json` through a PR to `main`, promoting the same
  runtime source as staging. Never fill it with arbitrary image references.
- Production resolves the exact artifact from GitHub, verifies repository,
  workflow path, push event, staging branch, run attempt and successful result,
  verifies the reviewed manifest hash, and compares runtime/deployment Git trees
  against the staging source. Merge/squash SHAs may differ; the trees must match.
  An expired/missing artifact, source difference or ambiguous artifact fails.
  There is no production image build or fallback to `latest`/a matching tag.
- The current controller version is part of the tested bundle. An operator must
  install matching trusted controller files before a release that changes them.

## Production approval modes

Set `PRODUCTION_APPROVAL_MODE` explicitly; unset means no production job runs.

1. `environment`: required reviewers on the `production` GitHub Environment
   approve a main push that changes `deploy/production-release.json`, or a manual
   rerun. Enable prevent-self-review and disable administrator bypass where
   supported. Do not select this mode until those rules are verified effective.
2. `manual`: when environment required reviewers are unavailable, use an
   explicit human `workflow_dispatch` on **main** after merging the reviewed
   release-selection PR. Main pushes do not deploy in this mode. Restrict Actions
   write/dispatch access to operators. This is an explicit operator gate, not
   enforced two-person approval; if two-person approval is mandatory, upgrade
   the plan/use a supported external gate before activation.

Environment protection features vary by plan and visibility. GitHub currently
restricts required-reviewer rules on several plans to public repositories.
See [GitHub environment availability](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments).
Both modes still require protected deployment branches: only `staging` may use
the staging environment, only `main` the production environment. If your plan
cannot enforce those branch restrictions, **do not activate this OIDC design**.
The environment OIDC subject replaces branch information; a workflow `if` is
not a substitute for server-side branch protection.

## Manual setup in execution order

Nothing in this section has been executed against AWS or GitHub by this task.

### 1. Protect GitHub branches and environments

- Create `staging` deliberately, then protect `staging` and `main`: PRs, successful
  required CI jobs, code-owner review, stale-approval dismissal, no force pushes,
  and no direct pushes except a tightly controlled emergency process.
- Require all CI jobs, including both `Production image` matrix checks and
  `Deployment configuration and safety tests`. Check exact names after the first
  actual GitHub run; local validation cannot prove branch-rule enforcement.
- Review workflow, controller, Compose and production Dockerfile changes under
  `.github/CODEOWNERS`. A CODEOWNERS file alone does not enforce approval.
- Create separate `staging`/`production` GitHub Environments and restrict their
  branches as above. Configure approval mode before adding deploy-role values.
- Keep Actions enabled only for trusted, SHA-pinned actions. Review the pinned
  action versions before activation and maintain them through reviewed updates.
- Do not run deployment jobs on self-hosted runners shared with untrusted PRs.

### 2. Configure GHCR access

Publishing uses the short-lived job `GITHUB_TOKEN` with `packages: write` only in
the trusted staging publish job. Other jobs do not get package write access.
Grant this repository Actions access to both packages. Version tags are unique
per run attempt; retain digests and do not delete active/rollback releases.
Vite receives only public same-origin configuration; no secrets are build args.

The simplest pull setup is **public GHCR packages**, requiring an explicit
visibility decision by the owner; newly created packages can be private. Inspect
the disclosure implications if the source repository is private. EC2 needs no
registry credential for public packages. Test pulls before activating deployment.

For private packages, this repository does not provision a token broker. Keep CD
disabled until an operator provides and tests a root-owned Docker credential
helper in `/etc/vellum-cd/docker/config.json` (`credHelpers` for `ghcr.io`). One
option is a dedicated machine account granted only these packages, a
`read:packages`-only classic PAT with an explicit short expiry (for example at
most 24 hours), encrypted in a narrowly scoped secret store and retrieved by a
helper using the instance role. Restrict secret/KMS access to that single secret,
rotate before expiry and never put a token in the checkout, a manifest, SSM
Run Command parameters, logs or build arguments. No broad repository/write
scope or long-lived stored token is needed. A short-lived job-token broker is
another option but requires an audited transfer/helper implementation first;
do not assume GitHub App tokens authenticate GHCR. Pulls fail closed if auth is
missing or expired. Public-package activation does not require this helper.

### 3. Prepare EC2 and SSM manually

- Confirm this is the x86 EC2 instance, Ubuntu 24.04, Docker and Compose >=2.24,
  Python 3.12+, working DNS/HTTPS and the existing `web` network/Caddy. No new
  listener or reverse proxy is installed. Keep backend/database ports private.
- Verify SSM Agent is installed, running, registered and **>=3.3.2746.0** for
  `ENV_VAR` parameters. Supply the instance's managed-node IAM role (typically
  `AmazonSSMManagedInstanceCore`) and outbound SSM connectivity/endpoints.
  This is not assumed to exist. Legacy agents fail because `SSM_Manifest` is
  required; no unsafe interpolation fallback is provided.
- Create the AWS OIDC provider for `https://token.actions.githubusercontent.com`
  with audience `sts.amazonaws.com` if absent. Create two distinct roles using
  `iam/github-oidc-trust.example.json`, substituting exact account/repository case
  and `staging`/`production` environment subjects. Never use a wildcard subject.
- Give each role only its environment's fixed custom document and exact instance
  ARN using `iam/deploy-role-policy.example.json`. Do not allow `AWS-RunShellScript`,
  arbitrary document creation/update, SSH, S3 changes or EC2 lifecycle actions.
  `GetCommandInvocation` requires `Resource: *` because that API does not support
  resource-level scoping; it can read other invocation results in the account.
  Consider a separate account/broker if that read exposure is unacceptable.
- Create custom documents named exactly `VellumDeploy-staging` and
  `VellumDeploy-production` from `ssm/staging.json` and `ssm/production.json`.
  Record their immutable numeric versions and SHA-256 hashes from AWS document
  metadata. The workflow sends both, not `$LATEST`/`$DEFAULT`. Deployment roles
  cannot modify documents. The documents take only base64 JSON data and invoke
  a fixed environment/controller; no caller-provided shell is executed.
- Review and install controller assets from the exact trusted revision outside
  the writable checkout. On EC2, an operator can use:

  ```sh
  sudo install -d -m 755 /usr/local/lib/vellum-cd /usr/local/lib/vellum-cd/cd
  sudo install -m 644 deploy/compose.yml deploy/compose.registry.yml /usr/local/lib/vellum-cd/
  sudo install -m 644 deploy/cd/controller.py deploy/cd/release.py deploy/cd/smoke.py /usr/local/lib/vellum-cd/cd/
  sudo install -d -m 700 /var/lib/vellum-cd /etc/vellum-cd /etc/vellum-cd/docker
  ```

  Files/directories must be root-owned and not group/world writable. SSM runs
  Python isolated mode; only this fixed trusted library path is added. Do not
  execute a newly downloaded manifest's scripts, `git pull`, or checkout hooks
  from SSM. This controller assumes this repository's exact package/domain names;
  forks must review and update its allowlist and IAM subjects deliberately.

### 4. Migrate each environment without changing data or Caddy

- Review the checked-out revision and preserve the protected
  `/opt/hosting/projects/vellum/deploy/staging.env` and `production.env` (mode600).
  Never source dotenv files as shell scripts or print resolved Compose config.
- Initialize each environment's PostgreSQL/volumes once through the existing
  manual procedure. CD requires exactly one running PostgreSQL container for
  that project, verifies readiness, and never creates/upgrades/restarts it.
- Keep existing project names, database credentials, named volumes, `web`
  aliases, JWT secret and CORS origin. Digest override uses the same base Compose
  and only overrides backend/frontend images. Existing `IMAGE_PREFIX`/full-SHA
  `IMAGE_TAG` manual build fallback remains available unchanged.
- Set `TRUSTED_PROXY_CIDRS` to stable Caddy `/32`/`/128` where possible. Exact
  subnet trust also trusts every container on that network. Review Caddy globals
  for CDN/load-balancer trust; don't publish backend ports. An operator must
  merge the auth **POST + OPTIONS** matchers into the existing Caddy config if
  not already applied. CD itself never reloads/recreates Caddy.
- The first staging release replaces only its frontend/backend with pulled
  digest images. Pulls of both complete and image architecture/source labels
  pass before any update. `up -d --no-deps --no-build --pull never --wait
  --wait-timeout 180 backend frontend` preserves PostgreSQL and all volumes.
  This is not an atomic two-service transaction; a partial update is possible.
- GitHub concurrency and a nonblocking host file lock serialize each environment,
  including controller invocations by manual operators. Staging and production
  are separate projects, though they still share the t3.small's CPU/memory/disk.

### 5. Provision GitHub variables, names and scopes

| Scope | Name | Meaning |
| --- | --- | --- |
| Repository | `VELLUM_CD_ENABLED` | Keep unset/false until human-approved activation |
| Repository | `PRODUCTION_APPROVAL_MODE` | `environment` or `manual`, as described above |
| Each environment | `AWS_ACCOUNT_ID` | Expected AWS account allowlist |
| Each environment | `AWS_REGION` | Region of the target instance/documents |
| Each environment | `AWS_DEPLOY_ROLE_ARN` | Different OIDC deployment role for that environment |
| Each environment | `SSM_INSTANCE_ID` | Exact existing instance ID |
| Each environment | `SSM_DOCUMENT_ARN` | Its fixed environment document ARN |
| Each environment | `SSM_DOCUMENT_VERSION` | Reviewed numeric document version |
| Each environment | `SSM_DOCUMENT_SHA256` | Reviewed document content hash from AWS metadata |

No AWS access-key secrets are required; OIDC credentials are temporary. The
built-in `GITHUB_TOKEN` is supplied by GitHub. Real application secrets stay in
EC2 env files; they are not GitHub build inputs. Registry helper secrets, if
needed, are provisioned independently as above, not as SSM command arguments.

### 6. Establish backup and schema gates before production activation

**S3 backup automation is not configured by this implementation.** An EC2-local
dump is not disaster recovery. `backup.sh` checking `pg_restore --list` is not a
restore drill. Before any schema-changing release, back up the target database
(including staging if it contains meaningful data). Keep staging synthetic.

Production additionally requires root-owned mode600
`/var/lib/vellum-cd/production-approval.json`, based on
`production-approval.example.json`. An operator must provide the exact selected
manifest SHA-256, UTC backup-creation and verification times less than 24 hours old, private evidence
reference, verified off-host copy and restore evidence, schema compatibility and
trusted-proxy review. False/missing/stale/wrong-release evidence blocks production
before pulling images. These are explicit operator attestations, not automated
proof that an off-host backup or restoration succeeded. Do not set booleans until
the checks have actually been performed; retain evidence privately.

Current Go startup always executes GORM AutoMigrate. Keep one backend per
environment. Review model/migration diffs and require backward-compatible schema
changes; there is no automated migration compatibility proof, down-migration or
rollback of database writes. Incompatible schema changes need a separately
planned maintenance/recovery procedure and must not pass this CD gate.

### 7. Activate staging, then promote deliberately

After human review and the prerequisites above, rename the two `.yml.disabled`
templates to `.yml` through a reviewed PR. Only then set `VELLUM_CD_ENABLED=true`.
No activation step was performed by this implementation.

Push/merge a feature into `staging`. Check the actual CI result, GHCR digest
visibility/pulls, SSM status and staging manifest summary. Through staging Caddy,
verify `/ping`, HTML `/`/`/login`, API POST errors and rate-limit headers. Gin
skips CORS for same-origin requests, so an artificial same-origin OPTIONS
normally returns404; browsers do not preflight those requests. Test204 preflight
behavior only from a separately configured allowed cross-origin Origin.
The automated staging smoke tries a random nonexistent login identity with
changing forged XFF/XReal values, requiring the configured allowance then429.
It supports login request counts1..100. Review very short custom windows, edge
rate-limit plugins or larger limits manually before activation. It never creates
accounts or writes financial records. Also use two genuinely different external
clients/networks to confirm Caddy is not collapsing all clients to its own IP;
synthetic headers alone cannot establish that operational fact.

Then open the promotion PR to `main`, selecting the successful run/attempt and
manifest hash. Prepare that release's backup/schema approval record on EC2.
Merge the PR and approve the production Environment job, or explicitly dispatch
on main in `manual` mode. Production deploys the selected digests unchanged.
Do not dispatch on a feature branch or attempt to bypass missing evidence.

## Failure handling, rollback and manual fallback

The controller emits only service state/health and bounded error summaries.
It does not dump `.env`, resolved Compose, raw Docker inspection, app/GORM logs
or private HTTP responses. SSM command input contains public manifest data, not
credentials. Inspect sensitive logs privately with appropriate access if needed.

Both images pull before mutation. Current/previous/per-release receipts in
`/var/lib/vellum-cd/` update only after health/smoke success. A failed update
leaves the prior successful pointer but may leave mixed application containers;
stop and inspect. It never automatically rolls images back across AutoMigrate.
If a GitHub wait times out, SSM may still be executing. Inspect the exact command
status and host lock before retrying; canceling a workflow does not stop SSM.

For rollback, select `production-previous.json` (or a retained tested release),
review schema compatibility and create fresh release-specific backup approval.
If the tested deployment bundle differs, review/install that trusted version
before invoking it. An authorized operator on EC2 can invoke the same controller
with the receipt as data; the host lock and production gate remain enforced:

```sh
sudo sh -c 'export SSM_Manifest="$(base64 -w0 /var/lib/vellum-cd/production-previous.json)"; exec /usr/bin/python3 -I /usr/local/lib/vellum-cd/cd/controller.py production'
```

Image rollback does not undo migrations. If an old binary cannot use the current
schema, follow the private database restore procedure, with downtime and explicit
data-loss review, before starting the matching image. No `down -v`, volume prune
or automatic database replacement is part of CD.

If Actions/OIDC/SSM is unavailable, the operator can use the existing manual
SHA-tagged Compose procedure in `README.md`, or the same installed digest
controller with a previously tested receipt. Retain previous tested images and
receipts. Manual operations outside the controller must coordinate with its host
lock; do not bypass serialization while a workflow is running.

## What remains unverified before activation

Actual GitHub Actions execution, required checks/approval enforcement, package
visibility and EC2 registry authentication, OIDC/IAM/SSM agent/document/version
configuration, existing server resources, off-host backup/restore evidence and
real public client-IP behavior require manual setup and staging verification.
Local unit/mock tests cannot certify those external conditions. This work does
not deploy, activate production, modify infrastructure or configure S3 backups.

Relevant references: [GitHub OIDC with AWS](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws),
[SSM safe parameter interpolation](https://docs.aws.amazon.com/systems-manager/latest/userguide/documents-syntax-data-elements-parameters.html),
[SSM agent compatibility](https://docs.aws.amazon.com/systems-manager/latest/userguide/documents-schemas-features.html).
