"""Synthetic signer/host contract tests. No production data, host or AWS access."""

from copy import deepcopy
from datetime import datetime, timezone
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import Mock, patch

import controller
import coordination
from test_release import manifest


NOW = datetime(2026, 11, 4, 12, 0, tzinfo=timezone.utc)
POSTGRES = {"image_id": "sha256:" + "1" * 64, "version": "170008"}


def request(risk="low"):
    return {
        "schema": 1,
        "nonce": "2" * 64,
        "manifest": manifest(),
        "live": {
            "current_manifest_sha256": "3" * 64,
            "postgres": POSTGRES.copy(),
            "schema_sha256": "4" * 64,
            "database_source_sha256": "a" * 64,
            "proxy_sha256": "5" * 64,
            "backup_contract": "6" * 64,
            "execution_sha256": "7" * 64,
            "runtime_sha256": "8" * 64,
            "risk": risk,
        },
    }


def evidence_for(value):
    live = value["live"]
    archive = "8" * 64
    return {
        "schema": 1,
        "issuer": controller.READINESS_ISSUER,
        "request_sha256": controller.digest(controller.canonical(value)),
        "issued_at": "2026-11-04T11:58:00Z",
        "expires_at": "2026-11-04T12:08:00Z",
        "backup": {
            "archive_sha256": archive,
            "completion_sha256": "9" * 64,
            "manifest_sha256": "a" * 64,
            "archive_bytes": 1000,
            "contract": live["backup_contract"],
            "source_sha256": live["database_source_sha256"],
            "created_at": "2026-11-04T02:00:00Z",
            "verified_at": "2026-11-04T11:55:00Z",
        },
        "drill": {
            "archive_sha256": "b" * 64,
            "contract": live["backup_contract"],
            "source_sha256": live["database_source_sha256"],
            "postgres": live["postgres"],
            "finished_at": "2026-11-01T02:10:00Z",
            "schema_sha256": live["schema_sha256"],
            "execution_sha256": live["execution_sha256"],
            "runtime_sha256": live["runtime_sha256"],
        },
        "rehearsal": None if live["risk"] == "low" else {
            "archive_sha256": archive,
            "postgres": live["postgres"],
            "candidate_backend": value["manifest"]["images"]["backend"],
            "schema_before": live["schema_sha256"],
            "schema_after": live["schema_sha256"],
            "data_before": "c" * 64,
            "data_after": "c" * 64,
            "finished_at": "2026-11-04T11:57:00Z",
        },
    }


class ReadinessEvidenceTests(unittest.TestCase):
    def validate(self, value, evidence, now=NOW):
        # Exercise schema/TTL policy independently of the signature test below.
        with patch.object(controller, "verify_readiness_signature") as signature:
            controller.validate_readiness({"evidence": evidence, "signature": "synthetic"}, value, now)
        signature.assert_called_once_with(evidence, "synthetic")

    def test_accepts_weekly_restore_for_unchanged_backend(self):
        value = request("low")
        self.validate(value, evidence_for(value))

    def test_release_live_state_and_attempt_replay_are_rejected(self):
        value = request()
        evidence = evidence_for(value)
        for field, parent in (("nonce", None), ("source_sha", "manifest"), ("schema_sha256", "live")):
            with self.subTest(field=field):
                changed = deepcopy(value)
                (changed[parent] if parent else changed)[field] = "d" * 64
                with self.assertRaisesRegex(ValueError, "does not cover"):
                    self.validate(changed, evidence)

    def test_issuer_expiry_and_missing_or_stale_evidence_fail(self):
        cases = (
            (None, "issuer", "someone-else"),
            (None, "expires_at", "2026-11-04T12:00:00Z"),
            (None, "issued_at", "2026-11-04T12:00:01Z"),
            ("backup", "created_at", "2026-11-03T12:00:00Z"),
            ("backup", "verified_at", "2026-11-04T11:47:00Z"),
            ("backup", "contract", "d" * 64),
            ("backup", "source_sha256", "d" * 64),
            ("drill", "source_sha256", "d" * 64),
            ("backup", "completion_sha256", None),
            ("backup", "archive_sha256", "invalid"),
            ("backup", "archive_bytes", 512 * 1024**2 + 1),
            ("drill", "finished_at", "2026-10-28T12:00:00Z"),
            ("drill", "postgres", {"image_id": POSTGRES["image_id"], "version": "160008"}),
            ("drill", "execution_sha256", "d" * 64),
            ("drill", "runtime_sha256", "d" * 64),
        )
        value = request()
        for section, field, invalid in cases:
            with self.subTest(section=section, field=field):
                evidence = evidence_for(value)
                (evidence[section] if section else evidence)[field] = invalid
                with self.assertRaises(ValueError):
                    self.validate(value, evidence)

    def test_equal_rehearsal_end_states_do_not_allow_migration(self):
        value = request("migration")
        evidence = evidence_for(value)
        with self.assertRaisesRegex(ValueError, "separate reviewed migration"):
            self.validate(value, evidence)

    def test_duplicate_fields_fail_closed(self):
        with self.assertRaisesRegex(ValueError, "duplicate"):
            controller.strict_json(b'{"evidence": {"schema": 1, "schema": 2}}')

    def test_revision_label_is_the_only_ignored_execution_input(self):
        image = {
            "Architecture": "amd64", "Os": "linux",
            "RootFS": {"Type": "layers", "Layers": ["sha256:" + "1" * 64]},
            "Config": {"Cmd": ["./server"], "Labels": {"org.opencontainers.image.revision": "old"}},
        }
        fingerprint = controller.Commands.execution_fingerprint
        updated = deepcopy(image)
        updated["Config"]["Labels"]["org.opencontainers.image.revision"] = "new"
        self.assertEqual(fingerprint(image), fingerprint(updated))
        updated["Config"]["Cmd"] = ["./other"]
        self.assertNotEqual(fingerprint(image), fingerprint(updated))
        updated = deepcopy(image)
        updated["RootFS"]["Layers"] = ["sha256:" + "2" * 64]
        self.assertNotEqual(fingerprint(image), fingerprint(updated))


class ReadinessSignatureTests(unittest.TestCase):
    def test_ed25519_signature_checks_actual_payload_and_key(self):
        openssl = shutil.which("openssl")
        self.assertIsNotNone(openssl)
        # Linux production uses /usr/bin/openssl 3. Some local systems supply
        # OpenSSL 3 via PATH, while their system openssl is LibreSSL.
        real_run = subprocess.run

        def use_local_openssl(args, **kwargs):
            return real_run([openssl, *args[1:]], **kwargs)

        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            key = state / "test-signing.pem"
            public_key = state / "production-readiness-key.pem"
            raw = state / "message"
            signature = state / "signature"
            value = request()
            evidence = evidence_for(value)
            raw.write_bytes(b"vellum-production-readiness-v1\n" + controller.canonical(evidence))
            real_run([openssl, "genpkey", "-algorithm", "ED25519", "-out", str(key)], check=True)
            real_run([openssl, "pkey", "-in", str(key), "-pubout", "-out", str(public_key)], check=True)
            real_run([openssl, "pkeyutl", "-sign", "-rawin", "-inkey", str(key),
                      "-in", str(raw), "-out", str(signature)], check=True)
            envelope = {"evidence": evidence, "signature": signature.read_bytes().hex()}
            with (
                patch.object(controller, "STATE", state),
                patch.object(controller, "private_file", side_effect=lambda path: path),
                patch.object(controller.subprocess, "run", side_effect=use_local_openssl),
            ):
                controller.validate_readiness(envelope, value, NOW)
                evidence["backup"]["archive_bytes"] += 1
                with self.assertRaisesRegex(ValueError, "signature"):
                    controller.validate_readiness(envelope, value, NOW)


class ReadinessCoordinationTests(unittest.TestCase):
    def test_readiness_refusal_does_not_create_pending_or_stop_writers(self):
        current = manifest()
        for scenario in ("broker missing", "changed baseline", "invalid evidence", "migration", "success", "mutation fails"):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                state = Path(directory)
                commands = Mock()
                commands.prepare.return_value = (10, {})
                baseline = request("migration" if scenario == "migration" else "low")["live"]
                baseline["migration_contract_sha256"] = "a" * 64
                baseline["proxy_sha256"] = "b" * 64
                commands.readiness_baseline.side_effect = [
                    baseline, {**baseline, "schema_sha256": "f" * 64} if scenario == "changed baseline" else baseline,
                ]
                if scenario == "mutation fails":
                    commands.replace.side_effect = RuntimeError("synthetic interrupted mutation")
                with (
                    patch.object(controller, "STATE", state),
                    patch.object(controller, "bundle_hashes", return_value=current["deployment_files"]),
                    patch.object(coordination, "validate_state"),
                    patch.object(coordination, "root_owned", return_value=True),
                    patch.object(controller, "request_readiness", return_value={},
                                 side_effect=ValueError("broker") if scenario == "broker missing" else None),
                    patch.object(controller, "validate_admission"),
                    patch.object(controller, "validate_readiness",
                                 side_effect=ValueError("evidence") if scenario == "invalid evidence" else None),
                ):
                    if scenario == "success":
                        controller.deploy("production", current, commands, "automated", current["staging"]["command_id"])
                        self.assertTrue((state / "production-current.json").is_file())
                    else:
                        with self.assertRaises((ValueError, RuntimeError)):
                            controller.deploy("production", current, commands, "automated", current["staging"]["command_id"])
                mutated = scenario in ("success", "mutation fails")
                self.assertEqual(commands.replace.called, mutated)
                self.assertEqual(len(list(state.glob("production-readiness-*.json"))), int(mutated))
                self.assertEqual((state / coordination.PENDING).exists(), scenario == "mutation fails")
                self.assertFalse(os.path.lexists(state / controller.PRELAUNCH_CLOSED))

    def test_installed_automated_policy_disallows_legacy_downgrade(self):
        current = manifest()
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory)
            (state / controller.READINESS_POLICY).touch()
            commands = Mock()
            with (
                patch.object(controller, "STATE", state),
                patch.object(controller, "bundle_hashes", return_value=current["deployment_files"]),
                self.assertRaisesRegex(ValueError, "automated readiness is installed"),
            ):
                controller.deploy("production", current, commands, "standard")
            self.assertFalse(commands.apply.called)
            self.assertFalse((state / coordination.PENDING).exists())
