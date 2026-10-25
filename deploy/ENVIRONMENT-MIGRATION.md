# Move EC2 environment files out of the Git checkout

These are proposed operator commands, not actions performed by this change.
Keep CD disabled and pause manual deployments, backups, secret edits and Git
updates for this maintenance. No container restart, Compose up/down, SQL change,
volume operation, AWS change or credential rotation is needed to move files.

The checkout remains owned/writable by ubuntu. The controller now reads only
`/etc/vellum-cd/staging.env` and `/etc/vellum-cd/production.env`, with no override
or checkout fallback. It checks every ancestor with `lstat` before each Compose
invocation: directory, root owner, no group/world write, no symlink. The final
file must be root-owned, regular, mode 600 (400 also accepted), and singly linked.
Do not symlink the old checkout paths to these secrets.

Root/sudo operators and Docker-group members are root-equivalent. These checks
protect against unprivileged filesystem replacement, not a malicious root
operator. Root must not edit/rotate secrets during an active deployment. The old
ubuntu-owned files and their parent directories are initially trusted input;
copying them cannot retroactively prove that their contents were never changed.
Have the operator verify the expected configuration privately before migration.

## Copy the existing bytes, without replacing existing destinations

Review the old paths with `namei -l` and confirm neither source nor its parents
is a symlink. Record existing Compose project names, running container IDs and
PostgreSQL volume identities privately. Do not change DB_NAME, DB_USER,
DB_PASSWORD, JWT values, IMAGE settings, APP_ORIGIN, proxy CIDR or limiter values.

The following root command verifies the destination directory chain, snapshots
both source files, creates private files atomically without overwriting, and
compares bytes silently. It deliberately refuses existing target files, including
symlinks: stop and inspect them rather than overwriting a possible newer config.

```sh
sudo /usr/bin/python3 -I - <<'PY'
import os
from pathlib import Path
import pwd
import stat
import tempfile

destination = Path('/etc/vellum-cd')
source_dir = Path('/opt/hosting/projects/vellum/deploy')
ubuntu_uid = pwd.getpwnam('ubuntu').pw_uid

def require(condition, message):
    if not condition:
        raise SystemExit(message)

for directory in (Path('/'), Path('/etc')):
    info = directory.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
            'Unsafe destination ancestor; stop')
try:
    destination.mkdir(mode=0o700)
except FileExistsError:
    pass
info = destination.lstat()
require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
        'Unsafe /etc/vellum-cd; stop')
os.chmod(destination, 0o700)

snapshots = {}
for environment in ('staging', 'production'):
    target = destination / (environment + '.env')
    require(not os.path.lexists(target), 'Destination already exists; stop')
    source = source_dir / (environment + '.env')
    for directory in reversed(source.parents):
        require(stat.S_ISDIR(directory.lstat().st_mode), 'Symlink/non-directory source parent; stop')
    fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid in (0, ubuntu_uid)
                and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
                'Unsafe source file; stop')
        snapshots[target] = stream.read()

for target, data in snapshots.items():
    fd, temporary = tempfile.mkstemp(prefix='.env-migration-', dir=destination)
    try:
        with os.fdopen(fd, 'wb') as stream:
            os.fchown(stream.fileno(), 0, 0)
            os.fchmod(stream.fileno(), 0o600)
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, target)  # Atomic, fails if the destination exists.
    finally:
        os.unlink(temporary)
    directory_fd = os.open(destination, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory_fd)
    finally:
        os.close(directory_fd)
    require(target.read_bytes() == data, 'Copy verification failed; stop')
print('Both managed files copied and byte-verified; no services changed')
PY
```

If interrupted between files, one target may already exist. Keep CD off and
inspect/compare both copies privately; do not blindly rerun or delete the managed
copy. Originals remain available throughout this step. Never paste file contents,
resolved Compose output or secrets into chat, shell tracing or CI logs.

## Validate Compose parity without deploying

With both copies still present, compare resolved configurations in memory using
the same installed Compose file, explicit project names and a clean environment.
Only the success message is printed. This checks that relocating the env file
does not change settings, image tags, database credentials, networks or volumes.
The installed Compose file must be the reviewed version already used on EC2.

```sh
sudo /usr/bin/python3 -I - <<'PY'
import json
from pathlib import Path
import subprocess

base = Path('/usr/local/lib/vellum-cd')
clean_env = {'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
             'HOME': '/root', 'DOCKER_CONFIG': '/etc/vellum-cd/docker'}
for environment in ('staging', 'production'):
    models = []
    for root in (Path('/opt/hosting/projects/vellum/deploy'), Path('/etc/vellum-cd')):
        result = subprocess.run(
            ['docker', 'compose', '--project-name', 'vellum-' + environment,
             '--env-file', str(root / (environment + '.env')),
             '-f', str(base / 'compose.yml'), 'config', '--format', 'json'],
            cwd=base, env=clean_env, capture_output=True, text=True)
        if result.returncode:
            raise SystemExit('Compose validation failed; inspect privately')
        models.append(json.loads(result.stdout))
    if models[0] != models[1] or models[1]['name'] != 'vellum-' + environment:
        raise SystemExit('Configuration mismatch; stop and inspect privately')
print('Both Compose models match; no services changed')
PY
```

Absolute paths avoid depending on the working directory. Keep the same explicit
project names; do not add `--project-directory /etc/vellum-cd`. If a future env
value is itself a relative path, inspect the resolved parity before accepting
the move. File-copy/config parity does not prove existing credentials match the
live database; confirm the previously running configuration was correct.

## Controller installation, manual operations and cleanup

Install the reviewed controller bundle through the existing root-owned procedure
in CI-CD.md, then compare all six bundle hashes against the exact reviewed
release revision. This change affects the manifest bundle hash. Production stays
disabled and the CD flag stays false until activation is separately approved.

Manual Compose operations must now run through authorized sudo/root with a clean
environment and the new absolute env path. For example, validation only:

```sh
sudo env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root \
  docker compose --project-name vellum-staging \
  --env-file /etc/vellum-cd/staging.env \
  -f /usr/local/lib/vellum-cd/compose.yml config --quiet
```

Use the corresponding fixed production path/project for authorized production
operations. Local SHA builds can still use the reviewed checkout's Compose file
and source contexts, but sudo must never run unreviewed code from that writable
checkout. IMAGE_TAG changes must now be made privately by root in the managed
file. Runtime credentials and project names must remain unchanged. The installed
digest controller remains the preferred update/recovery path, with its host lock
and production approvals; manual Compose must not overlap a controller run.

**Backup compatibility:** use the root-installed daily S3 implementation and
[paired host-coordination installation](HOST-COORDINATION.md), including
`cd/coordination.py`. Both environments' backups/deployments/guarded schema work
share the host gate and keep the old environment locks. Do not resume older
checkout jobs or active mixed code versions: old code ignores the pending record.
Existing dumps, secrets and volumes remain untouched. Raw manual Compose/SQL
also requires the maintenance gate, not just the clean environment shown above.

**Rollback compatibility:** retain old images and reviewed bundle revisions.
Old receipts require their original bundle hashes, and older controllers expect
checkout env paths. Do not recreate checkout secrets, add symlink fallbacks or
rewrite manifest hashes to replay them. Before deployment, prepare an explicitly
reviewed recovery procedure using the previous known-good digests, new `/etc`
paths, trusted installed Compose files, the host lock and schema/backup approval.
Target only backend/frontend with `--no-deps --no-build --pull never`; image
rollback cannot undo AutoMigrate or database writes. A historical receipt is not
automatically compatible with the new controller bundle.

After both configurations and all operational callers are verified, remove only
the two superseded checkout files (not directories, dumps, images or volumes):

```sh
sudo rm -- /opt/hosting/projects/vellum/deploy/staging.env \
  /opt/hosting/projects/vellum/deploy/production.env
```

Keep the managed copies backed up securely. Do not remove originals before the
copy/parity checks and operational updates. There is no need to chown the Git
checkout, regenerate credentials, reload Caddy or restart any service.
