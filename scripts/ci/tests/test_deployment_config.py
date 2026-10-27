"""Run the real validator with synthetic Docker/client fixtures, never live data."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]


class DeploymentConfigTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        (self.root / "scripts").mkdir()
        (self.root / "deploy").mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        shutil.copyfile(ROOT / "scripts/check-deployment-config.sh",
                        self.root / "scripts/check-deployment-config.sh")
        for name in ("compose.yml", "compose.registry.yml", "Caddyfile.example"):
            shutil.copyfile(ROOT / "deploy" / name, self.root / "deploy" / name)
        # Any default dotenv discovery must fail, rather than quietly using it.
        (self.root / "deploy/.env").write_text("POISON='unterminated\n")
        (self.root / ".env").write_text("POISON='unterminated\n")
        self.log = self.root / "calls.jsonl"
        self.executable("docker", """
import json, os, pathlib, sys
args = sys.argv[1:]
environment = {key: value for key, value in os.environ.items()
               if key.startswith(('COMPOSE_', 'PASSWORD_RESET_', 'SES_', 'AWS_', 'DOCKER_'))
               or key in ('APP_ENV', 'APP_ORIGIN', 'DB_PASSWORD', 'JWT_SECRET')}
with pathlib.Path(LOG).open('a') as output:
    output.write(json.dumps({'args': args, 'env': environment}) + '\\n')
if args[0] == 'compose':
    if '--env-file' not in args:
        raise SystemExit(21)
    if '--quiet' in args:
        raise SystemExit(FAIL_CONFIG)
    path = pathlib.Path(args[args.index('--env-file') + 1])
    values = dict(line.split('=', 1) for line in path.read_text().splitlines())
    project = args[args.index('--project-name') + 1]
    print(json.dumps({'name': project, 'volumes': {'postgres_data': {
        'name': project + '_postgres_data'}}, 'values': values}))
elif args[:1] != ['run']:
    raise SystemExit(22)
""")
        self.executable("python3", """
import os, pathlib, sys
if sys.argv[1:] == ['scripts/check-recovery-routing.py']:
    pathlib.Path(RECOVERY).write_text('checked')
else:
    os.execv(sys.executable, [sys.executable, *sys.argv[1:]])
""")
        self.env = {
            "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
            "HOME": str(self.root),
            "COMPOSE_FILE": "/missing/compose.yml", "COMPOSE_PROFILES": "ambient",
            "COMPOSE_ENV_FILES": "/missing/private.env", "COMPOSE_PROJECT_NAME": "ambient",
            "APP_ENV": "production", "PASSWORD_RESET_EMAIL_MODE": "ses",
            "PASSWORD_RESET_ALLOWED_RECIPIENTS": "ambient@example.invalid",
            "SES_FROM_ADDRESS": "ambient@example.invalid", "AWS_REGION": "ambient",
            "DB_PASSWORD": "ambient-synthetic-password", "JWT_SECRET": "ambient-synthetic-jwt",
            "DOCKER_HOST": "unix:///synthetic/docker.sock", "DOCKER_CONTEXT": "synthetic-context",
            "DOCKER_CONFIG": str(self.root / "docker-client"),
        }

    def executable(self, name, source, fail_config=0):
        path = self.bin / name
        constants = (f"LOG = {str(self.log)!r}\nFAIL_CONFIG = {fail_config}\n"
                     f"RECOVERY = {str(self.root / 'recovery')!r}\n")
        path.write_text(f"#!{sys.executable}\n" + constants + source)
        path.chmod(0o755)

    def run_validator(self):
        return subprocess.run(["bash", "scripts/check-deployment-config.sh"],
                              cwd=self.root, env=self.env, capture_output=True, text=True)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_all_compose_calls_isolate_dotenv_application_and_compose_settings(self):
        result = self.run_validator()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        compose = [call for call in calls if call["args"][0] == "compose"]
        self.assertEqual(len(compose), 6)
        for index, call in enumerate(compose):
            args, env = call["args"], call["env"]
            self.assertIn("--env-file", args)
            self.assertEqual({key: env[key] for key in self.env if key.startswith("DOCKER_")},
                             {key: self.env[key] for key in self.env if key.startswith("DOCKER_")})
            self.assertFalse(any(key.startswith(("PASSWORD_RESET_", "SES_", "AWS_")) for key in env))
            self.assertNotIn("APP_ENV", env)
            self.assertFalse(any(key in env for key in ("COMPOSE_FILE", "COMPOSE_PROFILES", "COMPOSE_ENV_FILES")))
            if index < 2:
                self.assertEqual(args[args.index("--env-file") + 1], os.devnull)
                self.assertEqual(env["COMPOSE_PROJECT_NAME"], "vellum-config-check")
                self.assertEqual(env["DB_PASSWORD"], "synthetic-test-only")
                self.assertEqual(env["JWT_SECRET"], "synthetic-config-validation-only")
                self.assertEqual(args[-2:], ["config", "--quiet"])
                self.assertEqual(args.count("-f"), index + 1)
            else:
                self.assertNotIn("COMPOSE_PROJECT_NAME", env)
                self.assertNotIn("DB_PASSWORD", env)
                self.assertNotIn("JWT_SECRET", env)
                self.assertNotEqual(args[args.index("--env-file") + 1], os.devnull)
                self.assertEqual(args[-3:], ["config", "--format", "json"])
        self.assertEqual(calls[-1]["args"][0], "run")
        self.assertIn("validate", calls[-1]["args"])
        self.assertEqual((self.root / "recovery").read_text(), "checked")
        self.assertNotIn("ambient", result.stdout + result.stderr)

    def test_compose_failure_stops_before_caddy_and_recovery_checks(self):
        docker = self.bin / "docker"
        docker.write_text(docker.read_text().replace("FAIL_CONFIG = 0", "FAIL_CONFIG = 7"))
        result = self.run_validator()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(self.calls()), 1)
        self.assertFalse((self.root / "recovery").exists())


if __name__ == "__main__":
    unittest.main()
