#!/usr/bin/env bash
set -euo pipefail

# Hosted ubuntu-24.04 uses a local mirror list. Validate that contract before
# changing it; leave source stanzas, signing keys and third-party repos intact.
# Run this script as the runner user, never with sudo.
python3 - /etc/os-release /etc/apt/sources.list.d/ubuntu.sources <<'PY'
from pathlib import Path
import sys
from urllib.parse import urlsplit

os_release = dict(
    line.split('=', 1) for line in Path(sys.argv[1]).read_text().splitlines()
    if line and not line.startswith('#') and '=' in line
)
if (os_release.get('ID', '').strip('"'), os_release.get('VERSION_ID', '').strip('"')) != ('ubuntu', '24.04'):
    raise SystemExit('Playwright APT preference requires Ubuntu 24.04.')

stanzas = []
fields = {}
key = None
for line in [*Path(sys.argv[2]).read_text().splitlines(), '']:
    if not line.strip():
        if fields:
            stanzas.append(fields)
        fields, key = {}, None
    elif line.lstrip().startswith('#'):
        continue
    elif line[0].isspace() and key:
        fields[key] += ' ' + line.strip()
    else:
        key, separator, value = line.partition(':')
        key = key.lower()
        if not separator or key in fields:
            raise SystemExit('Unsupported Ubuntu DEB822 source layout.')
        fields[key] = value.strip()

suites = set()
for fields in stanzas:
    if fields.get('enabled', 'yes') == 'no':
        continue
    if (fields.get('uris', '').split() != ['mirror+file:/etc/apt/apt-mirrors.txt']
            or fields.get('signed-by') != '/usr/share/keyrings/ubuntu-archive-keyring.gpg'
            or set(fields.get('components', '').split()) != {'main', 'restricted', 'universe', 'multiverse'}
            or not set(fields.get('types', '').split()) <= {'deb', 'deb-src'}
            or 'deb' not in fields.get('types', '').split()):
        # Report only public package-source metadata, never URI credentials or
        # queries, so runner-image changes can be diagnosed without guessing.
        locations = [(uri.scheme, uri.hostname, uri.path) for uri in
                     map(urlsplit, fields.get('uris', '').split())]
        raise SystemExit('Unsupported Ubuntu source trust/mirror configuration; no changes made. '
                         f'Locations={locations!r}; types={fields.get("types")!r}; '
                         f'components={fields.get("components")!r}; signed-by={fields.get("signed-by")!r}')
    suites.update(fields.get('suites', '').split())
if suites != {'noble', 'noble-updates', 'noble-backports', 'noble-security'}:
    raise SystemExit('Expected complete Noble suites including security; no changes made.')
print('Validated Noble mirror-list sources; retaining source stanzas and Ubuntu signing keyring.')
PY

# Fixed literals only: no repository-generated APT config (which can contain
# root-executed hooks). APT performs per-file failover to the second HTTPS mirror.
printf 'https://archive.ubuntu.com/ubuntu/\tpriority:1\nhttps://security.ubuntu.com/ubuntu/\tpriority:2\n' \
  | sudo tee /etc/apt/apt-mirrors.txt >/dev/null
# Load after the hosted image's zz-retries. These are inactivity timeouts, not a
# wall-clock kill of the installer: dpkg must always be allowed to finish.
printf '%s\n' 'Acquire::Retries "1";' 'Acquire::http::Timeout "15";' \
  'Acquire::https::Timeout "15";' 'APT::Update::Error-Mode "any";' \
  | sudo tee /etc/apt/apt.conf.d/zz-vellum-playwright >/dev/null

apt-config dump | python3 -c '
import sys
settings = set(sys.stdin.read().splitlines())
expected = {
    "Acquire::Retries \"1\";",
    "Acquire::http::Timeout \"15\";",
    "Acquire::https::Timeout \"15\";",
    "APT::Update::Error-Mode \"any\";",
}
if not expected <= settings:
    raise SystemExit("Playwright APT acquisition settings were overridden; refusing installation.")
print("Using HTTPS Ubuntu archives with native failover, one retry, 15s inactivity timeouts and strict index updates.")
'
