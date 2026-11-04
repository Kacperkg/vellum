#!/usr/bin/env bash
set -euo pipefail

# Hosted Noble uses a local mirror list and may keep a direct security source.
# Validate the whole file before changing either transport. Run as the runner
# user, never with sudo.
updated_sources=$(mktemp)
trap 'rm -f "$updated_sources"' EXIT
python3 - /etc/os-release /etc/apt/sources.list.d/ubuntu.sources >"$updated_sources" <<'PY'
from pathlib import Path
import re
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
uri_line = None
source = Path(sys.argv[2]).read_bytes()
# Use the runner's ASCII/LF format so Python cannot reinterpret non-APT
# whitespace as an empty line or silently turn a malformed Enabled into "no".
if any(byte not in (9, 10) and not 32 <= byte <= 126 for byte in source):
    raise SystemExit('Unsupported Ubuntu DEB822 source encoding.')
lines = source.decode('ascii').splitlines(keepends=True)
for index, line in enumerate([*lines, '']):
    if line in ('', '\n'):
        if fields:
            stanzas.append((fields, uri_line))
        fields, key, uri_line = {}, None, None
    elif line.startswith('#'):
        continue
    elif line[0] in ' \t':
        if not key or key == 'enabled' or not line.strip():
            raise SystemExit('Unsupported Ubuntu DEB822 source continuation.')
        fields[key] += ' ' + line.strip()
    else:
        key, separator, value = line.partition(':')
        key = key.lower()
        if not separator or not re.fullmatch(r'[a-z][a-z0-9-]*', key) or key in fields:
            raise SystemExit('Unsupported Ubuntu DEB822 source layout.')
        fields[key] = value.strip()
        if key == 'uris':
            uri_line = index

suites = set()
updated = False
allowed_fields = {'types', 'uris', 'suites', 'components', 'signed-by', 'architectures', 'enabled'}
for fields, uri_line in stanzas:
    if fields.get('enabled', 'yes') == 'no':
        continue
    uris = fields.get('uris', '').split()
    stanza_suites = set(fields.get('suites', '').split())
    direct_security = uris in (
        ['http://security.ubuntu.com/ubuntu'], ['http://security.ubuntu.com/ubuntu/'],
        ['https://security.ubuntu.com/ubuntu'], ['https://security.ubuntu.com/ubuntu/'],
    ) and stanza_suites == {'noble-security'}
    if (not set(fields) <= allowed_fields
            or fields.get('enabled', 'yes') != 'yes'
            or (uris != ['mirror+file:/etc/apt/apt-mirrors.txt'] and not direct_security)
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
    suites.update(stanza_suites)
    if direct_security and uris[0].startswith('http:'):
        # Change only the literal URI on its own field line. A wrapped URI
        # is outside the observed layout and must not silently retain HTTP.
        if lines[uri_line].partition(':')[2].strip() != uris[0]:
            raise SystemExit('Unsupported wrapped Ubuntu security URI; no changes made.')
        lines[uri_line] = lines[uri_line].replace('http:', 'https:', 1)
        updated = True
if suites != {'noble', 'noble-updates', 'noble-backports', 'noble-security'}:
    raise SystemExit('Expected complete Noble suites including security; no changes made.')
if updated:
    sys.stdout.buffer.write(''.join(lines).encode())
PY

if [[ -s "$updated_sources" ]]; then
  sudo tee /etc/apt/sources.list.d/ubuntu.sources <"$updated_sources" >/dev/null
fi

# APT configuration uses fixed literals only (config can contain root-executed
# hooks). Mirror-list sources get per-file HTTPS failover.
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
print("Using HTTPS Ubuntu archives with mirror-list failover, one retry, 15s inactivity timeouts and strict index updates.")
'
