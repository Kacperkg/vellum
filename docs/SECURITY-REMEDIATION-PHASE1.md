# Security remediation phase 1 — dependencies and images

Review date: 25 October 2026. Integration baseline: `67b022b` on `staging`.

## Result and scope

**Ready with caveats** for review through a draft PR. This change updates dependency/toolchain versions and production image inputs. Application authentication, API contracts, migrations, CI selection/release logic, deployment controllers, and live infrastructure are unchanged. CI installer versions are aligned to patched pnpm and supported Node LTS releases. No images were published and no deployment was performed.

The current CI optimisation is retained. The changed dependency/Dockerfile paths select all required checks, including browser tests and both production images. Merging into `staging` can trigger a release; draft status is not deployment approval.

## Versions

| Component | Baseline | Updated |
| --- | --- | --- |
| Go | 1.26.4 | 1.26.10 |
| pnpm | 10.17.0 | 10.34.8 |
| Node (CI/development) | 25.2.1 / 25 | 24.22.1 LTS |
| pgx/v5 | 5.6.0 | 5.9.2 |
| quic-go | 0.59.0 | 0.59.1 |
| x/crypto | 0.53.0 | 0.57.0 |
| x/net | 0.55.0 | 0.60.0 |
| x/text | 0.38.0 | 0.42.0 |
| x/sys / x/sync | 0.46.0 / 0.21.0 | 0.48.0 / 0.23.0 |
| Axios | 1.18.1 | 1.20.0 |
| TanStack React Router | 1.170.16 | 1.170.46 |
| Router CLI / plugin | 1.167.17 / 1.168.18 | 1.167.46 / 1.168.48 |
| seroval | 1.5.4 | 1.7.2 |
| Vite | 8.1.0 | 8.3.5 |
| PostCSS | 8.5.16 | 8.5.31 |
| brace-expansion | 5.0.7 | 5.0.12 |
| nanoid | 3.3.15 | 3.3.20 |
| browserslist | 4.28.4 | 4.29.4 |
| baseline-browser-mapping | 2.10.40 | 2.11.32 |
| source-map-js | 1.2.1 | 1.2.2 |
| follow-redirects | 1.16.0 | 1.16.1 |
| Zod | 4.4.3 | 4.6.7 |

Go 1.26 remains supported under the [Go release policy](https://go.dev/doc/devel/release); its current patch was verified using the [official download metadata](https://go.dev/dl/?mode=json&include=all). The selected x/net release requires the higher x/crypto/x/text versions. Gin, GORM, JWT, React and React DOM versions are retained. `go mod tidy` also corrects direct versus indirect declarations.

Frontend updates use supported package ranges and the frozen pnpm lockfile, without forced upgrades, overrides, custom registries or dependency replacements. The new TanStack generator reorders the generated route tree; route IDs, paths and parents are identical. All lockfile package resolutions retain SHA-512 integrity. Release/version evidence came from official Go/module, npm, Alpine and container-registry metadata.

## Production images

All four production builder/runtime base references now have explicit versions and immutable OCI index digests in their Dockerfiles:

- Backend builder: Go `1.26.10-alpine3.23`; runtime: Alpine `3.23.7`.
- Frontend builder: Node `24.22.1-alpine3.24`; runtime: Nginx unprivileged `1.30.6-alpine3.24`.
- `GOTOOLCHAIN=local` makes the production Go build fail rather than silently download a different compiler. The extracted binary reports `go1.26.10`.
- Latest upstream Nginx still contains the affected libraries, so its runtime explicitly installs the official fixed Alpine packages: Brotli `1.2.1-r0`, musl and musl-utils `1.2.6-r3`, zlib `1.3.2-r1`. This is a targeted update, not a blanket package upgrade.
- Builder npm is updated from bundled `11.19.1` to supported same-major `11.21.1` before bootstrapping patched same-major pnpm `10.34.8`. Docker, CI and the packageManager field agree on that exact pnpm version.

Both images were built locally with `docker buildx build --platform linux/amd64 --load`. Final users remain `10001:10001` and `101:101`; ports, health checks, entrypoints and multi-stage copy boundaries remain intact. Temporary build root is confined to package installation. The development Go image is aligned to the same compiler patch. The development frontend uses the same pinned Node Alpine base and npm/pnpm versions as the production builder; its existing shell entrypoint works on this base. CI Node changes from EOL 25.2.1 to supported LTS 24.22.1 without changing job/selection logic. The [official Node schedule](https://github.com/nodejs/Release/blob/main/schedule.json) records Node 25 EOL on 1 June 2026. Frontend quality/browser tests were repeated with a task-local official Node 24.22.1 binary checked against its published SHA-256.

## Scanner comparison

Baselines were reproduced before dependency edits using Linux/amd64 source analysis, pnpm audit and the audited exact public GHCR image digests. Counts are scanner advisory/package matches, not demonstrated exploitable application vulnerabilities.

| Check | Baseline | Updated |
| --- | --- | --- |
| pnpm audit | 35: 1 Critical, 22 High, 11 Moderate, 1 Low | 0 |
| Linux Go source | 34 affected IDs, 16 with called-symbol traces | 1 module-only ID, 0 called |
| Backend runtime OS | 0 | 0 |
| Backend executable, Trivy | 40: 2 Critical, 18 High, 17 Medium, 2 Low, 1 Unknown | 1 Unknown, module-only |
| Frontend runtime OS | 4: 1 High, 1 Medium, 2 Unknown | 0 |
| Full frontend builder (intermediate scan after npm update) | 59: 1 Critical, 34 High, 24 Medium | 3: 1 High, 2 Medium |

The pgx Critical-rated advisories, applicable Go patch findings, x/text/quic-go findings and affected frontend dependency versions have been removed from the resolved graph. This does not establish that every baseline advisory was externally exploitable. Earlier pgx SQL-injection findings required non-default simple-query/dollar-quoted SQL conditions that were not found in Vellum. Browser Axios usage and a static Nginx runtime also differ from Node adapter/SSR/dev-server prerequisites.

No scanner ignore rules or skipped security tests were added. Trivy's shared-cache lock caused one initial scan attempt to fail; sequential reruns completed successfully. Scans used Gitleaks 8.30.1, govulncheck 1.9.0 and Trivy 0.76.1.

## Remaining findings

- **Medium build-tool risk:** the patched npm 11.21.1 bundle retains three advisory matches in ip-address 10.7.2 and undici 6.29.0 (scanner: one High, two Medium). The original pinned Node base had 15 bundled npm matches (five High, nine Medium, one Low); this base scan was not a complete builder scan. The subsequent full builder scan found 59 matches after the npm patch: three in npm and 56 in the original pnpm 10.17.0 bundle, including Critical-rated tar archive decompression/resource-exhaustion. Package installation consumes archives, making that build CPU/disk denial-of-service prerequisite relevant; application RCE was not demonstrated. A supported pnpm 10.34.8 update fixes those installer/bundled-library findings; only the three npm matches remain. Remaining paths require crafted IPv6-zone input, EventSource parsing or malicious content-encoding responses. They are build-stage dependencies, not shipped in Nginx. Unlike the pnpm tar extraction issue, the remaining npm paths were not demonstrated in the controlled bootstrap. No corresponding Vellum exploit was established. No forced bundled-dependency surgery or npm major upgrade was used to manufacture a clean scan.
- **Informational:** `GO-2026-5932` concerns unmaintained `golang.org/x/crypto/openpgp`. The x/crypto module is retained for bcrypt; OpenPGP is absent from `go list -deps`. Source/binary module metadata and Trivy retain the advisory as Unknown without a used vulnerable package/call path.
- **Low build-availability risk:** exact Alpine package revisions can disappear from moving repositories. Builds intentionally fail closed. Refresh the base digest and package pins together after reviewing upstream fixes, then rebuild, scan and test. Digest/version pins improve reproducibility but do not make registry/repository-backed builds hermetic.
- **Informational operational limit:** PostgreSQL and Caddy configuration were validated, but live EC2 image identities, infrastructure and production data were not inspected or changed. The broader audit's session architecture, DB privileges and release-approval findings remain outside this phase.

## Validation actually executed

Backend, using explicit Go 1.26.10:

- `go mod tidy`, `go mod verify`, `go build ./...`, `go vet ./...`.
- `go test -count=1 -p 1 -cover -json ./...` with isolated PostgreSQL 17: 191 passing tests/subtests, no test skips or failures.
- `go test -race -count=1 -p 1 ./...` with the same isolated database.
- Repeated the complete test and race commands against a separate PostgreSQL 17 instance configured for SCRAM password authentication, using a synthetic Unicode password: 191 passing tests/subtests, no test skips or failures. This specifically exercises the changed pgx/x/text authentication path.
- `GOOS=linux GOARCH=amd64 govulncheck -json ./...` and `govulncheck -mode=binary -json` on the actual production-image executable.

Frontend:

- Frontend quality and browser validation use Node 24.22.1 and pnpm 10.34.8; prior successful 10.17 runs were repeated after the installer update.
- `pnpm install --frozen-lockfile`, `pnpm test`: 15 unit/architecture tests.
- `pnpm test:e2e:check`, `pnpm build` (includes application TypeScript), `pnpm lint`, `pnpm format:check`.
- `pnpm test:e2e`: all 10 desktop/mobile browser tests, including registration/login/logout, protected redirects, concurrent successful refresh, rejected refresh and late stale-token retry.
- `pnpm audit --json` on the resolved lockfile; an independent fresh audit agreed.

Infrastructure/images:

- Development frontend image build and loopback HTTP startup smoke also passed, exercising its Alpine shell/native-package installation.
- Both production Docker builds and local Trivy image scans; architecture, non-root users and embedded compiler version verified.
- Production-image smoke checks with read-only roots, bounded tmpfs, all capabilities dropped and no-new-privileges: health checks, backend SCRAM connection/startup migrations, registration/refresh/authenticated read/logout, Nginx `/accounts` deep link and seven referenced assets.
- `python3 -m unittest discover -s deploy/tests -v`: 35 passed.
- `python3 -m unittest discover -s scripts/ci/tests -v`: 16 passed.
- `go test -count=1 ./deploy/tests/ssm_regex_test.go`, `bash scripts/check-deployment-config.sh` (Compose and Caddy), `bash scripts/check-docker-context.sh`.
- `actionlint -shellcheck=` on active workflows and the inactive production template; `git diff --check`.
- Gitleaks with full redaction on image configurations/every regular-file layer and backend binary strings: no findings. Worktree scan reports only the already-reviewed empty example-config false positive in `deploy/production.env.example:14`. No secret values were printed or included here.

## Independent agents

All five requested roles actually ran:

1. **A — Backend:** selected the supported Go patch and compatible minimal module fixes, verified the graph and no imported OpenPGP path.
2. **B — Frontend:** resolved direct/transitive advisories through supported ranges, verified frozen installation and frontend quality checks; identified deterministic generated-route reordering.
3. **C — Containers:** reproduced baseline scans and discovered that updating Nginx's tag alone leaves vulnerable OS packages; verified official fixed packages and digest pins.
4. **D — Independent security:** challenged lockfile/supply-chain integrity and runtime claims; found bundled npm advisories, then caught the complete-builder pnpm/tar vulnerability that the base-only scan missed. Verified practical same-major npm and pnpm updates, including official tarball integrity and bundled-package advisory metadata. No confirmed new runtime security regression.
5. **E — Independent reliability:** verified route/CI-selection semantics; identified SCRAM and production-static-serving coverage gaps. Both were addressed with additional isolated validation.

Agents edited disjoint files. D and E reviewed read-only and returned Ready with caveats; their residual findings are retained above rather than suppressed.

## Deployment and rollback

This task stops at a draft PR against `staging`. Review required GitHub checks before any later merge. No AWS, IAM/OIDC/SSM, repository settings, branch protections, credentials or production databases were modified. No GHCR publication, release activation or PR merge occurred.

A later approved release should use the existing immutable-digest path and retain known-good image digests. No schema changes are introduced, but existing startup migrations/seeding still run; normal backups and migration review remain necessary. Reverting images does not reverse database changes. Runtime secret files and Compose projects remain unchanged.

## Files changed

`.gitignore`; `README.md`; `.github/workflows/ci.yml` (Node/pnpm installer versions only); `backend/Dockerfile`; `backend/Dockerfile.prod`; `backend/go.mod`; `backend/go.sum`; `frontend/Dockerfile`; `frontend/Dockerfile.prod`; `frontend/package.json`; `frontend/pnpm-lock.yaml`; generated `frontend/src/routeTree.gen.ts`; this report.

`AGENTS.md` remains local, untracked and ignored. Generated test bytecode and diagnostics are excluded from the commit. Detailed scanner JSON, image inventories and command logs are retained as separate review evidence, without embedding binaries or secret configuration in Git.
