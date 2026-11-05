"""Adversarial worker tests with versioned S3 fixtures and real Ed25519."""
import base64
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "recovery"))
import worker


class FakeCloud:
    def __init__(self):
        self.objects = {}
        self.calls = []
        self.replace_get = {}
        self.runner = worker.Runner(time.monotonic() + 30)

    def add(self, key, version, data):
        self.objects[key, version] = data
        return {"key": key, "version_id": version, "sha256": worker.digest(data), "size": len(data)}

    def aws(self, args, label, **_options):
        self.calls.append(args)
        if args[:2] == ["s3api", "get-bucket-location"]:
            return {"LocationConstraint": worker.REGION}
        assert "--expected-bucket-owner" in args
        key = args[args.index("--key") + 1]
        version = next(argument.split("=", 1)[1] for argument in args if argument.startswith("--version-id="))
        assert args[args.index("--checksum-mode") + 1] == "ENABLED"
        data = self.objects[key, version]
        metadata = {"VersionId": version, "ServerSideEncryption": "AES256", "ContentLength": len(data),
                    "ChecksumSHA256": base64.b64encode(bytes.fromhex(worker.digest(data))).decode()}
        if args[1] == "get-object":
            Path(args[-1]).write_bytes(self.replace_get.get((key, version), data))
        else:
            assert args[1] == "head-object"
        return metadata

    def call(self, args, label, **options):
        return self.runner.call(args, label, **options)


class WorkerTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.openssl = shutil.which("openssl")
        cls.clean_env = patch.dict(worker.CLEAN_ENV, PATH=os.environ["PATH"])
        cls.clean_env.start()
        cls.addClassCleanup(cls.clean_env.stop)

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        self.private = self.root / "private.pem"
        self.public = self.root / "public.pem"
        self.openssl_call("genpkey", "-algorithm", "ED25519", "-out", self.private)
        self.openssl_call("pkey", "-in", self.private, "-pubout", "-out", self.public)
        self.now = datetime.now(timezone.utc)
        self.run_id = (self.now - timedelta(minutes=10)).strftime("%Y%m%dT%H%M%SZ-") + "a" * 32
        self.marker = f"production/completed/{self.run_id}.json"
        self.profile = {
            "account_id": "123456789012", "bucket": "synthetic-backups",
            "receipt_bucket": "synthetic-receipts",
            "producer_key_id": "producer-v1", "producer_key_sha256": worker.file_digest(self.public),
            "source": {"system_identifier": "7253446013106688026", "database_oid": 16385,
                       "database_name": "synthetic"},
            "backup_contract": "a" * 64, "postgres_version": "170008", "postgres_role": "fixture",
            "postgres_image": "postgres@sha256:" + "b" * 64, "postgres_image_id": "sha256:" + "b" * 64,
            "postgres_locale": "C", "execution_sha256": "c" * 64, "schema_sha256": "d" * 64,
            "backend_image": "ghcr.io/kacperkg/vellum-backend@sha256:" + "e" * 64,
            "harness_sha256": worker.harness_fingerprint(),
            "backend_runtime": {"PORT": "8080", "DB_HOST": "postgres", "DB_PORT": "5432",
                                "DB_NAME": "synthetic", "DB_USER": "vellum_runtime", "DB_SSLMODE": "disable"},
            "worker_key_id": "arn:aws:kms:eu-west-2:123456789012:key/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
            "alert_topic": "arn:aws:sns:eu-west-2:123456789012:synthetic-alerts",
        }
        self.profile["runtime_sha256"] = worker.digest(worker.canonical(self.profile["backend_runtime"]))
        worker.validate_profile(self.profile)
        self.cloud = FakeCloud()
        self.store = worker.BackupStore(self.cloud, self.profile)
        self.manifest_changes = {}
        self.marker_changes = {}

    def openssl_call(self, *args):
        return subprocess.run([self.openssl, *args], capture_output=True, check=True).stdout

    def fixture(self, signer=None):
        work = self.root / "message"
        base = f"production/runs/{self.run_id}"
        archive = self.cloud.add(base + ".dump", "archive-version", b"PGDMP synthetic")
        started = worker.timestamp(self.now - timedelta(minutes=10))
        manifest = {"format_version": 2, "environment": "production", "run_id": self.run_id,
                    "source": deepcopy(self.profile["source"]), "snapshot_id": "00000003-0000004B-1",
                    "snapshot_started_at": started, "dump_completed_at": started,
                    "postgres_version": "17.8", "archive_sha256": archive["sha256"],
                    "archive_size": archive["size"], **self.manifest_changes}
        ref = self.cloud.add(base + ".json", "manifest-version", worker.producer_canonical(manifest))
        marker = {"format_version": 2, "environment": "production", "run_id": self.run_id,
                  "bucket": self.profile["bucket"], "account_id": self.profile["account_id"],
                  "region": worker.REGION, "completion_key": self.marker, "archive": archive,
                  "manifest": ref, "key_id": self.profile["producer_key_id"],
                  "snapshot_started_at": started, "uploaded_at": worker.timestamp(self.now),
                  **self.marker_changes}
        work.write_bytes(worker.producer_canonical(marker))
        signature = self.openssl_call("pkeyutl", "-sign", "-rawin", "-inkey",
                                      signer or self.private, "-in", work)
        envelope = {"payload": marker, "signature": base64.b64encode(signature).decode()}
        self.cloud.add(self.marker, "completion-version", worker.producer_canonical(envelope))
        return archive

    def verify(self, proof=None):
        return worker.verify_backup(self.store, self.cloud, self.profile, self.marker, "completion-version",
                                    self.public, self.root, self.now, proof)

    def test_round_trip_pins_three_versions_and_source(self):
        archive = self.fixture()
        backup, refs = self.verify()
        self.assertEqual(refs["archive"], archive)
        self.assertEqual(refs["completion"]["version_id"], "completion-version")
        self.assertEqual(refs["manifest"]["version_id"], "manifest-version")
        self.assertEqual(backup["archive_bytes"], 15)
        self.assertEqual(backup["source_sha256"], worker.digest(worker.canonical(
            {"system_id": self.profile["source"]["system_identifier"], "database_oid": "16385"})))

    def test_matching_archive_proof_rechecks_only_archive_metadata(self):
        archive = self.fixture()
        _, refs = self.verify(archive)
        self.assertEqual(refs["archive"], archive)
        self.assertEqual([args[1] for args in self.cloud.calls],
                         ["head-object", "get-object", "head-object", "get-object", "head-object"])

    def test_wrong_database_and_cluster(self):
        for key, value in (("database_oid", 2222), ("system_identifier", "999")):
            with self.subTest(field=key):
                self.manifest_changes["source"] = {**self.profile["source"], key: value}
                self.fixture()
                with self.assertRaisesRegex(worker.RecoveryError, "manifest provenance"):
                    self.verify()

    def test_wrong_snapshot_or_early_completion(self):
        for changes in ({"snapshot_id": "unknown"},
                        {"snapshot_started_at": worker.timestamp(self.now - timedelta(hours=1))},
                        {"dump_completed_at": worker.timestamp(self.now + timedelta(seconds=1))}):
            with self.subTest(fields=changes):
                self.manifest_changes = changes
                self.fixture()
                with self.assertRaisesRegex(worker.RecoveryError, "manifest provenance"):
                    self.verify()

    def test_stale_snapshot(self):
        self.marker_changes["snapshot_started_at"] = worker.timestamp(self.now - timedelta(hours=24))
        self.fixture()
        with self.assertRaisesRegex(worker.RecoveryError, "stale"):
            self.verify()

    def test_untrusted_producer_cannot_sign(self):
        other = self.root / "other.pem"
        self.openssl_call("genpkey", "-algorithm", "ED25519", "-out", other)
        self.fixture(other)
        with self.assertRaisesRegex(worker.RecoveryError, "producer signature"):
            self.verify()

    def test_archive_substitution_detected_even_when_get_metadata_claims_original(self):
        archive = self.fixture()
        self.cloud.replace_get[archive["key"], archive["version_id"]] = b"PGDMP corrupted"
        with self.assertRaisesRegex(worker.RecoveryError, "downloaded backup checksum"):
            self.verify()

    def test_null_version_legacy_and_duplicate_json_rejected(self):
        self.fixture()
        with self.assertRaisesRegex(worker.RecoveryError, "completion key or version"):
            worker.verify_backup(self.store, self.cloud, self.profile, self.marker, "null",
                                 self.public, self.root, self.now)
        self.cloud.add(self.marker, "completion-version", b'{"format_version":1}')
        with self.assertRaisesRegex(worker.RecoveryError, "unsupported"):
            self.verify()
        self.cloud.add(self.marker, "completion-version", b'{"payload":{},"payload":{}}')
        with self.assertRaisesRegex(worker.RecoveryError, "duplicate"):
            self.verify()

    def test_runner_file_limit_and_deadline(self):
        runner = worker.Runner(time.monotonic() + 10)
        with self.assertRaisesRegex(worker.RecoveryError, "bounded subprocess"):
            runner.call([sys.executable, "-c", "import sys; sys.stdout.buffer.write(b'x' * 8192)"],
                        "bounded subprocess", limit=1024)
        with self.assertRaisesRegex(worker.RecoveryError, "timed out"):
            runner.call([sys.executable, "-c", "import time; time.sleep(2)"], "sleeping subprocess",
                        timeout=0.05)
        runner.deadline = 0
        with self.assertRaisesRegex(worker.RecoveryError, "timed out"):
            runner.call([sys.executable, "-c", "raise SystemExit(99)"], "expired subprocess")

    def test_cleanup_failure_prevents_success(self):
        class FakeDocker:
            def call(self, args, label, **kwargs):
                if label == "private sandbox inventory during cleanup":
                    return b"a" * 64
                if label == "private sandbox cleanup":
                    raise worker.RecoveryError("private sandbox cleanup failed")
                raise AssertionError(label)

        with self.assertRaisesRegex(worker.RecoveryError, "cleanup failed"):
            worker.remove_sandbox(FakeDocker(), "org.vellum.recovery.job=" + "a" * 32)

    def test_interrupted_verification_leaves_failure_and_cleans_exact_scratch(self):
        state = self.root / "state"
        state.mkdir()
        scratch = self.root / "scratch"
        scratch.mkdir()
        job = "a" * 32
        abandoned = scratch / ("job-" + job + "-fixture")
        abandoned.mkdir()
        unrelated = scratch / ("job-" + "b" * 32 + "-fixture")
        unrelated.mkdir()
        worker.write_private_json(state, "inflight.json", {"job": job, "mode": "verify-backup"})
        worker.write_state(state, job, "verify-backup", "running")
        with patch.object(worker, "safe_file", side_effect=lambda path: path):
            worker.recover_inflight(state, scratch)
        result = worker.strict_json((state / (job + ".json")).read_bytes())
        self.assertEqual(result["status"], "failed")
        self.assertFalse(result["alert_accepted"])
        self.assertFalse((state / "inflight.json").exists())
        self.assertFalse(abandoned.exists())
        self.assertTrue(unrelated.is_dir())

    def test_stale_drill_and_missing_backup_publish_failed_freshness_metrics(self):
        class FakeMetrics:
            def aws(self, args, label):
                self.metrics = worker.strict_json(args[args.index("--metric-data") + 1])
                return {}

        worker.write_private_json(self.root, "latest-drill.json", {
            "profile_sha256": worker.digest(worker.canonical(self.profile)),
            "finished_at": worker.timestamp(self.now - timedelta(days=5, minutes=1)),
        })
        runner = FakeMetrics()
        with patch.object(worker, "safe_file", side_effect=lambda path: path):
            worker.publish_health(self.profile, self.root, runner, self.now)
        self.assertEqual({item["MetricName"]: item["Value"] for item in runner.metrics},
                         {"FreshBackupVerification": 0, "RestoreDrillBeforeExpiry": 0})

    def test_candidate_profile_cannot_be_rebound_to_other_worker_profile(self):
        candidate = {
            "profile_sha256": worker.digest(worker.canonical(self.profile)),
            "image": self.profile["backend_image"], "image_id": "sha256:" + "a" * 64,
            "execution_sha256": "b" * 64, "migration_contract": "c" * 64,
        }
        worker.validate_candidate(candidate, self.profile)
        candidate["profile_sha256"] = "d" * 64
        with self.assertRaisesRegex(worker.RecoveryError, "pinned candidate profile"):
            worker.validate_candidate(candidate, self.profile)

    def test_missing_alert_has_distinct_error(self):
        class FakeAlert:
            def aws(self, *args, **kwargs):
                return {}

        with self.assertRaisesRegex(worker.RecoveryError, "alert was not accepted"):
            worker.send_alert(FakeAlert(), self.profile, "a" * 32, "drill")


if __name__ == "__main__":
    unittest.main()
