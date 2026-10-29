"""Production runner gates use synthetic GitHub data; no AWS calls or deployment."""
import argparse
import hashlib
import importlib.util
import io
import itertools
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("production_release", ROOT / "scripts/ci/production_release.py")
production = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(production)
release = production.release
SHA = "a" * 40
RUN = 12345
ATTEMPT = 2


def context():
    return {
        "GITHUB_REPOSITORY": "Kacperkg/vellum",
        "GITHUB_EVENT_NAME": "workflow_dispatch",
        "GITHUB_REF": "refs/heads/main",
        "GITHUB_REF_PROTECTED": "true",
        "GITHUB_WORKFLOW_REF": "Kacperkg/vellum/.github/workflows/release-production.yml@refs/heads/main",
        "VELLUM_CD_ENABLED": "true",
        "PRODUCTION_APPROVAL_MODE": "manual",
        "CONFIRM_PRODUCTION_DEPLOYMENT": "true",
        "EXPECTED_SHA": SHA,
        "GITHUB_SHA": SHA,
        "GITHUB_RUN_ID": "67890",
        "GITHUB_RUN_ATTEMPT": "1",
    }


def selector(digest="d" * 64):
    return {"staging_run_id": RUN, "staging_run_attempt": ATTEMPT, "manifest_sha256": digest}


def run():
    return {
        "id": RUN, "run_attempt": ATTEMPT,
        "head_repository": {"full_name": "Kacperkg/vellum"},
        "event": "push", "head_branch": "staging", "path": release.WORKFLOW,
        "status": "completed", "conclusion": "success", "head_sha": SHA,
    }


def jobs():
    return [
        {"name": name, "run_id": RUN, "run_attempt": ATTEMPT,
         "head_sha": SHA, "status": "completed", "conclusion": "success"}
        for name in production.REQUIRED_JOBS
    ]


def manifest():
    return {
        "schema": 1, "repository": production.REPOSITORY, "source_sha": SHA,
        "run_id": RUN, "run_attempt": ATTEMPT,
        "images": {
            component: f"ghcr.io/{production.REPOSITORY}-{component}@sha256:" + char * 64
            for component, char in (("backend", "b"), ("frontend", "c"))
        },
        "deployment_files": dict.fromkeys(release.BUNDLE, "d" * 64),
        "staging": {"verified": True, "command_id": "12345678-1234-1234-1234-123456789abc"},
    }


class GuardTests(unittest.TestCase):
    def test_prelaunch_requires_two_explicit_inputs_and_default_is_standard(self):
        for values, expected in (({}, "standard"),
            ({"PRODUCTION_RECOVERY_APPROVAL": "standard", "CONFIRM_PRELAUNCH_DATA_LOSS": "false"}, "standard"),
            ({"PRODUCTION_RECOVERY_APPROVAL": "prelaunch", "CONFIRM_PRELAUNCH_DATA_LOSS": "true"}, "prelaunch")):
            with self.subTest(values=values), patch.dict(os.environ, values, clear=True):
                self.assertEqual(production.recovery_approval_mode(), expected)
        for mode, ack in (("prelaunch", None), ("prelaunch", "false"), ("prelaunch", "True"),
                          ("prelaunch", "1"), ("standard", "true"), ("", "true"),
                          ("Prelaunch", "true"), ("$(id)", "true")):
            values = {"PRODUCTION_RECOVERY_APPROVAL": mode}
            if ack is not None: values["CONFIRM_PRELAUNCH_DATA_LOSS"] = ack
            with self.subTest(values=values), patch.dict(os.environ, {**context(), **values}, clear=True), patch.object(release, "git") as git:
                with self.assertRaises(ValueError): production.guard()
                git.assert_not_called()

    def test_exact_protected_main_and_checkout_are_required(self):
        with patch.dict(os.environ, context(), clear=True), patch.object(release, "git", return_value=SHA) as git:
            self.assertEqual(production.guard(), SHA)
            self.assertEqual([call.args for call in git.call_args_list], [
                ("rev-parse", "HEAD"), ("rev-parse", "refs/remotes/origin/main"),
            ])

    def test_untrusted_unprotected_automatic_and_unapproved_contexts_fail(self):
        mutations = {
            "GITHUB_REPOSITORY": ("attacker/vellum", "Kacperkg/Vellum", ""),
            "GITHUB_EVENT_NAME": ("push", "pull_request", "workflow_call", "repository_dispatch", ""),
            "GITHUB_REF": ("refs/heads/staging", "refs/heads/feature/test", "refs/tags/main", "main", ""),
            "GITHUB_REF_PROTECTED": ("false", "True", "1", ""),
            "GITHUB_WORKFLOW_REF": (
                "Kacperkg/vellum/.github/workflows/other.yml@refs/heads/main",
                "Kacperkg/vellum/.github/workflows/release-production.yml@refs/heads/staging",
                "attacker/vellum/.github/workflows/release-production.yml@refs/heads/main", "",
            ),
            "VELLUM_CD_ENABLED": ("false", "True", "1", ""),
            "PRODUCTION_APPROVAL_MODE": ("environment", "Manual", "", "true"),
            "CONFIRM_PRODUCTION_DEPLOYMENT": ("false", "True", "1", ""),
            "EXPECTED_SHA": ("", "a" * 39, "a" * 41, "A" * 40, "0" * 40, "b" * 40, "$(id)", SHA + "\n"),
            "GITHUB_SHA": ("", "b" * 40),
            "GITHUB_RUN_ID": ("", "0", "-1", "01", "1.0", "1\n", "1" * 19),
            "GITHUB_RUN_ATTEMPT": ("", "0", "-1", "01", "1.0", "1\n", "1" * 19),
        }
        for key, values in mutations.items():
            for value in values:
                with self.subTest(key=key, value=value), patch.dict(os.environ, {**context(), key: value}, clear=True), patch.object(release, "git", return_value=SHA), self.assertRaises(ValueError):
                    production.guard()
        for key in context():
            missing = context()
            del missing[key]
            with self.subTest(missing=key), patch.dict(os.environ, missing, clear=True), patch.object(release, "git", return_value=SHA), self.assertRaises(ValueError):
                production.guard()

    def test_stale_main_and_incorrect_checkout_fail(self):
        for revisions in (("b" * 40, SHA), (SHA, "b" * 40)):
            with self.subTest(revisions=revisions), patch.dict(os.environ, context(), clear=True), patch.object(release, "git", side_effect=revisions), self.assertRaisesRegex(ValueError, "main head changed"):
                production.guard()

    def test_main_advance_is_rejected_from_current_github_ref(self):
        with patch.object(release, "api", return_value={"object": {"sha": SHA}}) as api:
            production.current_main(SHA)
            api.assert_called_once_with(f"/repos/{production.REPOSITORY}/git/ref/heads/main")
        for response in ({}, {"object": {}}, {"object": {"sha": "b" * 40}}):
            with self.subTest(response=response), patch.object(release, "api", return_value=response), self.assertRaisesRegex(ValueError, "main advanced"):
                production.current_main(SHA)


class SelectionTests(unittest.TestCase):
    def test_exact_schema_and_values_are_required(self):
        with patch.object(release, "read_json", return_value=selector()) as read:
            self.assertEqual(production.selection(), selector())
            read.assert_called_once_with("deploy/production-release.json")
        invalid = [None, [], "manifest", {}, {**selector(), "artifact_id": 9}]
        for key in selector():
            missing = selector()
            del missing[key]
            invalid.append(missing)
        for key in ("staging_run_id", "staging_run_attempt"):
            for value in (None, True, False, 0, -1, 10**18, "12345", 1.5):
                invalid.append({**selector(), key: value})
        for value in (None, True, 123, "", "D" * 64, "d" * 63, "d" * 65, "d" * 64 + "\n", "$(id)"):
            invalid.append({**selector(), "manifest_sha256": value})
        for selected in invalid:
            with self.subTest(selection=selected), patch.object(release, "read_json", return_value=selected), self.assertRaises(ValueError):
                production.selection()

    def test_null_selector_fails_before_any_github_or_release_access(self):
        selected = dict.fromkeys(selector())
        with patch.object(production, "guard", return_value=SHA), patch.object(release, "read_json", return_value=selected), patch.object(release, "api") as api, patch.object(release, "resolve") as resolve, self.assertRaisesRegex(ValueError, "fresh successful ordinary staging-push"):
            production.resolve(argparse.Namespace(output="unused"))
        api.assert_not_called()
        resolve.assert_not_called()

    def test_repository_selector_is_unconfigured_or_complete_and_valid(self):
        selected = json.loads((ROOT / production.SELECTION).read_bytes())
        if selected == dict.fromkeys(selector()):
            return
        # A later reviewed promotion must be able to configure this file without
        # changing regression tests. Partial or malformed selections still fail.
        with patch.object(release, "read_json", return_value=selected):
            self.assertEqual(production.selection(), selected)


class EligibilityTests(unittest.TestCase):
    def api(self, path, raw=False):
        self.paths.append(path)
        if "/jobs?" in path:
            page = int(path.rsplit("=", 1)[1])
            return {"jobs": self.pages[page - 1]}
        return self.exact if "/attempts/" in path else self.latest

    def setUp(self):
        self.paths = []
        self.latest, self.exact = run(), run()
        self.pages = [jobs()]

    def eligible(self):
        with patch.object(release, "api", side_effect=self.api), patch.object(production.subprocess, "run", return_value=subprocess.CompletedProcess([], 0)) as git:
            production.eligible(selector())
            return git

    def test_successful_latest_exact_attempt_and_required_jobs_are_accepted(self):
        git = self.eligible()
        base = f"/repos/{production.REPOSITORY}/actions/runs/{RUN}"
        self.assertEqual(self.paths, [base, f"{base}/attempts/{ATTEMPT}", f"{base}/attempts/{ATTEMPT}/jobs?per_page=100&page=1"])
        git.assert_called_once_with(["git", "merge-base", "--is-ancestor", SHA, "refs/remotes/origin/staging"], capture_output=True)

    def test_manual_wrong_repository_workflow_branch_and_unsuccessful_runs_fail(self):
        mutations = (
            ("id", 42), ("run_attempt", 1), ("run_attempt", 3),
            ("event", "workflow_dispatch"), ("event", "pull_request"),
            ("head_repository", {"full_name": "attacker/vellum"}),
            ("head_branch", "main"), ("head_branch", "feature/test"),
            ("path", ".github/workflows/bootstrap-ghcr.yml"),
            ("status", "in_progress"), ("status", "queued"),
            *(("conclusion", conclusion) for conclusion in ("failure", "cancelled", "timed_out", "skipped", None)),
        )
        for target, (key, value) in itertools.product(("latest", "exact"), mutations):
            self.latest, self.exact = run(), run()
            getattr(self, target)[key] = value
            with self.subTest(target=target, key=key, value=value), self.assertRaises(ValueError):
                self.eligible()

    def test_exact_attempt_source_must_match_latest_and_be_valid(self):
        for source in ("b" * 40, "", "a" * 39, "A" * 40, None):
            self.latest, self.exact = run(), {**run(), "head_sha": source}
            with self.subTest(source=source), self.assertRaisesRegex(ValueError, "source mismatch"):
                self.eligible()
        self.latest = self.exact = {**run(), "head_sha": "not-a-sha"}
        with self.assertRaisesRegex(ValueError, "source mismatch"):
            self.eligible()

    def test_every_required_ci_and_publication_job_must_succeed(self):
        mutations = (
            ("status", "queued"), ("status", "in_progress"),
            ("conclusion", "skipped"), ("conclusion", "failure"),
            ("conclusion", "cancelled"), ("conclusion", "timed_out"),
            ("conclusion", None), ("run_attempt", ATTEMPT - 1),
            ("run_id", RUN + 1), ("head_sha", "b" * 40),
        )
        for index, (key, value) in itertools.product(range(len(production.REQUIRED_JOBS)), mutations):
            self.pages = [jobs()]
            self.pages[0][index][key] = value
            with self.subTest(job=production.REQUIRED_JOBS[index], key=key, value=value), self.assertRaisesRegex(ValueError, "did not succeed"):
                self.eligible()

    def test_missing_duplicate_and_ambiguous_required_jobs_fail(self):
        for index in range(len(production.REQUIRED_JOBS)):
            for duplicate in (False, True):
                self.pages = [jobs()]
                if duplicate:
                    self.pages[0].append(dict(self.pages[0][index]))
                else:
                    self.pages[0].pop(index)
                with self.subTest(index=index, duplicate=duplicate), self.assertRaisesRegex(ValueError, "missing or ambiguous"):
                    self.eligible()

    def test_attempt_jobs_are_paginated_and_bounded(self):
        self.pages = [[{"name": f"unrelated-{i}"} for i in range(100)], jobs()]
        self.eligible()
        self.assertTrue(self.paths[-1].endswith("page=2"))
        self.pages = [[{"name": "unrelated"}] * 100] * 20
        with self.assertRaisesRegex(ValueError, "too many staging jobs"):
            self.eligible()

    def test_unrelated_source_or_git_failure_refuses_release(self):
        for code in (1, 128):
            with self.subTest(returncode=code), patch.object(release, "api", side_effect=self.api), patch.object(production.subprocess, "run", return_value=subprocess.CompletedProcess([], code)), self.assertRaisesRegex(ValueError, "outside current staging history"):
                production.eligible(selector())


class OperationTests(unittest.TestCase):
    def test_resolve_calls_existing_resolver_only_after_all_runner_gates(self):
        order = []
        with patch.object(production, "guard", side_effect=lambda: order.append("guard") or SHA), patch.object(production, "selection", side_effect=lambda: order.append("selection") or selector()), patch.object(production, "current_main", side_effect=lambda sha: order.append("main")), patch.object(production, "eligible", side_effect=lambda chosen: order.append("eligible")), patch.object(release, "resolve", side_effect=lambda args: order.append("resolve")) as resolve:
            production.resolve(argparse.Namespace(output="approved.json"))
        self.assertEqual(order, ["guard", "selection", "main", "eligible", "resolve"])
        self.assertEqual(vars(resolve.call_args.args[0]), {"selection": production.SELECTION, "output": "approved.json"})

    def test_gate_failures_cannot_reach_resolve_or_dispatch(self):
        for operation, gate in itertools.product(("resolve", "dispatch"), ("guard", "selection", "current_main", "eligible")):
            with self.subTest(operation=operation, gate=gate), patch.object(production, "guard", return_value=SHA), patch.object(production, "selection", return_value=selector()), patch.object(production, "current_main"), patch.object(production, "eligible"), patch.object(production, gate, side_effect=ValueError("gate rejected")), patch.object(release, "resolve") as resolve, patch.object(release, "dispatch") as dispatch:
                with self.assertRaisesRegex(ValueError, "gate rejected"):
                    getattr(production, operation)(argparse.Namespace(output="unused", manifest="unused"))
                resolve.assert_not_called()
                dispatch.assert_not_called()

    def test_dispatch_rechecks_gates_and_exact_reviewed_hash_before_aws(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "approved.json"
            data = release.canonical(manifest())
            path.write_bytes(data)
            selected = selector(hashlib.sha256(data).hexdigest())
            order = []
            with patch.object(production, "guard", side_effect=lambda: order.append("guard") or SHA), patch.object(production, "selection", return_value=selected), patch.object(production, "current_main", side_effect=lambda sha: order.append("main")), patch.object(production, "eligible", side_effect=lambda value: order.append("eligible")), patch.object(release, "dispatch", side_effect=lambda args: order.append("dispatch")) as dispatch:
                production.dispatch(argparse.Namespace(manifest=path))
                self.assertEqual(order, ["guard", "main", "eligible", "dispatch"])
                self.assertEqual(vars(dispatch.call_args.args[0]), {"environment": "production", "manifest": path, "approval_mode": "standard"})
                path.write_bytes(data + b"\n")
                dispatch.reset_mock()
                with self.assertRaisesRegex(ValueError, "differs from the reviewed selector"):
                    production.dispatch(argparse.Namespace(manifest=path))
                dispatch.assert_not_called()

    def test_dispatch_failure_propagates_without_fallback(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "approved.json"
            path.write_bytes(b"manifest")
            with patch.object(production, "guard", return_value=SHA), patch.object(production, "selection", return_value=selector(hashlib.sha256(b"manifest").hexdigest())), patch.object(production, "current_main"), patch.object(production, "eligible"), patch.object(release, "dispatch", side_effect=ValueError("remote command may still be running")) as dispatch, self.assertRaisesRegex(ValueError, "still be running"):
                production.dispatch(argparse.Namespace(manifest=path))
            dispatch.assert_called_once()

    def test_real_resolver_rejects_bad_artifacts_checksums_digests_and_provenance(self):
        original = release.canonical(manifest())
        mutations = [
            ("valid", None), ("manual artifact", None), ("expired", None),
            ("missing", None), ("duplicate", None), ("bad checksum", None),
            ("unexpected member", None), ("nested member", None), ("oversized member", None),
            ("mutable image", lambda value: value["images"].update(backend="ghcr.io/kacperkg/vellum-backend:latest")),
            ("foreign image", lambda value: value["images"].update(backend="ghcr.io/attacker/backend@sha256:" + "b" * 64)),
            ("five files", lambda value: value["deployment_files"].pop("cd/coordination.py")),
            ("wrong bundle", lambda value: value["deployment_files"].update({"cd/controller.py": "e" * 64})),
            ("wrong source", lambda value: value.update(source_sha="b" * 40)),
            ("wrong run", lambda value: value.update(run_id=RUN + 1)),
            ("wrong attempt", lambda value: value.update(run_attempt=ATTEMPT + 1)),
            ("unverified", lambda value: value["staging"].update(verified=False, command_id=None)),
            *((f"different tree {path}", None) for path in release.SOURCE_PATHS),
        ]
        for case, mutate in mutations:
            value = manifest()
            if mutate:
                mutate(value)
            data = release.canonical(value)
            archive = io.BytesIO()
            with zipfile.ZipFile(archive, "w") as zipped:
                zipped.writestr("nested/release.json" if case == "nested member" else "release.json", b"x" * (release.MAX_MANIFEST + 1) if case == "oversized member" else data)
                if case == "unexpected member":
                    zipped.writestr("controller.py", "raise RuntimeError('untrusted artifact must never execute')")
            artifact = {"id": 987, "name": f"vellum-release-{RUN}-{ATTEMPT}", "expired": case == "expired"}
            if case == "manual artifact":
                artifact["name"] = f"vellum-staging-verification-{RUN}-{ATTEMPT}"

            def api(path, raw=False):
                if path.endswith("/zip"):
                    return archive.getvalue()
                if "/artifacts?" in path:
                    return {"artifacts": [] if case == "missing" else [artifact] * (2 if case == "duplicate" else 1)}
                return run()

            def git(*args):
                if case.startswith("different tree ") and args[-1] == "HEAD:" + case.removeprefix("different tree "):
                    return "different-tree"
                return SHA

            with self.subTest(case=case), tempfile.TemporaryDirectory() as temp:
                selected = Path(temp) / "selection.json"
                selected.write_bytes(release.canonical(selector(hashlib.sha256(data).hexdigest())))
                # Make checksum failure observable while retaining a valid ZIP.
                if case == "bad checksum":
                    selected.write_bytes(release.canonical(selector("e" * 64)))
                target = Path(temp) / "approved.json"
                with patch.dict(os.environ, context(), clear=True), patch.object(production, "SELECTION", str(selected)), patch.object(production, "current_main"), patch.object(production, "eligible"), patch.object(release, "api", side_effect=api), patch.object(release, "git", side_effect=git), patch.object(release, "bundle_hashes", return_value=manifest()["deployment_files"]):
                    if case == "valid":
                        production.resolve(argparse.Namespace(output=target))
                        self.assertEqual(target.read_bytes(), original)
                    else:
                        with self.assertRaises(ValueError):
                            production.resolve(argparse.Namespace(output=target))
                        self.assertFalse(target.exists())


class WorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.path = ROOT / ".github/workflows/release-production.yml"
        cls.workflow = cls.path.read_text()
        cls.promote = cls.workflow.split("  promote:\n", 1)[1]

    def test_active_workflow_has_only_explicit_manual_dispatch_inputs(self):
        self.assertFalse((ROOT / ".github/workflows/release-production.yml.disabled").exists())
        triggers = self.workflow.split("on:\n", 1)[1].split("\npermissions:", 1)[0]
        self.assertEqual(re.findall(r"^  ([a-z_]+):", triggers, re.M), ["workflow_dispatch"])
        self.assertEqual(re.findall(r"^      ([a-z_]+):", triggers, re.M), [
            "expected_sha", "confirm_production_deployment", "recovery_approval", "confirm_prelaunch_data_loss"])
        self.assertIn("required: true\n        default: false\n        type: boolean", triggers)
        self.assertIn("default: standard\n        type: choice\n        options:\n          - standard\n          - prelaunch", triggers)
        self.assertIn("PRODUCTION_RECOVERY_APPROVAL: ${{ inputs.recovery_approval }}", self.promote)
        self.assertIn("CONFIRM_PRELAUNCH_DATA_LOSS: ${{ inputs.confirm_prelaunch_data_loss }}", self.promote)
        self.assertNotIn("pull_request_target", self.workflow)
        self.assertNotIn("workflow_run:", self.workflow)

    def test_actual_job_condition_rejects_every_unauthorized_context(self):
        condition = re.search(r"^    if: >-\n(.*?)(?=^    [a-z])", "  promote:\n" + self.promote, re.M | re.S).group(1).strip()
        for event, repository, ref, protected, enabled, mode, confirmed in itertools.product(
            ("workflow_dispatch", "push", "pull_request"),
            ("Kacperkg/vellum", "attacker/vellum"),
            ("refs/heads/main", "refs/heads/staging", "refs/heads/feature/test", "refs/tags/main"),
            (True, False), ("true", "false"), ("manual", "environment", ""), (True, False),
        ):
            values = {
                "github.event_name": event, "github.repository": repository,
                "github.ref": ref, "github.ref_protected": protected,
                "vars.VELLUM_CD_ENABLED": enabled, "vars.PRODUCTION_APPROVAL_MODE": mode,
                "inputs.confirm_production_deployment": confirmed,
            }
            expression = condition
            for key in sorted(values, key=len, reverse=True):
                expression = expression.replace(key, repr(values[key]))
            expression = re.sub(r"== true\b", "== True", expression).replace("&&", " and ").replace("||", " or ")
            actual = bool(eval(" ".join(expression.split()), {"__builtins__": {}}, {}))
            expected = event == "workflow_dispatch" and repository == "Kacperkg/vellum" and ref == "refs/heads/main" and protected and enabled == "true" and mode == "manual" and confirmed
            self.assertEqual(actual, expected, values)

    def test_environment_permissions_pins_and_fixed_dispatch_preserve_boundaries(self):
        self.assertEqual(self.workflow.count("environment: production"), 1)
        self.assertNotIn("environment: staging", self.workflow)
        self.assertEqual(self.workflow.count("id-token: write"), 1)
        self.assertNotIn("packages: write", self.workflow)
        self.assertIn("permissions:\n  contents: read", self.workflow)
        self.assertIn("permissions:\n      contents: read\n      actions: read\n      id-token: write", self.promote)
        self.assertIn("group: vellum-deploy-production\n  cancel-in-progress: false", self.workflow)
        self.assertIn("timeout-minutes: 20", self.promote)
        self.assertEqual(re.findall(r"uses: ([^\s]+)", self.workflow), [
            "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803",
            "aws-actions/configure-aws-credentials@e1253824e5c10ff9df46874f81ed3ec929e19cfd",
        ])
        self.assertIn("ref: ${{ github.sha }}", self.promote)
        self.assertIn("fetch-depth: 0", self.promote)
        self.assertIn("persist-credentials: false", self.promote)
        for name in ("AWS_DEPLOY_ROLE_ARN", "AWS_REGION", "AWS_ACCOUNT_ID", "SSM_INSTANCE_ID", "SSM_DOCUMENT_ARN"):
            self.assertIn("${{ vars." + name + " }}", self.promote)
        self.assertIn("SSM_DOCUMENT_VERSION: '2'", self.promote)
        self.assertIn("SSM_DOCUMENT_SHA256: 8d43e90f07c95456d7f781b591cc187eaf448e0c80b3a87d0433b9665297f83a", self.promote)
        self.assertNotIn("${{ vars.SSM_DOCUMENT_VERSION }}", self.promote)
        self.assertNotIn("${{ vars.SSM_DOCUMENT_SHA256 }}", self.promote)
        self.assertIn("role-session-name: vellum-production-${{ github.run_id }}", self.promote)
        self.assertLess(self.promote.index("production_release.py guard"), self.promote.index("production_release.py resolve"))
        self.assertLess(self.promote.index("production_release.py resolve"), self.promote.index("aws-actions/configure-aws-credentials"))
        self.assertLess(self.promote.index("aws-actions/configure-aws-credentials"), self.promote.index("production_release.py dispatch"))
        self.assertIn("production_release.py dispatch --manifest approved-release.json", self.promote)
        self.assertNotIn("release.py dispatch --environment production", self.promote)
        self.assertNotIn("continue-on-error", self.promote)
        self.assertNotIn("if: always()", self.promote)
        self.assertNotIn("if: failure()", self.promote)


if __name__ == "__main__":
    unittest.main()
