#!/usr/bin/env python3
"""Operator-installed SSM controller; never execute it from a writable checkout."""

import base64
from datetime import datetime, timezone, timedelta
import hashlib
import json
import os
import re
import secrets
import socket
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import time

BASE = Path("/usr/local/lib/vellum-cd")
STATE = Path("/var/lib/vellum-cd")
ENVIRONMENT_DIRECTORY = Path("/etc/vellum-cd")
REPOSITORY = "kacperkg/vellum"
ORIGINS = {
    "staging": "https://staging.vellum.kacpergajdarski.com",
    "production": "https://vellum.kacpergajdarski.com",
}
TRUSTED_PROXY_CIDRS = "172.18.255.254/32"


# Validate library files before importing them: a safe parent directory alone
# does not prevent changes to the contents of a writable library file.
def validate_installation():
    for directory in (BASE, BASE / "cd"):
        info = directory.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("Unsafe controller installation directory")
    for relative in (
        "compose.yml",
        "compose.registry.yml",
        "cd/controller.py",
        "cd/coordination.py",
        "cd/release.py",
        "cd/smoke.py",
    ):
        info = (BASE / relative).lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise ValueError("Unsafe controller installation file")


# -I deliberately removes script/PYTHONPATH imports. Add only our verified
# installation directory after checking every local import's ownership/mode.
if __name__ == "__main__":
    if Path(__file__).resolve() != BASE / "cd/controller.py":
        raise SystemExit("Refuse execution from an application checkout")
    try:
        validate_installation()
    except ValueError as error:
        raise SystemExit(str(error)) from None
sys.path.insert(0, str(Path(__file__).resolve().parent))
from release import (  # noqa: E402 - import only after validating the installed directory
    BUNDLE,
    HEX,
    MAX_MANIFEST,
    bundle_hashes,
    canonical,
    read_json,
    require,
    validate_manifest,
)
from smoke import smoke  # noqa: E402
from coordination import PENDING, host_locks, operation, sync_state  # noqa: E402

MAX_ENCODED_MANIFEST_CHARS = 45000
# Deliberately fixed in reviewed source; extending this needs a new release.
PRELAUNCH_SUNSET = datetime(2026, 11, 4, tzinfo=timezone.utc)
PRELAUNCH_ACKNOWLEDGEMENT = "I_ACCEPT_PRELAUNCH_DATA_LOSS_WITHOUT_RESTORE_VERIFICATION"
PRELAUNCH_CLOSED = "production-prelaunch-closed.json"


def validate_approval_mode(environment, approval_mode):
    require(approval_mode in ("standard", "prelaunch", "automated"), "unknown production approval mode")
    require(environment == "production" or approval_mode == "standard",
            "nonstandard approval is production-only")


def decode_manifest_parameter(encoded):
    # Keep independent bounds even if the document is invoked outside SSM or
    # its native minChars/maxChars checks are absent on an older agent.
    require(
        isinstance(encoded, str) and 1 <= len(encoded) <= MAX_ENCODED_MANIFEST_CHARS,
        "manifest parameter must contain 1..45000 characters",
    )
    decoded = base64.b64decode(encoded, validate=True)
    require(len(decoded) <= MAX_MANIFEST, "decoded manifest exceeds size limit")
    return json.loads(decoded)


def protected(path, private=False):
    info = path.lstat()
    require(
        stat.S_ISREG(info.st_mode) and info.st_uid == 0,
        "controller assets must be root-owned regular files",
    )
    require(
        info.st_mode & (0o077 if private else 0o022) == 0,
        "unsafe controller asset permissions",
    )


def validate_environment_file(environment):
    require(environment in ORIGINS, "unknown deployment environment")
    path = ENVIRONMENT_DIRECTORY / (environment + ".env")
    require(path.is_absolute() and ".." not in path.parts, "invalid environment path")
    # Do not resolve(): following a symlink would hide an unsafe path component.
    # Root-owned, non-writable parents prevent unprivileged replacement between
    # these checks and Compose reopening the path. Root operators must serialize
    # edits with deployments; root/sudo/Docker access is outside this boundary.
    for directory in reversed(path.parents):
        info = directory.lstat()
        require(
            stat.S_ISDIR(info.st_mode)
            and info.st_uid == 0
            and info.st_mode & 0o022 == 0,
            "environment parents must be root-owned directories without group/world write",
        )
    info = path.lstat()
    require(
        stat.S_ISREG(info.st_mode)
        and info.st_uid == 0
        and stat.S_IMODE(info.st_mode) in (0o400, 0o600)
        and info.st_nlink == 1,
        "environment file must be root-owned, private, regular and singly linked",
    )
    return path


def validate_approval(approval, manifest, now=None, approval_mode="standard"):
    now = now or datetime.now(timezone.utc)
    validate_approval_mode("production", approval_mode)
    require(approval_mode != "automated", "automated evidence requires signature validation")
    fields = {
        "manifest_sha256", "verified_at", "backup_created_at", "backup_reference",
        "off_host_copy_verified", "restore_verified", "schema_compatible",
        "trusted_proxy_reviewed",
    }
    if approval_mode == "prelaunch":
        fields |= {"approval_mode", "no_public_users", "accept_data_loss",
                   "acknowledgement", "expires_at"}
    require(
        isinstance(approval, dict) and set(approval) == fields,
        "invalid operator approval record",
    )
    require(
        approval["manifest_sha256"] == hashlib.sha256(canonical(manifest)).hexdigest(),
        "backup approval does not cover this exact release",
    )
    verified_at = datetime.fromisoformat(approval["verified_at"].replace("Z", "+00:00"))
    require(
        verified_at.tzinfo is not None
        and timedelta(0) <= now - verified_at <= timedelta(hours=24),
        "verified backup approval must be less than 24 hours old",
    )
    # Checking an old archive today must not count as a recent backup.
    created_at = datetime.fromisoformat(
        approval["backup_created_at"].replace("Z", "+00:00")
    )
    require(
        created_at.tzinfo is not None
        and timedelta(0) <= now - created_at <= timedelta(hours=24),
        "backup must have been created within the last 24 hours",
    )
    require(created_at <= verified_at, "backup verification predates the backup")
    require(
        isinstance(approval["backup_reference"], str)
        and 1 <= len(approval["backup_reference"]) <= 256,
        "missing backup evidence",
    )
    for field in (
        "off_host_copy_verified",
        "schema_compatible",
        "trusted_proxy_reviewed",
    ):
        require(
            approval[field] is True,
            "production requires explicit backup, restore, schema and proxy review",
        )
    if approval_mode == "standard":
        require(approval["restore_verified"] is True,
                "production requires genuine restore verification")
        return
    require(approval["approval_mode"] == "prelaunch"
            and approval["restore_verified"] is False
            and approval["no_public_users"] is True
            and approval["accept_data_loss"] is True
            and approval["acknowledgement"] == PRELAUNCH_ACKNOWLEDGEMENT,
            "prelaunch requires explicit no-public-users and data-loss acknowledgement; restore remains unverified")
    expires_at = datetime.fromisoformat(approval["expires_at"].replace("Z", "+00:00"))
    require(expires_at.tzinfo is not None
            and now < expires_at <= min(verified_at + timedelta(hours=24), PRELAUNCH_SUNSET),
            "prelaunch approval expired or exceeds the fixed sunset/24-hour limit")


def close_prelaunch(approval, manifest, approval_mode):
    """Called under host locks; closure survives failure and is never reset here."""
    path = STATE / PRELAUNCH_CLOSED
    if approval_mode == "prelaunch":
        require(not os.path.lexists(path), "prelaunch exception already consumed or retired")
        require(not os.path.lexists(STATE / "production-current.json"),
                "prelaunch exception is only for the first managed production deployment")
    if not os.path.lexists(path):
        atomic_json(path, {
            "schema": 1,
            "reason": "consumed" if approval_mode == "prelaunch" else "standard-mode-retirement",
            "closed_at": datetime.now(timezone.utc).isoformat(),
            "manifest_sha256": hashlib.sha256(canonical(manifest)).hexdigest(),
            "approval": approval,
        })
        # File fsync alone cannot guarantee persistence of the directory entry.
        sync_state(STATE)


def atomic_json(path, value):
    fd, temporary = tempfile.mkstemp(prefix=".release-", dir=path.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(canonical(value))
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


READINESS_MAX_BYTES = 32768
READINESS_MUTATION_DEADLINE = 480  # Leave 420 seconds of the 900-second SSM budget.
READINESS_ISSUER = "vellum-production-recovery-v1"
READINESS_POLICY = "production-readiness-policy.json"


def strict_json(data):
    def unique_object(pairs):
        value = dict(pairs)
        require(len(value) == len(pairs), "duplicate production readiness fields")
        return value

    return json.loads(data, object_pairs_hook=unique_object)



def digest(data):
    return hashlib.sha256(data).hexdigest()


def hex_digest(value):
    return isinstance(value, str) and re.fullmatch(HEX, value) is not None


def private_file(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1
            and stat.S_IMODE(info.st_mode) in (0o400, 0o600),
            "unsafe production readiness file")
    return path


def request_readiness(request):
    path = STATE / "production-readiness.sock"
    try:
        info = path.lstat()
        require(stat.S_ISSOCK(info.st_mode) and info.st_uid == 0
                and stat.S_IMODE(info.st_mode) == 0o600,
                "unsafe production readiness socket")
        deadline = time.monotonic() + 420
        with socket.socket(socket.AF_UNIX) as connection:
            connection.settimeout(420)
            connection.connect(str(path))
            connection.sendall(canonical(request))
            connection.shutdown(socket.SHUT_WR)
            response = bytearray()
            while len(response) <= READINESS_MAX_BYTES:
                remaining = deadline - time.monotonic()
                require(remaining > 0, "production readiness broker timed out")
                connection.settimeout(remaining)
                chunk = connection.recv(4096)
                if not chunk:
                    break
                response.extend(chunk)
    except OSError:
        raise ValueError("production readiness broker unavailable or timed out") from None
    require(0 < len(response) <= READINESS_MAX_BYTES,
            "production readiness response missing or oversized")
    try:
        return strict_json(response)
    except (ValueError, UnicodeError):
        raise ValueError("invalid production readiness response") from None


def verify_readiness_signature(evidence, signature):
    require(isinstance(signature, str) and re.fullmatch(r"[0-9a-f]{128}", signature),
            "invalid production readiness signature encoding")
    key = private_file(STATE / "production-readiness-key.pem")
    with tempfile.TemporaryDirectory(prefix=".readiness-", dir=STATE) as temporary:
        message = Path(temporary) / "message"
        detached = Path(temporary) / "signature"
        message.write_bytes(b"vellum-production-readiness-v1\n" + canonical(evidence))
        detached.write_bytes(bytes.fromhex(signature))
        result = subprocess.run(
            ["/usr/bin/openssl", "pkeyutl", "-verify", "-rawin", "-pubin",
             "-inkey", str(key), "-in", str(message), "-sigfile", str(detached)],
            env={"PATH": "/usr/bin:/bin"}, stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL, timeout=5,
        )
    require(result.returncode == 0, "invalid production readiness signature")


def evidence_time(value):
    require(isinstance(value, str)
            and re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", value),
            "invalid production readiness timestamp")
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise ValueError("invalid production readiness timestamp") from None


def validate_readiness(envelope, request, now=None):
    now = now or datetime.now(timezone.utc)
    require(isinstance(envelope, dict) and set(envelope) == {"evidence", "signature"},
            "invalid production readiness envelope")
    evidence = envelope["evidence"]
    fields = {"schema", "issuer", "request_sha256", "issued_at", "expires_at",
              "backup", "drill", "rehearsal"}
    require(isinstance(evidence, dict) and set(evidence) == fields,
            "invalid production readiness evidence")
    verify_readiness_signature(evidence, envelope["signature"])
    require(type(evidence["schema"]) is int and evidence["schema"] == 1
            and evidence["issuer"] == READINESS_ISSUER
            and evidence["request_sha256"] == digest(canonical(request)),
            "production evidence does not cover this invocation, release and live baseline")
    issued = evidence_time(evidence["issued_at"])
    expires = evidence_time(evidence["expires_at"])
    require(issued <= now < expires <= issued + timedelta(minutes=10),
            "production readiness evidence is stale or future-dated")

    live = request["live"]
    backup = evidence["backup"]
    require(isinstance(backup, dict)
            and set(backup) == {"archive_sha256", "completion_sha256", "manifest_sha256",
                               "archive_bytes", "contract", "source_sha256", "created_at", "verified_at"}
            and all(hex_digest(backup[key]) for key in
                    ("archive_sha256", "completion_sha256", "manifest_sha256"))
            and type(backup["archive_bytes"]) is int and 0 < backup["archive_bytes"] <= 512 * 1024**2
            and backup["contract"] == live["backup_contract"]
            and backup["source_sha256"] == live["database_source_sha256"],
            "invalid completed off-host backup evidence")
    created, verified = (evidence_time(backup[key]) for key in ("created_at", "verified_at"))
    require(now - timedelta(hours=24) < created <= verified <= issued
            and issued - verified <= timedelta(minutes=10),
            "production backup is stale or was not verified for this readiness request")

    drill = evidence["drill"]
    require(isinstance(drill, dict)
            and set(drill) == {"archive_sha256", "contract", "postgres", "finished_at",
                              "schema_sha256", "execution_sha256", "runtime_sha256", "source_sha256"}
            and all(hex_digest(drill[key]) for key in ("archive_sha256", "schema_sha256", "execution_sha256", "runtime_sha256"))
            and drill["source_sha256"] == live["database_source_sha256"]
            and drill["contract"] == live["backup_contract"]
            and drill["postgres"] == live["postgres"],
            "missing or incompatible isolated restore drill evidence")
    drill_finished = evidence_time(drill["finished_at"])
    require(now - timedelta(days=7) < drill_finished <= issued,
            "isolated restore drill expired")

    rehearsal = evidence["rehearsal"]
    if live["risk"] == "low":
        require(rehearsal is None
                and drill["schema_sha256"] == live["schema_sha256"]
                and drill["execution_sha256"] == live["execution_sha256"]
                and drill["runtime_sha256"] == live["runtime_sha256"],
                "weekly drill does not cover the live schema and backend startup")
        return
    require(isinstance(rehearsal, dict)
            and set(rehearsal) == {"archive_sha256", "postgres", "candidate_backend",
                                   "schema_before", "schema_after", "data_before", "data_after",
                                   "finished_at"}
            and rehearsal["archive_sha256"] == backup["archive_sha256"]
            and rehearsal["postgres"] == live["postgres"]
            and rehearsal["candidate_backend"] == request["manifest"]["images"]["backend"]
            and rehearsal["schema_before"] == live["schema_sha256"]
            and hex_digest(rehearsal["data_before"]),
            "missing or mismatched candidate migration rehearsal")
    require(verified <= evidence_time(rehearsal["finished_at"]) <= issued,
            "candidate rehearsal does not follow backup verification")
    require(rehearsal["schema_after"] == rehearsal["schema_before"]
            and rehearsal["data_after"] == rehearsal["data_before"],
            "candidate changes schema or retained data; separate reviewed maintenance required")


class Commands:
    def __init__(self, environment, manifest, approval_mode="standard"):
        require(environment in ORIGINS, "unknown deployment environment")
        validate_approval_mode(environment, approval_mode)
        self.environment = environment
        self.approval_mode = approval_mode
        self.project = "vellum-" + environment
        self.env = {
            "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
            "HOME": "/root",
            # Dedicated, root-managed Docker credential helpers may be installed
            # here for private GHCR. No other project's login state is inherited.
            "DOCKER_CONFIG": "/etc/vellum-cd/docker",
            "IMAGE_PREFIX": "vellum",
            "IMAGE_TAG": manifest["source_sha"],
            "VELLUM_BACKEND_IMAGE": manifest["images"]["backend"],
            "VELLUM_FRONTEND_IMAGE": manifest["images"]["frontend"],
        }
        self.compose = [
            "docker",
            "compose",
            "--project-name",
            self.project,
            "--env-file",
            str(ENVIRONMENT_DIRECTORY / (environment + ".env")),
            "-f",
            str(BASE / "compose.yml"),
            "-f",
            str(BASE / "compose.registry.yml"),
        ]

    def run(self, label, args, timeout=None):
        if timeout is None:
            timeout = 20 if self.approval_mode == "automated" else 240
        if args[: len(self.compose)] == self.compose:
            validate_environment_file(self.environment)
        result = subprocess.run(
            args,
            env=self.env,
            cwd=BASE,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        # Raw stderr may contain credentials, SQL/financial data or resolved env.
        require(
            result.returncode == 0, label + " failed; inspect sensitive logs privately"
        )
        return result.stdout

    def backend_ids(self):
        output = self.run(
            f"{self.environment} backend inventory",
            ["docker", "ps", "--all", "--no-trunc", "--filter",
             f"label=com.docker.compose.project={self.project}", "--filter",
             "label=com.docker.compose.service=backend", "--format", "{{.ID}}"],
            timeout=15,
        )
        ids = output.split()
        require(len(ids) <= 64 and len(ids) == len(set(ids))
                and all(re.fullmatch(r"[0-9a-f]{64}", item) for item in ids),
                f"invalid {self.environment} backend inventory; operator recovery required")
        return sorted(ids)

    def inspect_backend(self, container):
        # Inspect only identity/state, never environment variables or app logs.
        template = ('{"id":{{json .Id}},"project":'
                    '{{json (index .Config.Labels "com.docker.compose.project")}},'
                    '"service":{{json (index .Config.Labels "com.docker.compose.service")}},'
                    '"oneoff":{{json (index .Config.Labels "com.docker.compose.oneoff")}},'
                    '"image":{{json .Config.Image}},"state":{{json .State}}}')
        info = json.loads(self.run(
            f"{self.environment} backend state",
            ["docker", "container", "inspect", "--format", template, container],
            timeout=15,
        ))
        require(isinstance(info, dict) and info.get("id") == container
                and info.get("project") == self.project
                and info.get("service") == "backend"
                and isinstance(info.get("state"), dict),
                f"unexpected {self.environment} backend identity; operator recovery required")
        return info

    def stop_backend(self):
        # All old instances, including one-offs and stopped/restarting replicas,
        # must lose their restart path before AutoMigrate/new auth code runs.
        ids = self.backend_ids()
        for container in ids:
            info = self.inspect_backend(container)
            state = info["state"]
            require(state.get("Status") in ("created", "running", "restarting", "exited")
                    and state.get("Paused") is False and state.get("Dead") is False,
                    f"ambiguous {self.environment} backend state; operator recovery required")
        if ids:
            self.run(f"{self.environment} backend stop",
                     ["docker", "container", "stop", "--time", "30", *ids], timeout=60)
            for container in ids:
                state = self.inspect_backend(container)["state"]
                require(state.get("Status") in ("created", "exited")
                        and state.get("Running") is False
                        and state.get("Restarting") is False
                        and state.get("Paused") is False and state.get("Dead") is False
                        and state.get("Pid") == 0,
                        f"old {self.environment} backend has not terminated; operator recovery required")
            # No --force and no --volumes: Engine must refuse a running target.
            # Images, receipts, PostgreSQL and every volume remain intact.
            self.run(f"Stopped {self.environment} backend removal",
                     ["docker", "container", "rm", *ids], timeout=30)
        require(not self.backend_ids(),
                f"{self.environment} backend inventory changed; operator recovery required")

    def wait_database(self):
        # An already submitted PostgreSQL statement can outlive its client.
        # Count only the target database's client sessions; never kill sessions
        # or inspect query text. Maintenance clients must also have disconnected.
        query = ("SELECT count(*) FROM pg_stat_activity "
                 "WHERE datname = current_database() AND pid <> pg_backend_pid() "
                 "AND backend_type = 'client backend'")
        script = ('exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" '
                  '-d "$POSTGRES_DB" -Atqc "$1"')
        deadline = time.monotonic() + 15
        while True:
            result = self.run(
                f"{self.environment} database session drain",
                self.compose + ["exec", "-T", "postgres", "sh", "-ec", script,
                                f"{self.environment}-session-check", query], timeout=5,
            ).strip()
            require(re.fullmatch(r"0|[1-9][0-9]*", result),
                    f"invalid {self.environment} database session state")
            if result == "0":
                return
            require(time.monotonic() < deadline,
                    f"{self.environment} database sessions remain; operator recovery required")
            time.sleep(0.25)

    def verify_backend(self, manifest):
        ids = self.backend_ids()
        require(len(ids) == 1, f"{self.environment} requires one backend; operator recovery required")
        info = self.inspect_backend(ids[0])
        state = info["state"]
        require(info.get("oneoff") == "False"
                and info.get("image") == manifest["images"]["backend"]
                and state.get("Status") == "running" and state.get("Running") is True
                and state.get("Restarting") is False and state.get("Paused") is False
                and state.get("Dead") is False and type(state.get("Pid")) is int
                and state["Pid"] > 0 and state.get("Health", {}).get("Status") == "healthy",
                f"unexpected {self.environment} backend after startup; operator recovery required")

    def prepare(self, manifest):
        validate_environment_file(self.environment)
        self.run("Compose validation", self.compose + ["config", "--quiet"])
        resolved = json.loads(
            self.run(
                "Compose inspection", self.compose + ["config", "--format", "json"]
            )
        )
        require(resolved["name"] == self.project, "wrong Compose project")
        for service in ("backend", "frontend"):
            require(
                resolved["services"][service]["image"] == manifest["images"][service],
                "Compose image override mismatch",
            )
            require(
                not resolved["services"][service].get("ports"),
                "backend/frontend ports must remain private",
            )
        require(
            resolved["services"]["backend"]["environment"]["CORS_ALLOWED_ORIGINS"]
            == ORIGINS[self.environment],
            "unexpected application origin",
        )
        backend_env = resolved["services"]["backend"]["environment"]
        if self.approval_mode == "prelaunch":
            require(backend_env.get("APP_ENV") == "production"
                    and backend_env.get("PASSWORD_RESET_EMAIL_MODE") == "disabled",
                    "prelaunch requires production runtime with SES sending disabled")
        require(
            backend_env.get("TRUSTED_PROXY_CIDRS") == TRUSTED_PROXY_CIDRS,
            "trusted proxy must match the reviewed fixed Caddy address",
        )
        count = int(backend_env["RATE_LIMIT_LOGIN_REQUESTS"])
        require(
            self.environment != "staging" or 1 <= count <= 100,
            "automated staging smoke supports login limits 1..100",
        )
        existing = self.run(
            "PostgreSQL identity check",
            [
                "docker",
                "ps",
                "--filter",
                f"label=com.docker.compose.project={self.project}",
                "--filter",
                "label=com.docker.compose.service=postgres",
                "--format",
                "{{.ID}}",
            ],
        )
        require(
            len(existing.split()) == 1,
            "initialize one PostgreSQL instance manually before enabling CD",
        )
        self.run(
            "PostgreSQL readiness",
            self.compose
            + [
                "exec",
                "-T",
                "postgres",
                "sh",
                "-ec",
                'exec pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"',
            ],
        )
        # Pull and inspect BOTH immutable images before touching either service.
        self.run(
            "Image pull", self.compose + ["pull", "backend", "frontend"],
            timeout=120 if self.approval_mode == "automated" else 600
        )
        for reference in manifest["images"].values():
            metadata = json.loads(
                self.run(
                    "Image platform check",
                    ["docker", "image", "inspect", reference, "--format", "{{json .}}"],
                )
            )
            require(
                metadata["Architecture"] == "amd64" and metadata["Os"] == "linux",
                "release must contain linux/amd64 images",
            )
            require(
                metadata.get("Config", {})
                .get("Labels", {})
                .get("org.opencontainers.image.revision")
                == manifest["source_sha"],
                "image revision label mismatch",
            )
        return count, backend_env

    def apply(self, manifest):
        count, _ = self.prepare(manifest)
        self.replace(manifest, count)

    def replace(self, manifest, count, expected_postgres=None):
        self.stop_backend()
        self.wait_database()
        if expected_postgres is not None:
            require(self.postgres_baseline() == expected_postgres,
                    "production PostgreSQL changed after writer drain; operator recovery required")
        self.run(
            "Application update",
            self.compose
            + ["up", "--scale", "backend=1"]
            + [
                "-d",
                "--no-deps",
                "--no-build",
                "--pull",
                "never",
                "--wait",
                "--wait-timeout",
                "180",
                "backend",
                "frontend",
            ],
            timeout=210,
        )
        self.verify_backend(manifest)
        smoke(
            ORIGINS[self.environment],
            staging=self.environment == "staging",
            login_requests=count,
        )

    def readiness_baseline(self, manifest, expected_env):
        policy = strict_json(private_file(STATE / READINESS_POLICY).read_bytes())
        require(isinstance(policy, dict)
                and set(policy) == {"backup_contract", "caddy_container", "caddy_image",
                                    "caddy_config_sha256"}
                and all(hex_digest(policy[key]) for key in
                        ("backup_contract", "caddy_config_sha256"))
                and isinstance(policy["caddy_image"], str)
                and re.fullmatch("sha256:" + HEX, policy["caddy_image"])
                and isinstance(policy["caddy_container"], str)
                and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}", policy["caddy_container"]),
                "invalid production readiness policy")
        require(self.run("Clock synchronisation",
                         ["/usr/bin/timedatectl", "show", "-p", "NTPSynchronized", "--value"],
                         timeout=5).strip() == "yes", "production clock is not synchronised")
        receipt = read_json(private_file(STATE / "production-current.json"))
        validate_manifest(receipt, REPOSITORY, verified=True)
        self.verify_backend(receipt)
        raw_services = self.run(
            "Production service health", self.compose + ["ps", "--all", "--no-trunc", "--format", "json"], timeout=15,
        ).strip()
        services = (json.loads(raw_services) if raw_services.startswith("[")
                    else [json.loads(line) for line in raw_services.splitlines()])
        require(isinstance(services, list), "unexpected production Compose inventory")
        service_by_name = {service["Service"]: service for service in services}
        require(len(services) == len(service_by_name)
                and set(service_by_name) == {"backend", "frontend", "postgres"}
                and all(service.get("State") == "running" and service.get("Health") == "healthy"
                        for service in services),
                "production pre-state must contain three healthy managed services")
        frontend = self.inspect_config(service_by_name["frontend"]["ID"])
        require(frontend["reference"] == receipt["images"]["frontend"]
                and frontend["image"] == self.image_config(receipt["images"]["frontend"])["Id"],
                "running frontend does not match the production receipt")

        backend_id = service_by_name["backend"]["ID"]
        running = self.inspect_config(backend_id)
        current = self.image_config(receipt["images"]["backend"])
        require(running["image"] == current["Id"]
                and running["readonly"] is True and running["privileged"] is False
                and running["network_mode"] not in ("host", "none", "bridge")
                and all(mount.get("Type") == "tmpfs" and mount.get("Destination") == "/tmp"
                        for mount in running["mounts"]),
                "running backend image or container isolation differs from reviewed production")
        candidate = self.image_config(manifest["images"]["backend"])
        expected = dict(item.split("=", 1) for item in current["Config"].get("Env") or [])
        expected.update(expected_env)
        # Require resolved settings and the actual container's startup contract
        # to agree. Secret values are compared in memory and never sent.
        require(dict(item.split("=", 1) for item in running["env"]) == expected
                and all(running[field] == current["Config"].get(field)
                        for field in ("Entrypoint", "Cmd", "User", "WorkingDir")),
                "running backend settings differ from reviewed production configuration")
        require(self.resolved_backend_env() == expected_env,
                "production settings changed during readiness; dispatch again")
        execution = self.execution_fingerprint(current)
        risk = "low" if (
            execution == self.execution_fingerprint(candidate)
            and receipt["deployment_files"] == manifest["deployment_files"]
        ) else "migration"

        postgres = self.inspect_config(service_by_name["postgres"]["ID"])
        pg_env = dict(item.split("=", 1) for item in postgres["env"])
        require(expected_env.get("DB_HOST") == "postgres" and expected_env.get("DB_PORT") == "5432"
                and pg_env.get("POSTGRES_DB") == expected_env.get("DB_NAME")
                and pg_env.get("POSTGRES_USER") == expected_env.get("DB_USER")
                and pg_env.get("POSTGRES_PASSWORD") == expected_env.get("DB_PASSWORD"),
                "running PostgreSQL and backend database settings differ")
        version, source, schema = self.postgres_baseline()
        proxy = self.proxy_baseline(policy)
        return {
            "current_manifest_sha256": digest(canonical(receipt)),
            "postgres": {"image_id": postgres["image"], "version": version},
            "schema_sha256": schema,
            "database_source_sha256": source,
            "proxy_sha256": proxy,
            "backup_contract": policy["backup_contract"],
            "execution_sha256": execution,
            "runtime_sha256": digest(canonical({
                name: value for name, value in expected_env.items()
                if name not in ("DB_PASSWORD", "JWT_SECRET")
            })),
            "risk": risk,
        }

    def resolved_backend_env(self):
        return json.loads(self.run(
            "Production settings recheck",
            self.compose + ["config", "--format", "json"], timeout=15,
        ))["services"]["backend"]["environment"]

    def inspect_config(self, target):
        template = ('{"image":{{json .Image}},"reference":{{json .Config.Image}},'
                    '"env":{{json .Config.Env}},'
                    '"Entrypoint":{{json .Config.Entrypoint}},"Cmd":{{json .Config.Cmd}},'
                    '"User":{{json .Config.User}},"WorkingDir":{{json .Config.WorkingDir}},'
                    '"readonly":{{json .HostConfig.ReadonlyRootfs}},'
                    '"privileged":{{json .HostConfig.Privileged}},'
                    '"network_mode":{{json .HostConfig.NetworkMode}},"mounts":{{json .Mounts}}}')
        info = json.loads(self.run(
            "Production container identity",
            ["docker", "container", "inspect", "--format", template, target], timeout=15,
        ))
        require(re.fullmatch("sha256:" + HEX, info["image"]), "invalid running image identity")
        return info

    def image_config(self, target):
        return json.loads(self.run(
            "Backend execution fingerprint",
            ["docker", "image", "inspect", "--format", "{{json .}}", target], timeout=15,
        ))

    @staticmethod
    def execution_fingerprint(image):
        config = dict(image["Config"])
        labels = dict(config.get("Labels") or {})
        labels.pop("org.opencontainers.image.revision", None)
        config["Labels"] = labels
        rootfs = image["RootFS"]
        require(rootfs.get("Type") == "layers" and isinstance(rootfs.get("Layers"), list)
                and rootfs["Layers"]
                and all(re.fullmatch("sha256:" + HEX, layer) for layer in rootfs["Layers"]),
                "cannot establish backend filesystem identity")
        return digest(canonical({
            "architecture": image["Architecture"], "os": image["Os"],
            "rootfs": rootfs, "config": config,
        }))

    def postgres_baseline(self):
        postgres = self.compose + ["exec", "-T", "postgres", "sh", "-ec"]
        query = ("SELECT current_setting('server_version_num'), system_identifier, "
                 "(SELECT oid FROM pg_database WHERE datname = current_database()) "
                 "FROM pg_control_system()")
        identity = self.run(
            "Production PostgreSQL identity",
            postgres + ['exec psql -X -v ON_ERROR_STOP=1 -Atqc "$1" '
                        '-U "$POSTGRES_USER" -d "$POSTGRES_DB"', "vellum-readiness", query],
            timeout=10,
        ).strip()
        require(re.fullmatch(r"17[0-9]{4}\|[0-9]{1,20}\|[0-9]{1,10}", identity),
                "production readiness requires a PostgreSQL 17 source identity")
        version, system_id, database_oid = identity.split("|")
        source = digest(canonical({"system_id": system_id, "database_oid": database_oid}))
        schema = self.run(
            "Production PostgreSQL schema fingerprint",
            postgres + ['exec pg_dump --schema-only --create --lock-wait-timeout=5s '
                        '--restrict-key=VellumReadinessSchemaFingerprintV1 '
                        '-U "$POSTGRES_USER" -d "$POSTGRES_DB"'], timeout=20,
        )
        require(len(schema.encode()) <= 1024 * 1024
                and "-- PostgreSQL database dump complete" in schema,
                "unsupported production schema fingerprint")
        # pg_dump uses the fixed restriction key only for reproducible hashing.
        # This plain-text dump is held in memory and never executed or logged.
        return version, source, digest(schema.encode())

    def proxy_baseline(self, policy):
        target = policy["caddy_container"]
        info = json.loads(self.run(
            "Trusted proxy identity",
            ["docker", "container", "inspect", "--format",
             '{"id":{{json .Id}},"image":{{json .Image}},'
             '"web":{{json (index .NetworkSettings.Networks "web")}}}', target],
            timeout=10,
        ))
        require(info["image"] == policy["caddy_image"]
                and (info["web"] or {}).get("IPAddress") == TRUSTED_PROXY_CIDRS.removesuffix("/32")
                and re.fullmatch(HEX, info["id"]),
                "trusted proxy image or network identity differs from reviewed production proxy")
        config = self.run(
            "Trusted proxy active configuration",
            ["docker", "exec", info["id"], "wget", "-qO-", "http://127.0.0.1:2019/config/"],
            timeout=5,
        )
        require(len(config.encode()) <= 1024 * 1024, "unsupported trusted proxy configuration")
        fingerprint = digest(canonical(json.loads(config)))
        require(fingerprint == policy["caddy_config_sha256"],
                "active proxy configuration requires separate operator review")
        return fingerprint

    def diagnostics(self):
        # Only labels/service state; do not emit env, inspect output or app logs.
        try:
            print(
                self.run(
                    "Service state",
                    [
                        "docker",
                        "ps",
                        "-a",
                        "--filter",
                        f"label=com.docker.compose.project={self.project}",
                        "--format",
                        '{{.Label "com.docker.compose.service"}} {{.State}} {{.Status}}',
                    ],
                ),
                end="",
            )
        except (ValueError, subprocess.SubprocessError):
            print("Service diagnostics unavailable", file=sys.stderr)


def deploy(environment, manifest, commands=None, approval_mode="standard"):
    started = time.monotonic()
    require(environment in ORIGINS, "unknown deployment environment")
    validate_approval_mode(environment, approval_mode)
    validate_manifest(manifest, REPOSITORY, verified=environment == "production")
    require(
        bundle_hashes(BASE) == manifest["deployment_files"],
        "installed deployment controller/Compose differs from reviewed release; operator update required",
    )
    if environment == "production" and approval_mode != "automated":
        require(not os.path.lexists(STATE / READINESS_POLICY),
                "automated readiness is installed; standard approval requires operator recovery")
        approval_path = STATE / "production-approval.json"
        protected(approval_path, private=True)
        validate_approval(read_json(approval_path), manifest, approval_mode=approval_mode)
    commands = commands or Commands(environment, manifest, approval_mode=approval_mode)
    preflight = None
    if environment == "production" and approval_mode != "automated":
        def preflight():
            require(not os.path.lexists(STATE / READINESS_POLICY),
                    "automated readiness is installed; standard approval requires operator recovery")
    if approval_mode == "automated":
        with host_locks(STATE):
            require(not os.path.lexists(STATE / PENDING),
                    "unfinished host operation; operator recovery required")
            count, expected_env = commands.prepare(manifest)
            live = commands.readiness_baseline(manifest, expected_env)
        require(time.monotonic() < started + READINESS_MUTATION_DEADLINE,
                "production readiness exceeded its preflight budget; dispatch again")
        request = {
            "schema": 1,
            "nonce": secrets.token_hex(32),
            "manifest": manifest,
            "live": live,
        }
        evidence = request_readiness(request)

        def preflight():
            require(commands.readiness_baseline(manifest, expected_env) == live,
                    "production baseline changed during readiness; dispatch again")
            validate_readiness(evidence, request)
            require(live["risk"] == "low",
                    "backend or bundle changed; separate reviewed migration approval required")
            require(time.monotonic() < started + READINESS_MUTATION_DEADLINE,
                    "production readiness exceeded its preflight budget; dispatch again")

    operation_options = {"preflight": preflight} if preflight is not None else {}
    with operation(STATE, "deployment", environment, **operation_options):
        if approval_mode == "automated":
            # The challenge is generated by this invocation and never accepted
            # as input. A retry needs new evidence. Existing approvals/closure
            # are left intact.
            atomic_json(STATE / f"production-readiness-{request['nonce']}.json", evidence)
            sync_state(STATE)
        elif environment == "production":
            # Re-read/freshness-check after locking; do not consume a pending operation.
            protected(approval_path, private=True)
            approval = read_json(approval_path)
            validate_approval(approval, manifest, approval_mode=approval_mode)
            close_prelaunch(approval, manifest, approval_mode)
            if approval_mode == "prelaunch":
                print("production: PRELAUNCH data-loss exception; restore verification NOT performed; exception consumed")
        try:
            if approval_mode == "automated":
                commands.replace(manifest, count, (
                    live["postgres"]["version"], live["database_source_sha256"], live["schema_sha256"],
                ))
            else:
                commands.apply(manifest)
        except Exception:
            commands.diagnostics()
            raise
        current = STATE / (environment + "-current.json")
        previous = STATE / (environment + "-previous.json")
        if current.exists():
            last_successful = read_json(current)
            if canonical(last_successful) != canonical(manifest):
                atomic_json(previous, last_successful)
        atomic_json(current, manifest)
        # Retain per-release receipts for explicit rollback, including staging smoke
        # evidence. A workflow artifact expiring does not delete locally tested data.
        atomic_json(
            STATE / f"{environment}-{manifest['run_id']}-{manifest['run_attempt']}.json",
            manifest,
        )
        print(
            f"{environment}: healthy deployment, release {manifest['run_id']}/{manifest['run_attempt']}"
        )


def main():
    require(
        os.geteuid() == 0, "controller must run through the installed operator/SSM path"
    )
    require(len(sys.argv) == 2 and sys.argv[1] in ORIGINS, "fixed environment required")
    environment = sys.argv[1]
    require(
        Path(__file__).resolve() == BASE / "cd/controller.py",
        "refuse execution from an application checkout",
    )
    for name in BUNDLE:
        protected(BASE / name)
    require(
        STATE.is_dir()
        and STATE.stat().st_uid == 0
        and STATE.stat().st_mode & 0o077 == 0,
        "unsafe deployment state directory",
    )
    manifest = decode_manifest_parameter(os.environ["SSM_Manifest"])
    deploy(environment, manifest, approval_mode=os.environ.get("SSM_ApprovalMode", "standard"))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        detail = str(error) if type(error) is ValueError else type(error).__name__
        print(
            f"Deployment failed: {detail}; no automatic rollback (AutoMigrate may have changed schema)",
            file=sys.stderr,
        )
        sys.exit(1)
