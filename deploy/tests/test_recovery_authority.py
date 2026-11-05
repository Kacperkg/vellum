"""Disposable approval/SSM/controller proof, with real GitHub and issuer signature formats."""

import base64
from datetime import datetime, timedelta, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import zipfile
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(ROOT / "cd"), str(ROOT / "recovery"), str(ROOT.parent / "scripts/ci")]
from authority import API, Authority, ISSUER, WORKFLOW, digest, timestamp
from aws_authority import GitHub, Recovery, SSM
import controller
import coordination
import release
import production_release

NOW = datetime(2026, 11, 5, 13, 0, tzinfo=timezone.utc)
ACCOUNT = "123456789012"
INSTANCE = "i-1234567890abcdef0"
COMMAND = "12345678-1234-1234-1234-123456789abc"
STAGING = "87654321-1234-1234-1234-123456789abc"
SHA = "a" * 40
PINS = {env: {"document": f"arn:aws:ssm:eu-west-2:{ACCOUNT}:document/VellumDeploy-{env}",
              "version": "3", "sha256": "b" * 64, "instance": INSTANCE}
        for env in ("production", "staging")}


def run(*args, data=None):
    return subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout


def url64(value):
    return base64.urlsafe_b64encode(value).decode().rstrip("=")


def manifest():
    return {"schema": 1, "repository": "kacperkg/vellum", "source_sha": SHA,
            "run_id": 92, "run_attempt": 2,
            "images": {name: f"ghcr.io/kacperkg/vellum-{name}@sha256:" + "c" * 64
                       for name in ("backend", "frontend")},
            "deployment_files": dict.fromkeys(release.BUNDLE, hashlib.sha256(b"fixture independently reviewed bundle file").hexdigest()),
            "staging": {"verified": True, "command_id": STAGING}}


class FakeGitHub:
    def __init__(self, workflow):
        self.workflow = workflow
        self.status = "in_progress"
        self.head = SHA
        self.attempt = 1

    def __call__(self, path):
        routes = {
            f"{API}/branches/main": {"protected": True, "commit": {"sha": self.head}},
            f"{API}/actions/runs/91": {"id": 91, "run_attempt": self.attempt, "repository": {"id": 90},
                                       "head_repository": {"id": 90}, "event": "workflow_dispatch",
                                       "head_branch": "main", "head_sha": SHA, "path": WORKFLOW,
                                       "status": self.status, "conclusion": None},
            f"{API}/actions/runs/91/attempts/1/jobs?per_page=100": {"total_count": 1, "jobs": [
                {"id": 93, "name": "promote", "run_id": 91, "head_sha": SHA,
                 "status": "in_progress", "conclusion": None}]},
            f"{API}/contents/{WORKFLOW}?ref={SHA}": {"content": base64.b64encode(self.workflow).decode()},
            f"{API}/environments/production": {"deployment_branch_policy": {
                "custom_branch_policies": True, "protected_branches": False}, "protection_rules": []},
            f"{API}/environments/production/deployment-branch-policies": {
                "total_count": 1, "branch_policies": [{"name": "main", "type": "branch"}]},
        }
        return routes[path]

    def verify_release(self, sha, value):
        with patch.object(release, "api", side_effect=self.staging_api):
            GitHub().verify_release(sha, value)

    def verify_latest_attempt(self, value):
        with patch.object(release, "api", side_effect=self.staging_api):
            GitHub().verify_latest_attempt(value)

    @staticmethod
    def staging_api(path, raw=False):
        value = manifest()
        bundle = b"fixture independently reviewed bundle file"
        value["deployment_files"] = dict.fromkeys(release.BUNDLE, hashlib.sha256(bundle).hexdigest())
        run = {"id": value["run_id"], "run_attempt": value["run_attempt"],
               "repository": {"id": 90}, "head_repository": {"full_name": "Kacperkg/vellum"},
               "status": "completed", "conclusion": "success", "event": "push",
               "head_branch": "staging", "path": release.WORKFLOW, "head_sha": SHA}
        base = f"{API}/actions/runs/{run['id']}"
        if path.startswith(f"{production_release.STAGING_WORKFLOW}/runs?"):
            return {"total_count": 1, "workflow_runs": [run]}
        if path == f"{API}/git/ref/heads/staging":
            return {"object": {"sha": SHA}}
        if path == f"{API}/git/commits/{SHA}":
            return {"tree": {"sha": "c" * 40}}
        if path.startswith(f"{API}/contents/deploy/"):
            return {"type": "file", "encoding": "base64", "content": base64.b64encode(bundle).decode()}
        if path.startswith(f"{API}/compare/"):
            return {"status": "identical", "merge_base_commit": {"sha": SHA}}
        if path in (base, base + "/attempts/2"):
            return run
        if path.startswith(base + "/attempts/2/jobs?"):
            return {"jobs": [{"name": name, "run_id": run["id"], "run_attempt": 2, "head_sha": SHA,
                              "status": "completed", "conclusion": "success"}
                             for name in production_release.REQUIRED_JOBS]}
        data = release.canonical(value)
        archive = io.BytesIO()
        with zipfile.ZipFile(archive, "w") as zipped:
            zipped.writestr(zipfile.ZipInfo("release.json"), data)
        blob = archive.getvalue()
        if path.startswith(base + "/artifacts?"):
            return {"artifacts": [{"id": 151, "name": f"vellum-release-92-2-{hashlib.sha256(data).hexdigest()}-"
                                  + "c" * 64 + "-" + "c" * 64,
                                  "digest": "sha256:" + hashlib.sha256(blob).hexdigest(), "expired": False,
                                  "workflow_run": {"id": 92, "repository_id": 90, "head_repository_id": 90,
                                                   "head_branch": "staging", "head_sha": SHA}}]}
        if path == f"{API}/actions/artifacts/151/zip" and raw:
            return blob
        raise AssertionError(path)


class FakeSSM:
    def __init__(self, value):
        self.commands = {STAGING: self.entry(STAGING, "staging", "Success", SSM.parameters(
            value | {"staging": {"verified": False, "command_id": None}}, False))}
        self.hash = "b" * 64

    @staticmethod
    def entry(command, environment, status, parameters):
        return {"CommandId": command, "DocumentName": f"VellumDeploy-{environment}",
                "DocumentVersion": "3", "InstanceIds": [INSTANCE], "Parameters": parameters, "Status": status}

    def describe_document(self, Name, DocumentVersion):
        return {"Document": {"Owner": ACCOUNT, "Name": Name.rsplit("/", 1)[-1], "DocumentVersion": DocumentVersion,
                             "HashType": "Sha256", "Hash": self.hash, "Status": "Active"}}

    def send_command(self, **params):
        self.commands[COMMAND] = self.entry(COMMAND, "production", "InProgress", params["Parameters"])
        return {"Command": {"CommandId": COMMAND}}

    def list_commands(self, CommandId):
        return {"Commands": [self.commands[CommandId]]}

    def get_command_invocation(self, CommandId, InstanceId, PluginName):
        record = self.commands[CommandId]
        return record | {"InstanceId": InstanceId, "PluginName": PluginName}


class MemoryLedger:
    def __init__(self):
        self.attempts = set()
        self.records = {}
        self.consumed = set()
        self.baselines = {}

    def reserve(self, attempt, _until):
        if attempt in self.attempts:
            raise ValueError("dispatch already attempted")
        self.attempts.add(attempt)

    def record(self, command, item, _until):
        self.records[command] = item

    def lookup(self, command):
        return self.records.get(command)

    def consume(self, command):
        if command in self.consumed:
            raise ValueError("invocation already signed")
        self.consumed.add(command)

    def baseline(self, checksum):
        return self.baselines.get(checksum)

    def remember_baseline(self, checksum, command):
        self.baselines[checksum] = {"command": command, "confirmed": False}

    def confirm_baseline(self, checksum, command):
        if self.baselines[checksum]["command"] != command:
            raise ValueError("command differs")
        self.baselines[checksum]["confirmed"] = True


class FakeReceiptBucket:
    def __init__(self, receipts):
        self.receipts = receipts

    def list_objects_v2(self, Bucket, ExpectedBucketOwner, Prefix, MaxKeys):
        assert Bucket == "fixture-receipts" and ExpectedBucketOwner == ACCOUNT
        return {"IsTruncated": False, "Contents": [{"Key": Prefix + "T120000Z-" + "0" * 32 + ".json"}]}

    def get_object(self, Bucket, ExpectedBucketOwner, Key):
        assert Bucket == "fixture-receipts" and ExpectedBucketOwner == ACCOUNT
        return {"VersionId": "pinned-fixture", "Body": io.BytesIO(self.receipts[Key.split("/")[2]])}


class AuthorityTests(unittest.TestCase):
    def setUp(self):
        self.root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.openssl = shutil.which("openssl")
        self.rsa = self.root / "oidc.pem"
        self.ed = self.root / "issuer.pem"
        run(self.openssl, "genpkey", "-algorithm", "RSA", "-out", str(self.rsa))
        run(self.openssl, "genpkey", "-algorithm", "ED25519", "-out", str(self.ed))
        run(self.openssl, "pkey", "-in", str(self.ed), "-pubout",
            "-out", str(self.root / "production-readiness-key.pem"))
        modulus = run(self.openssl, "rsa", "-in", str(self.rsa), "-noout", "-modulus").strip().split(b"=")[1]
        self.jwks = {"keys": [{"kid": "fixture", "kty": "RSA", "n": url64(bytes.fromhex(modulus.decode())),
                               "e": url64(b"\x01\x00\x01")}]}
        self.value = manifest()
        self.github = FakeGitHub((ROOT.parent / WORKFLOW).read_bytes())
        self.ssm = FakeSSM(self.value)
        self.ledger = MemoryLedger()
        self.live = {"current_manifest_sha256": "0" * 64, "database_source_sha256": "1" * 64,
                     "backup_contract": "2" * 64, "schema_sha256": "3" * 64, "migration_contract_sha256": "a" * 64,
                     "proxy_sha256": "b" * 64,
                     "execution_sha256": "4" * 64, "runtime_sha256": "5" * 64,
                     "postgres": {"image_id": "sha256:" + "6" * 64, "version": "170011"}, "risk": "low"}
        self.backup = {"archive_sha256": "7" * 64, "completion_sha256": "8" * 64,
                       "manifest_sha256": "9" * 64, "archive_bytes": 64,
                       "contract": "2" * 64, "source_sha256": "1" * 64,
                       "created_at": "2026-11-05T11:00:00Z", "verified_at": "2026-11-05T12:59:00Z"}
        self.drill = {"archive_sha256": "7" * 64, "contract": "2" * 64, "source_sha256": "1" * 64,
                      "postgres": self.live["postgres"], "finished_at": "2026-11-04T13:00:00Z",
                      **{key: self.live[key] for key in ("schema_sha256", "execution_sha256", "runtime_sha256")}}
        self.now = NOW
        self.projection = None
        self.profile_sha256 = "a" * 64
        synthetic_evidence = os.environ.get("VELLUM_SYNTHETIC_RECOVERY_EVIDENCE")
        if synthetic_evidence:
            proof = json.loads(Path(synthetic_evidence).read_bytes())
            self.assertIs(proof["synthetic"], True)
            self.projection = proof["drill_evidence"]
            self.backup = self.projection["backup"]
            self.drill = self.projection["drill"]
            self.now = timestamp(self.drill["finished_at"]) + timedelta(seconds=1)
            self.profile_sha256 = self.projection["profile_sha256"]
            for key in ("postgres", "schema_sha256", "execution_sha256", "runtime_sha256"):
                self.live[key] = self.drill[key]
            self.live["database_source_sha256"] = self.backup["source_sha256"]
            self.live["backup_contract"] = self.backup["contract"]
            self.live["migration_contract_sha256"] = proof["migration_contract"]
        config = {"account": ACCOUNT, **PINS, "audience": "fixture", "repository_id": 90,
                  "workflow_sha256": hashlib.sha256(self.github.workflow).hexdigest(),
                  "environment_protection_sha256": digest([]),
                  "initial_manifest_sha256": self.live["current_manifest_sha256"],
                  "receipt_bucket": "fixture-receipts", "receipt_account": ACCOUNT,
                  "worker_profile_sha256": self.profile_sha256, "live_policy": {
                      key: value for key, value in self.live.items()
                      if key not in ("risk", "current_manifest_sha256")},
                  "worker_public_keys": {}}
        worker_key = self.root / "worker.pem"
        public_key = self.root / "worker-public.pem"
        run(self.openssl, "genpkey", "-algorithm", "ED25519", "-out", str(worker_key))
        run(self.openssl, "pkey", "-in", str(worker_key), "-pubout", "-out", str(public_key))
        config["worker_public_keys"] = dict.fromkeys(("backup", "drill"), public_key.read_text())
        self.worker_key = worker_key
        self.receipts = FakeReceiptBucket({})
        self.refresh_receipts()
        self.recovery = Recovery(self.receipts, config)
        real_run = subprocess.run
        self.enterContext(patch.object(subprocess, "run", side_effect=lambda args, **kw: (
            real_run([self.openssl, *args[1:]], **kw) if args[0] == "/usr/bin/openssl" else real_run(args, **kw))))
        self.authority = Authority(config, self.github, SSM(self.ssm, config), self.ledger, self.recovery,
                                   self.sign, now=lambda: self.now)

    def refresh_receipts(self):
        for prefix, domain, projection in (
            ("verified-backup", "vellum-production-verified-backup-v1", {"backup": self.backup}),
            ("restore-drill", "vellum-production-restore-drill-v1",
             {"drill": self.drill, "checks": ["baseline-backend-read-only-smoke"]}),
        ):
            evidence = {"schema": 1, "issuer": domain, "profile_sha256": self.profile_sha256} | projection
            if self.projection is not None:
                if prefix == "restore-drill":
                    evidence = self.projection
                else:
                    evidence["objects"] = self.projection["objects"]
            signature = self.sign(domain.encode() + b"\n" + release.canonical(evidence), self.worker_key)
            self.receipts.receipts[prefix] = release.canonical({"evidence": evidence, "signature": signature})

    def sign(self, data, key=None):
        path = self.root / "signed"
        path.write_bytes(data)
        return run(self.openssl, "pkeyutl", "-sign", "-rawin", "-inkey", str(key or self.ed), "-in", str(path)).hex()

    def token(self, **changes):
        epoch = int(self.now.timestamp())
        claims = {"iss": ISSUER, "aud": "fixture", "iat": epoch - 1, "nbf": epoch - 1, "exp": epoch + 120,
                  "jti": "jti-123", "repository": "Kacperkg/vellum", "repository_id": "90",
                  "ref": "refs/heads/main", "event_name": "workflow_dispatch", "environment": "production",
                  "workflow_ref": f"Kacperkg/vellum/{WORKFLOW}@refs/heads/main",
                  "sha": SHA, "workflow_sha": SHA, "run_id": "91", "run_attempt": "1", "check_run_id": "93"}
        claims.update(changes)
        message = (url64(b'{"alg":"RS256","kid":"fixture"}') + "." + url64(json.dumps(claims).encode())).encode()
        signature = run(self.openssl, "dgst", "-sha256", "-sign", str(self.rsa), data=message)
        return message.decode() + "." + url64(signature)

    def dispatch(self):
        return self.authority.dispatch({"token": self.token(), "manifest": self.value}, self.jwks)

    def test_approval_ssm_and_controller_lock_to_durable_receipt(self):
        self.assertEqual(self.dispatch(), {"command_id": COMMAND})
        commands = Mock()
        commands.prepare.return_value = (10, {})
        commands.readiness_baseline.return_value = self.live
        pending = self.root / coordination.PENDING

        def replace(*_args, **_kwargs):
            self.assertTrue(pending.is_file())

        commands.replace.side_effect = replace

        def relay(request, method="sign"):
            self.assertFalse(pending.exists())
            return self.authority.host({"method": method, "request": request})

        self.enterContext(patch.object(controller, "STATE", self.root))
        self.enterContext(patch.object(controller, "bundle_hashes", return_value=self.value["deployment_files"]))
        self.enterContext(patch.object(controller, "private_file", side_effect=lambda path: path))
        self.enterContext(patch.object(controller, "request_readiness", side_effect=relay))
        self.enterContext(patch.object(coordination, "validate_state"))
        self.enterContext(patch.object(coordination, "root_owned", return_value=True))
        self.enterContext(patch.object(controller, "datetime", wraps=datetime)).now.return_value = self.now
        controller.deploy("production", self.value, commands, "automated", COMMAND)
        self.assertFalse(pending.exists())
        commands.replace.assert_called_once()
        self.assertEqual(json.loads((self.root / "production-current.json").read_bytes()), self.value)
        proof = json.loads((self.root / f"production-readiness-{COMMAND}.json").read_bytes())
        self.assertEqual(proof["request"]["command_id"], COMMAND)
        with self.assertRaisesRegex(ValueError, "already signed"):
            self.authority.host({"method": "sign", "request": proof["request"]})

        checksum = digest(self.value)
        event = {"token": self.token(), "command_id": COMMAND, "manifest_sha256": checksum}
        self.assertEqual(self.authority.finish(event, self.jwks), {"pending": True})
        self.ssm.commands[COMMAND].update({
            "Status": "Success",
            "StandardOutputContent": f"Verified production receipt {COMMAND} {checksum}\n",
        })
        self.assertEqual(self.authority.finish(event, self.jwks), {"verified": True})
        self.assertTrue(self.ledger.baseline(checksum)["confirmed"])

    def test_dispatch_rejects_token_drift_or_reuse(self):
        for changed in ({"aud": "evil"}, {"ref": "refs/heads/staging"}, {"run_attempt": "2"}, {"exp": 1}):
            with self.subTest(changed=changed), self.assertRaises(ValueError):
                self.authority.dispatch({"token": self.token(**changed), "manifest": self.value}, self.jwks)
        self.assertEqual(self.dispatch(), {"command_id": COMMAND})
        with self.assertRaisesRegex(ValueError, "already attempted"):
            self.dispatch()

    def test_refuses_expired_and_tampered_worker_evidence_before_ssm(self):
        self.drill["finished_at"] = "2026-10-29T13:00:00Z"
        self.refresh_receipts()
        with self.assertRaisesRegex(ValueError, "unavailable"):
            self.dispatch()
        self.drill["finished_at"] = "2026-11-04T13:00:00Z"
        self.refresh_receipts()
        receipt = json.loads(self.receipts.receipts["verified-backup"])
        receipt["evidence"]["backup"]["archive_bytes"] += 1
        self.receipts.receipts["verified-backup"] = release.canonical(receipt)
        with self.assertRaises(subprocess.CalledProcessError):
            self.dispatch()
        self.assertNotIn(COMMAND, self.ssm.commands)

    def test_cancellation_during_receipt_reads_refuses_to_sign(self):
        self.dispatch()
        event = {"method": "sign", "request": {
            "schema": 2, "nonce": "f" * 64, "command_id": COMMAND,
            "manifest": self.value, "live": self.live,
        }}
        verify = self.recovery.evidence

        def cancel(live, now):
            evidence = verify(live, now)
            self.ssm.commands[COMMAND]["Status"] = "Cancelled"
            return evidence

        with patch.object(self.recovery, "evidence", side_effect=cancel):
            with self.assertRaisesRegex(ValueError, "status differs"):
                self.authority.host(event)
        self.assertNotIn(COMMAND, self.ledger.consumed)

    def test_signed_path_rejects_cancelled_run_wrong_pins_and_wrong_manifest(self):
        self.dispatch()
        challenge = {"schema": 2, "nonce": "f" * 64, "command_id": COMMAND, "manifest": self.value}
        event = {"method": "admit", "request": challenge}
        self.github.status = "completed"
        with self.assertRaisesRegex(ValueError, "cancelled"):
            self.authority.host(event)
        self.github.status = "in_progress"
        self.ssm.hash = "e" * 64
        with self.assertRaisesRegex(ValueError, "pins"):
            self.authority.host(event)
        self.ssm.hash = "b" * 64
        challenge["manifest"] = self.value | {"run_attempt": 3}
        with self.assertRaisesRegex(ValueError, "mismatched"):
            self.authority.host(event)


if __name__ == "__main__":
    unittest.main()
