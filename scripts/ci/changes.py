#!/usr/bin/env python3
"""Conservative, local-git change plan; no PR API truncation or extra permissions."""
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

CHECKS = ("frontend", "backend", "browser", "docker", "deployment", "image_backend", "image_frontend")
KEYS = (*CHECKS, "deploy")
DOC_FILES = {"README.md"}


def classify(paths):
    plan = dict.fromkeys(KEYS, False)
    categories = set()
    if not paths:
        return dict.fromkeys(KEYS, True), ["ambiguous"]
    for path in paths:
        parts = PurePosixPath(path).parts
        if not parts or path.startswith("/") or ".." in parts:
            return dict.fromkeys(KEYS, True), ["ambiguous"]
        if path in DOC_FILES or path.startswith("docs/") or (
            path.startswith("deploy/") and len(parts) == 2 and path.endswith(".md")
        ):
            categories.add("documentation")
            continue
        # Component documentation participates in schema 1's whole-tree
        # promotion comparison, so it still needs a tested release today.
        if path in {"frontend/README.md", "backend/README.md"} or path.startswith("backend/doc/"):
            categories.add("documentation")
        if path.startswith("frontend/"):
            categories.add("frontend")
            plan.update(frontend=True, browser=True, image_frontend=True)
            if path.startswith(("frontend/tests/", "frontend/scripts/", "frontend/e2e/")) or path in {"frontend/playwright.config.ts", "frontend/tsconfig.e2e.json"}:
                categories.add("tests")
        elif path.startswith("backend/"):
            categories.add("backend")
            plan.update(backend=True, browser=True, image_backend=True)
            if path.endswith("_test.go"):
                categories.add("tests")
        elif path.startswith("deploy/"):
            categories.add("deployment")
            # Includes IAM, SSM, manifests and controller changes. Fresh pair
            # remains required by schema 1's single source revision contract.
            plan.update(deploy=True)
            if path.startswith("deploy/tests/"):
                categories.add("tests")
        else:
            categories.add("workflows" if path.startswith(".github/") else "shared")
            if path.startswith("scripts/"):
                categories.add("tests")
            plan.update(dict.fromkeys(KEYS, True))
        plan.update(docker=True, deployment=True, deploy=True)
    return plan, sorted(categories)


def git(*args):
    return subprocess.check_output(["git", *args], stderr=subprocess.PIPE)


def changed_paths(event, event_name):
    if event_name == "pull_request":
        base = event["pull_request"]["base"]["sha"]
        head = event["pull_request"]["head"]["sha"]
    elif event_name == "push":
        base, head = event["before"], event["after"]
    else:
        raise ValueError("unsupported event")
    if not all(isinstance(ref, str) and re.fullmatch(r"[0-9a-f]{40}", ref) and int(ref, 16) for ref in (base, head)):
        raise ValueError("missing commit")
    if event_name == "pull_request":
        base = git("merge-base", base, head).decode().strip()
    # No rename collapsing: both the removed and added names are classified.
    raw = git("diff", "--name-only", "--no-renames", "-z", base, head, "--")
    return [name.decode("utf-8", errors="strict") for name in raw.split(b"\0") if name]


def guard(result, raw, key):
    if result != "success":
        raise ValueError("change detection did not succeed")
    plan = json.loads(raw)
    if not isinstance(plan, dict) or set(plan) != set(KEYS) or any(type(v) is not bool for v in plan.values()) or key not in CHECKS:
        raise ValueError("invalid or incomplete change plan")
    return plan[key]


def main():
    if sys.argv[1:] == ["detect"]:
        try:
            event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text())
            plan, categories = classify(changed_paths(event, os.environ["GITHUB_EVENT_NAME"]))
        except (KeyError, ValueError, OSError, subprocess.SubprocessError):
            # A missing parent/initial push/unsupported path selects everything.
            plan, categories = dict.fromkeys(KEYS, True), ["fallback: full validation"]
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.write("plan=" + json.dumps(plan, separators=(",", ":")) + "\n")
            output.write("deploy=" + str(plan["deploy"]).lower() + "\n")
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write("Change categories: " + ", ".join(categories) + "\n\n```json\n" + json.dumps(plan, indent=2) + "\n```\n")
    elif sys.argv[1:] == ["guard"]:
        selected = guard(os.environ["DETECTION_RESULT"], os.environ["CHANGE_PLAN"], os.environ["CHECK_KEY"])
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.write("run=" + str(selected).lower() + "\n")
        with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
            summary.write(os.environ["CHECK_KEY"] + (": selected; required steps follow.\n" if selected else ": not affected; validated change plan.\n"))
    else:
        raise ValueError("expected detect or guard")


if __name__ == "__main__":
    main()
