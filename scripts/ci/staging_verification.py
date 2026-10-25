#!/usr/bin/env python3
"""Runner-only manual staging release adapter; never installed/executed as root.

Keep the six-file controller bundle unchanged. Manual evidence is deliberately
ineligible for the existing push-only production promotion resolver.
"""
import argparse
import os
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "deploy/cd"))
import release


def guard():
    release.require(
        os.environ.get("GITHUB_REPOSITORY") == "Kacperkg/vellum"
        and os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch"
        and os.environ.get("GITHUB_REF") == "refs/heads/staging"
        and os.environ.get("GITHUB_REF_PROTECTED") == "true"
        and os.environ.get("GITHUB_WORKFLOW_REF")
        == "Kacperkg/vellum/.github/workflows/release-staging.yml@refs/heads/staging",
        "manual verification requires the canonical workflow on protected staging",
    )
    release.require(
        os.environ.get("VELLUM_CD_ENABLED") == "true"
        and os.environ.get("CONFIRM_STAGING_DEPLOYMENT") == "true",
        "manual verification requires enabled CD and explicit staging deployment approval",
    )
    sha = os.environ.get("EXPECTED_SHA", "")
    release.require(
        re.fullmatch(release.SHA, sha) and int(sha, 16)
        and sha == os.environ.get("GITHUB_SHA"),
        "expected SHA must be the exact immutable dispatch revision",
    )
    release.require(
        release.git("rev-parse", "HEAD") == sha
        and release.git("rev-parse", "refs/remotes/origin/staging") == sha,
        "checkout or fetched staging head differs from the approved revision; dispatch a fresh request",
    )
    for key in ("GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT"):
        value = os.environ.get(key, "")
        release.require(
            re.fullmatch(r"[1-9][0-9]{0,17}", value), "invalid manual workflow identity"
        )
    return sha


def candidate(args):
    sha = guard()
    repository = "kacperkg/vellum"
    value = dict(
        schema=1,
        repository=repository,
        source_sha=sha,
        run_id=int(os.environ["GITHUB_RUN_ID"]),
        run_attempt=int(os.environ["GITHUB_RUN_ATTEMPT"]),
        images={
            component: f"ghcr.io/{repository}-{component}@{digest}"
            for component, digest in (("backend", args.backend), ("frontend", args.frontend))
        },
        deployment_files=release.bundle_hashes("deploy"),
        staging={"verified": False, "command_id": None},
    )
    release.validate_manifest(value, repository)
    Path(args.output).write_bytes(release.canonical(value))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("guard")
    build = commands.add_parser("candidate")
    for name in ("backend", "frontend", "output"):
        build.add_argument("--" + name, required=True)
    args = parser.parse_args()
    if args.command == "guard":
        sha = guard()
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(
                f"Manual staging deployment verification: `{sha}`.\n\n"
                "Explicit full CI, fresh digest-pinned images, staging container replacement, "
                "AutoMigrate/seeding and smoke requests follow. This is not a read-only probe. "
                "Its evidence is not eligible for production promotion.\n"
            )
    else:
        candidate(args)


if __name__ == "__main__":
    main()
