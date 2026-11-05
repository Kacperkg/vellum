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
        "GITHUB_WORKFLOW_SHA": SHA,
        "GITHUB_SHA": SHA,
        "GITHUB_RUN_ID": "67890",
        "GITHUB_RUN_ATTEMPT": "1",
    }


def run():
    return {
        "id": RUN, "run_attempt": ATTEMPT, "repository": {"id": 42},
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
            "GITHUB_WORKFLOW_SHA": ("", "a" * 39, "a" * 41, "A" * 40, "0" * 40, "b" * 40, "$(id)", SHA + "\n"),
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


class DiscoveryTests(unittest.TestCase):
    def setUp(self):
        self.runs = [run()]
        self.latest = run()
        self.exact = run()
        self.jobs = jobs()
        self.artifacts = []
        self.publish(manifest())
        for name, kwargs in (
            ("api", {"side_effect": self.api}),
            ("git", {"return_value": SHA}),
            ("bundle_hashes", {"return_value": manifest()["deployment_files"]}),
        ):
            mock = patch.object(release, name, **kwargs)
            mock.start()
            self.addCleanup(mock.stop)
        mock = patch.object(production, "git_matches", return_value=True)
        self.git_matches = mock.start()
        self.addCleanup(mock.stop)

    def publish(self, value, *, checksum=None, backend="b" * 64, frontend="c" * 64):
        self.data = release.canonical(value)
        checksum = checksum or hashlib.sha256(self.data).hexdigest()
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("release.json", self.data)
        self.archive = buffer.getvalue()
        self.artifacts[:] = [{
            "id": 987, "expired": False,
            "name": f"vellum-release-{RUN}-{ATTEMPT}-{checksum}-{backend}-{frontend}",
            "digest": "sha256:" + hashlib.sha256(self.archive).hexdigest(),
            "workflow_run": {
                "id": RUN, "repository_id": 42, "head_repository_id": 42,
                "head_branch": "staging", "head_sha": SHA,
            },
        }]

    def api(self, path, raw=False):
        base = f"{production.API}/actions/runs/{RUN}"
        if "/git/ref/heads/" in path:
            return {"object": {"sha": SHA}}
        if path.startswith(production.STAGING_WORKFLOW + "/runs?"):
            self.assertIn("branch=staging&event=push&created=", path)
            self.assertNotIn("status=success", path)
            return {"total_count": len(self.runs), "workflow_runs": self.runs}
        if path == base:
            return self.latest
        if path == f"{base}/attempts/{ATTEMPT}":
            return self.exact
        if path.startswith(f"{base}/attempts/{ATTEMPT}/jobs?"):
            return {"jobs": self.jobs}
        if path.startswith(f"{base}/artifacts?"):
            return {"artifacts": self.artifacts}
        if path.endswith("/artifacts/987/zip"):
            self.assertTrue(raw)
            return self.archive
        self.fail(f"unexpected API request: {path}")

    def test_selects_exact_tested_release_for_a_different_main_sha(self):
        main = "e" * 40
        identity, data, value = production.discover(main)
        self.assertEqual(identity, f"{RUN}:{ATTEMPT}:987:{hashlib.sha256(data).hexdigest()}")
        self.assertEqual(value, manifest())
        self.git_matches.assert_any_call("diff", "--quiet", "--no-ext-diff",
                                         "--ignore-submodules=none", SHA, main, "--",
                                         *production.RELEASE_PATHS)

    def test_refuses_multiple_eligible_releases(self):
        self.runs.append({**run(), "id": RUN + 1})
        with patch.object(production, "exact_run"), patch.object(production, "check_jobs", return_value=True), patch.object(
            production, "verified_artifact", side_effect=[("first", b"x", {}), ("second", b"y", {})]
        ), patch.object(release, "api", side_effect=lambda path, **kw: (
            run() if f"/runs/{RUN + 1}" in path else self.api(path, **kw)
        )), self.assertRaisesRegex(ValueError, "multiple eligible staging releases"):
            production.discover(SHA)

    def test_refuses_no_release_or_failed_rerun(self):
        self.runs = [{**run(), "run_attempt": ATTEMPT + 1, "conclusion": "failure"}]
        with self.assertRaisesRegex(ValueError, "no eligible staging release"):
            production.discover(SHA)
        self.runs = [run()]
        self.latest = {**run(), "run_attempt": ATTEMPT + 1, "conclusion": "failure"}
        with self.assertRaisesRegex(ValueError, "attempt mismatch"):
            production.discover(SHA)

    def test_manual_runs_and_artifacts_cannot_promote(self):
        self.runs = [{**run(), "event": "workflow_dispatch"}]
        with self.assertRaisesRegex(ValueError, "no eligible"):
            production.discover(SHA)
        self.runs = [run()]
        self.artifacts[0]["name"] = f"vellum-staging-verification-{RUN}-{ATTEMPT}"
        with self.assertRaisesRegex(ValueError, "no eligible"):
            production.discover(SHA)

    def test_docs_only_push_without_publication_is_not_eligible(self):
        self.jobs[-1]["conclusion"] = "skipped"
        with self.assertRaisesRegex(ValueError, "no eligible"):
            production.discover(SHA)

    def test_rejects_missing_artifact_or_source_mismatch(self):
        self.artifacts.clear()
        with self.assertRaisesRegex(ValueError, "no eligible"):
            production.discover(SHA)
        self.publish(manifest())
        self.git_matches.side_effect = [True, False]
        with self.assertRaisesRegex(ValueError, "no eligible"):
            production.discover(SHA)

    def test_rejects_wrong_exact_attempt_and_required_job(self):
        self.exact["head_sha"] = "e" * 40
        with self.assertRaisesRegex(ValueError, "staging source"):
            production.discover(SHA)
        self.exact = run()
        self.jobs[0]["run_attempt"] = ATTEMPT - 1
        with self.assertRaisesRegex(ValueError, "wrong attempt"):
            production.discover(SHA)

    def test_rejects_bad_artifact_provenance_and_checksums(self):
        for key, value, pattern in (
            ("workflow_run", {}, "id"),
            ("workflow_run", {**self.artifacts[0]["workflow_run"], "id": RUN + 1}, "provenance"),
            ("digest", "sha256:" + "0" * 64, "archive checksum"),
        ):
            with self.subTest(key=key, value=value):
                self.publish(manifest())
                self.artifacts[0][key] = value
                with self.assertRaisesRegex((ValueError, KeyError), pattern):
                    production.discover(SHA)
        self.publish(manifest(), checksum="e" * 64)
        with self.assertRaisesRegex(ValueError, "manifest checksum"):
            production.discover(SHA)

    def test_rejects_valid_but_wrong_image_digest_and_bundle_hashes(self):
        value = manifest()
        value["images"]["backend"] = value["images"]["backend"][:-64] + "e" * 64
        self.publish(value)
        with self.assertRaisesRegex(ValueError, "image digests differ"):
            production.discover(SHA)
        value = manifest()
        value["deployment_files"]["cd/controller.py"] = "e" * 64
        self.publish(value)
        with self.assertRaisesRegex(ValueError, "six-file deployment bundle"):
            production.discover(SHA)

    def test_rejects_truncated_listing_instead_of_assuming_uniqueness(self):
        for total in (2, 1000):
            with patch.object(release, "api", return_value={"total_count": total, "workflow_runs": [run()]}):
                with self.assertRaisesRegex(ValueError, "incomplete|safe limit"):
                    production.paged("/test", "workflow_runs", limit=1000, count=True)


class GitEquivalenceTests(unittest.TestCase):
    def test_different_commit_ids_with_only_inert_changes_can_promote(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            git_run = subprocess.run

            def git(*args):
                return subprocess.check_output(["git", "-C", directory, *args], text=True).strip()

            def commit():
                git("add", ".")
                git("-c", "user.name=CI", "-c", "user.email=ci@example.invalid", "commit", "-qm", "test")
                return git("rev-parse", "HEAD")

            git("init", "-q")
            for path in ("backend/code", "frontend/code", "deploy/compose.yml", "deploy/ssm/production.json",
                         "deploy/production-release.json", "deploy/CI-CD.md", ".github/workflows/release-production.yml",
                         ".github/workflows/release-staging.yml", "README.md", "docs/guide.md"):
                (root / path).parent.mkdir(parents=True, exist_ok=True)
                (root / path).write_text("staging\n")
            stage = commit()
            for path in ("README.md", "docs/guide.md", "deploy/CI-CD.md", "deploy/production-release.json"):
                (root / path).write_text("main\n")
            main = commit()
            with patch.object(production.subprocess, "run", side_effect=lambda *a, **kw: git_run(*a, cwd=root, **kw)):
                self.assertNotEqual(main, stage)
                self.assertTrue(production.equivalent(stage, main))
                for path in ("backend/code", "frontend/code", "deploy/compose.yml", "deploy/ssm/production.json",
                             ".github/workflows/release-production.yml", ".github/workflows/release-staging.yml"):
                    with self.subTest(path=path):
                        (root / path).write_text("main\n")
                        different = commit()
                        self.assertFalse(production.equivalent(stage, different))
                        git("reset", "--hard", main)
                git("update-index", "--chmod=+x", "deploy/compose.yml")
                git("-c", "user.name=CI", "-c", "user.email=ci@example.invalid", "commit", "-qm", "mode")
                self.assertFalse(production.equivalent(stage, git("rev-parse", "HEAD")))


class DispatchTests(unittest.TestCase):
    def test_automated_status_never_reads_account_wide_ssm_output(self):
        command = "12345678-1234-1234-1234-123456789abc"
        value = manifest()
        with tempfile.TemporaryDirectory() as directory:
            summary = Path(directory) / "summary"
            output = Path(directory) / "output"
            with (patch.dict(os.environ, {"GITHUB_STEP_SUMMARY": str(summary), "GITHUB_OUTPUT": str(output)}),
                  patch.object(production, "invoke_authority", side_effect=[
                      {"command_id": command}, {"pending": True}, {"verified": True},
                  ]) as invoke, patch.object(production.time, "sleep")):
                production.independent_dispatch(value)
            checksum = hashlib.sha256(release.canonical(value)).hexdigest()
            self.assertEqual([call.args[0] for call in invoke.call_args_list], [
                {"manifest": value},
                {"command_id": command, "manifest_sha256": checksum},
                {"command_id": command, "manifest_sha256": checksum},
            ])
            self.assertIn(command, summary.read_text())
            self.assertEqual(output.read_text(), f"command_id={command}\n")

    def test_dispatch_uses_standard_host_approval_and_rechecks_main_last(self):
        data = release.canonical(manifest())
        identity = f"{RUN}:{ATTEMPT}:987:{hashlib.sha256(data).hexdigest()}"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "manifest"
            path.write_bytes(data)
            args = argparse.Namespace(manifest=str(path))
            with patch.dict(os.environ, {"EXPECTED_RELEASE": identity}), patch.object(production, "guard", return_value=SHA), patch.object(
                production, "discover", return_value=(identity, data, manifest())
            ) as discover, patch.object(production, "current_staging"), patch.object(release, "api", return_value=run()), patch.object(
                production, "current_main", side_effect=ValueError("main advanced")
            ) as current, patch.object(release, "dispatch") as dispatch:
                with self.assertRaisesRegex(ValueError, "main advanced"):
                    production.dispatch(args)
                dispatch.assert_not_called()
                current.side_effect = None
                production.dispatch(args)
                self.assertEqual(dispatch.call_args.args[0].approval_mode, "standard")
                dispatch.reset_mock()
                with (patch.dict(os.environ, {"PRODUCTION_READINESS_MODE": "automated"}),
                      patch.object(production, "independent_dispatch") as independent):
                    production.dispatch(args)
                    independent.assert_called_once_with(manifest())
                    dispatch.assert_not_called()
                dispatch.reset_mock()
                discover.return_value = (identity.replace(":987:", ":988:"), data, manifest())
                with self.assertRaisesRegex(ValueError, "artifact changed"):
                    production.dispatch(args)
                dispatch.assert_not_called()


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
            "confirm_production_deployment"])
        self.assertIn("required: true\n        default: false\n        type: boolean", triggers)
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
        self.assertIn("timeout-minutes: 30", self.promote)
        self.assertEqual(re.findall(r"uses: ([^\s]+)", self.workflow), [
            "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803",
            "actions/checkout@d23441a48e516b6c34aea4fa41551a30e30af803",
            "aws-actions/configure-aws-credentials@e1253824e5c10ff9df46874f81ed3ec929e19cfd",
            "aws-actions/configure-aws-credentials@e1253824e5c10ff9df46874f81ed3ec929e19cfd",
        ])
        self.assertIn("ref: ${{ github.sha }}", self.promote)
        self.assertIn("fetch-depth: 0", self.promote)
        self.assertIn("persist-credentials: false", self.promote)
        for name in ("AWS_DEPLOY_ROLE_ARN", "AWS_REGION", "AWS_ACCOUNT_ID", "SSM_INSTANCE_ID", "SSM_DOCUMENT_ARN"):
            self.assertIn("${{ vars." + name + " }}", self.promote)
        self.assertIn("needs: discover", self.promote)
        discover = self.workflow.split("  discover:\n", 1)[1].split("  promote:", 1)[0]
        self.assertNotIn("environment:", discover)
        self.assertNotIn("id-token:", discover)
        for name in ("PRODUCTION_READINESS_MODE", "VELLUM_DISPATCH_FUNCTION",
                     "VELLUM_DISPATCH_AUDIENCE", "AWS_READINESS_ROLE_ARN"):
            self.assertIn("${{ vars." + name + " }}", self.promote)
        self.assertIn("SSM_DOCUMENT_VERSION: '2'", self.promote)
        self.assertIn("SSM_DOCUMENT_SHA256: 8d43e90f07c95456d7f781b591cc187eaf448e0c80b3a87d0433b9665297f83a", self.promote)
        self.assertIn("role-session-name: vellum-production-${{ github.run_id }}", self.promote)
        self.assertLess(self.promote.index("production_release.py prepare"), self.promote.index("aws-actions/configure-aws-credentials"))
        self.assertLess(self.promote.index("aws-actions/configure-aws-credentials"), self.promote.index("production_release.py dispatch"))
        self.assertIn("production_release.py dispatch --manifest approved-release.json", self.promote)
        self.assertNotIn("release.py dispatch --environment production", self.promote)
        self.assertNotIn("continue-on-error", self.promote)
        self.assertNotIn("if: always()", self.promote)
        self.assertNotIn("if: failure()", self.promote)


if __name__ == "__main__":
    unittest.main()
