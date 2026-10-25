import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("changes", ROOT / "scripts/ci/changes.py")
changes = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(changes)


class ChangeTests(unittest.TestCase):
    def test_component_scopes_and_cross_application_browser_suite(self):
        for component, path in (("frontend", "frontend/src/app.tsx"), ("backend", "backend/internal/routes/routes.go")):
            with self.subTest(component=component):
                plan, _ = changes.classify([path])
                other = "frontend" if component == "backend" else "backend"
                self.assertTrue(plan[component])
                self.assertTrue(plan["image_" + component])
                self.assertFalse(plan[other])
                self.assertFalse(plan["image_" + other])
                for check in ("browser", "docker", "deployment", "deploy"):
                    self.assertTrue(plan[check])

    def test_documentation_does_not_publish_or_deploy(self):
        plan, categories = changes.classify(["README.md", "docs/screenshots/login.png", "deploy/CI-CD.md"])
        self.assertEqual(categories, ["documentation"])
        self.assertFalse(any(plan.values()))

    def test_component_docs_preserve_whole_tree_promotion_contract(self):
        for path in ("backend/README.md", "frontend/README.md", "backend/doc/ERD.png"):
            plan, categories = changes.classify([path])
            component = path.split("/")[0]
            self.assertIn("documentation", categories)
            self.assertTrue(plan[component])
            self.assertTrue(plan["image_" + component])
            self.assertTrue(plan["deploy"])

    def test_shared_workflow_and_ambiguous_paths_select_everything(self):
        for path in (".github/workflows/ci.yml", ".github/CODEOWNERS", ".dockerignore", "scripts/ci/changes.py", "new-config", "../escape", "/absolute", "package.json", "README.MD"):
            with self.subTest(path=path):
                self.assertTrue(all(changes.classify([path])[0].values()))
        self.assertTrue(all(changes.classify([])[0].values()))

    def test_dependencies_and_test_changes_are_not_docs(self):
        for path in ("frontend/pnpm-lock.yaml", "frontend/package.json", "frontend/.dockerignore", "frontend/Dockerfile.prod", "frontend/tests/login.spec.ts", "backend/go.mod", "backend/go.sum", "backend/.dockerignore", "backend/internal/a_test.go"):
            plan, _ = changes.classify([path])
            self.assertTrue(plan[path.split('/')[0]])
            self.assertTrue(plan['browser'])
            self.assertTrue(plan['deploy'])

    def test_frontend_browser_test_files_are_classified_as_tests(self):
        for path in ("frontend/e2e/mobile.spec.ts", "frontend/playwright.config.ts", "frontend/tsconfig.e2e.json"):
            plan, categories = changes.classify([path])
            self.assertIn("tests", categories)
            self.assertTrue(plan["frontend"] and plan["browser"])

    def test_security_and_deployment_always_validate_safety(self):
        for path in ("deploy/iam/policy.json", "deploy/ssm/staging.json", "deploy/cd/controller.py", "deploy/tests/test_release.py", "deploy/compose.registry.yml", "deploy/staging.env.example"):
            plan, _ = changes.classify([path])
            self.assertTrue(plan['deployment'])
            self.assertTrue(plan['docker'])
            self.assertTrue(plan['deploy'])

    def test_docs_cannot_hide_runtime_changes(self):
        plan, _ = changes.classify(["docs/guide.md", "backend/cmd/server/main.go"])
        self.assertTrue(plan['backend'])
        self.assertTrue(plan['deploy'])

    def test_guard_fails_closed_on_all_unsuccessful_detector_results(self):
        for result in ("failure", "cancelled", "skipped", "", "neutral", "timed_out"):
            with self.subTest(result=result), self.assertRaises(ValueError):
                changes.guard(result, json.dumps(dict.fromkeys(changes.KEYS, False)), "frontend")

    def test_guard_rejects_missing_malformed_or_non_boolean_outputs(self):
        for raw in ("", "null", "[]", "{}", '{"frontend":"false"}', json.dumps(dict.fromkeys(changes.KEYS, "false"))):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                changes.guard("success", raw, "frontend")
        with self.assertRaises(ValueError):
            changes.guard("success", json.dumps(dict.fromkeys(changes.KEYS, True)), "unknown")

    def test_explicit_unaffected_result_is_successful_but_not_selected(self):
        for key in changes.CHECKS:
            self.assertFalse(changes.guard("success", json.dumps(dict.fromkeys(changes.KEYS, False)), key))
            self.assertTrue(changes.guard("success", json.dumps(dict.fromkeys(changes.KEYS, True)), key))

    def test_failed_diff_and_initial_push_fall_back_to_full_plan(self):
        for event in ({}, {"before": "0" * 40, "after": "a" * 40}, {"before": "a" * 40, "after": "b" * 40}):
            with tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                (root / "event").write_text(json.dumps(event))
                env = {**os.environ, "GITHUB_EVENT_PATH": str(root / "event"), "GITHUB_EVENT_NAME": "push", "GITHUB_OUTPUT": str(root / "output"), "GITHUB_STEP_SUMMARY": str(root / "summary")}
                # Empty repository also exercises unavailable history.
                subprocess.run(["git", "init", "-q", temp], check=True)
                subprocess.run(["python3", str(ROOT / "scripts/ci/changes.py"), "detect"], cwd=temp, env=env, check=True)
                output = dict(line.split("=", 1) for line in (root / "output").read_text().splitlines())
                self.assertTrue(all(json.loads(output["plan"]).values()))
                self.assertEqual(output["deploy"], "true")

    def test_multicommit_push_deletions_renames_and_pr_merge_base(self):
        with tempfile.TemporaryDirectory() as temp:
            def git(*args):
                return subprocess.check_output(["git", "-C", temp, *args], stderr=subprocess.PIPE).decode().strip()
            def commit():
                git("add", ".")
                git("-c", "user.name=CI", "-c", "user.email=ci@example.invalid", "commit", "-qm", "test")
                return git("rev-parse", "HEAD")
            root = Path(temp)
            git("init", "-q")
            (root / "backend").mkdir()
            (root / "docs").mkdir()
            (root / "backend/code.go").write_text("runtime")
            base = commit()
            git("mv", "backend/code.go", "docs/code.md")
            commit()
            (root / "docs/guide.md").write_text("docs")
            head = commit()
            def local_git(*args):
                return subprocess.check_output(["git", "-C", temp, *args], stderr=subprocess.PIPE)
            with patch.object(changes, "git", side_effect=local_git):
                paths = changes.changed_paths({"before": base, "after": head}, "push")
                self.assertIn("backend/code.go", paths)
                self.assertIn("docs/code.md", paths)
                self.assertTrue(changes.classify(paths)[0]['backend'])
                git("checkout", "-qb", "base-branch", base)
                (root / "base-only").write_text("new base")
                newer_base = commit()
                paths = changes.changed_paths({"pull_request": {"base": {"sha": newer_base}, "head": {"sha": head}}}, "pull_request")
                self.assertNotIn("base-only", paths)
                self.assertIn("backend/code.go", paths)


class WorkflowContractTests(unittest.TestCase):
    def test_seven_contexts_are_always_guarded_and_not_conditionally_skipped(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertNotRegex(workflow, r'(?m)^  push:')
        self.assertIn('branches: [staging, main]', workflow)
        jobs = dict(re.findall(r'^  ([a-z-]+):\n(.*?)(?=^  [a-z-]+:|\Z)', workflow.split('jobs:\n', 1)[1], re.M | re.S))
        self.assertEqual(set(jobs), {'changes', 'frontend', 'backend', 'browser', 'docker-context', 'deployment-config', 'production-images'})
        for key, block in jobs.items():
            if key == 'changes':
                continue
            self.assertIn('    needs: changes\n    if: always()', block)
            self.assertIn('DETECTION_RESULT: ${{ needs.changes.result }}', block)
            self.assertIn('CHANGE_PLAN: ${{ needs.changes.outputs.plan }}', block)
            self.assertIn('run: python3 scripts/ci/changes.py guard', block)
            later = block.split('run: python3 scripts/ci/changes.py guard', 1)[1]
            for step in re.split(r'^      - ', later, flags=re.M)[1:]:
                self.assertIn("steps.scope.outputs.run == 'true'", step)
                self.assertNotIn('continue-on-error', step)
        self.assertNotIn('services:', jobs['backend'])
        self.assertIn('component: [backend, frontend]', jobs['production-images'])
        self.assertIn('Race detector with PostgreSQL integration tests', jobs['backend'])

    def test_caches_keep_dependency_and_trust_boundaries(self):
        workflow = (ROOT / '.github/workflows/ci.yml').read_text()
        self.assertEqual(workflow.count('cache: false'), 3)
        self.assertEqual(workflow.count("${{ github.job }}-${{ hashFiles('backend/go.mod', 'backend/go.sum') }}"), 6)
        self.assertIn("key: playwright-ubuntu-24.04-${{ runner.arch }}-${{ hashFiles('frontend/pnpm-lock.yaml') }}", workflow)
        self.assertIn('pnpm exec playwright install --with-deps chromium', workflow)
        self.assertIn('pnpm install --frozen-lockfile', workflow)
        self.assertIn("github.event_name == 'push' && github.ref == 'refs/heads/staging'", workflow)
        self.assertIn('type=gha,scope=release-${{ matrix.component }}', workflow)
        self.assertNotIn('id-token:', workflow)
        self.assertNotIn('packages:', workflow)

    def test_staging_requires_successful_ci_and_non_docs_plan_before_credentials(self):
        workflow = (ROOT / '.github/workflows/release-staging.yml').read_text()
        checks, publish = workflow.split('  publish-and-deploy:', 1)
        self.assertNotIn("vars.VELLUM_CD_ENABLED == 'true'", checks)
        self.assertIn('uses: ./.github/workflows/ci.yml', checks)
        self.assertIn("needs.checks.result == 'success' && needs.checks.outputs.deploy == 'true'", publish)
        self.assertIn('github.ref_protected', publish)
        self.assertIn('environment: staging', publish)
        self.assertIn('cancel-in-progress: false', workflow)


if __name__ == '__main__':
    unittest.main()
