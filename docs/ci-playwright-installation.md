# Playwright installation on CI

The browser job runs
`pnpm exec playwright install --with-deps chromium firefox webkit`, then requires
a clean `dpkg --audit`. The local CLI from `pnpm install --frozen-lockfile`
installs the exact browser revisions required by the locked Playwright package
for the desktop and mobile CSP projects, in both report-only and enforcing modes.

Browser binaries use a CSP-specific Ubuntu/architecture/lockfile cache key. The
old Chromium-only key remains an exact restore fallback, so Chromium can be
reused while the complete browser set is saved under the new key. Installation
runs on every selected browser job, including cache hits, to fill any missing
browsers and install all required operating-system dependencies. Other jobs do
not install browsers.

## Why the mirror preference exists

The October 27, 2026 CI audit examined staging revision
`cd400c698ffa287e1f41a01ff81ce7190d5055e5`. Installer outliers took 435, 641 and
681 seconds. Logs showed Ubuntu package downloads taking 7–11 minutes; both
recent staging outliers had already restored the browser cache successfully.
Normal sampled installer durations were approximately 11–34 seconds.

GitHub's Ubuntu runner configures `ubuntu.sources` to use
`mirror+file:/etc/apt/apt-mirrors.txt`, initially preferring Azure HTTP, then
Ubuntu archive/security HTTPS endpoints. See the upstream
[mirror configuration](https://github.com/actions/runner-images/blob/main/images/ubuntu/scripts/build/configure-apt-sources.sh).

`scripts/ci/prepare-playwright-apt.sh` validates the expected hosted Ubuntu 24.04
layout, then sets the existing mirror list to:

1. `https://archive.ubuntu.com/ubuntu/`
2. `https://security.ubuntu.com/ubuntu/`

APT performs per-file fallback using explicit priorities. The helper leaves
source stanzas, Noble/security suites, components, keyring and third-party
repositories unchanged. It adds no trust, signature, TLS or freshness bypasses.
Fixed literal privileged writes avoid executing repository Python as root or
installing generated APT configuration that could carry root-executed hooks.

The late-loading configuration sets one acquisition retry, HTTP/HTTPS inactivity
timeouts of 15 seconds, and strict index-update error handling. Effective values
are checked before continuing. These are **not total installation deadlines**;
a slow but continuously transferring server can exceed them. No hard timeout or
kill wraps APT/dpkg. If installation, audit, configuration or validation fails,
the browser check fails. The helper intentionally rejects future unsupported
runner layouts rather than silently omitting the preference.

Strict index errors also apply to third-party repositories: an unavailable
Microsoft index can fail this job. HTTPS archive performance still depends on
runner region and network conditions. Both mirrors share upstream infrastructure;
this is resilience against the observed Azure endpoint, not independent hosting.

## Validation and measurement

Run `python3 -m unittest discover -s scripts/ci/tests -v` for source preservation,
HTTPS fallback configuration, unsupported-layout, write/configuration failure,
installer failure and incomplete-package regression tests. They execute the real
shell helper against synthetic files with mocked privileged/system commands;
they do not modify local APT or prove network fallback behavior.

A pull request targeting staging runs all seven checks without deployment or
image publication. Compare the mirror-preparation and complete-installation step
durations, browser-cache hit/miss and total browser-job duration with the audit
baseline. A single fast run verifies compatibility, not sustained tail-latency
improvement. Keep measured timings separate from estimated savings; repeat on
fresh hosted runners before drawing percentile conclusions. Do not run the
staging release workflow to benchmark this change.
