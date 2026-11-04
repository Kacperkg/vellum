"""Honest one-use recovery exception; synthetic Docker/SQL, real host locks/latch."""
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import types
import unittest
from unittest.mock import patch

from test_release import controller, manifest
from test_staging_replacement import (
    Runtime, private_state, CANDIDATE_BACKEND, running_state,
)
import coordination

NOW = datetime(2026, 10, 28, 12, tzinfo=timezone.utc)


def approval(prelaunch=True):
    value = {
        "manifest_sha256": hashlib.sha256(controller.canonical(manifest())).hexdigest(),
        "verified_at": NOW.isoformat(),
        "backup_created_at": (NOW - timedelta(hours=1)).isoformat(),
        "backup_reference": "private-synthetic-backup-evidence",
        "off_host_copy_verified": True, "restore_verified": not prelaunch,
        "schema_compatible": True, "trusted_proxy_reviewed": True,
    }
    if prelaunch:
        value.update(approval_mode="prelaunch", no_public_users=True, accept_data_loss=True,
                     acknowledgement=controller.PRELAUNCH_ACKNOWLEDGEMENT,
                     expires_at=(NOW + timedelta(hours=1)).isoformat())
    return value


@contextmanager
def deployment(prelaunch=True):
    with tempfile.TemporaryDirectory() as tmp:
        state = Path(tmp)
        value = manifest()
        runtime = Runtime(value, "production")
        with (private_state(state),
              patch.object(controller, "datetime", wraps=datetime) as clock,
              patch.object(controller, "bundle_hashes", return_value=value["deployment_files"]),
              patch.object(controller, "validate_environment_file"),
              patch.object(controller.subprocess, "run", side_effect=runtime),
              patch.object(controller.Commands, "diagnostics"),
              patch.object(controller, "smoke") as smoke):
            clock.now.return_value = NOW
            (state / "production-approval.json").write_bytes(controller.canonical(approval(prelaunch)))
            yield types.SimpleNamespace(state=state, value=value, runtime=runtime, smoke=smoke,
                                        deploy=lambda: controller.deploy("production", value,
                                            approval_mode="prelaunch" if prelaunch else "standard"))


class ApprovalTests(unittest.TestCase):
    def validate(self, value, mode="prelaunch", now=NOW):
        controller.validate_approval(value, manifest(), now, approval_mode=mode)

    def test_normal_mode_requires_real_restore_and_rejects_exception_schema(self):
        self.validate(approval(False), "standard")
        with self.assertRaises(ValueError):
            self.validate(approval(), "standard")
        for false in (False, None, 0, 1, "true"):
            with self.subTest(restore=false), self.assertRaises(ValueError):
                self.validate(approval(False) | {"restore_verified": false}, "standard")

    def test_all_missing_fields_and_false_attestations_fail_closed(self):
        self.validate(approval())
        for field in approval():
            value = approval()
            del value[field]
            with self.subTest(missing=field), self.assertRaises(ValueError):
                self.validate(value)
        for field in ("no_public_users", "accept_data_loss", "off_host_copy_verified",
                      "schema_compatible", "trusted_proxy_reviewed"):
            for false in (False, None, 0, 1, "true"):
                with self.subTest(field=field, value=false), self.assertRaises(ValueError):
                    self.validate(approval() | {field: false})
        for field, values in {
            "restore_verified": (True, None, 0, "false"),
            "approval_mode": ("standard", "", None),
            "acknowledgement": ("", "I accept", None),
            "manifest_sha256": ("e" * 64, "", None),
            "backup_reference": ("", None),
        }.items():
            for false in values:
                with self.subTest(field=field, value=false), self.assertRaises(ValueError):
                    self.validate(approval() | {field: false})
        with self.assertRaises(ValueError):
            self.validate(approval() | {"extra": True})

    def test_fresh_backup_expiry_and_fixed_sunset_are_enforced(self):
        invalid = {
            "verified_at": (NOW - timedelta(hours=25), NOW + timedelta(seconds=1)),
            "backup_created_at": (NOW - timedelta(hours=25), NOW + timedelta(seconds=1)),
            "expires_at": (NOW, NOW - timedelta(seconds=1), NOW + timedelta(hours=24, seconds=1),
                           NOW.replace(tzinfo=None)),
        }
        for field, dates in invalid.items():
            for date in dates:
                with self.subTest(field=field, date=date), self.assertRaises(ValueError):
                    self.validate(approval() | {field: date.isoformat()})
        sunset = controller.PRELAUNCH_SUNSET
        for now in (sunset, sunset + timedelta(days=1)):
            value = approval() | {"verified_at": now.isoformat(),
                "backup_created_at": now.isoformat(), "expires_at": (now + timedelta(minutes=1)).isoformat()}
            with self.subTest(now=now), self.assertRaises(ValueError):
                self.validate(value, now=now)
        value = approval() | {"verified_at": (sunset - timedelta(hours=1)).isoformat(),
            "backup_created_at": (sunset - timedelta(hours=2)).isoformat(),
            "expires_at": sunset.isoformat()}
        self.validate(value, now=sunset - timedelta(minutes=1))

    def test_staging_and_unknown_modes_are_rejected_before_any_state_or_docker_access(self):
        for environment, mode in (("staging", "prelaunch"), ("production", ""),
                                  ("production", "true"), ("production", None)):
            with self.subTest(environment=environment, mode=mode), patch.object(controller, "read_json") as read:
                with self.assertRaises(ValueError):
                    controller.deploy(environment, manifest(), approval_mode=mode)
                read.assert_not_called()


class DeploymentTests(unittest.TestCase):
    def test_success_consumes_before_docker_and_records_truthful_evidence(self):
        with deployment() as f:
            original_run = f.runtime.__call__
            def run(*args, **kwargs):
                closed = json.loads((f.state / controller.PRELAUNCH_CLOSED).read_bytes())
                self.assertEqual(closed["approval"], approval())
                self.assertIs(closed["approval"]["restore_verified"], False)
                self.assertEqual(closed["reason"], "consumed")
                return original_run(*args, **kwargs)
            with patch.object(controller.subprocess, "run", side_effect=run):
                f.deploy()
            self.assertFalse((f.state / coordination.PENDING).exists())
            self.assertEqual(json.loads((f.state / "production-current.json").read_bytes()), f.value)
            f.smoke.assert_called_once_with(controller.ORIGINS["production"], staging=False, login_requests=10)
            remove = next(i for i, args in enumerate(f.runtime.calls) if "rm" in args)
            drain = next(i for i, args in enumerate(f.runtime.calls) if any("psql" in arg for arg in args))
            start = next(i for i, args in enumerate(f.runtime.calls) if "up" in args)
            self.assertLess(remove, drain)
            self.assertLess(drain, start)
            before = len(f.runtime.calls)
            with self.assertRaisesRegex(ValueError, "consumed or retired"):
                f.deploy()
            self.assertEqual(len(f.runtime.calls), before)

    def test_closed_current_receipt_or_pending_marker_prevents_use(self):
        for blocker in (controller.PRELAUNCH_CLOSED, "production-current.json", coordination.PENDING, "symlink"):
            with self.subTest(blocker=blocker), deployment() as f:
                if blocker == "symlink":
                    (f.state / controller.PRELAUNCH_CLOSED).symlink_to(f.state / "absent")
                else:
                    (f.state / blocker).write_text("{}")
                with self.assertRaises(ValueError):
                    f.deploy()
                self.assertFalse(f.runtime.calls)
                if blocker == coordination.PENDING:
                    self.assertFalse((f.state / controller.PRELAUNCH_CLOSED).exists())

    def test_normal_dispatch_cannot_select_exception_and_retires_it(self):
        with deployment() as f:
            with self.assertRaises(ValueError):
                controller.deploy("production", f.value)
            self.assertFalse(f.runtime.calls)
        with deployment(False) as f:
            f.deploy()
            closed = json.loads((f.state / controller.PRELAUNCH_CLOSED).read_bytes())
            self.assertEqual(closed["reason"], "standard-mode-retirement")
            self.assertIs(closed["approval"]["restore_verified"], True)

    def test_evidence_write_or_directory_sync_failure_blocks_application_mutation(self):
        for function in ("atomic_json", "sync_state"):
            with self.subTest(function=function), deployment() as f:
                with patch.object(controller, function, side_effect=OSError("synthetic audit failure")):
                    with self.assertRaises(OSError):
                        f.deploy()
                self.assertFalse(f.runtime.calls)
                self.assertTrue((f.state / coordination.PENDING).exists())

    def test_prelaunch_requires_production_runtime_and_disabled_ses(self):
        for change in ({"APP_ENV": "staging"}, {"PASSWORD_RESET_EMAIL_MODE": "ses"},
                       {"PASSWORD_RESET_EMAIL_MODE": ""}):
            with self.subTest(change=change), deployment() as f:
                f.runtime.backend_env.update(change)
                with self.assertRaisesRegex(ValueError, "SES sending disabled"):
                    f.deploy()
                self.assertFalse(any("stop" in call or "up" in call for call in f.runtime.calls))

    def test_provenance_bundle_and_release_specific_approval_still_fail_before_docker(self):
        for fault in ("digest", "bundle", "unverified", "approval_hash"):
            with self.subTest(fault=fault), deployment() as f:
                if fault == "digest": f.value["images"]["backend"] = "untrusted:latest"
                elif fault == "bundle": f.value["deployment_files"] = dict.fromkeys(f.value["deployment_files"], "e" * 64)
                elif fault == "unverified": f.value["staging"] = {"verified": False, "command_id": None}
                else:
                    (f.state / "production-approval.json").write_bytes(controller.canonical(approval() | {"manifest_sha256": "e" * 64}))
                with self.assertRaises(ValueError):
                    f.deploy()
                self.assertFalse(f.runtime.calls)
                self.assertFalse((f.state / controller.PRELAUNCH_CLOSED).exists())

    def test_runtime_failures_preserve_consumption_and_recovery_marker_without_rollback(self):
        for fault in ("pull", "revision", "stop", "termination", "remove", "remaining", "drain",
                      "migration", "health", "replicas", "smoke", "receipt", "cancel"):
            with self.subTest(fault=fault), deployment() as f:
                if fault == "pull": f.runtime.fail_label = "pull"
                elif fault == "revision": f.runtime.metadata_revision = "f" * 40
                elif fault == "stop": f.runtime.fail_stop = True
                elif fault == "termination": f.runtime.after_stop = running_state()
                elif fault == "remove": f.runtime.fail_remove = True
                elif fault == "remaining":
                    f.runtime.inventory_override = lambda runtime: "\n".join(runtime.records) or "f" * 64
                elif fault == "drain": f.runtime.drain_error = subprocess.TimeoutExpired(["psql"], 5)
                elif fault == "migration": f.runtime.up_code = 1; f.runtime.candidate_on_failure = True
                elif fault == "health": f.runtime.candidate_inspect_override = lambda meta: meta["state"].update(Health={"Status": "unhealthy"})
                elif fault == "replicas":
                    f.runtime.inventory_override = lambda runtime: (CANDIDATE_BACKEND + "\n" + "f" * 64) if runtime.started else "\n".join(runtime.records)
                elif fault == "smoke": f.smoke.side_effect = ValueError("synthetic smoke fault")
                elif fault == "cancel": f.runtime.up_error = KeyboardInterrupt()
                original_write = controller.atomic_json
                def write(path, value):
                    if fault == "receipt" and path.name == "production-current.json":
                        raise OSError("synthetic receipt failure")
                    original_write(path, value)
                with patch.object(controller, "atomic_json", side_effect=write):
                    with self.assertRaises((ValueError, OSError, subprocess.TimeoutExpired, KeyboardInterrupt)):
                        f.deploy()
                self.assertTrue((f.state / coordination.PENDING).exists())
                evidence = (f.state / controller.PRELAUNCH_CLOSED).read_bytes()
                self.assertFalse((f.state / "production-current.json").exists())
                count = len(f.runtime.calls)
                with self.assertRaisesRegex(ValueError, "operator recovery required"):
                    f.deploy()
                self.assertEqual(len(f.runtime.calls), count)
                self.assertEqual((f.state / controller.PRELAUNCH_CLOSED).read_bytes(), evidence)
                self.assertFalse(any("start" in args for args in f.runtime.calls))


if __name__ == "__main__":
    unittest.main()
