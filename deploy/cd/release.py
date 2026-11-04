#!/usr/bin/env python3
"""Data-only release manifests and verified GitHub artifact promotion (stdlib)."""

import argparse
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.parse
import urllib.request
import uuid
import zipfile

HEX = r"[0-9a-f]{64}"
SHA = r"[0-9a-f]{40}"
REPO = r"[a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*"
BUNDLE = (
    "compose.yml",
    "compose.registry.yml",
    "cd/controller.py",
    "cd/coordination.py",
    "cd/release.py",
    "cd/smoke.py",
)
SOURCE_PATHS = (
    "backend",
    "frontend",
    "deploy/compose.yml",
    "deploy/compose.registry.yml",
    "deploy/cd",
)
WORKFLOW = ".github/workflows/release-staging.yml"
MAX_MANIFEST = 32768


def require(condition, message):
    if not condition:
        raise ValueError(message)


def positive(value):
    return type(value) is int and 0 < value < 10**18


def read_json(path):
    with Path(path).open("rb") as source:
        data = source.read(MAX_MANIFEST + 1)
    require(len(data) <= MAX_MANIFEST, "JSON input exceeds size limit")
    return json.loads(data)


def canonical(value):
    return (json.dumps(value, sort_keys=True, indent=2) + "\n").encode()


def validate_manifest(value, repository, verified=False):
    require(isinstance(value, dict), "manifest must be an object")
    require(
        set(value)
        == {
            "schema",
            "repository",
            "source_sha",
            "run_id",
            "run_attempt",
            "images",
            "deployment_files",
            "staging",
        },
        "unexpected manifest fields",
    )
    require(
        type(value["schema"]) is int and value["schema"] == 1,
        "unsupported manifest schema",
    )
    require(
        re.fullmatch(REPO, repository) and value["repository"] == repository,
        "repository mismatch",
    )
    require(
        isinstance(value["source_sha"], str) and re.fullmatch(SHA, value["source_sha"]),
        "invalid source revision",
    )
    require(
        positive(value["run_id"]) and positive(value["run_attempt"]),
        "invalid workflow identity",
    )
    require(
        isinstance(value["images"], dict)
        and set(value["images"]) == {"backend", "frontend"},
        "invalid image set",
    )
    for component, reference in value["images"].items():
        require(
            isinstance(reference, str)
            and re.fullmatch(
                re.escape(f"ghcr.io/{repository}-{component}@sha256:") + HEX, reference
            ),
            "unapproved image reference",
        )
    require(
        isinstance(value["deployment_files"], dict)
        and set(value["deployment_files"]) == set(BUNDLE),
        "invalid deployment bundle",
    )
    require(
        all(
            isinstance(v, str) and re.fullmatch(HEX, v)
            for v in value["deployment_files"].values()
        ),
        "invalid deployment file hash",
    )
    staging = value["staging"]
    require(
        isinstance(staging, dict) and set(staging) == {"verified", "command_id"},
        "invalid staging result",
    )
    require(type(staging["verified"]) is bool, "invalid staging verification flag")
    if staging["verified"]:
        require(
            isinstance(staging["command_id"], str)
            and str(uuid.UUID(staging["command_id"])) == staging["command_id"],
            "invalid staging invocation ID",
        )
    else:
        require(staging["command_id"] is None, "unverified staging command")
    if verified:
        require(staging["verified"], "release has not passed staging")
    return value


def bundle_hashes(deploy):
    return {
        name: hashlib.sha256((Path(deploy) / name).read_bytes()).hexdigest()
        for name in BUNDLE
    }


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def candidate(args):
    repository = os.environ["GITHUB_REPOSITORY"].lower()
    require(
        os.environ.get("GITHUB_REF") == "refs/heads/staging"
        and os.environ.get("GITHUB_EVENT_NAME") == "push",
        "release build requires trusted staging push",
    )
    images = {
        component: f"ghcr.io/{repository}-{component}@{digest}"
        for component, digest in (
            ("backend", args.backend),
            ("frontend", args.frontend),
        )
    }
    value = dict(
        schema=1,
        repository=repository,
        source_sha=os.environ["GITHUB_SHA"],
        run_id=int(os.environ["GITHUB_RUN_ID"]),
        run_attempt=int(os.environ["GITHUB_RUN_ATTEMPT"]),
        images=images,
        deployment_files=bundle_hashes("deploy"),
        staging={"verified": False, "command_id": None},
    )
    validate_manifest(value, repository)
    require(
        git("rev-parse", "HEAD") == value["source_sha"],
        "checkout differs from release revision",
    )
    Path(args.output).write_bytes(canonical(value))


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        require(
            urllib.parse.urlsplit(newurl).scheme == "https",
            "artifact redirect must use HTTPS",
        )
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        # GitHub redirects artifact downloads to signed storage URLs. Never send
        # GITHUB_TOKEN to another origin, even though that redirect is trusted.
        if (
            urllib.parse.urlsplit(req.full_url).netloc
            != urllib.parse.urlsplit(newurl).netloc
        ):
            redirected.remove_header("Authorization")
        return redirected


def api(path, raw=False):
    require(path.startswith("/repos/") and ".." not in path, "invalid GitHub API path")
    request = urllib.request.Request(
        "https://api.github.com" + path,
        headers={
            "Authorization": "Bearer " + os.environ["GH_TOKEN"],
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    with urllib.request.build_opener(SafeRedirect()).open(
        request, timeout=30
    ) as response:
        data = response.read((2 << 20) + 1)
    require(len(data) <= 2 << 20, "GitHub response exceeds size limit")
    return data if raw else json.loads(data)


def validate_run(run, repository, run_id, attempt):
    require(
        run.get("id") == run_id and run.get("run_attempt") == attempt,
        "staging run attempt mismatch",
    )
    require(
        run.get("head_repository", {}).get("full_name", "").lower() == repository,
        "foreign staging repository",
    )
    require(
        run.get("event") == "push" and run.get("head_branch") == "staging",
        "not a staging push",
    )
    require(run.get("path") == WORKFLOW, "wrong staging workflow")
    require(
        run.get("status") == "completed" and run.get("conclusion") == "success",
        "staging workflow did not succeed",
    )
    return run["head_sha"]


def resolve(args):
    repository = os.environ["GITHUB_REPOSITORY"].lower()
    require(
        os.environ.get("GITHUB_REF") == "refs/heads/main",
        "promotion requires protected main",
    )
    selection = read_json(args.selection)
    require(
        isinstance(selection, dict)
        and set(selection)
        == {"staging_run_id", "staging_run_attempt", "manifest_sha256"},
        "invalid release selection",
    )
    run_id, attempt = selection["staging_run_id"], selection["staging_run_attempt"]
    require(
        positive(run_id) and positive(attempt),
        "select a tested staging run and attempt in a reviewed PR",
    )
    require(
        isinstance(selection["manifest_sha256"], str)
        and re.fullmatch(HEX, selection["manifest_sha256"]),
        "select the exact manifest SHA-256",
    )
    run = api(f"/repos/{repository}/actions/runs/{run_id}/attempts/{attempt}")
    source_sha = validate_run(run, repository, run_id, attempt)
    artifact_name = f"vellum-release-{run_id}-{attempt}"
    matches = []
    page = 1
    while True:
        listing = api(
            f"/repos/{repository}/actions/runs/{run_id}/artifacts?per_page=100&page={page}"
        )
        matches.extend(a for a in listing["artifacts"] if a["name"] == artifact_name)
        if len(listing["artifacts"]) < 100:
            break
        page += 1
        require(page <= 20, "too many workflow artifacts")
    require(
        len(matches) == 1 and not matches[0]["expired"],
        "tested release artifact missing, ambiguous or expired",
    )
    artifact = matches[0]
    require(positive(artifact["id"]), "invalid artifact identity")
    archive = api(
        f"/repos/{repository}/actions/artifacts/{artifact['id']}/zip", raw=True
    )
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
        require(
            zipped.namelist() == ["release.json"],
            "unexpected release artifact contents",
        )
        require(
            zipped.getinfo("release.json").file_size <= MAX_MANIFEST,
            "release artifact too large",
        )
        data = zipped.read("release.json")
    require(
        hashlib.sha256(data).hexdigest() == selection["manifest_sha256"],
        "reviewed manifest digest mismatch",
    )
    value = validate_manifest(json.loads(data), repository, verified=True)
    require(
        value["source_sha"] == source_sha
        and value["run_id"] == run_id
        and value["run_attempt"] == attempt,
        "manifest/run provenance mismatch",
    )
    # Compare runtime/deployment trees, not commit identity: squash and merge
    # commits may differ while containing the exact tested release source.
    for path in SOURCE_PATHS:
        require(
            git("rev-parse", f"{source_sha}:{path}")
            == git("rev-parse", f"HEAD:{path}"),
            f"main differs from staging at {path}",
        )
    require(
        bundle_hashes("deploy") == value["deployment_files"],
        "deployment bundle differs from tested release",
    )
    Path(args.output).write_bytes(data)


def mark_verified(args):
    value = read_json(args.manifest)
    validate_manifest(value, os.environ["GITHUB_REPOSITORY"].lower())
    value["staging"] = {"verified": True, "command_id": args.command_id}
    validate_manifest(value, value["repository"], verified=True)
    Path(args.output).write_bytes(canonical(value))
    print("manifest_sha256=" + hashlib.sha256(canonical(value)).hexdigest())


def dispatch(args):
    approval_mode = getattr(args, "approval_mode", "standard")
    require(approval_mode in ("standard", "prelaunch"), "unknown approval mode")
    require(args.environment == "production" or approval_mode == "standard",
            "prelaunch approval is production-only")
    value = read_json(args.manifest)
    validate_manifest(
        value,
        os.environ["GITHUB_REPOSITORY"].lower(),
        verified=args.environment == "production",
    )
    require(
        os.environ.get("GITHUB_REF")
        == "refs/heads/" + ("main" if args.environment == "production" else "staging"),
        "deployment ref mismatch",
    )
    instance = os.environ["SSM_INSTANCE_ID"]
    document = os.environ["SSM_DOCUMENT_ARN"]
    version = os.environ["SSM_DOCUMENT_VERSION"]
    digest = os.environ["SSM_DOCUMENT_SHA256"]
    require(re.fullmatch(r"i-[0-9a-f]{8,17}", instance), "invalid instance ID")
    require(
        re.fullmatch(
            r"arn:aws:ssm:[a-z0-9-]+:[0-9]{12}:document/VellumDeploy-"
            + args.environment,
            document,
        ),
        "unexpected SSM document",
    )
    require(
        re.fullmatch(r"[1-9][0-9]*", version) and re.fullmatch(HEX, digest),
        "SSM document version/hash must be pinned",
    )
    parameters = {"Manifest": [base64.b64encode(canonical(value)).decode()]}
    if args.environment == "production":
        parameters["ApprovalMode"] = [approval_mode]
    parameters = json.dumps(parameters)
    command = subprocess.check_output(
        [
            "aws",
            "ssm",
            "send-command",
            "--instance-ids",
            instance,
            "--document-name",
            document,
            "--document-version",
            version,
            "--document-hash",
            digest,
            "--document-hash-type",
            "Sha256",
            "--parameters",
            parameters,
            "--timeout-seconds",
            "900",
            "--query",
            "Command.CommandId",
            "--output",
            "text",
        ],
        text=True,
    ).strip()
    require(str(uuid.UUID(command)) == command, "invalid command ID")
    deadline = time.monotonic() + 900
    while time.monotonic() < deadline:
        result = subprocess.run(
            [
                "aws",
                "ssm",
                "get-command-invocation",
                "--command-id",
                command,
                "--instance-id",
                instance,
                "--query",
                "Status",
                "--output",
                "text",
            ],
            capture_output=True,
            text=True,
        )
        status = result.stdout.strip()
        if result.returncode != 0 and "InvocationDoesNotExist" not in result.stderr:
            raise ValueError(
                "SSM status lookup failed; remote command may still be running"
            )
        if result.returncode == 0 and status == "Success":
            with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
                output.write("command_id=" + command + "\n")
            return
        if result.returncode == 0 and status not in {
            "Pending",
            "InProgress",
            "Delayed",
        }:
            raise ValueError(
                f"SSM deployment failed ({status}); inspect secret-redacted service diagnostics privately"
            )
        time.sleep(5)
    raise ValueError(
        "SSM deployment timed out; remote command may still be running; do not overlap another deployment"
    )


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    create = sub.add_parser("candidate")
    create.add_argument("--backend", required=True)
    create.add_argument("--frontend", required=True)
    create.add_argument("--output", required=True)
    promote = sub.add_parser("resolve")
    promote.add_argument("--selection", default="deploy/production-release.json")
    promote.add_argument("--output", required=True)
    verify = sub.add_parser("mark-verified")
    verify.add_argument("--manifest", required=True)
    verify.add_argument("--command-id", required=True)
    verify.add_argument("--output", required=True)
    send = sub.add_parser("dispatch")
    send.add_argument("--manifest", required=True)
    send.add_argument("--environment", choices=("staging", "production"), required=True)
    args = parser.parse_args()
    {
        "candidate": candidate,
        "resolve": resolve,
        "mark-verified": mark_verified,
        "dispatch": dispatch,
    }[args.command](args)


if __name__ == "__main__":
    try:
        main()
    except (
        ValueError,
        KeyError,
        OSError,
        subprocess.SubprocessError,
        zipfile.BadZipFile,
    ) as error:
        # Never dump response bodies, credentials, resolved Compose or app logs.
        print(
            f"Release operation failed: {type(error).__name__}: {error}",
            file=sys.stderr,
        )
        sys.exit(1)
