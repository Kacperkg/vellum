#!/usr/bin/env python3
"""Trusted production backup verifier and isolated PostgreSQL restore worker."""
import argparse
import base64
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import secrets
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time

REGION = "eu-west-2"
MAX_ARCHIVE = 512 * 1024**2
MAX_METADATA = 16 * 1024
MAX_SCHEMA = 1024**2
HEX = r"[0-9a-f]{64}"
RUN = r"[0-9]{8}T[0-9]{6}Z-[0-9a-f]{32}"
BUCKET = r"[a-z0-9][a-z0-9-]{1,61}[a-z0-9]"
IDENTIFIER = r"[a-z_][a-z0-9_]{0,62}"
VERIFY_DOMAIN = "vellum-production-verified-backup-v1"
DRILL_DOMAIN = "vellum-production-restore-drill-v1"
REHEARSAL_DOMAIN = "vellum-production-candidate-rehearsal-v1"
RECEIPTS = {"verify-backup": (VERIFY_DOMAIN, "verified-backup"),
            "drill": (DRILL_DOMAIN, "restore-drill"),
            "rehearse": (REHEARSAL_DOMAIN, "candidate-rehearsal")}
CHECKS = ["postgresql17", "transactional-restore", "schema-fingerprint",
          "validated-catalog", "budget-uniqueness", "ownership",
          "auth-session-families", "financial-invariants", "sequence-safety",
          "baseline-backend-read-only-smoke"]
DOCKER = ["docker", "--host", "unix:///var/run/docker.sock"]
ROOT = Path(__file__).resolve().parents[2]
BOOTSTRAP_SQL = ROOT / "deploy/recovery/runtime/prepare-roles.sql"
INVARIANTS_SQL = ROOT / "deploy/recovery/restore-checks.sql"
RUNTIME_SQL = ROOT / "backend/internal/database/runtime_policy.sql"
RUNTIME_KEYS = {
    "PORT", "BIND_HOST", "GIN_MODE", "DB_HOST", "DB_PORT", "DB_USER", "DB_NAME", "DB_SSLMODE",
    "JWT_EXPIRY", "JWT_REFRESH_EXPIRY", "CORS_ALLOWED_ORIGINS", "TRUSTED_PROXY_CIDRS",
    "APP_ENV", "AWS_REGION", "PASSWORD_RESET_EMAIL_MODE", "SES_FROM_ADDRESS",
    "PASSWORD_RESET_FRONTEND_URL", "PASSWORD_RESET_TOKEN_TTL", "PASSWORD_RESET_COOLDOWN",
    "PASSWORD_RESET_ALLOWED_RECIPIENTS", "PASSWORD_RESET_QUEUE_SIZE",
    "PASSWORD_RESET_SEND_TIMEOUT", "RATE_LIMIT_MAX_ENTRIES", "RATE_LIMIT_SHARED_IP_MULTIPLIER",
    *(f"RATE_LIMIT_{kind}_{suffix}" for kind in (
        "LOGIN", "REGISTER", "REFRESH", "LOGOUT", "FORGOT_PASSWORD", "RESET_PASSWORD")
      for suffix in ("REQUESTS", "WINDOW")),
}
CLEAN_ENV = {
    "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
    "HOME": "/root", "DOCKER_CONFIG": "/etc/vellum-recovery/docker",
    "AWS_CONFIG_FILE": "/dev/null", "AWS_SHARED_CREDENTIALS_FILE": "/dev/null",
    "AWS_PAGER": "", "AWS_CLI_AUTO_PROMPT": "off", "AWS_MAX_ATTEMPTS": "1",
    "AWS_METADATA_SERVICE_TIMEOUT": "2", "AWS_METADATA_SERVICE_NUM_ATTEMPTS": "2",
}


class RecoveryError(Exception):
    """A redacted worker failure suitable for the private job record and alert."""


def require(condition, message):
    if not condition:
        raise RecoveryError(message)


def matches(pattern, value):
    return isinstance(value, str) and re.fullmatch(pattern, value, re.ASCII) is not None


def fields(value, expected):
    return isinstance(value, dict) and set(value) == set(expected.split())


def canonical(value):
    # Same bytes as deploy/cd/release.py, which is installed on a different host.
    return (json.dumps(value, sort_keys=True, indent=2) + "\n").encode()


def producer_canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON field")
        result[key] = value
    return result


def strict_json(value):
    try:
        return json.loads(value, object_pairs_hook=unique_pairs)
    except (ValueError, RecursionError):
        raise RecoveryError("invalid JSON") from None


def digest(data):
    return hashlib.sha256(data).hexdigest()


def file_digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def harness_fingerprint():
    files = (Path(__file__).resolve(), BOOTSTRAP_SQL, INVARIANTS_SQL, RUNTIME_SQL)
    return digest(canonical({str(path.relative_to(ROOT)): file_digest(path) for path in files}))


def timestamp(value=None):
    return (value or datetime.now(timezone.utc)).strftime("%Y-%m-%dT%H:%M:%SZ")


def parse_time(value):
    require(matches(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?Z",
                    value), "invalid backup timestamp")
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise RecoveryError("invalid backup timestamp") from None


def safe_file(path, directory=False, private=True):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "unsafe worker path")
    for parent in reversed(path.parents):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                "unsafe worker path parent")
    info = path.lstat()
    require(info.st_uid == 0 and not info.st_mode & (0o077 if private else 0o022),
            "unsafe worker path mode")
    require(stat.S_ISDIR(info.st_mode) if directory else
            stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "unsafe worker path type")
    return path


def limited_read(path, limit=MAX_METADATA):
    with path.open("rb") as source:
        data = source.read(limit + 1)
    require(0 < len(data) <= limit, "invalid worker input size")
    return data


def version_id(value):
    return matches(r"[\x21-\x7e]{1,1024}", value) and value != "null"


def source_fields(source):
    return (fields(source, "system_identifier database_oid database_name")
            and matches(r"[1-9][0-9]{0,19}", source["system_identifier"])
            and int(source["system_identifier"]) < 2**64
            and type(source["database_oid"]) is int and 0 < source["database_oid"] < 2**32
            and matches(IDENTIFIER, source["database_name"])
            and source["database_name"] not in {"postgres", "template0", "template1"})


def validate_profile(profile):
    require(fields(profile, "account_id bucket receipt_bucket producer_key_id producer_key_sha256 "
                   "worker_key_id source backup_contract postgres_image postgres_image_id "
                   "postgres_version postgres_role postgres_locale "
                   "schema_sha256 execution_sha256 runtime_sha256 harness_sha256 "
                   "backend_image backend_runtime alert_topic"),
            "invalid recovery profile fields")
    require(matches(r"[0-9]{12}", profile["account_id"])
            and all(matches(BUCKET, profile[key]) for key in ("bucket", "receipt_bucket"))
            and profile["bucket"] != profile["receipt_bucket"]
            and matches(r"[a-z0-9][a-z0-9_-]{0,63}", profile["producer_key_id"])
            and source_fields(profile["source"]), "invalid backup source profile")
    account = profile["account_id"]
    require(matches(rf"arn:aws:kms:{REGION}:{account}:key/[a-f0-9-]{{36}}", profile["worker_key_id"])
            and profile["worker_key_id"] != profile["producer_key_id"]
            and matches(rf"arn:aws:sns:{REGION}:{account}:[A-Za-z0-9_-]{{1,256}}", profile["alert_topic"])
            and all(matches(HEX, profile[key]) for key in
                    ("producer_key_sha256", "backup_contract", "schema_sha256", "harness_sha256",
                     "execution_sha256", "runtime_sha256")), "invalid worker identity or policy")
    require(matches(r"(?:docker\.io/library/)?postgres@sha256:" + HEX, profile["postgres_image"])
            and matches("sha256:" + HEX, profile["postgres_image_id"])
            and matches(r"17[0-9]{4}", profile["postgres_version"])
            and matches(IDENTIFIER, profile["postgres_role"])
            and not profile["postgres_role"].startswith("pg_")
            and profile["postgres_role"] not in ("vellum_runtime", "vellum_migrator")
            and profile["postgres_locale"] in ("C", "C.UTF-8", "en_US.utf8"),
            "unsupported PostgreSQL restore profile")
    env = profile["backend_runtime"]
    require(matches(r"ghcr\.io/kacperkg/vellum-backend@sha256:" + HEX, profile["backend_image"])
            and isinstance(env, dict) and set(env) <= RUNTIME_KEYS
            and all(isinstance(v, str) and len(v) <= 1024 and not any(c in v for c in "\r\n\0")
                    for v in env.values())
            and env.get("DB_USER") == "vellum_runtime" and env.get("DB_HOST") == "postgres"
            and env.get("DB_PORT") == "5432" and env.get("DB_SSLMODE") == "disable"
            and env.get("DB_NAME") == profile["source"]["database_name"]
            and env.get("PORT") == "8080" and digest(canonical(env)) == profile["runtime_sha256"],
            "wrong approved backend runtime profile")



def validate_candidate(candidate, profile):
    require(fields(candidate, "profile_sha256 image image_id execution_sha256 migration_contract")
            and candidate["profile_sha256"] == digest(canonical(profile))
            and matches(r"ghcr\.io/kacperkg/vellum-backend@sha256:" + HEX, candidate["image"])
            and matches("sha256:" + HEX, candidate["image_id"])
            and matches(HEX, candidate["execution_sha256"])
            and matches(HEX, candidate["migration_contract"]), "invalid independently pinned candidate profile")

def object_ref(ref, key, maximum):
    require(fields(ref, "key size sha256 version_id") and ref["key"] == key
            and type(ref["size"]) is int and 0 < ref["size"] <= maximum
            and matches(HEX, ref["sha256"]) and version_id(ref["version_id"]),
            "invalid versioned backup reference")
    return ref


def object_metadata(metadata, key, version, maximum):
    require(isinstance(metadata, dict) and metadata.get("VersionId") == version
            and version_id(version) and not metadata.get("DeleteMarker")
            and metadata.get("ServerSideEncryption") == "AES256"
            and metadata.get("ChecksumType", "FULL_OBJECT") == "FULL_OBJECT",
            "invalid backup object version or encryption")
    try:
        checksum = base64.b64decode(metadata.get("ChecksumSHA256", ""), validate=True)
    except (TypeError, ValueError):
        raise RecoveryError("invalid backup object checksum") from None
    return object_ref({"key": key, "size": metadata.get("ContentLength"), "sha256": checksum.hex(),
                       "version_id": version}, key, maximum)


class Runner:
    def __init__(self, deadline, scratch=None):
        self.deadline = deadline
        self.scratch = scratch

    def call(self, args, label, *, stdin=None, timeout=30, limit=MAX_METADATA, cleanup=False):
        # File-backed captures and RLIMIT_FSIZE bound both CLI output and any download.
        def cap_output():
            resource.setrlimit(resource.RLIMIT_FSIZE, (limit, limit))

        remaining = timeout if cleanup else min(timeout, self.deadline - time.monotonic())
        require(remaining > 0, "recovery job timed out")
        try:
            with tempfile.TemporaryFile(dir=self.scratch) as output:
                proc = subprocess.run(args, env=CLEAN_ENV, stdout=output, stderr=subprocess.DEVNULL,
                                      stdin=stdin if stdin is not None else subprocess.DEVNULL,
                                      preexec_fn=cap_output, timeout=remaining, check=False)
                require(proc.returncode == 0, label + " failed")
                output.seek(0)
                result = output.read(min(limit, MAX_SCHEMA) + 1)
                require(len(result) <= min(limit, MAX_SCHEMA), label + " output exceeded limit")
                return result
        except (OSError, subprocess.TimeoutExpired, subprocess.SubprocessError):
            raise RecoveryError(label + " failed or timed out") from None

    def aws(self, args, label, **kwargs):
        command = ["aws", "--region", REGION, "--no-cli-pager", "--cli-connect-timeout", "5",
                   "--cli-read-timeout", "45", "--output", "json"]
        return strict_json(self.call(command + args, label, **kwargs) or b"{}")


class BackupStore:
    def __init__(self, runner, profile):
        self.runner = runner
        self.profile = profile
        self.target = ["--bucket", profile["bucket"], "--expected-bucket-owner", profile["account_id"]]

    def check_region(self):
        result = self.runner.aws(["s3api", "get-bucket-location", *self.target], "backup bucket region")
        require(result.get("LocationConstraint") == REGION, "unexpected backup bucket region")

    def discover(self, now):
        start = (now - timedelta(hours=24)).strftime("%Y%m%dT%H%M%SZ")
        result = self.runner.aws(["s3api", "list-objects-v2", *self.target, "--no-paginate",
                                  "--prefix", "production/completed/", "--start-after",
                                  "production/completed/" + start, "--max-keys", "64"],
                                 "completed backup discovery", limit=64 * 1024)
        entries = result.get("Contents")
        require(result.get("IsTruncated") is False and isinstance(entries, list)
                and 0 < len(entries) <= 64, "missing or ambiguous completed backup discovery")
        keys = [entry.get("Key") for entry in entries]
        require(len(keys) == len(set(keys))
                and all(matches(r"production/completed/" + RUN + r"\.json", key) for key in keys),
                "invalid completed backup discovery")
        key = max(keys)
        result = self.runner.aws(["s3api", "head-object", *self.target, "--key", key,
                                  "--checksum-mode", "ENABLED"], "current completion version")
        return key, object_metadata(result, key, result.get("VersionId"), MAX_METADATA)["version_id"]

    def fetch(self, key, version, maximum, destination, expected=None):
        args = [*self.target, "--key", key, "--version-id=" + version, "--checksum-mode", "ENABLED"]
        head = self.runner.aws(["s3api", "head-object", *args], "versioned backup metadata")
        reference = object_metadata(head, key, version, maximum)
        require(expected is None or reference == expected, "backup metadata differs from signed reference")
        # Request the entire object. Range GETs can omit full-object checksums.
        response = self.runner.aws(["s3api", "get-object", *args, str(destination)],
                                   "versioned backup download", timeout=180,
                                   limit=max(MAX_METADATA, reference["size"]))
        require(object_metadata(response, key, version, maximum) == reference
                and destination.stat().st_size == reference["size"]
                and file_digest(destination) == reference["sha256"],
                "downloaded backup checksum, size or version mismatch")
        return reference


def verify_producer(runner, public_key, envelope, work):
    message = work / "producer-message"
    signature = work / "producer-signature"
    message.write_bytes(producer_canonical(envelope["payload"]))
    try:
        raw = base64.b64decode(envelope["signature"], validate=True)
    except (TypeError, ValueError):
        raise RecoveryError("invalid backup producer signature") from None
    require(len(raw) == 64 and base64.b64encode(raw).decode() == envelope["signature"],
            "invalid backup producer signature")
    signature.write_bytes(raw)
    runner.call(["openssl", "pkeyutl", "-verify", "-rawin", "-pubin", "-inkey", str(public_key),
                 "-in", str(message), "-sigfile", str(signature)], "backup producer signature")


def verify_backup(store, runner, profile, key, version, public_key, work, now, archive_proof=None):
    require(matches(r"production/completed/" + RUN + r"\.json", key) and version_id(version),
            "invalid completion key or version")
    run_id = key.removeprefix("production/completed/").removesuffix(".json")
    complete = work / "completion.json"
    completion_ref = store.fetch(key, version, MAX_METADATA, complete)
    encoded_completion = limited_read(complete)
    envelope = strict_json(encoded_completion)
    require(fields(envelope, "payload signature") and producer_canonical(envelope) == encoded_completion,
            "unsupported backup completion format")
    marker = envelope["payload"]
    require(fields(marker, "format_version environment run_id bucket account_id region completion_key "
                   "key_id snapshot_started_at uploaded_at archive manifest")
            and type(marker["format_version"]) is int and marker["format_version"] == 2
            and marker["environment"] == "production" and marker["run_id"] == run_id
            and marker["bucket"] == profile["bucket"] and marker["account_id"] == profile["account_id"]
            and marker["region"] == REGION and marker["completion_key"] == key
            and marker["key_id"] == profile["producer_key_id"], "wrong backup completion provenance")
    verify_producer(runner, public_key, envelope, work)
    started = parse_time(marker["snapshot_started_at"])
    uploaded = parse_time(marker["uploaded_at"])
    require(now - timedelta(hours=24) < started <= uploaded <= now, "backup is stale or future-dated")
    require(run_id.startswith(started.strftime("%Y%m%dT%H%M%SZ-")),
            "backup key does not bind the signed snapshot time")
    base = "production/runs/" + run_id
    archive = object_ref(marker["archive"], base + ".dump", MAX_ARCHIVE)
    manifest_ref = object_ref(marker["manifest"], base + ".json", MAX_METADATA)
    manifest_path = work / "manifest.json"
    store.fetch(manifest_ref["key"], manifest_ref["version_id"], MAX_METADATA, manifest_path, manifest_ref)
    encoded_manifest = limited_read(manifest_path)
    manifest = strict_json(encoded_manifest)
    require(producer_canonical(manifest) == encoded_manifest
            and fields(manifest, "format_version environment run_id source snapshot_id snapshot_started_at "
                   "dump_completed_at postgres_version archive_size archive_sha256")
            and type(manifest["format_version"]) is int and manifest["format_version"] == 2
            and manifest["environment"] == "production" and manifest["run_id"] == run_id
            and source_fields(manifest["source"]) and manifest["source"] == profile["source"]
            and matches(r"[0-9A-F]{8}-[0-9A-F]{8}-[0-9]+", manifest["snapshot_id"])
            and manifest["snapshot_started_at"] == marker["snapshot_started_at"]
            and started <= parse_time(manifest["dump_completed_at"]) <= uploaded
            and postgres_version_num(manifest["postgres_version"]) == profile["postgres_version"]
            and type(manifest["archive_size"]) is int and manifest["archive_size"] == archive["size"]
            and manifest["archive_sha256"] == archive["sha256"], "wrong backup manifest provenance")
    archive_path = work / "archive.dump"
    if archive_proof != archive:
        store.fetch(archive["key"], archive["version_id"], MAX_ARCHIVE, archive_path, archive)
    else:
        args = ["--key", archive["key"], "--version-id=" + archive["version_id"],
                "--checksum-mode", "ENABLED"]
        metadata = runner.aws(["s3api", "head-object", *store.target, *args], "versioned archive metadata")
        require(object_metadata(metadata, archive["key"], archive["version_id"], MAX_ARCHIVE) == archive,
                "archive metadata differs from signed reference")
    source = manifest["source"]
    backup = {
        "archive_sha256": archive["sha256"], "completion_sha256": completion_ref["sha256"],
        "manifest_sha256": manifest_ref["sha256"], "archive_bytes": archive["size"],
        "contract": profile["backup_contract"], "created_at": timestamp(started),
        "source_sha256": digest(canonical({"system_id": source["system_identifier"],
                                           "database_oid": str(source["database_oid"])})),
        "verified_at": timestamp(),
    }
    require(parse_time(backup["verified_at"]) - timedelta(hours=24) < parse_time(backup["created_at"]),
            "backup expired during verification")
    return backup, {"completion": completion_ref, "manifest": manifest_ref, "archive": archive}


def postgres_version_num(version):
    if matches(r"17[0-9]{4}", version):
        return version
    require(matches(r"17\.[0-9]{1,2}(?: [A-Za-z0-9_. ()-]+)?", version),
            "invalid PostgreSQL dump version")
    return str(170000 + int(version.split()[0].split(".")[1]))


@contextmanager
def postgres_sandbox(runner, profile, job):
    image = profile["postgres_image"]
    inspected = runner.call([*DOCKER, "image", "inspect", "--format", "{{.Id}}", image],
                            "pinned PostgreSQL image").decode().strip()
    require(inspected == profile["postgres_image_id"], "PostgreSQL image identity mismatch")
    name = "vellum-recovery-" + job
    role = profile["postgres_role"]
    database = profile["source"]["database_name"]
    options = ["--network=none", "--read-only", "--cap-drop=ALL",
               "--security-opt=no-new-privileges:true", "--user=70:70",
               "--pids-limit=96", "--memory=3g", "--memory-swap=3g", "--cpus=2",
               "--ulimit=nofile=2048:2048", "--shm-size=128m", "--log-driver=none",
               "--no-healthcheck", "--pull=never",
               "--tmpfs=/var/lib/postgresql/data:rw,noexec,nosuid,nodev,size=2g,uid=70,gid=70,mode=0700",
               "--tmpfs=/var/run/postgresql:rw,noexec,nosuid,nodev,size=8m,uid=70,gid=70,mode=0700",
               "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16m",
               "--env=POSTGRES_USER=" + role, "--env=POSTGRES_DB=" + database,
               "--env=PGDATA=/var/lib/postgresql/data/primary",
               "--env=POSTGRES_PASSWORD=" + secrets.token_hex(32),
               "--env=POSTGRES_INITDB_ARGS=--encoding=UTF8 --auth-host=scram-sha-256 --locale="
               + profile["postgres_locale"],
               "--label=org.vellum.recovery.job=" + job]
    try:
        runner.call([*DOCKER, "create", "--name", name, *options, image, "postgres",
                     "-c", "listen_addresses=", "-c", "max_connections=20",
                     "-c", "max_wal_size=256MB", "-c", "statement_timeout=180000",
                     "-c", "lock_timeout=5000"], "private PostgreSQL sandbox creation")
        runner.call([*DOCKER, "start", name], "private PostgreSQL sandbox start")
        # The kernel may expose unconfigured tunnel devices in a new namespace.
        # Only loopback may be up, and neither metadata address may be routable.
        isolation = ('seen=; for i in /sys/class/net/*/flags; do flags=$(cat "$i"); '
                     'if [ "$((flags & 1))" -ne 0 ]; then '
                     'test "$i" = /sys/class/net/lo/flags || exit 1; seen=yes; fi; done; '
                     'test "$seen" = yes; command -v ip; if ip -4 route get 169.254.169.254 || '
                     'ip -6 route get fd00:ec2::254; then exit 1; fi')
        runner.call([*DOCKER, "exec", name, "sh", "-ec", isolation],
                    "private PostgreSQL loopback-only network")
        for _ in range(30):
            try:
                runner.call([*DOCKER, "exec", name, "sh", "-ec",
                             'test "$(cat /proc/1/comm)" = postgres && exec pg_isready '
                             '-h /var/run/postgresql -U "$1" -d "$2"',
                             "vellum-recovery", role, database], "PostgreSQL readiness", timeout=3)
                break
            except RecoveryError:
                time.sleep(1)
        else:
            raise RecoveryError("private PostgreSQL did not become ready")
        yield name
    finally:
        remove_sandbox(runner, "org.vellum.recovery.job=" + job)


def remove_sandbox(runner, label):
    # Exact labels cover create timing out after the daemon accepted the request.
    args = [*DOCKER, "container", "ls", "--all", "--quiet", "--no-trunc", "--filter", "label=" + label]
    ids = runner.call(args, "private sandbox inventory during cleanup",
                      cleanup=True, timeout=10).decode().split()
    require(len(ids) <= 1 and all(matches(HEX, value) for value in ids),
            "private sandbox cleanup inventory is ambiguous")
    if ids:
        runner.call([*DOCKER, "rm", "-f", "--volumes", ids[0]], "private sandbox cleanup",
                    cleanup=True, timeout=10)
    require(not runner.call(args, "private sandbox cleanup verification", cleanup=True, timeout=10).strip(),
            "private sandbox cleanup incomplete")


def smoke_backend(runner, profile, container, role, database, password, job, candidate=None):
    image = candidate["image"] if candidate is not None else profile["backend_image"]
    expected_execution = candidate["execution_sha256"] if candidate is not None else profile["execution_sha256"]
    inspected = strict_json(runner.call([*DOCKER, "image", "inspect", "--format", "{{json .}}", image],
                                       "pinned backend image", limit=MAX_SCHEMA))
    config = dict(inspected["Config"])
    labels = dict(config.get("Labels") or {})
    labels.pop("org.opencontainers.image.revision", None)
    config["Labels"] = labels
    execution = {"architecture": inspected["Architecture"], "os": inspected["Os"],
                 "rootfs": inspected["RootFS"], "config": config}
    require(digest(canonical(execution)) == expected_execution
            and (candidate is None or inspected["Id"] == candidate["image_id"])
            and config.get("Entrypoint") == ["/app/vellum"] and config.get("User") == "10001:10001"
            and not config.get("Volumes"), "backend execution fingerprint differs from approved baseline")
    exec_pg = [*DOCKER, "exec", container]
    standby = "/var/lib/postgresql/data/standby"
    # Use a physical standby, since a client can change a default read-only GUC.
    # Only the standby listens on TCP. The primary socket is private to postgres.
    runner.call([*exec_pg, "pg_basebackup", "-R", "--wal-method=stream", "--checkpoint=fast",
                 "--pgdata=" + standby, "--host=/var/run/postgresql", "--username=" + role],
                "read-only recovery copy", timeout=150)
    runner.call([*exec_pg, "pg_ctl", "-D", standby, "-w", "-t", "30", "-l", "/tmp/standby.log",
                 "-o", "-p 5433 -c listen_addresses=127.0.0.1", "start"],
                "private read-only PostgreSQL standby start", timeout=35)
    readonly = [*exec_pg, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1",
                "--host=/var/run/postgresql", "--port=5433", "--username=" + role, "--dbname=" + database,
                "-c", "SELECT pg_is_in_recovery() AND current_setting('transaction_read_only') = 'on'"]
    require(runner.call(readonly, "read-only standby check").strip() == b"t",
            "backend smoke database is writable")

    before = standby_fingerprints(runner, exec_pg, role, database) if candidate is not None else None
    env = dict(profile["backend_runtime"])
    for key in ("SES_FROM_ADDRESS", "PASSWORD_RESET_ALLOWED_RECIPIENTS", "PASSWORD_RESET_FRONTEND_URL"):
        env.pop(key, None)
    env.update({"DB_HOST": "127.0.0.1", "DB_PORT": "5433", "DB_PASSWORD": password,
                "BIND_HOST": "127.0.0.1", "PASSWORD_RESET_EMAIL_MODE": "disabled",
                "JWT_SECRET": secrets.token_hex(32), "AWS_EC2_METADATA_DISABLED": "true"})
    label = "org.vellum.recovery.backend=" + job
    name = container + "-backend"
    options = ["--network=container:" + container, "--read-only", "--cap-drop=ALL",
               "--security-opt=no-new-privileges:true", "--user=10001:10001", "--pids-limit=64",
               "--memory=256m", "--memory-swap=256m", "--cpus=1", "--log-driver=none",
               "--ulimit=nofile=512:512", "--no-healthcheck", "--pull=never", "--label=" + label,
               "--tmpfs=/tmp:rw,noexec,nosuid,nodev,size=16m"]
    try:
        runner.call([*DOCKER, "create", "--name", name, *options,
                     *("--env=" + key + "=" + value for key, value in env.items()), image, "serve"],
                    "private migration-free backend creation")
        runner.call([*DOCKER, "start", name], "private migration-free backend start")
        for _ in range(15):
            try:
                pong = runner.call([*exec_pg, "wget", "-q", "-T", "2", "-O", "-",
                                    "http://127.0.0.1:8080/ping"], "isolated migration-free backend smoke",
                                   timeout=3, limit=1024)
                require(strict_json(pong) == {"message": "pong"}, "unexpected backend smoke response")
                break
            except RecoveryError:
                time.sleep(1)
        else:
            raise RecoveryError("migration-free backend did not pass the isolated startup smoke")
        require(runner.call(readonly, "read-only standby recheck").strip() == b"t",
                "backend smoke database stopped enforcing read-only recovery")
    finally:
        remove_sandbox(runner, label)
    if candidate is not None:
        after = standby_fingerprints(runner, exec_pg, role, database)
        require(before == after and before[0] == profile["schema_sha256"],
                "candidate rehearsal schema or data differed")
        return {"candidate_backend": image, "schema_before": before[0], "schema_after": after[0],
                "data_before": before[1], "data_after": after[1]}


def standby_fingerprints(runner, exec_pg, role, database):
    # The data never leaves the disposable PostgreSQL container. A fixed
    # restriction key makes identical dump bytes comparable; they are never run.
    script = ('set -o pipefail; pg_dump "$1" --create --lock-wait-timeout=5s '
              '--restrict-key=VellumReadinessSchemaFingerprintV1 -h /var/run/postgresql -p 5433 '
              '-U "$2" -d "$3" | sha256sum')
    def fingerprint(flag):
        line = runner.call([*exec_pg, "sh", "-ec", script, "vellum-rehearsal", flag, role, database],
                           "trusted candidate rehearsal database fingerprint", timeout=90).decode().strip()
        require(matches(HEX + r"  -", line), "invalid candidate rehearsal fingerprint")
        return line.split()[0]

    return fingerprint("--schema-only"), fingerprint("--data-only")


def run_restore(runner, profile, archive, job, candidate=None):
    role = profile["postgres_role"]
    database = profile["source"]["database_name"]
    with postgres_sandbox(runner, profile, job) as container:
        command = [*DOCKER, "exec", "-i", container]
        connection = ["--host=/var/run/postgresql", "--username=" + role, "--dbname=" + database]
        sql = [*command, "psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", *connection]
        version = runner.call(sql + ["-c", "SHOW server_version_num"], "restored PostgreSQL version")
        require(version.decode().strip() == profile["postgres_version"], "restore version mismatch")
        # Reconstruct the approved global roles and database-level settings;
        # pg_restore replays the archived object ownership and privileges.
        with BOOTSTRAP_SQL.open("rb") as source:
            runner.call(sql + ["-v", "expected_database=" + database],
                        "restore database role policy", stdin=source)
        with archive.open("rb") as source:
            runner.call([*command, "pg_restore", "--exit-on-error", "--single-transaction",
                         *connection], "transactional backup restore", stdin=source, timeout=360)
        schema = runner.call([*command, "pg_dump", "--schema-only", "--create",
                              "--lock-wait-timeout=5s",
                              "--restrict-key=VellumReadinessSchemaFingerprintV1", *connection],
                             "restored schema fingerprint", timeout=45, limit=MAX_SCHEMA)
        require(b"-- PostgreSQL database dump complete" in schema
                and digest(schema) == profile["schema_sha256"], "restored schema differs from approved baseline")
        with INVARIANTS_SQL.open("rb") as source:
            runner.call(sql, "restored database invariant check", stdin=source, timeout=180)
        with RUNTIME_SQL.open("rb") as source:
            result = runner.call(sql, "restored backend runtime role policy", stdin=source)
        require(result.strip() == b"t", "restored runtime role policy differs")
        if candidate is not None:
            contract = runner.call(sql + ["-c",
                "SELECT contract_sha256 FROM public.vellum_schema_migrations WHERE version = 1"],
                "restored migration ledger contract").decode().strip()
            require(contract == candidate["migration_contract"],
                    "candidate changes the migration contract; reviewed maintenance required")
        password = secrets.token_hex(32)
        runner.call(sql + ["-c", "ALTER ROLE vellum_runtime PASSWORD '" + password + "'"],
                    "disposable backend password")
        result = smoke_backend(runner, profile, container, role, database, password, job, candidate)
        return result if candidate is not None else digest(schema)


def sign_receipt(runner, profile, evidence, work):
    message = evidence["issuer"].encode() + b"\n" + canonical(evidence)
    require(len(message) <= 4096, "worker receipt exceeds signing limit")
    path = work / "worker-message"
    path.write_bytes(message)
    key = profile["worker_key_id"]
    result = runner.aws(["kms", "sign", "--key-id", key, "--message-type", "RAW",
                         "--signing-algorithm", "ED25519_SHA_512", "--message", "fileb://" + str(path)],
                        "worker receipt signing")
    try:
        signature = base64.b64decode(result.get("Signature", ""), validate=True)
    except (TypeError, ValueError):
        raise RecoveryError("invalid worker signing response") from None
    require(result.get("KeyId") == key and result.get("SigningAlgorithm") == "ED25519_SHA_512"
            and len(signature) == 64, "invalid worker signing response")
    return {"evidence": evidence, "signature": signature.hex()}


def publish_receipt(runner, profile, kind, job, receipt, work):
    key = "production/recovery/" + kind + "/" + datetime.now(timezone.utc).strftime(
        "%Y%m%dT%H%M%SZ-") + job + ".json"
    target = ["--bucket", profile["receipt_bucket"], "--expected-bucket-owner", profile["account_id"]]
    region = runner.aws(["s3api", "get-bucket-location", *target], "receipt bucket region")
    require(region.get("LocationConstraint") == REGION, "unexpected receipt bucket region")
    path = work / "worker-receipt.json"
    encoded = canonical(receipt)
    path.write_bytes(encoded)
    checksum = base64.b64encode(hashlib.sha256(encoded).digest()).decode()
    args = [*target, "--key", key]
    published = runner.aws(["s3api", "put-object", *args, "--body", str(path), "--if-none-match", "*",
                            "--checksum-algorithm", "SHA256", "--checksum-sha256", checksum,
                            "--server-side-encryption", "AES256"],
                           "immutable worker receipt publication")
    version = published.get("VersionId")
    require(version_id(version) and published.get("ChecksumSHA256") == checksum,
            "invalid published worker receipt version or checksum")
    head = runner.aws(["s3api", "head-object", *args, "--version-id=" + version,
                       "--checksum-mode", "ENABLED"], "worker receipt readback")
    require(object_metadata(head, key, version, MAX_METADATA)["sha256"] == digest(encoded),
            "worker receipt readback differs")
    return key, version


def make_evidence(mode, profile, backup, refs, started, result=None, candidate=None):
    evidence = {
        "schema": 1, "issuer": RECEIPTS[mode][0],
        "backup": backup, "objects": refs, "profile_sha256": digest(canonical(profile)),
    }
    if mode == "drill":
        require(result == profile["schema_sha256"], "restore did not verify the approved schema")
        finished = timestamp()
        evidence.update({
            "started_at": timestamp(started), "finished_at": finished, "checks": CHECKS,
            "drill": {
                "archive_sha256": backup["archive_sha256"], "contract": backup["contract"],
                "source_sha256": backup["source_sha256"], "finished_at": finished,
                "postgres": {"image_id": profile["postgres_image_id"], "version": profile["postgres_version"]},
                "schema_sha256": result, "execution_sha256": profile["execution_sha256"],
                "runtime_sha256": profile["runtime_sha256"],
            },
        })
    if mode == "rehearse":
        evidence.update({
            "candidate_profile_sha256": digest(canonical(candidate)),
            "rehearsal": {
                **result, "archive_sha256": backup["archive_sha256"], "finished_at": timestamp(),
                "postgres": {"image_id": profile["postgres_image_id"], "version": profile["postgres_version"]},
            },
        })
    return evidence


def read_archive_proof(state, profile):
    try:
        proof = strict_json(limited_read(safe_file(state / "archive-proof.json")))
    except FileNotFoundError:
        return None
    require(fields(proof, "profile_sha256 archive"), "invalid private archive proof")
    return proof["archive"] if proof["profile_sha256"] == digest(canonical(profile)) else None


def write_state(state, job, mode, status, **details):
    # A durable start record makes abrupt termination distinguishable from success.
    value = {"job": job, "mode": mode, "status": status, "at": timestamp(), **details}
    write_private_json(state, job + ".json", value)


def write_private_json(state, filename, value):
    with tempfile.NamedTemporaryFile(mode="wb", dir=state, prefix=".pending-", delete=False) as out:
        pending = Path(out.name)
        try:
            out.write(canonical(value))
            out.flush()
            os.fsync(out.fileno())
            os.replace(pending, state / filename)
            sync_directory(state)
        finally:
            pending.unlink(missing_ok=True)



def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def recover_inflight(state, scratch):
    try:
        pending = strict_json(limited_read(safe_file(state / "inflight.json")))
    except FileNotFoundError:
        return
    require(fields(pending, "job mode") and matches(r"[0-9a-f]{32}", pending["job"])
            and pending["mode"] in RECEIPTS, "invalid pending worker job")
    job, mode = pending["job"], pending["mode"]
    runner = Runner(time.monotonic() + 60, scratch)
    if mode != "verify-backup":
        remove_sandbox(runner, "org.vellum.recovery.backend=" + job)
        remove_sandbox(runner, "org.vellum.recovery.job=" + job)
    for abandoned in scratch.glob("job-" + job + "-*"):
        require(abandoned.is_dir() and not abandoned.is_symlink(), "unsafe abandoned worker scratch")
        shutil.rmtree(abandoned)
    try:
        latest = strict_json(limited_read(safe_file(state / (job + ".json"))))
    except FileNotFoundError:
        latest = {}
    if latest.get("status") not in ("succeeded", "failed"):
        write_state(state, job, mode, "failed", reason="recovery job ended before final state",
                    alert_accepted=False)
    clear_inflight(state)


def clear_inflight(state):
    (state / "inflight.json").unlink()
    sync_directory(state)


def publish_health(profile, state, runner, now):
    def freshness(mode, maximum, field):
        try:
            result = strict_json(limited_read(safe_file(state / ("latest-" + mode + ".json"))))
            return int(result["profile_sha256"] == digest(canonical(profile))
                       and now - maximum < parse_time(result[field]) <= now
                       and (mode != "verify-backup"
                            or now - timedelta(hours=24) < parse_time(result["created_at"]) <= now))
        except (FileNotFoundError, RecoveryError, TypeError, KeyError):
            return 0

    metrics = [
        {"MetricName": "FreshBackupVerification", "Value": freshness("verify-backup", timedelta(minutes=10),
                                                                       "verified_at"), "Unit": "Count"},
        {"MetricName": "RestoreDrillBeforeExpiry", "Value": freshness("drill", timedelta(days=5),
                                                                    "finished_at"), "Unit": "Count"},
    ]
    runner.aws(["cloudwatch", "put-metric-data", "--namespace", "Vellum/Recovery",
                "--metric-data", json.dumps(metrics, separators=(",", ":"))], "recovery health heartbeat")


def send_alert(runner, profile, job, mode):
    result = runner.aws(["sns", "publish", "--topic-arn", profile["alert_topic"],
                         "--subject", "Vellum recovery worker failure",
                         "--message", f"Private Vellum recovery job {job} failed in {mode} mode."],
                        "recovery failure alert", timeout=15, cleanup=True)
    require(matches(r"[0-9a-f-]{36}", result.get("MessageId")), "recovery alert was not accepted")


def check_scratch(scratch):
    # Require the reviewed, bounded private tmpfs. Swap must be disabled or
    # encrypted on the isolated VM by the operator; the systemd mount uses noswap.
    require(os.path.ismount(scratch), "worker scratch requires its own bounded mount")
    require(len(Path("/proc/swaps").read_text().splitlines()) == 1,
            "worker VM swap must be disabled")
    mounts = Path("/proc/self/mountinfo").read_text().splitlines()
    require(any(left.split()[4] == str(scratch) and right.split()[0] == "tmpfs"
                for left, right in (line.split(" - ", 1) for line in mounts)), "worker scratch is not tmpfs")
    stats = os.statvfs(scratch)
    require(600 * 1024**2 <= stats.f_frsize * stats.f_blocks <= 1024**3,
            "worker scratch capacity is outside policy")


def execute(args, profile, public_key, scratch, state, candidate=None):
    job = secrets.token_hex(16)
    started = datetime.now(timezone.utc)
    runner = Runner(time.monotonic() + 900, scratch)
    write_private_json(state, "inflight.json", {"job": job, "mode": args.mode})
    write_state(state, job, args.mode, "running")
    try:
        with tempfile.TemporaryDirectory(prefix="job-" + job + "-", dir=scratch) as tmp:
            work = Path(tmp)
            store = BackupStore(runner, profile)
            store.check_region()
            key, version = ((args.completion_key, args.completion_version)
                            if args.completion_key else store.discover(started))
            # Timestamp after discovery; newly completed legitimate objects may
            # have appeared while the discovery calls were running.
            proof = read_archive_proof(state, profile) if args.mode == "verify-backup" else None
            backup, refs = verify_backup(store, runner, profile, key, version, public_key, work,
                                         datetime.now(timezone.utc), proof)
            if (work / "archive.dump").is_file():
                write_private_json(state, "archive-proof.json", {
                    "profile_sha256": digest(canonical(profile)), "archive": refs["archive"],
                })
            result = (run_restore(runner, profile, work / "archive.dump", job, candidate)
                      if args.mode != "verify-backup" else None)
            evidence = make_evidence(args.mode, profile, backup, refs, started, result, candidate)
            kind = RECEIPTS[args.mode][1]
            receipt = sign_receipt(runner, profile, evidence, work)
            receipt_key, receipt_version = publish_receipt(runner, profile, kind, job, receipt, work)
        if args.mode != "rehearse":
            projection = evidence["drill"] if args.mode == "drill" else backup
            write_private_json(state, "latest-" + args.mode + ".json",
                               {**projection, "profile_sha256": evidence["profile_sha256"]})
        write_state(state, job, args.mode, "succeeded", receipt_key=receipt_key,
                    receipt_version=receipt_version)
        clear_inflight(state)
        print(json.dumps({"status": "succeeded", "receipt_key": receipt_key,
                          "receipt_version": receipt_version}))
    except (RecoveryError, OSError, KeyError, TypeError, ValueError) as exc:
        message = str(exc) if isinstance(exc, RecoveryError) else "recovery worker operation failed"
        write_state(state, job, args.mode, "failed", reason=message, alert_accepted=False)
        try:
            send_alert(runner, profile, job, args.mode)
        except RecoveryError:
            raise RecoveryError("recovery failed and alert acceptance could not be verified") from None
        write_state(state, job, args.mode, "failed", reason=message, alert_accepted=True)
        raise RecoveryError(message) from None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=(*RECEIPTS, "clean-abandoned", "health", "harness-fingerprint"))
    parser.add_argument("--config", default="/etc/vellum-recovery/worker.json")
    parser.add_argument("--producer-key", default="/etc/vellum-recovery/producer-public.pem")
    parser.add_argument("--scratch", default="/run/vellum-recovery")
    parser.add_argument("--state", default="/var/lib/vellum-recovery")
    parser.add_argument("--completion-key")
    parser.add_argument("--completion-version")
    parser.add_argument("--candidate-profile")
    args = parser.parse_args()
    if args.mode == "harness-fingerprint":
        print(harness_fingerprint())
        return 0

    def interrupted(_signum, _frame):
        # A second signal must not interrupt the bounded cleanup of the first.
        signal.signal(signal.SIGINT, signal.SIG_IGN)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        raise RecoveryError("recovery job interrupted")

    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGTERM, interrupted)
    os.umask(0o077)
    try:
        require(os.geteuid() == 0, "worker requires its dedicated isolated VM")
        require(bool(args.completion_key) == bool(args.completion_version),
                "completion key and version must be provided together")
        scratch = safe_file(args.scratch, directory=True)
        state = safe_file(args.state, directory=True)
        check_scratch(scratch)
        if args.mode == "clean-abandoned":
            with (state / "worker.lock").open("ab") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                recover_inflight(state, scratch)
            return 0
        profile = strict_json(limited_read(safe_file(args.config)))
        validate_profile(profile)
        require(all(safe_file(path, private=False) for path in
                    (Path(__file__).resolve(), BOOTSTRAP_SQL, INVARIANTS_SQL, RUNTIME_SQL))
                and profile["harness_sha256"] == harness_fingerprint(), "worker harness differs from pinned profile")
        clock = Runner(time.monotonic() + 15, scratch)
        require(clock.call(["timedatectl", "show", "-p", "NTPSynchronized", "--value"],
                           "worker clock synchronisation").strip() == b"yes", "worker clock is unsynchronised")
        if args.mode == "health":
            publish_health(profile, state, clock, datetime.now(timezone.utc))
            return 0
        require(bool(args.candidate_profile) == (args.mode == "rehearse"),
                "a separately protected candidate profile is required only for rehearsal")
        candidate = None
        if args.mode == "rehearse":
            candidate = strict_json(limited_read(safe_file(args.candidate_profile)))
            validate_candidate(candidate, profile)
        public_key = safe_file(args.producer_key)
        require(digest(limited_read(public_key, 4096)) == profile["producer_key_sha256"],
                "wrong configured producer public key")
        der = clock.call(["openssl", "pkey", "-pubin", "-in", str(public_key), "-outform", "DER"],
                         "producer public key format")
        require(len(der) == 44 and der.startswith(bytes.fromhex("302a300506032b6570032100")),
                "producer public key must be Ed25519")
        with (state / "worker.lock").open("ab") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            recover_inflight(state, scratch)
            stats = os.statvfs(scratch)
            require(stats.f_frsize * stats.f_bavail >= 550 * 1024**2, "worker scratch is nearly full")
            execute(args, profile, public_key, scratch, state, candidate)
    except (RecoveryError, OSError, KeyError, TypeError, ValueError) as exc:
        print(str(exc) if isinstance(exc, RecoveryError) else "recovery worker startup failed",
              file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
