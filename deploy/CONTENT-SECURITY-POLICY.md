# Browser Content Security Policy

Issue #23 introduces a restrictive candidate policy in the **report-only** HTTP
header in `frontend/nginx.prod.conf`. It is deliberately not live enforcement:
the repository cannot establish the headers or configuration of the shared live
Caddy proxy. Local browser tests exercise report-only first and then the identical
policy in enforcing mode. Promotion requires the separately authorised staging
verification below. Report-only observes injection attempts; it does not stop them.

## Resource inventory and decisions

| Resource | Current production requirement | Policy |
| --- | --- | --- |
| Scripts | Vite external module entry, module preload and TanStack lazy route chunks | `script-src 'self'`; inline handlers forbidden |
| Styles | Vite extracted CSS; React assigns individual CSSOM properties for chart heights, donut gradients, category colours and progress widths | `style-src 'self'`; `style-src-attr` permits one fixed WebKit native-control hash; no inline stylesheet exception |
| Fonts | System font stack; Inter is a local font preference, not a downloaded webfont | `font-src 'none'` |
| Images | Same-origin hero, favicon and SVG icon sprite; overview chart is inline SVG | `img-src 'self'`; no remote, data or blob allowance |
| Connections | Production Docker build fixes `VITE_API_BASE_URL=/`; Caddy routes authentication and `/api` to Go | `connect-src 'self'` |
| Other resources | No plugins, embedded frames, workers, web manifest or media | `default-src`, `object-src`, `frame-src`, `worker-src`, `manifest-src`, `media-src` are `'none'` |
| Navigation | SPA links/history; same-origin form actions; no base tag or embedding | `base-uri 'none'; form-action 'self'; frame-ancestors 'none'` |

No `unsafe-inline`, `unsafe-eval`, nonce, wildcard origin, external font/CDN,
or reporting endpoint is necessary. Client-side TanStack routing does not use SSR
bootstrap scripts.

WebKit 27.2 checks a native `<select>` shadow-tree attribute against the document
policy ([upstream regression](https://github.com/microsoft/playwright/issues/43416),
[reproduction](https://github.com/microsoft/playwright/issues/43497)). The strict
candidate failed six WebKit report-only workflows and a focused enforced budget
workflow. A minimal browser reproduction confirmed the exact internal string
`text-overflow:inherit`. `style-src-attr 'unsafe-hashes'
'sha256-3oFxofzq3N//6J8H86e9ul4WT2OkJrtonB66gn/TLJw='` permits only this inert
attribute value, on any element. Other attribute values and inline style blocks
remain blocked in enforcing mode. No style violations are filtered from the audit.
Remove this compatibility hash after the supported WebKit/Safari versions fix the
regression and native-select plus full browser tests pass without it.

Vite development/HMR has different requirements and is not served
with this production policy. An alternate production API origin, new asset source,
SSR, workers or styling library needs a fresh inventory and regression run.

Nginx is the single policy owner. Its pinned 1.30.6 runtime supports
`add_header_inherit merge`, preserving the server policy through the index and
asset locations' cache/privacy headers; `always` covers response errors too.
Caddy passes the upstream header unchanged. Do not add another CSP at Caddy or in
HTML: multiple enforced policies intersect, and report-only cannot be a meta tag.
Backend JSON/auth responses and Caddy-generated proxy failures (for example 502)
are outside the Nginx policy's scope. This policy protects the frontend document.
The existing Caddy recovery headers also protect recovery POST/OPTIONS responses.

Recovery entry documents retain `Cache-Control: no-store` and
`Referrer-Policy: no-referrer`; Caddy retains its recovery `nosniff` header. The
HTML referrer meta and early reset-token query/fragment removal remain intact.

## Reproducible regression checks

From `frontend`, with Go, Node/pnpm and Docker available:

```sh
pnpm install --frozen-lockfile
pnpm exec playwright install --with-deps chromium firefox webkit
pnpm test
pnpm test:e2e:check
pnpm build
pnpm lint
pnpm format:check
pnpm test:e2e:csp
```

`test:e2e:csp` runs report-only before enforcement; a failed first phase prevents
the second. `test:e2e` runs only the shipped report-only mode. The runner builds
the SPA with same-origin API configuration and serves it with the exact pinned
Nginx base runtime/config and disposable Caddy site blocks. A Linux Go binary,
synthetic API and PostgreSQL run on a unique disposable Docker bridge; only test
ports 15173/18080 and the random direct-Nginx port are published to loopback.
No host-network opt-in, live application, developer dotenv, AWS credentials or SES
email delivery is used. The API runs as an unprivileged user with a read-only root.
The fixture changes only local hostnames/upstreams/listen ports and disables
Caddy's test admin listener. It does not modify the real proxy. Temporary containers
and their network are removed by the runner; it never removes existing volumes.

CI runs desktop Chromium/Firefox/WebKit plus Chromium mobile and iPhone WebKit
authentication, onboarding, refresh, navigation, forms, recovery, chart and CSP checks. Existing real-backend workflows cover registration,
mandatory account onboarding, login/refresh/logout, finance forms/dialogs, budgets,
goals, subscriptions, CSV, settings, failure/retry states and reloads. Additional
tests check computed chart/progress styling, lazy navigation/back/forward/reload,
headers directly and through both Caddy site blocks, immutable assets/304,
missing assets/404 and unsupported methods/405. Recovery browser tests stub reset
redemption with synthetic tokens; they do not test SES delivery or real mail links.

Every browser flow fails on unexpected CSP violations. Intentional injection tests
verify that report-only reports and allows synthetic inline code, while enforcement
blocks it and an inline event handler, and rejects a foreign connection. Further
probes cover eval from an allowed external script and unrelated inline styles. Test
observers keep only directive/disposition, never full violation objects or URLs.
WebKit emits no eval violation event in report-only mode; the test still verifies
execution there, and requires both a violation and blocked execution in enforcement
on every engine. Inline script/handler/style/connection report assertions remain
strict in both modes.

## Sanitised local validation record — 27 October 2026

Base: staging `cd400c698ffa287e1f41a01ff81ce7190d5055e5`. Host: macOS arm64;
Node 25.2.1, pnpm 10.34.8, Go 1.26.4, Docker 29.0.1. Playwright 1.64.1 used
Chromium 156.0.8078.4 (revision 1248), Firefox 157.0 (1555), WebKit 27.2 (2370).
The fixture used the production-pinned Nginx 1.30.6 runtime and local Caddy 2.11.7.

| Check actually executed | Result |
| --- | --- |
| `pnpm test:e2e:csp`, report-only | 96 passed, no skipped tests, 52.1 seconds |
| Same command, enforcing candidate | 96 passed, no skipped tests, 53.1 seconds |
| `pnpm test` | 18 passed |
| `pnpm test:e2e:check`, `pnpm build`, `pnpm lint`, `pnpm format:check` | Passed |
| `python3 -m unittest discover -s deploy/tests -v` | 97 passed, including controller/security/release provenance tests |
| `python3 -m unittest discover -s scripts/ci/tests -v` | 28 passed |
| `bash scripts/check-docker-context.sh` | Both component contexts passed secret exclusions |
| `bash scripts/check-deployment-config.sh` | Compose models, environment relocation, Caddy configuration and both-site recovery routing passed |
| `go test -count=1 ./deploy/tests/ssm_regex_test.go` | Passed |
| actionlint 1.7.7, active and disabled workflow files | Passed |
| Backend `go build ./...`, `go vet ./...` | Passed |
| `git diff --check`; ignored/untracked `AGENTS.md`; disposable stack cleanup | Passed |

The browser runs verified one matching CSP header directly from Nginx and through
both fixture Caddy hosts on entry/deep/recovery routes, assets/304 and errors/404/405.
Recovery documents retained only `no-store`/`no-referrer` values (identical values
can appear twice because both layers set them). Normal flows emitted no policy
violations; intentional probes reported in report-only and were blocked in
enforcement, with the WebKit eval-event difference described above. Chart gradients,
nonzero bar heights and 50% budget progress rendered in every desktop/mobile project.

Independent frontend-security, browser-compatibility and deployment reviewers
approved the final changes. They found and helped resolve the WebKit native-select
regression, missing mobile acceptance coverage, promotion testability and paired
rollback documentation. This record contains no account data, tokens or raw reports.

Not performed: live HTTPS/header audit, physical-device Safari checks, SES delivery,
production-image build, or full backend unit/integration/race rerun. Backend source
is unchanged; browser workflows used real isolated PostgreSQL and the current API.
Linux CI and production image checks run on the draft PR. No live AWS, GitHub
settings, Caddy, databases or deployments were modified.

## Separately authorised staging and production rollout

1. Review this frontend/configuration change and CI results. Merging to `staging`
   triggers the existing release workflow; merge and deployment need separate
   approval and are not part of this implementation task.
2. Release the report-only frontend through the existing digest-verified controller.
   Preserve IAM/OIDC/SSM restrictions, deployment locks, root-owned `/etc/vellum-cd/`
   files, host coordination and database volumes. No live Caddy edit is needed if
   the existing proxy already passes upstream headers and has the reviewed recovery
   routing. Inspect any difference before proposing a separately approved edit.
3. With synthetic accounts, verify the public HTTPS staging origin on supported
   desktop/mobile browsers. Check one report-only header, no conflicting CSP/meta
   policy, same-origin API, no violations on login, refresh, logout, onboarding,
   recovery, finance dialogs/charts, CSV and router navigation/reload. Check index,
   deep links, recovery privacy headers, cached assets and missing assets. Confirm
   auth POST/OPTIONS still reach Go. Record browser versions, release digest,
   status/header/directive results and pass/fail counts only.
4. Review the sanitised evidence, then change only the header name to
   `Content-Security-Policy` in a reviewed follow-up. Rerun browser checks for the
   candidate and obtain separate staging deployment approval. Verify injection is
   blocked and legitimate flows have no violations. Keep the same directive values.
5. Production promotion remains separately controlled. Approve the exact reviewed
   digest only after enforcing staging validation. Repeat sanitised HTTPS header
   and synthetic-flow checks after the authorised production rollout.

Local test evidence is not a live proxy audit, TLS verification or production
deployment verification. Chromium mobile is emulation; iPhone WebKit is the
Playwright engine, not a physical-device Safari certification.

## Monitoring, privacy and rollback

There is deliberately no `report-uri`, `report-to`, `Reporting-Endpoints` or
`report-sample`. Raw browser violation payloads can include document URLs, source
URLs, tokens, script snippets or financial data. Inspect the console locally with
synthetic users; record only aggregate directive/disposition counts and browser
versions. Do not upload console dumps, HARs or screenshots from real accounts.
Automated traces/screenshots/videos are limited to the synthetic isolated stack.
A future collector requires a separate privacy/security review, ingress sanitisation,
size/rate limits, retention rules and recovery-route exclusion before activation.

Before rollout, retain the current known-good frontend digest/configuration and
release receipts, and review the controller's compatible recovery path. If the
candidate causes breakage, stop promotion. For an enforcing candidate, prepare a
reviewed frontend roll-forward restoring the same policy to report-only, rerun the
tests and deploy only with explicit approval. If report-only itself causes a header
or serving failure, prepare the same kind of reviewed roll-forward release with
the reverted frontend configuration. The controller deploys paired images and
requires both revision labels to match the manifest source SHA: an old frontend
digest cannot be substituted beside the current backend. Rebuild the approved
backend code and corrected frontend together under one fresh reviewed source SHA.
An older paired receipt is usable only after separate review of both image versions,
authentication/session revocation, schema and controller compatibility. Never rewrite
revision labels or manifest hashes to bypass this constraint.
Do not restore an older paired backend merely to undo CSP: older authentication
code can invalidate the session-revocation guarantees introduced with password
recovery. Do not bypass digest checks, locks, writer barriers or promotion controls.

This change has no schema migration. It needs no database restore, credential
change, volume deletion or live Caddy/AWS change. Validate public headers, recovery
privacy, login/refresh/logout and finance navigation again after any approved
rollback; record only sanitised results.

Remaining boundaries: report-only does not block injection until promotion;
same-origin script allowlisting trusts same-origin JavaScript assets; CSP does not
replace escaping, server authorisation or the browser-storage work in Issue #22.

References: [CSP specification](https://www.w3.org/TR/CSP/) and
[Nginx header inheritance](https://nginx.org/en/docs/http/ngx_http_headers_module.html).
