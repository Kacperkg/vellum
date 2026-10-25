"""Manual release boundaries, exercised entirely with synthetic runner context."""
import hashlib
import importlib.util
import itertools
import json
import os
from pathlib import Path
import re
import tempfile
import types
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("staging_verification", ROOT / "scripts/ci/staging_verification.py")
verification = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(verification)
release = verification.release
SHA = "a" * 40


def context():
    return {
        "GITHUB_REPOSITORY": "Kacperkg/vellum",
        "GITHUB_EVENT_NAME": "workflow_dispatch",
        "GITHUB_REF": "refs/heads/staging",
        "GITHUB_REF_PROTECTED": "true",
        "GITHUB_WORKFLOW_REF": "Kacperkg/vellum/.github/workflows/release-staging.yml@refs/heads/staging",
        "VELLUM_CD_ENABLED": "true",
        "CONFIRM_STAGING_DEPLOYMENT": "true",
        "EXPECTED_SHA": SHA,
        "GITHUB_SHA": SHA,
        "GITHUB_RUN_ID": "12345",
        "GITHUB_RUN_ATTEMPT": "2",
    }


class ManualRequestTests(unittest.TestCase):
    def test_exact_protected_staging_context_and_checkout_are_required(self):
        with patch.dict(os.environ, context(), clear=True), patch.object(release, "git", return_value=SHA) as git:
            self.assertEqual(verification.guard(), SHA)
            self.assertEqual(git.call_args_list[0].args, ("rev-parse", "HEAD"))
            self.assertEqual(git.call_args_list[1].args, ("rev-parse", "refs/remotes/origin/staging"))

    def test_foreign_unprotected_unapproved_and_nonmanual_contexts_fail(self):
        mutations = {
            "GITHUB_REPOSITORY": ("attacker/vellum", "Kacperkg/Vellum", ""),
            "GITHUB_EVENT_NAME": ("push", "pull_request", "workflow_call", ""),
            "GITHUB_REF": ("refs/heads/main", "refs/heads/feature/test", "refs/tags/staging", ""),
            "GITHUB_REF_PROTECTED": ("false", "True", "1", ""),
            "GITHUB_WORKFLOW_REF": (
                "Kacperkg/vellum/.github/workflows/other.yml@refs/heads/staging",
                "Kacperkg/vellum/.github/workflows/release-staging.yml@refs/heads/main",
                "attacker/vellum/.github/workflows/release-staging.yml@refs/heads/staging",
                "",
            ),
            "VELLUM_CD_ENABLED": ("false", "True", "1", ""),
            "CONFIRM_STAGING_DEPLOYMENT": ("false", "True", "1", ""),
            "EXPECTED_SHA": ("", "a" * 39, "a" * 41, "A" * 40, "0" * 40, "b" * 40, "$(id)", SHA + "\n"),
            "GITHUB_SHA": ("", "b" * 40),
            "GITHUB_RUN_ID": ("", "0", "-1", "01", "1.0", "1\n", "1" * 19),
            "GITHUB_RUN_ATTEMPT": ("", "0", "-1", "01", "x", "1" * 19),
        }
        for key, values in mutations.items():
            for value in values:
                with self.subTest(key=key, value=value), patch.dict(os.environ, {**context(), key: value}, clear=True), patch.object(release, "git", return_value=SHA), self.assertRaises(ValueError):
                    verification.guard()
        for key in context():
            missing = context()
            del missing[key]
            with self.subTest(missing=key), patch.dict(os.environ, missing, clear=True), patch.object(release, "git", return_value=SHA), self.assertRaises(ValueError):
                verification.guard()

    def test_stale_dispatch_and_incorrect_checkout_fail(self):
        for revisions in (("b" * 40, SHA), (SHA, "b" * 40)):
            with self.subTest(revisions=revisions), patch.dict(os.environ, context(), clear=True), patch.object(release, "git", side_effect=revisions), self.assertRaisesRegex(ValueError, "approved revision"):
                verification.guard()

    def test_candidate_preserves_schema_digest_provenance_and_real_six_file_hashes(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / "candidate.json"
            args = types.SimpleNamespace(backend="sha256:" + "b" * 64, frontend="sha256:" + "c" * 64, output=target)
            original = Path.cwd()
            try:
                os.chdir(ROOT)
                with patch.dict(os.environ, context(), clear=True), patch.object(release, "git", return_value=SHA):
                    verification.candidate(args)
            finally:
                os.chdir(original)
            data = target.read_bytes()
            value = release.validate_manifest(json.loads(data), "kacperkg/vellum")
            self.assertEqual(value["source_sha"], SHA)
            self.assertEqual((value["run_id"], value["run_attempt"]), (12345, 2))
            self.assertEqual(value["staging"], {"verified": False, "command_id": None})
            self.assertEqual(len(value["deployment_files"]), 6)
            self.assertEqual(set(value["deployment_files"]), set(release.BUNDLE))
            for name, digest in value["deployment_files"].items():
                self.assertEqual(digest, hashlib.sha256((ROOT / "deploy" / name).read_bytes()).hexdigest())
            self.assertEqual(value["images"]["backend"], "ghcr.io/kacperkg/vellum-backend@" + args.backend)
            self.assertEqual(value["images"]["frontend"], "ghcr.io/kacperkg/vellum-frontend@" + args.frontend)
            self.assertEqual(data, release.canonical(value))

    def test_bad_manual_guard_cannot_hash_bundle_or_write_candidate(self):
        with patch.dict(os.environ, {**context(), "CONFIRM_STAGING_DEPLOYMENT": "false"}, clear=True), patch.object(release, "bundle_hashes") as hashes:
            with self.assertRaises(ValueError):
                verification.candidate(types.SimpleNamespace())
            hashes.assert_not_called()

    def test_candidate_rejects_mutable_invalid_and_foreign_digest_inputs(self):
        for digest in ("latest", "sha-" + SHA, "sha256:" + "B" * 64, "sha256:" + "b" * 63, "ghcr.io/attacker/backend@sha256:" + "b" * 64, "sha256:" + "b" * 64 + ";id"):
            for component in ("backend", "frontend"):
                with tempfile.TemporaryDirectory() as temp:
                    target = Path(temp) / "candidate.json"
                    args = {"backend": "sha256:" + "b" * 64, "frontend": "sha256:" + "c" * 64, "output": target}
                    args[component] = digest
                    with self.subTest(component=component, digest=digest), patch.dict(os.environ, context(), clear=True), patch.object(release, "git", return_value=SHA), patch.object(release, "bundle_hashes", return_value=dict.fromkeys(release.BUNDLE, "d" * 64)), self.assertRaises(ValueError):
                        verification.candidate(types.SimpleNamespace(**args))
                    self.assertFalse(target.exists())

    def test_manual_or_unsuccessful_evidence_cannot_be_promoted(self):
        run = {
            "id": 12345, "run_attempt": 2,
            "head_repository": {"full_name": "Kacperkg/vellum"},
            "event": "push", "head_branch": "staging", "path": release.WORKFLOW,
            "status": "completed", "conclusion": "success", "head_sha": SHA,
        }
        self.assertEqual(release.validate_run(run, "kacperkg/vellum", 12345, 2), SHA)
        for event, conclusion in itertools.product(("push", "workflow_dispatch"), ("failure", "cancelled", "timed_out", "skipped", "success")):
            if event == "push" and conclusion == "success":
                continue
            with self.subTest(event=event, conclusion=conclusion), self.assertRaises(ValueError):
                release.validate_run({**run, "event": event, "conclusion": conclusion}, "kacperkg/vellum", 12345, 2)


class WorkflowBoundaryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.workflow = (ROOT / ".github/workflows/release-staging.yml").read_text()
        cls.jobs = dict(re.findall(r"^  ([a-z-]+):\n(.*?)(?=^  [a-z-]+:|\Z)", cls.workflow.split("jobs:\n", 1)[1], re.M | re.S))

    @staticmethod
    def condition(block):
        return re.search(r"^    if: >-\n(.*?)(?=^    [a-z])", block, re.M | re.S).group(1).strip()

    @staticmethod
    def evaluate(expression, values):
        # Evaluate the actual workflow conditions with a bounded synthetic context.
        for key in sorted(values, key=len, reverse=True):
            expression = expression.replace(key, repr(values[key]))
        expression = expression.replace("always()", "True").replace("cancelled()", "False")
        expression = expression.replace("&&", " and ").replace("||", " or ").replace("!", " not ")
        return eval(" ".join(expression.split()), {"__builtins__": {}}, {})

    def test_only_deployment_job_has_oidc_and_package_write_permissions(self):
        self.assertEqual(self.workflow.count("id-token: write"), 1)
        self.assertEqual(self.workflow.count("packages: write"), 1)
        self.assertIn("environment: staging", self.jobs["publish-and-deploy"])
        for key in ("verification-request", "checks"):
            self.assertNotIn("id-token:", self.jobs[key])
            self.assertNotIn("packages:", self.jobs[key])
        self.assertNotIn("environment: production", self.workflow)
        self.assertNotIn("pull_request_target", self.workflow)
        self.assertIn("group: vellum-deploy-staging\n  cancel-in-progress: false", self.workflow)

    def test_invalid_request_or_failed_ci_cannot_reach_privileged_job(self):
        condition = self.condition(self.jobs["publish-and-deploy"])
        for event, request, checks, selected, enabled, protected, repository, ref in itertools.product(
            ("push", "workflow_dispatch", "pull_request"),
            ("success", "failure", "skipped", "cancelled"),
            ("success", "failure", "skipped", "cancelled"),
            ("true", "false"), ("true", "false"), (True, False),
            ("Kacperkg/vellum", "attacker/vellum"),
            ("refs/heads/staging", "refs/heads/main", "refs/tags/staging"),
        ):
            values = {
                "github.event_name": event, "github.repository": repository,
                "github.ref": ref, "github.ref_protected": protected,
                "needs.verification-request.result": request,
                "needs.checks.result": checks, "needs.checks.outputs.deploy": selected,
                "vars.VELLUM_CD_ENABLED": enabled,
            }
            expected = (
                checks == "success" and selected == "true" and enabled == "true"
                and (event == "push" or (event == "workflow_dispatch" and request == "success"))
                and repository == "Kacperkg/vellum" and ref == "refs/heads/staging" and protected
            )
            self.assertEqual(bool(self.evaluate(condition, values)), expected, values)
        self.assertIn("!cancelled()", condition)
        self.assertIn("needs: [checks, verification-request]", self.jobs["publish-and-deploy"])

    def test_manual_ci_requires_successful_guard_and_push_ci_survives_skipped_guard(self):
        condition = self.condition(self.jobs["checks"])
        for event, request in itertools.product(("push", "workflow_dispatch", "pull_request"), ("success", "failure", "skipped", "cancelled")):
            values = {
                "github.event_name": event, "github.repository": "Kacperkg/vellum",
                "github.ref": "refs/heads/staging", "github.ref_protected": True,
                "needs.verification-request.result": request,
            }
            self.assertEqual(bool(self.evaluate(condition, values)), event == "push" or (event == "workflow_dispatch" and request == "success"), values)
        self.assertIn("uses: ./.github/workflows/ci.yml", self.jobs["checks"])
        self.assertIn("if: github.event_name == 'workflow_dispatch'", self.jobs["verification-request"])

    def test_release_rechecks_request_before_login_or_build_and_pins_checkout(self):
        deploy = self.jobs["publish-and-deploy"]
        self.assertLess(deploy.index("scripts/ci/staging_verification.py guard"), deploy.index("docker login"))
        self.assertLess(deploy.index("scripts/ci/staging_verification.py guard"), deploy.index("docker/build-push-action"))
        self.assertIn("ref: ${{ github.sha }}", deploy)
        self.assertIn("persist-credentials: false", deploy)
        self.assertIn("scripts/ci/staging_verification.py candidate", deploy)
        self.assertIn("python3 deploy/cd/release.py candidate", deploy)
        self.assertIn("SSM_DOCUMENT_VERSION: ${{ vars.SSM_DOCUMENT_VERSION }}", deploy)
        self.assertIn("SSM_DOCUMENT_SHA256: ${{ vars.SSM_DOCUMENT_SHA256 }}", deploy)
        self.assertIn("release.py dispatch --environment staging --manifest candidate.json", deploy)
        self.assertIn("'vellum-staging-verification' || 'vellum-release'", deploy)
        self.assertIn("overwrite: false", deploy)
        self.assertIn("if-no-files-found: error", deploy)
        self.assertIn("Manual staging deployment verification", self.workflow)


if __name__ == "__main__":
    unittest.main()
