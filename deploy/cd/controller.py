#!/usr/bin/env python3
"""Operator-installed SSM controller; never execute it from a writable checkout."""

import base64
from datetime import datetime, timezone, timedelta
import fcntl
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile

BASE = Path("/usr/local/lib/vellum-cd")
STATE = Path("/var/lib/vellum-cd")
CHECKOUT = Path("/opt/hosting/projects/vellum")
REPOSITORY = "kacperkg/vellum"
ORIGINS = {
    "staging": "https://staging.vellum.kacpergajdarski.com",
    "production": "https://vellum.kacpergajdarski.com",
}


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
            str(CHECKOUT / "deploy" / (environment + ".env")),
            "-f",
            str(BASE / "compose.yml"),
            "-f",
            str(BASE / "compose.registry.yml"),
        ]

    def run(self, label, args, timeout=240):
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

    def apply(self, manifest):
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
        self.run(
            "Application update",
            self.compose
            + [
                "up",
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
        count = int(
            resolved["services"]["backend"]["environment"]["RATE_LIMIT_LOGIN_REQUESTS"]
        )
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
    with open(STATE / (environment + ".lock"), "a", encoding="utf-8") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError(
                "deployment already running; do not overlap manual and automated releases"
            ) from None
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
