#!/usr/bin/env python3
"""Operator-installed SSM controller; never execute it from a writable checkout."""

import base64
from datetime import datetime, timezone, timedelta
import hashlib
import json
import os
import re
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
    MAX_MANIFEST,
    bundle_hashes,
    canonical,
    read_json,
    require,
    validate_manifest,
)
from smoke import smoke  # noqa: E402
from coordination import operation  # noqa: E402

MAX_ENCODED_MANIFEST_CHARS = 45000


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


def validate_approval(approval, manifest, now=None):
    now = now or datetime.now(timezone.utc)
    require(
        isinstance(approval, dict)
        and set(approval)
        == {
            "manifest_sha256",
            "verified_at",
            "backup_created_at",
            "backup_reference",
            "off_host_copy_verified",
            "restore_verified",
            "schema_compatible",
            "trusted_proxy_reviewed",
        },
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
        "restore_verified",
        "schema_compatible",
        "trusted_proxy_reviewed",
    ):
        require(
            approval[field] is True,
            "production requires explicit backup, restore, schema and proxy review",
        )


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


class Commands:
    def __init__(self, environment, manifest):
        require(environment in ORIGINS, "unknown deployment environment")
        self.environment = environment
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

    def run(self, label, args, timeout=240):
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

    def apply(self, manifest):
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
            "Image pull", self.compose + ["pull", "backend", "frontend"], timeout=600
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
        self.stop_backend()
        self.wait_database()
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


def deploy(environment, manifest, commands=None):
    require(environment in ORIGINS, "unknown deployment environment")
    validate_manifest(manifest, REPOSITORY, verified=environment == "production")
    require(
        bundle_hashes(BASE) == manifest["deployment_files"],
        "installed deployment controller/Compose differs from reviewed release; operator update required",
    )
    if environment == "production":
        approval_path = STATE / "production-approval.json"
        protected(approval_path, private=True)
        validate_approval(read_json(approval_path), manifest)
    commands = commands or Commands(environment, manifest)
    with operation(STATE, "deployment", environment):
        try:
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
    deploy(environment, manifest)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        detail = str(error) if isinstance(error, ValueError) else type(error).__name__
        print(
            f"Deployment failed: {detail}; no automatic rollback (AutoMigrate may have changed schema)",
            file=sys.stderr,
        )
        sys.exit(1)
