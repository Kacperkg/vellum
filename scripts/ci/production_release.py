#!/usr/bin/env python3
"""Runner-only production gates around the verified six-file controller bundle."""
import argparse
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "deploy/cd"))
import release

REPOSITORY = "kacperkg/vellum"
SELECTION = "deploy/production-release.json"
REQUIRED_JOBS = (
    "checks / Change plan",
    "checks / Frontend quality",
    "checks / Backend and PostgreSQL",
    "checks / Browser workflows",
    "checks / Docker secret exclusions",
    "checks / Deployment configuration and safety tests",
    "checks / Production image (backend, linux/amd64)",
    "checks / Production image (frontend, linux/amd64)",
    "publish-and-deploy",
)


def recovery_approval_mode():
    mode = os.environ.get("PRODUCTION_RECOVERY_APPROVAL", "standard")
    confirmed = os.environ.get("CONFIRM_PRELAUNCH_DATA_LOSS", "false")
    release.require(
        (mode == "standard" and confirmed == "false")
        or (mode == "prelaunch" and confirmed == "true"),
        "select standard recovery approval or explicitly acknowledge prelaunch data loss",
    )
    return mode


def guard():
    release.require(
        os.environ.get("GITHUB_REPOSITORY") == "Kacperkg/vellum"
        and os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch"
        and os.environ.get("GITHUB_REF") == "refs/heads/main"
        and os.environ.get("GITHUB_REF_PROTECTED") == "true"
        and os.environ.get("GITHUB_WORKFLOW_REF")
        == "Kacperkg/vellum/.github/workflows/release-production.yml@refs/heads/main",
        "production requires the canonical workflow dispatched on protected main",
    )
    release.require(
        os.environ.get("VELLUM_CD_ENABLED") == "true"
        and os.environ.get("PRODUCTION_APPROVAL_MODE") == "manual"
        and os.environ.get("CONFIRM_PRODUCTION_DEPLOYMENT") == "true",
        "production requires enabled CD, manual approval mode and explicit acknowledgement",
    )
    recovery_approval_mode()
    sha = os.environ.get("EXPECTED_SHA", "")
    release.require(
        re.fullmatch(release.SHA, sha) and int(sha, 16)
        and sha == os.environ.get("GITHUB_SHA"),
        "expected SHA must equal the exact immutable dispatch revision",
    )
    release.require(
        release.git("rev-parse", "HEAD") == sha
        and release.git("rev-parse", "refs/remotes/origin/main") == sha,
        "checkout or fetched main head changed; review and dispatch a fresh request",
    )
    for key in ("GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT"):
        release.require(
            re.fullmatch(r"[1-9][0-9]{0,17}", os.environ.get(key, "")),
            "invalid production workflow identity",
        )
    return sha


def selection():
    value = release.read_json(SELECTION)
    release.require(
        isinstance(value, dict)
        and set(value) == {"staging_run_id", "staging_run_attempt", "manifest_sha256"},
        "invalid production release selector schema",
    )
    release.require(
        release.positive(value["staging_run_id"])
        and release.positive(value["staging_run_attempt"])
        and isinstance(value["manifest_sha256"], str)
        and re.fullmatch(release.HEX, value["manifest_sha256"]),
        "production release selector is unconfigured or invalid: obtain a fresh successful "
        "ordinary staging-push vellum-release artifact, then select its exact run, attempt "
        "and manifest SHA-256 in a reviewed promotion PR; manual verification is ineligible",
    )
    return value


def current_main(sha):
    ref = release.api(f"/repos/{REPOSITORY}/git/ref/heads/main")
    release.require(
        ref.get("object", {}).get("sha") == sha,
        "main advanced after dispatch; review and dispatch a fresh request",
    )


def eligible(selected):
    run_id, attempt = selected["staging_run_id"], selected["staging_run_attempt"]
    base = f"/repos/{REPOSITORY}/actions/runs/{run_id}"
    # The latest attempt must also succeed: an earlier receipt cannot substitute
    # for a subsequently failed, cancelled or still-running rerun.
    latest = release.api(base)
    source = release.validate_run(latest, REPOSITORY, run_id, attempt)
    exact = release.api(f"{base}/attempts/{attempt}")
    release.require(
        release.validate_run(exact, REPOSITORY, run_id, attempt) == source
        and isinstance(source, str) and re.fullmatch(release.SHA, source),
        "selected staging source mismatch",
    )
    jobs = []
    for page in range(1, 21):
        batch = release.api(f"{base}/attempts/{attempt}/jobs?per_page=100&page={page}")["jobs"]
        jobs.extend(batch)
        if len(batch) < 100:
            break
    else:
        raise ValueError("too many staging jobs")
    for name in REQUIRED_JOBS:
        matches = [job for job in jobs if job.get("name") == name]
        release.require(len(matches) == 1, f"required staging job missing or ambiguous: {name}")
        job = matches[0]
        release.require(
            job.get("run_id") == run_id and job.get("head_sha") == source
            and job.get("run_attempt") == attempt
            and job.get("status") == "completed" and job.get("conclusion") == "success",
            f"required staging job did not succeed for the selected attempt: {name}",
        )
    result = subprocess.run(
        ["git", "merge-base", "--is-ancestor", source, "refs/remotes/origin/staging"],
        capture_output=True,
    )
    release.require(result.returncode == 0, "selected source is outside current staging history")


def resolve(args):
    sha = guard()
    selected = selection()  # Fail before API/OIDC for the intentionally null selector.
    current_main(sha)
    eligible(selected)
    # Existing resolver enforces checksum, exact artifact/manifest identity,
    # immutable digests, six-file hashes and exact source/deployment Git trees.
    release.resolve(argparse.Namespace(selection=SELECTION, output=args.output))


def dispatch(args):
    sha = guard()
    selected = selection()
    current_main(sha)
    eligible(selected)  # Recheck eligibility after any Environment/credential delay.
    data = Path(args.manifest).read_bytes()
    release.require(
        hashlib.sha256(data).hexdigest() == selected["manifest_sha256"],
        "resolved manifest differs from the reviewed selector",
    )
    release.dispatch(argparse.Namespace(environment="production", manifest=args.manifest,
                                        approval_mode=recovery_approval_mode()))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("guard")
    commands.add_parser("resolve").add_argument("--output", required=True)
    commands.add_parser("dispatch").add_argument("--manifest", required=True)
    args = parser.parse_args()
    if args.command == "guard":
        guard()
    else:
        {"resolve": resolve, "dispatch": dispatch}[args.command](args)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print(f"Production gate failed: {type(error).__name__}: {error}", file=sys.stderr)
        sys.exit(1)
