#!/usr/bin/env python3
"""Runner-only production gates around the verified six-file controller bundle."""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlencode
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "deploy/cd"))
import release

REPOSITORY = "kacperkg/vellum"
API = f"/repos/{REPOSITORY}"
STAGING_WORKFLOW = f"{API}/actions/workflows/release-staging.yml"
STAGING_REF = "refs/remotes/origin/staging"
# Compare every tracked input unless explicitly known to be outside build,
# CI and deployment. Component docs are inside their Docker contexts.
RELEASE_PATHS = (
    ".", ":(exclude)README.md", ":(exclude)docs",
    ":(exclude,glob)deploy/*.md", ":(exclude)deploy/production-release.json",
)
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


def recovery_mode():
    mode = os.environ.get("PRODUCTION_READINESS_MODE", "") or "standard"
    release.require(mode in ("standard", "automated"), "invalid production readiness mode")
    return mode


def guard():
    recovery_mode()
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
    sha = os.environ.get("GITHUB_SHA", "")
    release.require(
        re.fullmatch(release.SHA, sha) and int(sha, 16)
        and sha == os.environ.get("GITHUB_WORKFLOW_SHA"),
        "workflow and dispatch must use the same immutable main revision",
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


def current_main(sha):
    ref = release.api(f"{API}/git/ref/heads/main")
    release.require(
        ref.get("object", {}).get("sha") == sha,
        "main advanced after dispatch; review and dispatch a fresh request",
    )


def current_staging():
    ref = release.api(f"{API}/git/ref/heads/staging")["object"]["sha"]
    release.require(ref == release.git("rev-parse", STAGING_REF),
                    "staging advanced after checkout; dispatch a fresh request")
    return ref


def git_matches(*args):
    result = subprocess.run(["git", *args], capture_output=True)
    release.require(result.returncode in (0, 1), "could not compare checked-out Git revisions")
    return result.returncode == 0


def equivalent(source, sha):
    # Git compares content, file types and modes, including gitlinks.
    return git_matches("diff", "--quiet", "--no-ext-diff", "--ignore-submodules=none",
                       source, sha, "--", *RELEASE_PATHS)


def paged(path, key, *, limit, count=False):
    items = []
    total = None
    size = 25  # Keep responses below release.api's size limit.
    separator = "&" if "?" in path else "?"
    for page in range(1, limit // size + 1):
        payload = release.api(f"{path}{separator}per_page={size}&page={page}")
        if count:
            reported = payload["total_count"]
            release.require(type(reported) is int and 0 <= reported < limit,
                            "staging run search exceeded its safe limit")
            if total is None:
                total = reported
            release.require(total == reported, "staging run listing changed during discovery")
        batch = payload[key]
        release.require(isinstance(batch, list) and len(batch) <= size,
                        "invalid GitHub result page")
        items.extend(batch)
        if len(batch) < size:
            release.require(not count or len(items) == total,
                            "incomplete staging run listing")
            return items
    raise ValueError(f"incomplete GitHub listing for {key}")


def exact_run(run, run_id, attempt, source):
    release.require(release.validate_run(run, REPOSITORY, run_id, attempt) == source,
                    "staging source or latest attempt changed")


def check_jobs(base, attempt, run_id, source):
    jobs = paged(f"{base}/attempts/{attempt}/jobs", "jobs", limit=500)
    for name in REQUIRED_JOBS:
        matches = [job for job in jobs if job.get("name") == name]
        release.require(len(matches) == 1, f"required staging job missing or ambiguous: {name}")
        job = matches[0]
        release.require(
            job.get("run_id") == run_id and job.get("head_sha") == source
            and job.get("run_attempt") == attempt and job.get("status") == "completed",
            f"required staging job has wrong attempt or source: {name}",
        )
        if name == "publish-and-deploy" and job.get("conclusion") == "skipped":
            return False
        release.require(job.get("conclusion") == "success",
                        f"required staging job did not succeed: {name}")
    return True


def verified_artifact(base, run_id, attempt, source, repo_id):
    prefix = f"vellum-release-{run_id}-{attempt}-"
    artifacts = paged(f"{base}/artifacts", "artifacts", limit=500)
    matches = [a for a in artifacts if a["name"].startswith(prefix)]
    release.require(len(matches) <= 1, "ambiguous staging release artifacts")
    if not matches or matches[0]["expired"] is True:
        return None
    artifact = matches[0]
    proof = artifact["name"][len(prefix):]
    release.require(re.fullmatch("-".join([release.HEX] * 3), proof),
                    "invalid staging manifest or build digest evidence")
    checksum, backend, frontend = proof.split("-")
    artifact_id = artifact["id"]
    provenance = artifact["workflow_run"]
    release.require(
        artifact["expired"] is False and release.positive(artifact_id)
        and release.positive(repo_id)
        and provenance["id"] == run_id and provenance["repository_id"] == repo_id
        and provenance["head_repository_id"] == repo_id
        and provenance["head_branch"] == "staging" and provenance["head_sha"] == source,
        "invalid staging artifact identity or provenance",
    )
    archive = release.api(f"{API}/actions/artifacts/{artifact_id}/zip", raw=True)
    release.require(artifact["digest"] == "sha256:" + hashlib.sha256(archive).hexdigest(),
                    "staging artifact archive checksum mismatch")
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
        release.require(zipped.namelist() == ["release.json"]
                        and zipped.getinfo("release.json").file_size <= release.MAX_MANIFEST,
                        "unexpected staging release archive contents")
        data = zipped.read("release.json")
    release.require(hashlib.sha256(data).hexdigest() == checksum,
                    "staging manifest checksum mismatch")
    value = release.validate_manifest(json.loads(data), REPOSITORY, verified=True)
    release.require(
        data == release.canonical(value) and value["source_sha"] == source
        and value["run_id"] == run_id and value["run_attempt"] == attempt,
        "manifest is not canonical or its run provenance differs",
    )
    release.require(
        all(value["images"][component].endswith("@sha256:" + digest)
            for component, digest in (("backend", backend), ("frontend", frontend))),
        "manifest image digests differ from staging build outputs",
    )
    release.require(release.bundle_hashes("deploy") == value["deployment_files"],
                    "six-file deployment bundle differs from tested release")
    return f"{run_id}:{attempt}:{artifact_id}:{checksum}", data, value


def discover(sha):
    staging = current_staging()
    since = (datetime.now(timezone.utc) - timedelta(days=90)).strftime("%Y-%m-%dT%H:%M:%SZ")
    query = urlencode({"branch": "staging", "event": "push", "created": f">={since}"})
    runs = paged(f"{STAGING_WORKFLOW}/runs?{query}", "workflow_runs", limit=1000, count=True)
    found = None
    seen = set()
    for run in runs:
        run_id, attempt, source = run["id"], run["run_attempt"], run["head_sha"]
        release.require(release.positive(run_id) and run_id not in seen,
                        "invalid or duplicate staging run in listing")
        seen.add(run_id)
        if (run.get("status"), run.get("conclusion"), run.get("event"), run.get("head_branch")) != (
            "completed", "success", "push", "staging",
        ):
            continue
        release.require(release.positive(attempt) and isinstance(source, str)
                        and re.fullmatch(release.SHA, source), "invalid staging run identity")
        exact_run(run, run_id, attempt, source)
        if not git_matches("merge-base", "--is-ancestor", source, staging) or not equivalent(source, sha):
            continue
        base = f"{API}/actions/runs/{run_id}"
        exact_run(release.api(base), run_id, attempt, source)
        exact_run(release.api(f"{base}/attempts/{attempt}"), run_id, attempt, source)
        if not check_jobs(base, attempt, run_id, source):
            continue
        candidate = verified_artifact(base, run_id, attempt, source, run["repository"]["id"])
        if candidate is None:
            continue
        release.require(found is None, "multiple eligible staging releases match main; promotion refused")
        found = candidate
    release.require(found is not None, "no eligible staging release matches main: require a successful "
                    "ordinary staging push with full CI, staging deployment and an unexpired release artifact")
    return found


def prepare(args):
    sha = guard()
    current_main(sha)
    identity, data, value = discover(sha)
    current_main(sha)
    if args.recheck:
        release.require(identity == os.environ.get("EXPECTED_RELEASE"),
                        "eligible staging artifact changed after initial discovery")
        Path(args.output).write_bytes(data)
        return
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write(f"release={identity}\n")
    run_id, attempt, artifact, checksum = identity.split(":")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as summary:
        summary.write(
            f"### Eligible staging release\n\n"
            f"- Reviewed main: `{sha}`\n"
            f"- Staging source: `{value['source_sha']}`\n"
            f"- [Staging run {run_id}, attempt {attempt}](https://github.com/{REPOSITORY}/actions/runs/{run_id}/attempts/{attempt})\n"
            f"- Artifact ID: `{artifact}`\n"
            f"- Recovery approval manifest SHA-256: `{checksum}`\n"
            f"- Backend: `{value['images']['backend']}`\n"
            f"- Frontend: `{value['images']['frontend']}`\n"
        )


def dispatch(args):
    sha = guard()
    identity = os.environ.get("EXPECTED_RELEASE", "")
    release.require(re.fullmatch(r"[1-9][0-9]{0,17}:[1-9][0-9]{0,17}:[1-9][0-9]{0,17}:"
                                 + release.HEX, identity), "missing resolved staging identity")
    run_id, attempt, _, checksum = identity.split(":")
    data = Path(args.manifest).read_bytes()
    release.require(hashlib.sha256(data).hexdigest() == checksum,
                    "prepared manifest differs from resolved staging artifact")
    current_identity, current_data, value = discover(sha)
    release.require(current_identity == identity and current_data == data,
                    "staging artifact changed before SSM dispatch")
    source = value["source_sha"]
    current_staging()
    exact_run(release.api(f"{API}/actions/runs/{run_id}"), int(run_id), int(attempt), source)
    current_main(sha)
    mode = recovery_mode()
    if mode == "automated":
        # Deliberately do not fall back to the v2/manual pin.
        os.environ["SSM_DOCUMENT_VERSION"] = os.environ.get("SSM_READINESS_DOCUMENT_VERSION", "")
        os.environ["SSM_DOCUMENT_SHA256"] = os.environ.get("SSM_READINESS_DOCUMENT_SHA256", "")
    release.dispatch(argparse.Namespace(environment="production", manifest=args.manifest,
                                        approval_mode=mode))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("discover")
    check = commands.add_parser("prepare")
    check.add_argument("--output", required=True)
    commands.add_parser("dispatch").add_argument("--manifest", required=True)
    args = parser.parse_args()
    if args.command == "dispatch":
        dispatch(args)
    else:
        args.recheck = args.command == "prepare"
        prepare(args)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, TypeError, OSError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print(f"Production gate failed: {type(error).__name__}: {error}", file=sys.stderr)
        sys.exit(1)
