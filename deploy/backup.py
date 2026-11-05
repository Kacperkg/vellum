#!/usr/bin/env python3
"""Root-installed daily logical backups. No deploy, restore or AWS administration."""
import base64
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import select
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
SNAPSHOT_TIMEOUT = 960
MAX_METADATA = 4096
PG_VERSION = r'17(?:\.[0-9]+)?(?: [A-Za-z0-9 .()_-]+)?'
RUN_ID = r'\d{8}T\d{6}Z-[0-9a-f]{32}'
SOURCE_QUERY = b"""\
BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path TO pg_catalog;
SELECT json_build_array(
    pg_export_snapshot(),
    to_char(transaction_timestamp() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
    current_setting('server_version'),
    system_identifier::text,
    oid::bigint,
    current_database()
)
FROM pg_control_system(), pg_database
WHERE datname = current_database() AND NOT pg_is_in_recovery();
"""
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
    fields = {'bucket', 'account_id', 'alert_topic_arn'}
    require(isinstance(config, dict) and set(config) in (fields, fields | {'production_provenance'}),
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
    if 'production_provenance' in config:
        provenance = config['production_provenance']
        require(isinstance(provenance, dict)
                and set(provenance) == {'source', 'key_id', 'public_key', 'signing_key'},
                'invalid backup provenance config fields')
        validate_source(provenance['source'])
        key_id = provenance['key_id']
        require(isinstance(key_id, str) and re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,63}', key_id),
                'invalid backup provenance key identifier')
        validate_key_path(provenance['public_key'])
        if provenance['signing_key'] is not None:
            validate_key_path(provenance['signing_key'])
            require(provenance['signing_key'] != provenance['public_key'], 'backup key paths must differ')
    return config


def validate_key_path(value):
    require(isinstance(value, str) and re.fullmatch(r'/etc/vellum-cd/[A-Za-z0-9_-]+\.pem', value),
            'invalid backup provenance key path')


def validate_source(source):
    require(isinstance(source, dict)
            and set(source) == {'system_identifier', 'database_oid', 'database_name'},
            'invalid backup source fields')
    system_id, oid, name = (source[k] for k in ('system_identifier', 'database_oid', 'database_name'))
    require(isinstance(system_id, str) and re.fullmatch(r'[1-9][0-9]{0,19}', system_id)
            and int(system_id) < 2**64 and type(oid) is int and 0 < oid < 2**32
            and isinstance(name, str) and re.fullmatch(r'[a-z_][a-z0-9_]{0,62}', name)
            and name not in {'postgres', 'template0', 'template1'},
            'invalid backup source identity')


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
        output.write(canonical(value).decode('ascii'))
        output.flush()
        os.fsync(output.fileno())


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode('ascii')


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


@contextmanager
def host_operation(environment):
    trusted(BASE / 'cd', directory=True)
    trusted(BASE / 'cd' / 'coordination.py')
    # -I removed implicit local imports. Add only the explicitly trusted library.
    sys.path.insert(0, str(BASE / 'cd'))
    from coordination import CoordinationError, operation
    try:
        with operation(STATE, 'backup', environment):
            yield
    except CoordinationError as error:
        raise BackupError(str(error)) from None


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


def utc_time(value):
    require(isinstance(value, str) and re.fullmatch(
        r'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d{1,6})?Z', value),
        'invalid backup timestamp')
    try:
        return datetime.fromisoformat(value.replace('Z', '+00:00'))
    except ValueError:
        raise BackupError('invalid backup timestamp') from None


def snapshot_metadata(raw, expected_source):
    try:
        result = json.loads(raw)
    except (ValueError, UnicodeError):
        raise BackupError('invalid PostgreSQL snapshot response') from None
    require(isinstance(result, list) and len(result) == 6, 'invalid PostgreSQL snapshot response')
    snapshot_id, started, version, system_id, oid, name = result
    source = {'system_identifier': system_id, 'database_oid': oid, 'database_name': name}
    validate_source(source)
    require(source == expected_source, 'unexpected PostgreSQL source identity')
    require(isinstance(snapshot_id, str) and re.fullmatch(r'[0-9A-F]{8}-[0-9A-F]{8}-[0-9]+', snapshot_id),
            'invalid PostgreSQL snapshot identifier')
    require(isinstance(version, str) and re.fullmatch(PG_VERSION, version), 'expected PostgreSQL 17')
    require(0 <= (datetime.now(timezone.utc) - utc_time(started)).total_seconds() <= 30,
            'unexpected PostgreSQL snapshot clock')
    return {'source': source, 'snapshot_id': snapshot_id, 'snapshot_started_at': started,
            'postgres_version': version}


@contextmanager
def exported_snapshot(compose, expected_source):
    # Use the protected Compose service and a Unix socket for both connections.
    # Clear libpq overrides. Keep psql's stdin open so the transaction and its
    # export survive until pg_dump has finished importing and reading the snapshot.
    args = [*compose, 'exec', '-T', '--user', 'postgres', 'postgres', 'sh', '-ec',
            'if timeout -k 5 -s TERM "$2" env -i PATH="$PATH" PGCONNECT_TIMEOUT=5 '
            'PGOPTIONS="-c statement_timeout=10000 -c idle_in_transaction_session_timeout=950000" '
            'psql -X -qAt --no-password -v ON_ERROR_STOP=1 -h /var/run/postgresql '
            '-U "$POSTGRES_USER" -d "$1"; then exit 0; else exit 87; fi',
            'sh', expected_source['database_name'], str(SNAPSHOT_TIMEOUT)]
    process = subprocess.Popen(args, env=CLEAN_ENV, cwd=BASE, stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    observed_at = None
    try:
        process.stdin.write(SOURCE_QUERY)
        process.stdin.flush()
        deadline = time.monotonic() + 15
        raw = b''
        while b'\n' not in raw:
            ready, _, _ = select.select([process.stdout], [], [], max(0, deadline - time.monotonic()))
            require(ready, 'PostgreSQL snapshot export timed out')
            block = os.read(process.stdout.fileno(), MAX_METADATA)
            require(block and len(raw) + len(block) <= MAX_METADATA, 'invalid PostgreSQL snapshot response')
            raw += block
        require(raw.endswith(b'\n') and raw.count(b'\n') == 1, 'invalid PostgreSQL snapshot response')
        observed_at = time.monotonic()
        yield snapshot_metadata(raw, expected_source)
        process.stdin.write(b'COMMIT;\n\\quit\n')
        process.stdin.flush()
        require(process.wait(timeout=5) == 0, 'PostgreSQL snapshot session failed')
    except (OSError, subprocess.TimeoutExpired):
        raise BackupError('PostgreSQL snapshot session failed or timed out') from None
    finally:
        try:
            process.stdin.close()
        except BrokenPipeError:
            pass
        process.stdout.close()
        try:
            status = process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            status = process.wait()
        if status not in (0, 87):
            # Losing the client does not prove that the exporting transaction
            # was closed on the daemon. Retain the shared host locks for its bound.
            print('Snapshot termination unconfirmed; retaining locks for timeout bound', file=sys.stderr)
            elapsed = 0 if observed_at is None else time.monotonic() - observed_at
            time.sleep(max(0, SNAPSHOT_TIMEOUT + 10 - elapsed))


def dump(compose, destination, spool, snapshot=None):
    # Alpine's in-container timeout bounds pg_dump even if docker exec is interrupted.
    job = '/tmp/vellum-backup-' + uuid.uuid4().hex
    if snapshot is None:
        args = [*compose, 'exec', '-T', '-e', 'PGOPTIONS=-c statement_timeout=600000']
        options = '--no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
        pg_dump = 'pg_dump'
        positional = [job]
    else:
        args = [*compose, 'exec', '-T', '--user', 'postgres']
        options = ('--no-password -h /var/run/postgresql -U "$POSTGRES_USER" '
                   '-d "$2" --snapshot="$3"')
        pg_dump = 'env -i PATH="$PATH" PGCONNECT_TIMEOUT=5 PGOPTIONS="-c statement_timeout=600000" pg_dump'
        positional = [job, snapshot['source']['database_name'], snapshot['snapshot_id']]
    args.extend(['postgres', 'sh', '-ec',
            'mkdir -m 700 "$1"; '
            f'timeout -k 5 -s TERM 900 {pg_dump} --format=custom --compress=gzip:3 '
            f'--lock-wait-timeout=30000 {options} & child=$!; '
            'printf "%s" "$child" > "$1/pid"; '
            "trap 'rm -f \"$1/pid\"; rmdir \"$1\"' EXIT; "
            'if wait "$child"; then exit 0; else exit 88; fi', 'sh', *positional])

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
            # The in-container shell uses 88 only after reaping a failed dump.
            # A generic Docker client/daemon failure does not prove quiescence.
            if not successful and process.poll() not in (0, 88):
                cancel_dump(compose, job)
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


def valid_version(value):
    return (isinstance(value, str) and value != 'null'
            and re.fullmatch(r'[\x21-\x7e]{1,1024}', value) is not None)


def verify_integrity(metadata, size, digest):
    require(metadata.get('ContentLength') == size and metadata.get('ServerSideEncryption') == 'AES256'
            and metadata.get('ChecksumSHA256') == base64.b64encode(bytes.fromhex(digest)).decode(),
            'uploaded object size/checksum/encryption mismatch')


def verify_object(config, key, size, digest):
    head = aws(config, ['s3api', 'head-object', '--bucket', config['bucket'], '--key', key,
                       '--expected-bucket-owner', config['account_id'],
                       '--checksum-mode', 'ENABLED'], 'upload verification')
    verify_integrity(head, size, digest)
    return head.get('VersionId')


def upload(config, key, path, versioned=False):
    size, digest = path.stat().st_size, sha256_file(path)
    # Single PUT only (max 512 MiB): bounded disk and simple whole-object SHA-256/IAM.
    try:
        uploaded = aws(config, ['s3api', 'put-object', '--bucket', config['bucket'], '--key', key,
                               '--body', str(path), '--expected-bucket-owner', config['account_id'],
                               '--server-side-encryption', 'AES256', '--checksum-algorithm', 'SHA256',
                               '--checksum-sha256', base64.b64encode(bytes.fromhex(digest)).decode(),
                               '--if-none-match', '*'], 'object upload')
    except BackupError:
        # A lost response may follow a successful PUT; exact HEAD makes retry idempotent.
        uploaded = None
    version = verify_object(config, key, size, digest)
    reference = {'key': key, 'size': size, 'sha256': digest}
    if versioned:
        require(valid_version(version) and (uploaded is None or uploaded.get('VersionId') == version),
                'uploaded object version mismatch')
        reference['version_id'] = version
    return reference


def check_bucket(config, publishing=False):
    bucket = ['--bucket', config['bucket'], '--expected-bucket-owner', config['account_id']]
    location = aws(config, ['s3api', 'get-bucket-location', *bucket], 'backup bucket region verification')
    require(location.get('LocationConstraint') == REGION, 'unexpected backup bucket region')
    if publishing:
        versioning = aws(config, ['s3api', 'get-bucket-versioning', *bucket], 'backup versioning verification')
        require(versioning.get('Status') == 'Enabled', 'backup bucket versioning is disabled')


def verify_signature(provenance, payload, signature):
    public_key = Path(provenance['public_key'])
    trusted(public_key)
    require(public_key.stat().st_size <= MAX_METADATA, 'oversized backup public key')
    key_der = run(['/usr/bin/openssl', 'pkey', '-pubin', '-in', str(public_key), '-outform', 'DER'],
                  'backup public key parsing', timeout=10)
    require(len(key_der) == 44 and key_der.startswith(bytes.fromhex('302a300506032b6570032100')),
            'backup producer key must use Ed25519')
    require(isinstance(signature, str) and re.fullmatch(r'[A-Za-z0-9+/]{86}==', signature),
            'invalid backup signature encoding')
    raw = base64.b64decode(signature, validate=True)
    require(base64.b64encode(raw).decode('ascii') == signature, 'invalid backup signature encoding')
    with tempfile.TemporaryDirectory(prefix='vellum-backup-signature-') as temporary:
        message = Path(temporary) / 'payload'
        detached = Path(temporary) / 'signature'
        message.write_bytes(canonical(payload))
        detached.write_bytes(raw)
        run(['/usr/bin/openssl', 'pkeyutl', '-verify', '-rawin', '-pubin', '-inkey', str(public_key),
             '-in', str(message), '-sigfile', str(detached)], 'backup signature verification', timeout=10)


def signed_completion(provenance, payload, work):
    message = work / 'completion-payload.json'
    write_json(message, payload)
    signature = run(['/usr/bin/openssl', 'pkeyutl', '-sign', '-rawin', '-inkey', provenance['signing_key'],
                     '-in', str(message)], 'backup producer signing', timeout=10)
    require(len(signature) == 64, 'invalid backup signature size')
    encoded = base64.b64encode(signature).decode('ascii')
    verify_signature(provenance, payload, encoded)
    return {'payload': payload, 'signature': encoded}


def now():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def backup(environment, config):
    provenance = config.get('production_provenance') if environment == 'production' else None
    if provenance is not None:
        require(provenance['signing_key'] is not None, 'backup producer signing key is required')
        signing_key = Path(provenance['signing_key'])
        trusted(signing_key, private=True)
        require(signing_key.stat().st_size <= MAX_METADATA, 'oversized backup signing key')
        check_bucket(config, publishing=True)
    trusted(BASE / 'compose.yml')
    env_file = Path('/etc/vellum-cd') / (environment + '.env')
    trusted(env_file, private=True)
    spool = STATE / 'backups'
    spool.mkdir(mode=0o700, exist_ok=True)
    trusted(spool, directory=True, private=True)
    # Shared host gate also excludes opposite-environment and legacy controllers.
    with host_operation(environment):
        check_space(spool)
        work = Path(tempfile.mkdtemp(prefix=environment + '-', dir=spool))
        archive = work / 'database.dump'
        complete_dump = False
        try:
            compose = ['docker', 'compose', '--project-name', 'vellum-' + environment,
                       '--env-file', str(env_file), '-f', str(BASE / 'compose.yml')]
            run([*compose, 'config', '--quiet'], 'Compose validation')
            if provenance is None:
                version = run([*compose, 'exec', '-T', 'postgres', 'sh', '-ec',
                               'exec psql -X -At -U "$POSTGRES_USER" -d "$POSTGRES_DB" '
                               '-c "SHOW server_version"'], 'PostgreSQL version').decode().strip()
                require(re.fullmatch(PG_VERSION, version), 'expected PostgreSQL 17')
                snapshot = {'snapshot_started_at': now(), 'postgres_version': version}
                dump(compose, archive, spool)
            else:
                with exported_snapshot(compose, provenance['source']) as snapshot:
                    dump(compose, archive, spool, snapshot)
            verify_archive(compose, archive)
            complete_dump = True
            publish_backup(environment, config, work, archive, snapshot, provenance)
            # Only this run is removed. Failed complete dumps are never auto-deleted.
            shutil.rmtree(work)
            sync_directory(spool)
        except BaseException:
            if not complete_dump:
                shutil.rmtree(work)
            raise
    print(environment + ': off-host backup verified; local temporary files removed')


def publish_backup(environment, config, work, archive, snapshot, provenance):
    started = snapshot['snapshot_started_at']
    timestamp = utc_time(started) if provenance else datetime.now(timezone.utc)
    run_id = timestamp.strftime('%Y%m%dT%H%M%SZ-') + uuid.uuid4().hex
    key = environment + '/runs/' + run_id
    completion_key = environment + '/completed/' + run_id + '.json'
    version = 2 if provenance else 1
    manifest = {'format_version': version, 'environment': environment, **snapshot,
                'dump_completed_at': now(), 'archive_size': archive.stat().st_size,
                'archive_sha256': sha256_file(archive)}
    if provenance:
        manifest['run_id'] = run_id
        require(timestamp <= utc_time(manifest['dump_completed_at']), 'backup clock moved backwards')
    manifest_path = work / 'manifest.json'
    write_json(manifest_path, manifest)
    sync_directory(work)
    upload_options = {'versioned': True} if provenance else {}
    archive_ref = upload(config, key + '.dump', archive, **upload_options)
    manifest_ref = upload(config, key + '.json', manifest_path, **upload_options)
    marker = {'format_version': version, 'environment': environment,
              'snapshot_started_at': started, 'uploaded_at': now(),
              'archive': archive_ref, 'manifest': manifest_ref}
    if provenance:
        uploaded = utc_time(marker['uploaded_at'])
        require(utc_time(manifest['dump_completed_at']) <= uploaded
                and (uploaded - timestamp).total_seconds() < 24 * 3600, 'invalid backup publication clock')
        marker.update(run_id=run_id, bucket=config['bucket'], account_id=config['account_id'],
                      region=REGION, completion_key=completion_key, key_id=provenance['key_id'])
        marker = signed_completion(provenance, marker, work)
    marker_path = work / 'complete.json'
    write_json(marker_path, marker)
    require(marker_path.stat().st_size <= MAX_METADATA, 'oversized backup completion marker')
    upload(config, completion_key, marker_path, **upload_options)


def check_freshness(environment, config):
    provenance = config.get('production_provenance') if environment == 'production' else None
    if provenance:
        check_bucket(config)
    listing = aws(config, ['s3api', 'list-objects-v2', '--bucket', config['bucket'],
                          '--expected-bucket-owner', config['account_id'],
                          '--prefix', environment + '/completed/', '--max-keys', '1000',
                          '--no-paginate'], 'backup freshness listing')
    require(not listing.get('IsTruncated'), 'too many completion objects; inspect retention')
    objects = listing.get('Contents', [])
    require(objects, 'no completed off-host backup')
    # Run names are fixed-width UTC timestamps followed by random IDs.
    latest = max(objects, key=lambda item: item['Key'])
    require(re.fullmatch(environment + r'/completed/' + RUN_ID + r'\.json',
                         latest['Key']), 'invalid completion object name')
    require(0 < latest['Size'] <= MAX_METADATA, 'oversized completion object')
    if provenance:
        check_signed_freshness(config, provenance, latest)
        print(environment + ': signed off-host backup is fresh')
        return
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


def read_versioned_metadata(config, reference, path):
    require(valid_version(reference['version_id']) and type(reference['size']) is int
            and 0 < reference['size'] <= MAX_METADATA, 'invalid backup metadata reference')
    args = ['--bucket', config['bucket'], '--key', reference['key'], '--version-id=' + reference['version_id'],
            '--expected-bucket-owner', config['account_id'], '--checksum-mode', 'ENABLED']
    head = aws(config, ['s3api', 'head-object', *args], 'backup metadata version verification')
    require(head.get('VersionId') == reference['version_id'] and head.get('ContentLength') == reference['size'],
            'backup metadata version/size mismatch')
    metadata = aws(config, ['s3api', 'get-object', *args, str(path)], 'backup metadata download')
    require(metadata.get('VersionId') == reference['version_id'] and path.stat().st_size == reference['size'],
            'downloaded backup metadata version/size mismatch')
    data = path.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    verify_integrity(head, reference['size'], digest)
    verify_integrity(metadata, reference['size'], digest)
    if 'sha256' in reference:
        require(digest == reference['sha256'], 'downloaded backup metadata checksum mismatch')
    value = json.loads(data)
    require(isinstance(value, dict) and data == canonical(value), 'invalid backup metadata JSON')
    return value


def check_signed_freshness(config, provenance, latest):
    key = latest['Key']
    run_id = Path(key).stem
    head = aws(config, ['s3api', 'head-object', '--bucket', config['bucket'], '--key', key,
                       '--expected-bucket-owner', config['account_id']], 'completion version selection')
    require(head.get('ContentLength') == latest['Size'], 'completion size changed')
    with tempfile.TemporaryDirectory(prefix='vellum-freshness-') as temporary:
        work = Path(temporary)
        marker = read_versioned_metadata(config, {'key': key, 'version_id': head.get('VersionId'),
                                                 'size': latest['Size']}, work / 'complete.json')
        require(set(marker) == {'payload', 'signature'} and isinstance(marker['payload'], dict),
                'signed backup completion is required')
        payload = marker['payload']
        verify_signature(provenance, payload, marker['signature'])
        require(payload['format_version'] == 2 and payload['environment'] == 'production'
                and payload['bucket'] == config['bucket'] and payload['account_id'] == config['account_id']
                and payload['region'] == REGION and payload['key_id'] == provenance['key_id']
                and payload['completion_key'] == key and payload['run_id'] == run_id,
                'signed backup completion scope mismatch')
        snapshot = utc_time(payload['snapshot_started_at'])
        uploaded = utc_time(payload['uploaded_at'])
        clock = datetime.now(timezone.utc)
        require(run_id.startswith(snapshot.strftime('%Y%m%dT%H%M%SZ-')) and snapshot <= uploaded <= clock
                and (clock - snapshot).total_seconds() <= MAX_AGE, 'signed off-host backup is stale or mistimed')
        for field, extension in (('archive', '.dump'), ('manifest', '.json')):
            reference = payload[field]
            require(reference['key'] == 'production/runs/' + run_id + extension
                    and valid_version(reference['version_id']), 'signed completion reference mismatch')
        manifest = read_versioned_metadata(config, payload['manifest'], work / 'manifest.json')
        archive = payload['archive']
        require(manifest['format_version'] == 2 and manifest['environment'] == 'production'
                and manifest['run_id'] == run_id and manifest['source'] == provenance['source']
                and manifest['snapshot_started_at'] == payload['snapshot_started_at']
                and snapshot <= utc_time(manifest['dump_completed_at']) <= uploaded
                and manifest['archive_size'] == archive['size'] and manifest['archive_sha256'] == archive['sha256'],
                'signed backup manifest mismatch')
        version = verify_object(config, archive['key'], archive['size'], archive['sha256'])
        require(version == archive['version_id'], 'signed backup archive version mismatch')


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
