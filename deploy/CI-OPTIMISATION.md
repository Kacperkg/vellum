# CI optimisation audit — 25 October 2026

This is phase 1: duplicate execution removal, conservative dependency selection
and cache improvements. Mixed-image staging reuse is deliberately deferred to
the coordinated migration below. No settings, AWS resources or installed
controller files are changed by this work. Production remains disabled.

## Measured baseline

Read-only audit of GitHub Actions API job/step timestamps, latest attempts.
These are elapsed runner seconds, not billing records; queue times and job-minute
rounding are separate. Raw API timestamps can be reproduced with:

```sh
gh api 'repos/Kacperkg/Vellum/actions/runs/40207640077/jobs?per_page=100'
```

| Run | Event | Total job seconds | Workflow wall time |
| --- | --- | ---: | ---: |
| [40207640114](https://github.com/Kacperkg/Vellum/actions/runs/40207640114) CI | staging push | 389 | 3m15s |
| [40207640077](https://github.com/Kacperkg/Vellum/actions/runs/40207640077) Release staging | same staging push | 468 | 4m19s |
| [40206819701](https://github.com/Kacperkg/Vellum/actions/runs/40206819701) CI | feature push | 397 | 2m57s |
| [40206842006](https://github.com/Kacperkg/Vellum/actions/runs/40206842006) CI | PR, same feature SHA | 726 | 8m08s |
| [40205619267](https://github.com/Kacperkg/Vellum/actions/runs/40205619267) CI | feature push | 613 | 5m24s |
| [40205685912](https://github.com/Kacperkg/Vellum/actions/runs/40205685912) CI | PR, same feature SHA | 440 | 2m44s |
| [40204119125](https://github.com/Kacperkg/Vellum/actions/runs/40204119125) CI | PR | 397 | 3m11s |

Successful staging release 40207640077 job timings:

| Job | Seconds | Main cost |
| --- | ---: | --- |
| Backend and PostgreSQL | 194 | Race step 113; build 27; DB startup 14; tests 15 |
| Browser workflows | 119 | Browser/OS installation 29; E2E 52; Go setup 10 |
| Frontend quality | 29 | pnpm/Node setup 10; build 4; lint 3 |
| Backend image preflight | 24 | Buildx setup 10; cached build 5 |
| Frontend image preflight | 20 | Buildx setup 5; cached build 6 |
| Deployment safety | 21 | Go setup 7; configuration validation 8 |
| Docker secret exclusions | 5 | Context validation |
| Publish and deploy | 56 | Checkout/Buildx 14; builds 6 + 5; SSM 17 |

The browser install step across these seven runs took 20–29s normally and
233s/401s in two outliers. In run 40206842006 the log shows Chromium download
starting 15:25:51 UTC, connection failure at 15:32:10, followed by a successful
retry. Apt downloads completed in seconds. This supports caching browser
binaries for this repository despite Playwright's general advice that cache
restore often costs as much as downloading. OS dependencies still install on
every fresh runner; the cache cannot eliminate apt/network delays.

## Why fourteen checks appeared

`ci.yml` originally subscribed to unrestricted `push`, PRs into staging/main,
and `workflow_call`. Five single jobs plus two instances of the production-image
matrix give seven check contexts. Every feature push with an open PR therefore
ran seven on the push and seven on the PR merge ref. They test different Git
refs but mostly duplicate work. Staging push also ran standalone CI while
Release staging invoked the same seven through reusable CI.

Now CI subscribes only to PRs into staging/main and reusable calls. Release
staging owns staging push validation, including when CD is disabled. Publication
requires a successful reusable CI result, an explicit non-documentation release
plan, the existing CD flag, protected staging ref, repository allowlist and
staging Environment. There is no standalone main push CI: the promotion PR is
checked; the inactive production workflow retains its existing provenance gates.

PR updates cancel superseded PR CI. Staging releases remain serialized and
non-cancelling. No `workflow_run` bridge or privileged PR execution is added.

## Change selection and expected behaviour

The detector uses full local Git history, not the paginated PR file API. PRs
compare merge-base(base SHA, head SHA)..head SHA; staging compares before..after,
covering all commits in a push. Renames are represented as deletion plus addition,
so an application file moved into docs still selects application checks. Missing
history, first push, invalid event, non-UTF8 paths and diff failures select the
full plan. Unknown files select everything. The detector's own regression suite
runs before every plan, including documentation changes.

| Change | Heavy checks | PR image preflight | Staging publication/deployment |
| --- | --- | --- | --- |
| Frontend only | Frontend, cross-app browser, Docker exclusions, deployment safety | Frontend only | Fresh backend/frontend pair, then verified deployment (phase 1 limitation) |
| Backend only | Backend incl. PostgreSQL/race, cross-app browser, Docker exclusions, deployment safety | Backend only | Fresh pair, then verified deployment (phase 1 limitation) |
| Docs outside component source trees | Explicit lightweight guard in all seven contexts | Neither | Neither; no credentials or SSM job |
| Shared/build/workflow/unknown | All checks | Both | Fresh pair and verified deployment |
| Deployment/IAM/SSM/controller/manifests | Docker exclusions, deployment safety | No application preflight unless its context changed | Fresh pair and verified deployment; installed bundle checks may require operator coordination |
| Component tests/dependency files | Component checks plus browser and safety | That component | Fresh pair; conservative because Docker COPY includes component context |
| CI/test tooling in scripts or unknown test paths | All checks | Both | Fresh pair |

The no-release documentation allowlist is root README.md, docs/, and immediate
deploy/*.md. Component-local README.md and backend/doc/ are categorized as
documentation but conservatively select component validation and a fresh paired
release: schema 1 promotion compares entire backend/frontend trees. Skipping
these releases would otherwise block main's provenance comparison. Narrowing
that comparison needs the coordinated phase-2 controller migration. This is a
phase-1 limitation: not every documentation-only change can avoid publication.
Markdown in .github/ or an arbitrary application directory is not automatically
eligible for bypass. Deployment examples,
policies, SSM documents and test files remain safety-relevant.

Build contexts are independently `backend/` and `frontend/`. Neither Dockerfile
copies the repository root or the other component. Each copies its own dependency
manifests before source. Component .dockerignore files exclude credentials,
local dependencies and generated output. No cross-component runtime inputs were
found. The browser stack does build and start both components, so browser tests
remain selected for changes to either one. A frontend-only PR still compiles the
Go server for those integration tests; it does not build the backend image or
run the backend unit/race suite.

### Required-check safety and actual rulesets

Read-only API inspection found active repository rulesets 25819804 (staging) and
25819760 (main). Both bind seven contexts to GitHub Actions integration 15368:

- `Frontend quality`
- `Backend and PostgreSQL`
- `Browser workflows`
- `Docker secret exclusions`
- `Deployment configuration and safety tests`
- `Production image (backend, linux/amd64)`
- `Production image (frontend, linux/amd64)`

**No required-context rename or removal is necessary.** Keep these exact seven
names on both branches; do not replace them with the new informational
`Change plan` context or reusable `checks / ...` names. Every original job uses
`needs: changes` and `if: always()`, checks out source, and validates that detection
succeeded with a complete boolean plan. It then runs selected steps or records
an explicit unaffected decision. Jobs themselves are never conditionally skipped.
Failed/cancelled/skipped detection or missing/malformed outputs fail every guard;
selected step failures fail their original required context. There is no
continue-on-error. GitHub's acceptance of skipped jobs is not used as a gate.
Workflow cancellation cannot produce a new successful guard result.

The existing staging ruleset requires up-to-date checks; main does not. Both
currently require zero approving reviews and have code-owner review disabled.
CODEOWNERS alone does not prevent a PR from changing its own detector/workflow.
Recommended operator follow-up: enable code-owner review and at least one
approval, with an appropriate reviewer/bypass policy, on both branches; consider
up-to-date checks on main. This is an existing trust assumption, not proof of
current enforcement. New CI selection scripts and Docker exclusion files have
CODEOWNERS entries. No settings are changed here. A merge queue would need a
separate reviewed `merge_group` trigger and diff strategy before enabling it.

## Implemented performance changes

- Remove both duplicate trigger paths; keep PR merge-ref validation and staging
  push validation exactly once each.
- Avoid irrelevant dependency setup/install, test and preflight build steps.
  Start the synthetic PostgreSQL container only when backend tests are selected.
- Keep all selected checks parallel after a short detector job. Keep the backend
  database tests serialized (`-p 1`) and race detector enabled.
- Keep frozen pnpm installs and the existing lockfile-based pnpm store cache.
  Do not cache node_modules or remove lifecycle/dependency verification.
- Separate Go module/build caches by job, OS, architecture and go.mod/go.sum,
  with a source-SHA key and same-dependency restore prefix. Previously all three
  setup-go jobs shared one immutable key; a short safety job could save its cache
  before the race build. This could leave later runs without the expensive race
  compilation cache. Per-job caches preserve each workload's build outputs.
  The actual speedup requires fresh hosted measurements; race execution itself
  is unchanged. New cache namespaces start cold.
- Read both trusted `release-{component}` and `production-{component}` BuildKit
  scopes. Previously release builds wrote release-* but nobody read them. PRs
  never export Docker caches; staging CI exports production-* and trusted staging
  publish exports release-*. No mutable image tag becomes a release input.
- Cache Chromium/FFmpeg/headless binaries by Ubuntu version, architecture and
  exact frontend lockfile. No broad restore prefix. Always run the pinned
  Playwright install command for version and OS dependency requirements. PR cache
  writes stay scoped to the merge ref under GitHub's isolation rules and cannot
  populate staging's cache. Cache contents are not release provenance.
- Keep the existing dependency-first Docker layers. Do not add BuildKit Go/pnpm
  cache mounts: these mounts are not automatically persisted by the GHA layer
  exporter and would add complexity without a measured benefit here.
- Keep publication's two builds sequential: observed warm builds total only 11s;
  parallel publish jobs would add runner setup, credentials and manifest fan-in.
  Independent preflight image builds already run in parallel. Reconsider if cold
  release build timings show publication is a material bottleneck.

The new actions/cache pin is the official v4 commit
0057852bfaa89a56745cba8c7296529d2fc39830 resolved during the audit. Permissions remain
contents:read in CI; packages:write and id-token:write remain only in the protected
staging publish/deploy job. No pull_request_target, broad token, AWS policy or
SSM permission change is introduced.

## Safe image reuse: coordinated phase 2, not implemented

Schema 1 stores the backend/frontend digest pair and one source_sha. The installed
controller pulls both immutable images, requires each image's OCI revision label
to equal that one SHA, and compares all six controller/Compose bundle hashes.
An old component digest retains its old label. Reusing it with a new source_sha
fails the controller, correctly. Simply relaxing label checks, editing manifests,
or relabelling old images would remove provenance guarantees. Changing release.py
also changes the installed bundle contract and needs an operator installation.

A separate reviewed migration should:

1. Define a schema with per-component build source/run provenance alongside the
   tested release source and paired immutable digests. Preserve strict bounded
   data-only manifest validation, explicit field allowlists, digest repository
   allowlists, fixed SSM parameters and exact installed bundle hashes. Design
   schema-1 receipt/rollback compatibility before installing anything.
2. Resolve a prior **completed successful** protected staging push from the exact
   release workflow/repository. Verify its exact run attempt, non-expired unique
   immutable artifact, artifact/run/source identities and verified smoke receipt;
   exclude failed/cancelled/in-progress releases and artifacts from earlier
   attempts. Require Git ancestry and compare dependency/build-context trees to
   the selected release, not just the latest push. A successful docs-only workflow
   without a release artifact is not a verified release. Bound pagination and API
   input sizes; API/auth ambiguity must fail safely rather than approve reuse.
3. Prove unchanged input equivalence for each component including Dockerfile,
   exclusion rules, lockfiles, public build args, shared inputs and platform.
   Mutable base tags are a separate freshness input: establish a base-digest/
   rebuild policy so unchanged source does not reuse a vulnerable base forever.
   If there is no verified ancestor, the artifact expired/is missing, or inputs
   differ, build a fresh component. Never resolve reuse from sha-* or latest tags.
4. Emit a full digest pair every time, retain provenance for the reused component,
   validate each image label against its component's actual build revision, and
   update Compose without replacing unchanged components unnecessarily. Test
   config-change recreation, platform checks, smoke failures/partial updates,
   controller locking and receipt updates before declaring the pair verified.
5. Extend production resolution to prove main runtime/deployment equivalence to
   the tested staging source and validate inherited component provenance without
   rebuilding. Preserve reviewed run/attempt/manifest hash selection. Extend
   rollback tests for schema-1 receipts, new paired receipts, missing images,
   old installed bundles and schema compatibility; never automate DB rollback.
6. Have an operator install the exact reviewed bundle and test the migration on
   staging before allowing partial reuse. Coordinate production compatibility
   while leaving production activation a separate decision.

Initial releases and failed/incomplete staging releases therefore continue to
build fresh pairs today. Rollbacks use the existing tested pair and controller
schema/backup checks unchanged. No image replacement savings are claimed for
unchanged staging components in this phase.

## Expected savings and remaining limits

The successful staging merge consumed 857 raw runner-seconds across CI + Release
staging (about 14.3 raw minutes). Removing standalone CI saves its 389 seconds;
the detector and seven guards add overhead. Expect roughly **40–45% fewer raw
runner-seconds for a full staging merge**, before cache gains, and similar
**40–50% savings for ordinary full PR updates** compared with push+PR duplication.

GitHub rounds each job to a minute where billable. The sampled staging merge is
approximately 23 rounded Ubuntu job-minutes; expect about 12–14 for full changes
and 8 for a docs-only push (one detector plus seven lightweight contexts).
Thus docs may save over 90% raw execution but only roughly **65% rounded minutes**
on this comparison. Frontend-only PRs should take about 9–11 rounded minutes
instead of roughly 22 for two ordinary full runs. Backend-only PR savings are
closer to duplicate removal because backend/race and browser remain selected.
Actual billing depends on repository plan/free allowance and runner accounting.

Wall time is different: removing a parallel duplicate does not halve release
latency. A full staging release may initially stay around 4–5 minutes, including
new detector latency and cold cache population. Per-job Go cache hits may remove
a large part of repeated compilation; frontend-only CI can avoid the backend
critical path (roughly 1.5–2.5 minutes with warm browser dependencies). Docs-only
runs should finish in roughly 20–60s plus runner queueing and have no deployment.
Browser download outliers may fall substantially on cache hits, but normal
speedup is uncertain and cache restore/storage costs must be monitored. Do not
claim these estimates as measured results of the new workflow.

Retain immutable images and verified artifacts for active/rollback releases.
Artifact expiration, GHCR retention and operator-installed bundles still constrain
promotion/rollback. No live AWS/SSM call, image publication, GitHub setting change,
production activation or deployment is part of this audit.

References: [required/skipped checks](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks),
[cache isolation](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching),
[Playwright cache tradeoffs](https://playwright.dev/docs/ci#caching-browsers),
[BuildKit GHA caching](https://docs.docker.com/build/ci/github-actions/cache/).

## Validation and independent review

Passed locally: actionlint v1.7.7 (active workflows and inactive production
workflow), 16 CI selection/required-check/cache regressions, 35 deployment
security/provenance/rollback tests, Go module verification/build/vet, 191 named
Go tests across 10 tested packages with real isolated PostgreSQL 17 and no
skipped tests, all-package race tests, 15 frontend tests, E2E typecheck, build,
lint, format check, 10 Chromium/mobile browser tests, Docker secret exclusions,
Compose/env relocation/Caddy validation, Go SSM regex tests and both linux/amd64
production Docker builds without publishing. Application sources were unchanged.

An independent security/reliability subagent reviewed the completed diff and ran
13 original CI regression tests plus all 35 deployment tests. No blocking
findings. Its browser-test categorization suggestion was fixed. A final review
identified the component-documentation promotion constraint; bypass was removed
for those paths and a regression added. The expanded 16-test suite passed. git diff --check passed.

No AWS calls, image publication, settings changes or live OIDC/SSM validation
were performed. Hosted cache gains and exact check reporting require the draft
PR run; performance estimates above are not new-workflow measurements.
