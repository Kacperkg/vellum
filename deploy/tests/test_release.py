from datetime import datetime, timezone, timedelta
import hashlib
import base64
import io
import json
import os
from pathlib import Path
import subprocess
import stat
import sys
import tempfile
import types
import unittest
from unittest.mock import patch
import urllib.request
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cd"))
import release
import controller
import smoke

REPOSITORY = "kacperkg/vellum"


def manifest(verified=True):
    return {
        "schema": 1,
        "repository": REPOSITORY,
        "source_sha": "a" * 40,
        "run_id": 12345,
        "run_attempt": 2,
        "images": {
            name: f"ghcr.io/{REPOSITORY}-{name}@sha256:"
            + ("b" if name == "backend" else "c") * 64
            for name in ("backend", "frontend")
        },
        "deployment_files": {name: "d" * 64 for name in release.BUNDLE},
        "staging": {
            "verified": verified,
            "command_id": "12345678-1234-1234-1234-123456789abc" if verified else None,
        },
    }


def successful_run():
    return {
        "id": 12345,
        "run_attempt": 2,
        "head_repository": {"full_name": "Kacperkg/vellum"},
        "event": "push",
        "head_branch": "staging",
        "path": release.WORKFLOW,
        "status": "completed",
        "conclusion": "success",
        "head_sha": "a" * 40,
    }


class ManifestTests(unittest.TestCase):
    def test_dispatch_pins_document_and_passes_data_without_shell(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp) / "release.json"
            source.write_bytes(release.canonical(manifest()))
            output = Path(tmp) / "outputs"
            env = {
                "GITHUB_REPOSITORY": REPOSITORY,
                "GITHUB_REF": "refs/heads/main",
                "SSM_INSTANCE_ID": "i-1234567890abcdef0",
                "SSM_DOCUMENT_ARN": "arn:aws:ssm:eu-west-2:123456789012:document/VellumDeploy-production",
                "SSM_DOCUMENT_VERSION": "3",
                "SSM_DOCUMENT_SHA256": "e" * 64,
                "GITHUB_OUTPUT": str(output),
            }
            with (
                patch.dict(os.environ, env),
                patch.object(
                    release.subprocess,
                    "check_output",
                    return_value="12345678-1234-1234-1234-123456789abc\n",
                ) as send,
                patch.object(
                    release.subprocess,
                    "run",
                    return_value=subprocess.CompletedProcess([], 0, "Success\n", ""),
                ),
            ):
                release.dispatch(
                    types.SimpleNamespace(manifest=source, environment="production")
                )
                argv = send.call_args.args[0]
                self.assertIn("--document-hash", argv)
                self.assertEqual(argv[argv.index("--document-version") + 1], "3")
                parameters = json.loads(argv[argv.index("--parameters") + 1])
                self.assertEqual(
                    json.loads(base64.b64decode(parameters["Manifest"][0])), manifest()
                )
                self.assertFalse(send.call_args.kwargs.get("shell", False))
                self.assertIn("command_id=", output.read_text())
            with (
                patch.dict(
                    os.environ,
                    {
                        **env,
                        "SSM_DOCUMENT_ARN": "arn:aws:ssm:eu-west-2:123456789012:document/AWS-RunShellScript",
                    },
                ),
                patch.object(release.subprocess, "check_output") as send,
            ):
                with self.assertRaisesRegex(ValueError, "unexpected SSM document"):
                    release.dispatch(
                        types.SimpleNamespace(manifest=source, environment="production")
                    )
                send.assert_not_called()

    def test_accepts_exact_digest_references(self):
        release.validate_manifest(manifest(), REPOSITORY, verified=True)

    def test_rejects_untrusted_or_unverified_manifest(self):
        mutations = [
            lambda m: m.update(repository="attacker/vellum"),
            lambda m: m.update(source_sha="--upload-pack=evil"),
            lambda m: m.update(run_id=True),
            lambda m: m["images"].update(
                backend="ghcr.io/attacker/backend@sha256:" + "b" * 64
            ),
            lambda m: m["images"].update(
                backend="ghcr.io/kacperkg/vellum-backend:latest"
            ),
            lambda m: m["images"].update(
                frontend="ghcr.io/kacperkg/vellum-frontend@sha256:" + "C" * 64
            ),
            lambda m: m["staging"].update(verified=False, command_id=None),
            lambda m: m["staging"].update(verified="true"),
            lambda m: m["deployment_files"].update({"../script": "a" * 64}),
            lambda m: m.update(shell="touch /tmp/evil"),
        ]
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                value = manifest()
                mutate(value)
                with self.assertRaises(ValueError):
                    release.validate_manifest(value, REPOSITORY, verified=True)

    def test_checks_workflow_identity_and_branch(self):
        release.validate_run(successful_run(), REPOSITORY, 12345, 2)
        for key, bad in (
            ("id", 42),
            ("run_attempt", 1),
            ("event", "pull_request"),
            ("head_branch", "feature"),
            ("path", ".github/workflows/evil.yml"),
            ("conclusion", "failure"),
            ("status", "in_progress"),
            ("head_repository", {"full_name": "attacker/vellum"}),
        ):
            with self.subTest(key=key):
                run = successful_run()
                run[key] = bad
                with self.assertRaises(ValueError):
                    release.validate_run(run, REPOSITORY, 12345, 2)

    def test_redirect_never_leaks_authorization_to_storage(self):
        request = urllib.request.Request(
            "https://api.github.com/repos/kacperkg/vellum/actions/artifacts/1/zip",
            headers={"Authorization": "Bearer synthetic-test-token"},
        )
        redirected = release.SafeRedirect().redirect_request(
            request, None, 302, "Found", {}, "https://storage.example.invalid/signed"
        )
        self.assertIsNone(redirected.get_header("Authorization"))
        with self.assertRaises(ValueError):
            release.SafeRedirect().redirect_request(
                request, None, 302, "Found", {}, "http://storage.example.invalid/signed"
            )

    def test_resolve_requires_reviewed_artifact_and_matching_trees(self):
        data = release.canonical(manifest())
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("release.json", data)

        def fake_api(path, raw=False):
            if "/attempts/" in path:
                return successful_run()
            if path.endswith("/zip"):
                return buffer.getvalue()
            return {
                "artifacts": [
                    {"id": 987, "name": "vellum-release-12345-2", "expired": False}
                ]
            }

        selection = {
            "staging_run_id": 12345,
            "staging_run_attempt": 2,
            "manifest_sha256": hashlib.sha256(data).hexdigest(),
        }
        with tempfile.TemporaryDirectory() as tmp:
            selected = Path(tmp) / "selection.json"
            selected.write_bytes(release.canonical(selection))
            output = Path(tmp) / "release.json"
            args = types.SimpleNamespace(selection=selected, output=output)
            with (
                patch.dict(
                    os.environ,
                    {"GITHUB_REPOSITORY": REPOSITORY, "GITHUB_REF": "refs/heads/main"},
                ),
                patch.object(release, "api", side_effect=fake_api),
                patch.object(
                    release,
                    "bundle_hashes",
                    return_value=manifest()["deployment_files"],
                ),
                patch.object(release, "git", return_value="same-tree"),
            ):
                release.resolve(args)
                self.assertEqual(output.read_bytes(), data)
                selection["manifest_sha256"] = "e" * 64
                selected.write_bytes(release.canonical(selection))
                with self.assertRaisesRegex(ValueError, "digest mismatch"):
                    release.resolve(args)
                selection["manifest_sha256"] = hashlib.sha256(data).hexdigest()
                selected.write_bytes(release.canonical(selection))
                with patch.object(
                    release, "git", side_effect=["staging-tree", "different-main-tree"]
                ):
                    with self.assertRaisesRegex(ValueError, "main differs"):
                        release.resolve(args)


class ControllerTests(unittest.TestCase):
    def test_repeat_release_preserves_previous_distinct_release(self):
        value = manifest()
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            current = state / "staging-current.json"
            previous = state / "staging-previous.json"
            current.write_bytes(release.canonical(value))
            previous.write_text('{"previous":"retained"}\n')
            with (
                patch.object(controller, "STATE", state),
                patch.object(
                    controller, "bundle_hashes", return_value=value["deployment_files"]
                ),
            ):
                commands = types.SimpleNamespace(apply=lambda _: None)
                controller.deploy("staging", value, commands)
            self.assertEqual(
                json.loads(previous.read_bytes()), {"previous": "retained"}
            )

    def test_bootstrap_checks_library_permissions_before_import(self):
        def metadata(path):
            mode = (
                stat.S_IFDIR | 0o755
                if path.name in ("vellum-cd", "cd")
                else stat.S_IFREG | 0o644
            )
            return types.SimpleNamespace(st_uid=0, st_mode=mode)

        with patch.object(Path, "lstat", autospec=True, side_effect=metadata):
            controller.validate_installation()

        def writable_library(path):
            info = metadata(path)
            if path.name == "release.py":
                info.st_mode |= 0o020
            return info

        with patch.object(Path, "lstat", autospec=True, side_effect=writable_library):
            with self.assertRaisesRegex(ValueError, "installation file"):
                controller.validate_installation()

    def test_production_requires_fresh_release_specific_backup(self):
        now = datetime.now(timezone.utc)
        approval = {
            "manifest_sha256": hashlib.sha256(
                release.canonical(manifest())
            ).hexdigest(),
            "verified_at": now.isoformat(),
            "backup_created_at": (now - timedelta(hours=1)).isoformat(),
            "backup_reference": "private-drill-42",
            "off_host_copy_verified": True,
            "restore_verified": True,
            "schema_compatible": True,
            "trusted_proxy_reviewed": True,
        }
        controller.validate_approval(approval, manifest(), now)
        for key, bad in (
            ("manifest_sha256", "e" * 64),
            ("verified_at", (now - timedelta(days=2)).isoformat()),
            ("verified_at", (now + timedelta(minutes=1)).isoformat()),
            ("backup_created_at", (now - timedelta(days=2)).isoformat()),
            ("backup_created_at", (now + timedelta(minutes=1)).isoformat()),
            ("off_host_copy_verified", False),
            ("restore_verified", False),
            ("schema_compatible", False),
            ("trusted_proxy_reviewed", False),
        ):
            with self.subTest(key=key):
                value = dict(approval)
                value[key] = bad
                with self.assertRaises(ValueError):
                    controller.validate_approval(value, manifest(), now)

    def test_applies_only_targeted_app_services_after_both_pulls(self):
        value = manifest()
        calls = []
        resolved = {
            "name": "vellum-staging",
            "services": {
                "backend": {
                    "image": value["images"]["backend"],
                    "environment": {
                        "CORS_ALLOWED_ORIGINS": controller.ORIGINS["staging"],
                        "RATE_LIMIT_LOGIN_REQUESTS": "10",
                    },
                },
                "frontend": {"image": value["images"]["frontend"]},
            },
        }

        def run(args, **kwargs):
            calls.append(args)
            self.assertFalse(kwargs.get("shell", False))
            self.assertNotIn("DB_PASSWORD", kwargs["env"])
            self.assertNotIn("source", args)
            if "config" in args and "json" in args:
                stdout = json.dumps(resolved)
            elif args[:2] == ["docker", "ps"]:
                stdout = "abc123\n"
            elif "inspect" in args:
                stdout = json.dumps(
                    {
                        "Architecture": "amd64",
                        "Os": "linux",
                        "Config": {
                            "Labels": {
                                "org.opencontainers.image.revision": value["source_sha"]
                            }
                        },
                    }
                )
            else:
                stdout = ""
            return subprocess.CompletedProcess(args, 0, stdout, "")

        with (
            patch.object(controller.subprocess, "run", side_effect=run),
            patch.object(controller, "smoke") as verify,
        ):
            controller.Commands("staging", value).apply(value)
        pull = next(
            i for i, args in enumerate(calls) if "pull" in args and "up" not in args
        )
        update = next(i for i, args in enumerate(calls) if "up" in args)
        self.assertLess(pull, update)
        self.assertEqual(calls[pull][-2:], ["backend", "frontend"])
        self.assertEqual(calls[update][-2:], ["backend", "frontend"])
        self.assertIn("--no-deps", calls[update])
        self.assertIn("--no-build", calls[update])
        self.assertIn("never", calls[update])
        self.assertFalse(any("down" in args or "prune" in args for args in calls))
        verify.assert_called_once_with(
            controller.ORIGINS["staging"], staging=True, login_requests=10
        )

    def test_failed_update_never_marks_release_successful(self):
        commands = types.SimpleNamespace(
            apply=lambda _: (_ for _ in ()).throw(ValueError("failure")),
            diagnostics=lambda: None,
        )
        with (
            patch.object(
                controller, "bundle_hashes", return_value=manifest()["deployment_files"]
            ),
            patch.object(controller, "atomic_json") as write,
        ):
            with self.assertRaises(ValueError):
                controller.deploy("staging", manifest(), commands)
            write.assert_not_called()

    def test_does_not_print_sensitive_compose_error(self):
        with patch.object(
            controller.subprocess,
            "run",
            return_value=subprocess.CompletedProcess(
                [], 1, "DB_PASSWORD=synthetic-sensitive", "secret SQL"
            ),
        ):
            with self.assertRaises(ValueError) as error:
                controller.Commands("staging", manifest()).run(
                    "Compose validation", ["docker", "compose", "config", "--quiet"]
                )
            self.assertNotIn("synthetic-sensitive", str(error.exception))
            self.assertNotIn("secret SQL", str(error.exception))


class SmokeTests(unittest.TestCase):
    def test_smoke_checks_auth_and_spoofed_headers(self):
        calls = []
        attempts = 0

        def response(origin, path, body=None, method=None, headers=None):
            nonlocal attempts
            calls.append((path, method, headers))
            if method == "OPTIONS":
                return 404, {}, b""
            if path == "/ping":
                return 200, {}, b'{"message":"pong"}'
            if body is None:
                return 200, {"Content-Type": "text/html"}, b"<html>"
            if body == {}:
                return 400, {}, b'{"error":"bad request"}'
            attempts += 1
            if attempts <= 2:
                return 401, {}, b'{"error":"invalid credentials"}'
            return 429, {"Retry-After": "60"}, b'{"error":"rate limit exceeded"}'

        with patch.object(smoke, "request", side_effect=response):
            smoke.smoke(controller.ORIGINS["staging"], staging=True, login_requests=2)
        self.assertTrue(
            any(headers and "X-Forwarded-For" in headers for _, _, headers in calls)
        )

    def test_smoke_rejects_missing_rate_enforcement(self):
        with patch.object(smoke, "request", return_value=(200, {}, b"{}")):
            with self.assertRaises(ValueError):
                smoke.smoke(controller.ORIGINS["staging"], staging=True)


if __name__ == "__main__":
    unittest.main()
