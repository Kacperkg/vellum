"""Exercise the real shell helper with synthetic runner files and no root/network."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
SCRIPT = ROOT / "scripts/ci/prepare-playwright-apt.sh"
SOURCE = """Types: deb
URIs: mirror+file:/etc/apt/apt-mirrors.txt
Suites: noble noble-updates noble-backports
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
Architectures: amd64

Types: deb
URIs: mirror+file:/etc/apt/apt-mirrors.txt
Suites: noble-security
Components: main restricted universe multiverse
Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg
"""
MIRRORS = ("https://archive.ubuntu.com/ubuntu/\tpriority:1\n"
           "https://security.ubuntu.com/ubuntu/\tpriority:2\n")
CONFIG = ('Acquire::Retries "1";\nAcquire::http::Timeout "15";\n'
          'Acquire::https::Timeout "15";\nAPT::Update::Error-Mode "any";\n')


class PlaywrightAptTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.fixture = Path(self.temporary.name)
        self.bin = self.fixture / "bin"
        self.bin.mkdir()
        self.sources = self.fixture / "etc/apt/sources.list.d/ubuntu.sources"
        self.sources.parent.mkdir(parents=True)
        self.sources.write_text(SOURCE)
        self.os_release = self.fixture / "etc/os-release"
        self.os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')
        self.mirrors = self.fixture / "etc/apt/apt-mirrors.txt"
        self.mirrors.write_text('http://azure.archive.ubuntu.com/ubuntu/\tpriority:1\n')
        self.config = self.fixture / "etc/apt/apt.conf.d/zz-vellum-playwright"
        self.config.parent.mkdir()
        self.extra = self.sources.with_name("microsoft.sources")
        self.extra.write_text("URIs: https://packages.microsoft.com/ubuntu/24.04/prod\n")
        self.env = {**os.environ, "PATH": str(self.bin) + os.pathsep + os.environ["PATH"],
                    "APT_FIXTURE": str(self.fixture)}
        # Forward embedded Python unchanged, mapping only its fixed read-only
        # system-file arguments. No production helper accepts a test-root option.
        self.executable("python3", """
import os, sys
args = [os.environ['APT_FIXTURE'] + a if a.startswith('/etc/') else a for a in sys.argv[1:]]
os.execv(sys.executable, [sys.executable, *args])
""")
        self.executable("sudo", """
import json, os, pathlib, sys
root = pathlib.Path(os.environ['APT_FIXTURE'])
args = sys.argv[1:]
with (root / 'commands').open('a') as log:
    log.write(json.dumps(args) + '\\n')
assert len(args) == 2 and args[0] == 'tee', args
assert args[1] in ['/etc/apt/apt-mirrors.txt', '/etc/apt/apt.conf.d/zz-vellum-playwright'], args
if os.environ.get('FAIL_WRITE') == args[1]:
    raise SystemExit(9)
(root / args[1].lstrip('/')).write_text(sys.stdin.read())
""")
        self.executable("apt-config", """
import os, pathlib, sys
assert sys.argv[1:] == ['dump']
if os.environ.get('FAIL_APT_CONFIG'):
    raise SystemExit(8)
config = pathlib.Path(os.environ['APT_FIXTURE']) / 'etc/apt/apt.conf.d/zz-vellum-playwright'
print(config.read_text().replace('"15"', '"30"') if os.environ.get('OVERRIDE_TIMEOUT') else config.read_text())
""")

    def executable(self, name, source):
        path = self.bin / name
        path.write_text(f"#!{sys.executable}\n" + source)
        path.chmod(0o755)

    def prepare(self):
        return subprocess.run(["bash", str(SCRIPT)], env=self.env, capture_output=True, text=True)

    def assert_unchanged_failure(self):
        before = self.mirrors.read_bytes()
        result = self.prepare()
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.mirrors.read_bytes(), before)
        self.assertFalse(self.config.exists())
        self.assertFalse((self.fixture / "commands").exists())

    def test_https_native_fallback_preserves_sources_keys_security_and_other_repos(self):
        original, extra = self.sources.read_bytes(), self.extra.read_bytes()
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.mirrors.read_text(), MIRRORS)
        self.assertEqual(self.config.read_text(), CONFIG)
        self.assertEqual(self.sources.read_bytes(), original)
        self.assertEqual(self.extra.read_bytes(), extra)
        self.assertIn("one retry, 15s inactivity timeouts", result.stdout)
        commands = [json.loads(line) for line in (self.fixture / "commands").read_text().splitlines()]
        self.assertEqual(commands, [["tee", "/etc/apt/apt-mirrors.txt"],
                                    ["tee", "/etc/apt/apt.conf.d/zz-vellum-playwright"]])
        self.assertEqual(self.prepare().returncode, 0)
        self.assertEqual(self.mirrors.read_text(), MIRRORS)

    def test_multiline_and_mixed_security_suites_are_preserved(self):
        self.sources.write_text(SOURCE.split("\n\n")[0].replace(
            "Suites: noble noble-updates noble-backports",
            "# Security may share the archive stanza.\nSuites: noble\n noble-updates noble-backports noble-security"))
        original = self.sources.read_bytes()
        self.assertEqual(self.prepare().returncode, 0)
        self.assertEqual(self.sources.read_bytes(), original)

    def test_unsupported_distro_or_missing_source_fails_before_privileged_writes(self):
        for value in ('ID=debian\nVERSION_ID="24.04"\n', 'ID=ubuntu\nVERSION_ID="22.04"\n'):
            with self.subTest(value=value):
                self.os_release.write_text(value)
                self.assert_unchanged_failure()
        self.os_release.write_text('ID=ubuntu\nVERSION_ID="24.04"\n')
        self.sources.unlink()
        self.assert_unchanged_failure()

    def test_unknown_uri_trust_components_or_suites_fail_closed(self):
        mutations = [
            ("mirror+file:/etc/apt/apt-mirrors.txt", uri) for uri in (
                "http://azure.archive.ubuntu.com/ubuntu/", "https://archive.ubuntu.com.evil/ubuntu/",
                "https://attacker@archive.ubuntu.com/ubuntu/", "mirror+file:/tmp/apt-mirrors.txt")
        ] + [
            ("/usr/share/keyrings/ubuntu-archive-keyring.gpg", "/tmp/other.gpg"),
            ("main restricted universe multiverse", "main universe"),
            ("noble-security", "noble-proposed"),
            ("Types: deb", "Types: deb-src"),
            ("URIs:", "URIs: invalid\nURIs:"),
        ]
        for old, new in mutations:
            with self.subTest(new=new):
                self.sources.write_text(SOURCE.replace(old, new))
                self.assert_unchanged_failure()

    def test_disabled_security_stanza_does_not_count_as_security_updates(self):
        self.sources.write_text(SOURCE.replace("Suites: noble-security", "Enabled: no\nSuites: noble-security"))
        self.assert_unchanged_failure()

    def test_write_or_effective_configuration_failure_is_fatal(self):
        for key, value in (("FAIL_WRITE", "/etc/apt/apt-mirrors.txt"),
                           ("FAIL_WRITE", "/etc/apt/apt.conf.d/zz-vellum-playwright"),
                           ("FAIL_APT_CONFIG", "1"), ("OVERRIDE_TIMEOUT", "1")):
            with self.subTest(key=key, value=value):
                self.env[key] = value
                result = self.prepare()
                self.assertNotEqual(result.returncode, 0)
                del self.env[key]

    def test_installer_and_package_audit_failures_are_not_masked(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        block = workflow.split("      - name: Install CSP browsers and complete operating-system dependencies\n", 1)[1]
        code = block.split("        run: |\n", 1)[1].split("      - ", 1)[0]
        code = "\n".join(line[10:] for line in code.splitlines())
        self.assertIn("pnpm exec playwright install --with-deps chromium firefox webkit", code)
        self.executable("pnpm", "import os, sys\nassert sys.argv[1:] == ['exec', 'playwright', 'install', '--with-deps', 'chromium', 'firefox', 'webkit']\nsys.exit(int(os.environ.get('INSTALL_EXIT', '0')))\n")
        self.executable("dpkg", "import os, sys\nassert sys.argv[1:] == ['--audit']\nprint(os.environ.get('AUDIT_OUTPUT', ''), end='')\nsys.exit(int(os.environ.get('AUDIT_EXIT', '0')))\n")
        for env, expected in (({}, 0), ({"INSTALL_EXIT": "7"}, 7),
                              ({"AUDIT_EXIT": "8"}, 8), ({"AUDIT_OUTPUT": "package left unconfigured"}, 1)):
            with self.subTest(env=env):
                result = subprocess.run(["bash", "-euo", "pipefail", "-c", code],
                                        env={**self.env, **env}, capture_output=True, text=True)
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
