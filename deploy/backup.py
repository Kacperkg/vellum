#!/usr/bin/env python3
"""Root-installed daily logical backups. No deploy, restore or AWS administration."""
import base64
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
import uuid

BASE = Path('/usr/local/lib/vellum-cd')
CONFIG = Path('/etc/vellum-cd/backup.json')
STATE = Path('/var/lib/vellum-cd')
REGION = 'eu-west-2'
ENVIRONMENTS = ('staging', 'production')
MAX_DUMP = 512 * 1024**2
MAX_SPOOL = 1024**3
RESERVE = 5 * 1024**3
MAX_AGE = 26 * 3600
# No caller-controlled Docker contexts, Compose interpolation or AWS endpoints.
CLEAN_ENV = {
    'PATH': '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
    'HOME': '/root', 'DOCKER_CONFIG': '/etc/vellum-cd/docker',
    'DOCKER_HOST': 'unix:///var/run/docker.sock',
    'AWS_CONFIG_FILE': '/dev/null', 'AWS_SHARED_CREDENTIALS_FILE': '/dev/null',
    'AWS_DEFAULT_REGION': REGION, 'AWS_REGION': REGION,
    'AWS_PAGER': '', 'AWS_CLI_AUTO_PROMPT': 'off', 'AWS_MAX_ATTEMPTS': '1',
    'AWS_METADATA_SERVICE_TIMEOUT': '2', 'AWS_METADATA_SERVICE_NUM_ATTEMPTS': '2',
}


class BackupError(Exception):
    """Messages are deliberately safe for journald/SNS (no subprocess output)."""


def require(condition, message):
    if not condition:
        raise BackupError(message)


def trusted(path, directory=False, private=False):
    """lstat every component; trusted parents prevent unprivileged replacement."""
    path = Path(path)
    require(path.is_absolute() and '..' not in path.parts, 'unsafe path')
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0
                and not info.st_mode & 0o022, 'unsafe path parent')
    info = path.lstat()
    require(info.st_uid == 0, 'unsafe path owner')
    if directory:
        require(stat.S_ISDIR(info.st_mode), 'unsafe directory type')
        require(not info.st_mode & (0o077 if private else 0o022), 'unsafe directory mode')
    else:
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, 'unsafe file type')
        if private:
            require(stat.S_IMODE(info.st_mode) in (0o400, 0o600), 'unsafe private file mode')
        else:
            require(not info.st_mode & 0o022, 'unsafe file mode')


def load_config():
    trusted(CONFIG, private=True)
    require(CONFIG.stat().st_size <= 4096, 'oversized backup config')
    config = json.loads(CONFIG.read_text())
    require(isinstance(config, dict) and set(config) == {'bucket', 'account_id', 'alert_topic_arn'},
            'invalid backup config fields')
    bucket = config['bucket']
    require(isinstance(bucket, str) and re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]', bucket)
            and '..' not in bucket and not re.fullmatch(r'[0-9.]+', bucket), 'invalid bucket')
    require(isinstance(config['account_id'], str)
            and re.fullmatch(r'\d{12}', config['account_id']), 'invalid account')
    topic = config['alert_topic_arn']
    require(topic is None or (isinstance(topic, str) and re.fullmatch(
        rf'arn:aws:sns:{REGION}:{config["account_id"]}:[A-Za-z0-9_-]{{1,256}}', topic)),
        'invalid alert topic')
    return config


def run(args, label, timeout=60):
    try:
        result = subprocess.run(args, env=CLEAN_ENV, cwd=BASE, stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                timeout=timeout, check=False)
    except (OSError, subprocess.TimeoutExpired):
        raise BackupError(label + ' failed or timed out') from None
    require(result.returncode == 0, label + ' failed')
    return result.stdout


def aws(config, args, label):
    # Region/endpoint/profile cannot be supplied by a caller or inherited env.
    command = ['aws', '--region', REGION, '--no-cli-pager', '--cli-connect-timeout', '5',
               '--cli-read-timeout', '45', *args]
    for attempt in range(3):
        try:
            return json.loads(run(command, label, timeout=60) or b'{}')
        except BackupError:
            if attempt == 2:
                raise
            time.sleep(2 ** attempt)


def sha256_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024**2), b''):
            digest.update(block)
    return digest.hexdigest()


def write_json(path, value):
    with path.open('x', encoding='utf-8') as output:
        json.dump(value, output, sort_keys=True, separators=(',', ':'))
        output.flush()
        os.fsync(output.fileno())


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


@contextmanager
def lock(path):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0
                and info.st_nlink == 1 and not info.st_mode & 0o022, 'unsafe lock file')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise BackupError('backup or deployment lock busy') from None
        yield
    finally:
        os.close(fd)


def spool_bytes(root):
    total = 0
    for path in root.rglob('*'):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode), 'unsafe spool symlink')
        require(info.st_uid == 0 and not info.st_mode & 0o077, 'unsafe spool permissions')
        if stat.S_ISREG(info.st_mode):
            require(info.st_nlink == 1, 'unsafe spool hardlink')
            total += info.st_size
        else:
            require(stat.S_ISDIR(info.st_mode), 'unsafe spool entry')
    return total


def check_space(root, new_dump=True):
    used = spool_bytes(root)
    require(used + (MAX_DUMP if new_dump else 0) <= MAX_SPOOL, 'backup spool cap reached')
    disk = shutil.disk_usage(root)
    reserve = max(RESERVE, disk.total // 5)
    require(disk.free >= reserve + (MAX_DUMP if new_dump else 0), 'insufficient disk reserve')


def dump(compose, destination, spool):
    # Alpine's in-container timeout bounds pg_dump even if docker exec is interrupted.
    args = [*compose, 'exec', '-T', '-e', 'PGOPTIONS=-c statement_timeout=600000',
            'postgres', 'sh', '-ec',
            'mkdir -m 700 "$1"; '
            'timeout -k 5 -s TERM 900 pg_dump --format=custom --compress=gzip:3 '
            '--no-owner --no-acl --lock-wait-timeout=30000 '
            '-U "$POSTGRES_USER" -d "$POSTGRES_DB" & child=$!; '
            'printf "%s" "$child" > "$1/pid"; '
            "trap 'rm -f \"$1/pid\"; rmdir \"$1\"' EXIT; "
            'wait "$child"', 'sh', '/tmp/vellum-backup-' + uuid.uuid4().hex]

    def limit_output():
        resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_DUMP, MAX_DUMP))

    with destination.open('xb') as output:
        process = subprocess.Popen(args, env=CLEAN_ENV, cwd=BASE, stdin=subprocess.DEVNULL,
                                   stdout=output, stderr=subprocess.DEVNULL,
                                   start_new_session=True, preexec_fn=limit_output)
        successful = False
        try:
            deadline = time.monotonic() + 930
            while process.poll() is None:
                require(time.monotonic() < deadline, 'database dump timed out')
                check_space(spool, new_dump=False)
                time.sleep(0.2)
            require(process.returncode == 0 and destination.stat().st_size > 0,
                    'database dump failed')
            output.flush()
            os.fsync(output.fileno())
            successful = True
        finally:
            if not successful:
                cancel_dump(compose, args[-1])
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait()


def cancel_dump(compose, job):
    try:
        run([*compose, 'exec', '-T', 'postgres', 'sh', '-ec',
             'test -f "$1/pid" || exit 1; '
             'pid=$(cat "$1/pid"); kill -TERM "$pid" 2>/dev/null || :; '
             'n=0; while test -d "$1"; do '
             'n=$((n+1)); test "$n" -le 10 || exit 1; sleep 1; done',
             'sh', job], 'dump cancellation', timeout=20)
    except BackupError:
        # Never release the deployment lock while the bounded inner dump may live.
        # A daemon outage prevents proof of termination. Wait past timeout+kill-after.
        print('Dump cancellation unconfirmed; retaining locks for timeout bound', file=sys.stderr)
        time.sleep(910)


def verify_archive(compose, archive):
    with archive.open('rb') as source:
        try:
            result = subprocess.run([*compose, 'exec', '-T', 'postgres', 'pg_restore', '--list'],
                                    env=CLEAN_ENV, cwd=BASE, stdin=source,
                                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                    timeout=60, check=False)
        except (OSError, subprocess.TimeoutExpired):
            raise BackupError('archive list check failed or timed out') from None
    require(result.returncode == 0, 'archive list check failed')


def verify_object(config, key, size, digest):
    head = aws(config, ['s3api', 'head-object', '--bucket', config['bucket'], '--key', key,
                       '--expected-bucket-owner', config['account_id'],
                       '--checksum-mode', 'ENABLED'], 'upload verification')
    require(head.get('ContentLength') == size and head.get('ServerSideEncryption') == 'AES256'
            and head.get('ChecksumSHA256') == base64.b64encode(bytes.fromhex(digest)).decode(),
            'uploaded object size/checksum/encryption mismatch')


def upload(config, key, path):
    size, digest = path.stat().st_size, sha256_file(path)
    # Single PUT only (max 512 MiB): bounded disk and simple whole-object SHA-256/IAM.
    try:
        aws(config, ['s3api', 'put-object', '--bucket', config['bucket'], '--key', key,
                 '--body', str(path), '--expected-bucket-owner', config['account_id'],
                 '--server-side-encryption', 'AES256', '--checksum-algorithm', 'SHA256',
                 '--checksum-sha256', base64.b64encode(bytes.fromhex(digest)).decode(),
                 '--if-none-match', '*'], 'object upload')
    except BackupError:
        # A lost response may follow a successful PUT; exact HEAD makes retry idempotent.
        verify_object(config, key, size, digest)
        return {'key': key, 'size': size, 'sha256': digest}
    verify_object(config, key, size, digest)
    return {'key': key, 'size': size, 'sha256': digest}


def now():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def backup(environment, config):
    trusted(BASE / 'compose.yml')
    env_file = Path('/etc/vellum-cd') / (environment + '.env')
    trusted(env_file, private=True)
    spool = STATE / 'backups'
    spool.mkdir(mode=0o700, exist_ok=True)
    trusted(spool, directory=True, private=True)
    # Global backup lock, then controller lock; all backups use this exact order.
    with lock(STATE / 'backup.lock'), lock(STATE / (environment + '.lock')):
        check_space(spool)
        work = Path(tempfile.mkdtemp(prefix=environment + '-', dir=spool))
        archive = work / 'database.dump'
        complete_dump = False
        try:
            compose = ['docker', 'compose', '--project-name', 'vellum-' + environment,
                       '--env-file', str(env_file), '-f', str(BASE / 'compose.yml')]
            run([*compose, 'config', '--quiet'], 'Compose validation')
            version = run([*compose, 'exec', '-T', 'postgres', 'sh', '-ec',
                           'exec psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB" '
                           '-c "SHOW server_version"'], 'PostgreSQL version').decode().strip()
            require(re.fullmatch(r'17(?:\.[0-9]+)?(?: [A-Za-z0-9 .()_-]+)?', version),
                    'expected PostgreSQL 17')
            started = now()
            dump(compose, archive, spool)
            verify_archive(compose, archive)
            complete_dump = True
            run_id = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ-') + uuid.uuid4().hex
            key = environment + '/runs/' + run_id
            manifest = {'format_version': 1, 'environment': environment,
                        'snapshot_started_at': started, 'dump_completed_at': now(),
                        'postgres_version': version, 'archive_size': archive.stat().st_size,
                        'archive_sha256': sha256_file(archive)}
            manifest_path = work / 'manifest.json'
            write_json(manifest_path, manifest)
            sync_directory(work)
            archive_ref = upload(config, key + '.dump', archive)
            manifest_ref = upload(config, key + '.json', manifest_path)
            marker = {'format_version': 1, 'environment': environment,
                      'snapshot_started_at': started, 'uploaded_at': now(),
                      'archive': archive_ref, 'manifest': manifest_ref}
            marker_path = work / 'complete.json'
            write_json(marker_path, marker)
            upload(config, environment + '/completed/' + run_id + '.json', marker_path)
            # Only this run is removed. Failed complete dumps are never auto-deleted.
            shutil.rmtree(work)
            sync_directory(spool)
        except BaseException:
            if not complete_dump:
                shutil.rmtree(work)
            raise
    print(environment + ': off-host backup verified; local temporary files removed')


def check_freshness(environment, config):
    listing = aws(config, ['s3api', 'list-objects-v2', '--bucket', config['bucket'],
                          '--expected-bucket-owner', config['account_id'],
                          '--prefix', environment + '/completed/', '--max-keys', '1000',
                          '--no-paginate'], 'backup freshness listing')
    require(not listing.get('IsTruncated'), 'too many completion objects; inspect retention')
    objects = listing.get('Contents', [])
    require(objects, 'no completed off-host backup')
    # Run names are fixed-width UTC timestamps followed by random IDs.
    latest = max(objects, key=lambda item: item['Key'])
    require(re.fullmatch(environment + r'/completed/\d{8}T\d{6}Z-[0-9a-f]{32}\.json',
                         latest['Key']), 'invalid completion object name')
    require(latest['Size'] <= 4096, 'oversized completion object')
    with tempfile.TemporaryDirectory(prefix='vellum-freshness-') as temporary:
        path = Path(temporary) / 'complete.json'
        aws(config, ['s3api', 'get-object', '--bucket', config['bucket'],
                     '--key', latest['Key'], '--expected-bucket-owner', config['account_id'],
                     str(path)], 'completion download')
        marker = json.loads(path.read_text())
    require(marker['environment'] == environment and marker['format_version'] == 1,
            'invalid completion marker')
    age = (datetime.now(timezone.utc) - datetime.fromisoformat(
        marker['snapshot_started_at'].replace('Z', '+00:00'))).total_seconds()
    require(0 <= age <= MAX_AGE, 'off-host backup is stale')
    run_key = environment + '/runs/' + Path(latest['Key']).stem
    for field, extension in (('archive', '.dump'), ('manifest', '.json')):
        reference = marker[field]
        require(reference['key'] == run_key + extension, 'completion key mismatch')
        verify_object(config, reference['key'], reference['size'], reference['sha256'])
    print(environment + ': completed off-host backup is fresh')


def notify(config, environment):
    if config and config['alert_topic_arn']:
        aws(config, ['sns', 'publish', '--topic-arn', config['alert_topic_arn'],
                     '--subject', 'Vellum backup failure: ' + environment,
                     '--message', 'Backup/freshness check failed for ' + environment
                     + '. Inspect the root systemd journal; off-host recovery may be stale.'],
            'failure notification')


def main():
    require(os.geteuid() == 0, 'run the installed backup entry point as root')
    require(Path(__file__).absolute() == BASE / 'backup.py', 'refuse checkout execution')
    require(len(sys.argv) in (2, 3) and sys.argv[1] in ENVIRONMENTS
            and (len(sys.argv) == 2 or sys.argv[2] == '--check-freshness'),
            'usage: backup.sh staging|production [--check-freshness]')
    os.umask(0o077)
    def interrupted(signum, frame):
        # A second stop request must not interrupt remote-dump cancellation.
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        raise BackupError('backup interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    config = None
    environment = sys.argv[1]
    try:
        trusted(BASE / 'backup.py')
        trusted(BASE / 'backup.sh')
        trusted(STATE, directory=True, private=True)
        config = load_config()
        if len(sys.argv) == 3:
            check_freshness(environment, config)
        else:
            backup(environment, config)
    except Exception:
        try:
            notify(config, environment)
        except Exception:
            print(environment + ': failure notification also failed', file=sys.stderr)
        raise


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        detail = str(error) if isinstance(error, BackupError) else type(error).__name__
        print('Vellum backup failed: ' + detail, file=sys.stderr)
        sys.exit(1)
