"""Staging and production writer replacement faults; synthetic Docker state, real recovery latch."""
from contextlib import ExitStack
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cd"))
import controller
import coordination
from test_release import manifest


OLD_BACKEND = "1" * 64
ONEOFF_BACKEND = "2" * 64
UNEXPECTED_BACKEND = "3" * 64
CANDIDATE_BACKEND = "4" * 64


def stopped_state():
    return {"Status": "exited", "Running": False, "Restarting": False,
            "Paused": False, "Dead": False, "Pid": 0}


def running_state():
    return {"Status": "running", "Running": True, "Restarting": False,
            "Paused": False, "Dead": False, "Pid": 1234}


class Runtime:
    """Only the two application services can mutate; never real Docker/SQL."""
    def __init__(self, value, environment="staging", identifiers=None):
        self.value = value
        self.environment = environment
        self.project = "vellum-" + environment
        self.calls = []
        other_project = "vellum-" + ("production" if environment == "staging" else "staging")
        self.foreign_records = {
            f"{index:064x}": {"project": project, "service": service, "state": running_state()}
            for index, (project, service) in enumerate((
                (other_project, "backend"), (other_project, "postgres"),
                (self.project, "postgres"), ("audi", "backend"),
                ("proxy", "caddy"), ("unrelated", "backend"),
            ), start=100)
        }
        self.records = {identifier: running_state() for identifier in
                        (identifiers if identifiers is not None else [OLD_BACKEND])}
        self.inspect_override = None
        self.after_stop = None
        self.inventory_override = None
        self.fail_label = None
        self.compose_image = None
        self.image_platform = "amd64"
        self.fail_stop = False
        self.fail_remove = False
        self.up_error = None
        self.up_code = 0
        self.metadata_revision = value["source_sha"]
        self.started = False
        self.candidate_inspect_override = None
        self.candidate_on_failure = False
        self.drain_results = ["0\n"]
        self.drain_error = None

    def __call__(self, args, **kwargs):
        self.calls.append(list(args))
        if args[:3] in (["docker", "container", "stop"], ["docker", "container", "rm"]):
            if any(identifier in args for identifier in self.foreign_records):
                raise AssertionError("attempt to mutate a foreign container")
        stdout = ""
        returncode = 0
        if self.fail_label == "pull" and "pull" in args and "up" not in args:
            return subprocess.CompletedProcess(args, 1, "", "sensitive pull error")
        if args[:2] == ["docker", "ps"]:
            if "label=com.docker.compose.service=postgres" in args:
                stdout = "postgres-fixture\n"
            elif "label=com.docker.compose.service=backend" in args:
                if self.inventory_override is not None:
                    stdout = self.inventory_override(self)
                else:
                    expected = {f"label=com.docker.compose.project={self.project}",
                                "label=com.docker.compose.service=backend"}
                    if not expected.issubset(args):
                        raise AssertionError("backend inventory must use exact target labels")
                    # Model the Docker label filters over target and foreign fixtures.
                    selected = list(self.records) + [identifier for identifier, meta in self.foreign_records.items()
                        if meta["project"] == self.project and meta["service"] == "backend"]
                    stdout = "\n".join(selected)
            else:
                stdout = ""
        elif args[:3] == ["docker", "image", "inspect"]:
            stdout = json.dumps({"Architecture": self.image_platform, "Os": "linux", "Config": {
                "Labels": {"org.opencontainers.image.revision": self.metadata_revision}}})
        elif args[:3] == ["docker", "container", "inspect"]:
            identifier = args[-1]
            metadata = {"id": identifier, "project": self.project, "service": "backend",
                        "oneoff": "True" if identifier == ONEOFF_BACKEND else "False",
                        "image": self.value["images"]["backend"],
                        "state": self.records[identifier]}
            if self.inspect_override is not None:
                self.inspect_override(metadata)
            if identifier == CANDIDATE_BACKEND and self.candidate_inspect_override is not None:
                self.candidate_inspect_override(metadata)
            stdout = json.dumps(metadata)
        elif args[:3] == ["docker", "container", "stop"]:
            if self.fail_stop:
                returncode = 1
            else:
                for identifier in [arg for arg in args if arg in self.records]:
                    self.records[identifier] = copy.deepcopy(self.after_stop or stopped_state())
        elif args[:3] == ["docker", "container", "rm"]:
            if self.fail_remove:
                returncode = 1
            else:
                for identifier in [arg for arg in args if arg in self.records]:
                    if self.records[identifier]["Running"]:
                        raise AssertionError("attempt to remove an executing backend")
                    del self.records[identifier]
        elif args[:2] == ["docker", "compose"]:
            if "config" in args and "json" in args:
                stdout = json.dumps({"name": self.project, "services": {
                    "backend": {"image": self.compose_image or self.value["images"]["backend"], "environment": {
                        "CORS_ALLOWED_ORIGINS": controller.ORIGINS[self.environment],
                        "RATE_LIMIT_LOGIN_REQUESTS": "10",
                        "TRUSTED_PROXY_CIDRS": controller.TRUSTED_PROXY_CIDRS}},
                    "frontend": {"image": self.value["images"]["frontend"]}}})
            elif "up" in args:
                if self.records:
                    raise AssertionError("new backend started with legacy backend present")
                if self.up_error is not None:
                    raise self.up_error
                returncode = self.up_code
                self.started = returncode == 0
                if self.started or self.candidate_on_failure:
                    self.records[CANDIDATE_BACKEND] = running_state() | {"Health": {"Status": "healthy" if self.started else "unhealthy"}}
            elif "exec" in args and any("psql" in arg for arg in args):
                if self.drain_error is not None:
                    raise self.drain_error
                stdout = self.drain_results.pop(0) if len(self.drain_results) > 1 else self.drain_results[0]
        else:
            raise AssertionError(f"Unexpected command: {args}")
        return subprocess.CompletedProcess(args, returncode, stdout, "sensitive fixture error")


def private_state(state):
    stack = ExitStack()
    stack.enter_context(patch.object(controller, "STATE", state))
    stack.enter_context(patch.object(controller, "protected"))
    from datetime import datetime, timezone
    import hashlib
    now = datetime.now(timezone.utc).isoformat()
    (state / "production-approval.json").write_bytes(controller.canonical({
        "manifest_sha256": hashlib.sha256(controller.canonical(manifest())).hexdigest(),
        "verified_at": now,
        "backup_created_at": now,
        "backup_reference": "synthetic-restore-evidence",
        "off_host_copy_verified": True, "restore_verified": True,
        "schema_compatible": True, "trusted_proxy_reviewed": True,
    }))
    stack.enter_context(patch.object(coordination, "validate_state"))
    stack.enter_context(patch.object(coordination, "root_owned", lambda info: info.st_uid == os.getuid()))
    return stack


class StagingReplacementTests(unittest.TestCase):
    environment = "staging"

    def runtime(self, value, **kwargs):
        return Runtime(value, environment=self.environment, **kwargs)

    def apply(self, runtime):
        with (patch.object(controller, "validate_environment_file"),
              patch.object(controller.subprocess, "run", side_effect=runtime),
              patch.object(controller, "smoke")):
            controller.Commands(self.environment, runtime.value).apply(runtime.value)

    def test_all_legacy_and_oneoff_writers_stop_and_disappear_before_start(self):
        runtime = self.runtime(manifest(), identifiers=[OLD_BACKEND, ONEOFF_BACKEND])
        self.apply(runtime)
        self.assertTrue(runtime.started)
        calls = runtime.calls
        stop = next(i for i, args in enumerate(calls) if args[:3] == ["docker", "container", "stop"])
        remove = next(i for i, args in enumerate(calls) if args[:3] == ["docker", "container", "rm"])
        update = next(i for i, args in enumerate(calls) if "up" in args)
        image_checks = [i for i, args in enumerate(calls) if args[:3] == ["docker", "image", "inspect"]]
        self.assertEqual(len(image_checks), 2)
        self.assertLess(max(image_checks), stop)
        self.assertLess(stop, remove)
        self.assertLess(remove, update)
        self.assertTrue(all(identifier in calls[stop] for identifier in (OLD_BACKEND, ONEOFF_BACKEND)))
        self.assertTrue(all(identifier in calls[remove] for identifier in (OLD_BACKEND, ONEOFF_BACKEND)))
        self.assertFalse(any(flag in args for args in calls for flag in ("down", "prune", "--volumes", "-v", "--force")))
        self.assertIn("--no-deps", calls[update])
        self.assertIn("--no-build", calls[update])
        self.assertIn("never", calls[update])
        self.assertIn("--scale", calls[update])
        self.assertIn("backend=1", calls[update])
        for args in calls:
            self.assertNotIn("vellum-" + ("production" if self.environment == "staging" else "staging"), " ".join(args))
            self.assertNotIn("audi", " ".join(args).lower())
            if args[:2] == ["docker", "ps"]:
                self.assertIn(f"label=com.docker.compose.project=vellum-{self.environment}", args)
            if args[:2] == ["docker", "compose"]:
                self.assertEqual(args[args.index("--project-name") + 1], "vellum-" + self.environment)
                self.assertEqual(args[args.index("--env-file") + 1], str(controller.ENVIRONMENT_DIRECTORY / (self.environment + ".env")))
            self.assertNotIn("caddy", " ".join(args).lower())
            if "stop" in args or "rm" in args:
                self.assertNotIn("postgres", args)
                self.assertNotIn("frontend", args)

    def test_foreign_container_sentinels_remain_untouched(self):
        runtime = self.runtime(manifest(), identifiers=[OLD_BACKEND, ONEOFF_BACKEND])
        original = copy.deepcopy(runtime.foreign_records)
        self.apply(runtime)
        self.assertTrue(runtime.started)
        self.assertEqual(runtime.foreign_records, original)
        for args in runtime.calls:
            if args[:3] in (["docker", "container", "stop"], ["docker", "container", "rm"]):
                targets = args[5:] if args[2] == "stop" else args[3:]
                self.assertEqual(set(targets), {OLD_BACKEND, ONEOFF_BACKEND})
            if "exec" in args:
                self.assertEqual(args[args.index("--project-name") + 1], runtime.project)
                self.assertIn("postgres", args)

    def test_no_existing_backend_is_safe_but_empty_inventory_still_verified(self):
        runtime = self.runtime(manifest(), identifiers=[])
        self.apply(runtime)
        self.assertTrue(runtime.started)
        self.assertFalse(any("stop" in args or "rm" in args for args in runtime.calls))

    def test_provenance_failure_does_not_stop_existing_backend(self):
        runtime = self.runtime(manifest())
        runtime.metadata_revision = "f" * 40
        with self.assertRaisesRegex(ValueError, "revision"):
            self.apply(runtime)
        self.assertEqual(runtime.records[OLD_BACKEND], running_state())
        self.assertFalse(any("stop" in args or "rm" in args or "up" in args for args in runtime.calls))

    def test_stop_failure_never_removes_or_starts(self):
        runtime = self.runtime(manifest())
        runtime.fail_stop = True
        with self.assertRaises(ValueError):
            self.apply(runtime)
        self.assertFalse(any("rm" in args or "up" in args for args in runtime.calls))

    def test_incomplete_termination_never_removes_or_starts(self):
        for change in ({"Status": "running", "Running": True, "Pid": 4},
                       {"Status": "restarting", "Restarting": True},
                       {"Status": "paused", "Paused": True}, {"Dead": True}, {"Pid": 99}):
            with self.subTest(state=change):
                runtime = self.runtime(manifest())
                runtime.after_stop = stopped_state() | change
                with self.assertRaises(ValueError):
                    self.apply(runtime)
                self.assertFalse(any("rm" in args or "up" in args for args in runtime.calls))

    def test_container_identity_mismatch_is_rejected_before_stop(self):
        for key, value in (("project", "vellum-" + ("production" if self.environment == "staging" else "staging")), ("service", "postgres"), ("id", ONEOFF_BACKEND)):
            with self.subTest(key=key):
                runtime = self.runtime(manifest())
                runtime.inspect_override = lambda meta, k=key, v=value: meta.update({k: v})
                with self.assertRaises(ValueError):
                    self.apply(runtime)
                self.assertFalse(any("stop" in args or "rm" in args or "up" in args for args in runtime.calls))

    def test_malformed_inventory_is_rejected_before_stop(self):
        for inventory in ("short-id\n", "--all\n", OLD_BACKEND + "\n" + OLD_BACKEND + "\n",
                          "\n".join(f"{index:064x}" for index in range(65))):
            with self.subTest(inventory=inventory):
                runtime = self.runtime(manifest())
                runtime.inventory_override = lambda _, data=inventory: data
                with self.assertRaises(ValueError):
                    self.apply(runtime)
                self.assertFalse(any("stop" in args or "rm" in args or "up" in args for args in runtime.calls))

    def test_new_unexpected_writer_blocks_start(self):
        runtime = self.runtime(manifest())
        runtime.inventory_override = lambda fixture: "\n".join(fixture.records) if fixture.records else UNEXPECTED_BACKEND
        with self.assertRaises(ValueError):
            self.apply(runtime)
        self.assertFalse(any("up" in args for args in runtime.calls))

    def test_remove_failure_does_not_restart_old_code(self):
        runtime = self.runtime(manifest())
        runtime.fail_remove = True
        with self.assertRaises(ValueError):
            self.apply(runtime)
        self.assertEqual(runtime.records[OLD_BACKEND], stopped_state())
        self.assertFalse(any("up" in args or "start" in args for args in runtime.calls))

    def test_database_drain_waits_after_old_backend_removal_before_start(self):
        runtime = self.runtime(manifest())
        runtime.drain_results = ["2\n", "0\n"]
        with patch.object(controller.time, "sleep") as pause:
            self.apply(runtime)
        pause.assert_called_once_with(0.25)
        remove = next(i for i, args in enumerate(runtime.calls) if "rm" in args)
        drains = [i for i, args in enumerate(runtime.calls) if any("psql" in arg for arg in args)]
        update = next(i for i, args in enumerate(runtime.calls) if "up" in args)
        self.assertEqual(len(drains), 2)
        self.assertLess(remove, min(drains))
        self.assertLess(max(drains), update)
        query = runtime.calls[drains[0]][-1]
        self.assertIn("current_database()", query)
        self.assertIn("pg_backend_pid()", query)
        self.assertNotIn("pg_terminate_backend", query)
        self.assertNotIn("pg_cancel_backend", query)

    def test_database_sessions_remaining_past_bound_prevent_start(self):
        runtime = self.runtime(manifest())
        runtime.drain_results = ["1\n"]
        with (patch.object(controller.time, "monotonic", side_effect=[0, 16]),
              patch.object(controller.time, "sleep") as pause):
            with self.assertRaisesRegex(ValueError, "database sessions remain"):
                self.apply(runtime)
        pause.assert_not_called()
        self.assertFalse(runtime.records)
        self.assertFalse(any("up" in args for args in runtime.calls))

    def test_invalid_or_failed_database_observation_prevents_start(self):
        for data, error in (("", None), ("secret connection error", None), ("-1", None),
                            ("0\n1", None), ("00", None),
                            ("0", subprocess.TimeoutExpired(["psql"], 5))):
            with self.subTest(data=data, error=type(error).__name__):
                runtime = self.runtime(manifest())
                runtime.drain_results = [data]
                runtime.drain_error = error
                with self.assertRaises((ValueError, subprocess.TimeoutExpired)):
                    self.apply(runtime)
                self.assertFalse(runtime.records)
                self.assertFalse(any("up" in args for args in runtime.calls))

    def test_new_backend_identity_health_and_scale_are_verified(self):
        for change in ({"oneoff": "True"}, {"image": "old-unverified-image"},
                       {"state": running_state() | {"Health": {"Status": "unhealthy"}}},
                       {"id": OLD_BACKEND}, {"project": "unrelated-project"},
                       {"service": "postgres"}, {"state": stopped_state()},
                       {"state": running_state() | {"Restarting": True, "Health": {"Status": "healthy"}}}):
            with self.subTest(change=change):
                runtime = self.runtime(manifest())
                runtime.candidate_inspect_override = lambda meta, value=change: meta.update(value)
                with self.assertRaises(ValueError):
                    self.apply(runtime)
                self.assertNotIn(OLD_BACKEND, runtime.records)
                self.assertEqual(sum("up" in args for args in runtime.calls), 1)

    def test_ambiguous_preexisting_state_is_rejected_without_mutation(self):
        for state in (running_state() | {"Status": "paused", "Paused": True},
                      stopped_state() | {"Status": "dead", "Dead": True},
                      stopped_state() | {"Status": "removing"}):
            with self.subTest(state=state):
                runtime = self.runtime(manifest())
                runtime.records[OLD_BACKEND] = state
                with self.assertRaisesRegex(ValueError, f"ambiguous {self.environment} backend"):
                    self.apply(runtime)
                self.assertFalse(any("stop" in args or "rm" in args or "up" in args for args in runtime.calls))

    def test_missing_or_multiple_candidate_backends_reject_success(self):
        for inventory in ("", CANDIDATE_BACKEND + "\n" + UNEXPECTED_BACKEND):
            with self.subTest(inventory=inventory):
                runtime = self.runtime(manifest())
                runtime.inventory_override = lambda fixture, output=inventory: output if fixture.started else "\n".join(fixture.records)
                with self.assertRaisesRegex(ValueError, f"{self.environment} requires one backend"):
                    self.apply(runtime)
                self.assertNotIn(OLD_BACKEND, runtime.records)

    def test_unhealthy_started_candidate_never_causes_automatic_rollback(self):
        runtime = self.runtime(manifest())
        runtime.up_code = 1
        runtime.candidate_on_failure = True
        with self.assertRaises(ValueError):
            self.apply(runtime)
        self.assertEqual(set(runtime.records), {CANDIDATE_BACKEND})
        self.assertEqual(runtime.records[CANDIDATE_BACKEND]["Health"]["Status"], "unhealthy")
        self.assertEqual(sum("up" in args for args in runtime.calls), 1)
        self.assertFalse(any("start" in args for args in runtime.calls))

    def test_pull_digest_override_and_platform_fail_before_downtime(self):
        for field, value in (("fail_label", "pull"), ("compose_image", "unapproved@sha256:" + "e" * 64),
                             ("image_platform", "arm64")):
            with self.subTest(field=field):
                runtime = self.runtime(manifest())
                setattr(runtime, field, value)
                with self.assertRaises(ValueError):
                    self.apply(runtime)
                self.assertEqual(runtime.records[OLD_BACKEND], running_state())
                self.assertFalse(any("stop" in args or "rm" in args or "up" in args for args in runtime.calls))

    def test_manifest_bundle_and_approval_rejection_before_docker(self):
        faults = ["manifest", "bundle"]
        if self.environment == "production":
            faults += ["unverified", "approval", "approval_permissions"]
        for fault in faults:
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temporary:
                state = Path(temporary)
                value = manifest()
                runtime = self.runtime(value)
                with (private_state(state),
                      patch.object(controller, "bundle_hashes", return_value=value["deployment_files"]) as hashes,
                      patch.object(controller, "validate_environment_file"),
                      patch.object(controller.subprocess, "run", side_effect=runtime)):
                    if fault == "manifest":
                        value["images"]["backend"] = "unapproved-image"
                    elif fault == "bundle":
                        hashes.return_value = {}
                    elif fault == "unverified":
                        value["staging"] = {"verified": False, "command_id": None}
                    elif fault == "approval":
                        (state / "production-approval.json").write_text("{}")
                    elif fault == "approval_permissions":
                        controller.protected.side_effect = ValueError("unsafe approval permissions")
                    with self.assertRaises(ValueError):
                        controller.deploy(self.environment, value)
                    self.assertFalse(runtime.calls)
                    self.assertFalse((state / coordination.PENDING).exists())

    def test_each_incomplete_stage_keeps_marker_and_success_receipts(self):
        # Run failures through deploy(), including the real production approval
        # validator and the real local pending/lock protocol, not only apply().
        for fault in ("pull", "revision", "state", "stop", "termination", "remove", "remaining",
                      "drain", "drain_timeout", "startup", "migration", "unhealthy", "digest",
                      "replicas", "smoke"):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as temporary:
                state = Path(temporary)
                value = manifest()
                runtime = self.runtime(value)
                smoke_error = None
                if fault == "pull": runtime.fail_label = "pull"
                elif fault == "revision": runtime.metadata_revision = "f" * 40
                elif fault == "state": runtime.records[OLD_BACKEND]["Paused"] = True
                elif fault == "stop": runtime.fail_stop = True
                elif fault == "termination": runtime.after_stop = running_state()
                elif fault == "remove": runtime.fail_remove = True
                elif fault == "remaining":
                    runtime.inventory_override = lambda fixture: "\n".join(fixture.records) if fixture.records else UNEXPECTED_BACKEND
                elif fault == "drain": runtime.drain_results = ["invalid"]
                elif fault == "drain_timeout": runtime.drain_error = subprocess.TimeoutExpired(["psql"], 5)
                elif fault in ("startup", "migration"):
                    # Migration exits before health; Compose reports startup failure.
                    runtime.up_code = 1
                    runtime.candidate_on_failure = fault == "migration"
                elif fault == "unhealthy":
                    runtime.candidate_inspect_override = lambda meta: meta["state"].update(Health={"Status": "unhealthy"})
                elif fault == "digest":
                    runtime.candidate_inspect_override = lambda meta: meta.update(image="wrong-digest")
                elif fault == "replicas":
                    runtime.inventory_override = lambda fixture: (CANDIDATE_BACKEND + "\n" + UNEXPECTED_BACKEND) if fixture.started else "\n".join(fixture.records)
                elif fault == "smoke": smoke_error = ValueError("smoke failure")
                paths = [state / (self.environment + suffix) for suffix in
                         ("-current.json", "-previous.json", "-12345-2.json")]
                for path in paths:
                    path.write_bytes(controller.canonical({"retained": path.name}))
                initial = [path.read_bytes() for path in paths]
                with (private_state(state),
                      patch.object(controller, "bundle_hashes", return_value=value["deployment_files"]),
                      patch.object(controller, "validate_environment_file"),
                      patch.object(controller.subprocess, "run", side_effect=runtime),
                      patch.object(controller.Commands, "diagnostics"),
                      patch.object(controller, "smoke", side_effect=smoke_error)):
                    with self.assertRaises((ValueError, subprocess.TimeoutExpired)):
                        controller.deploy(self.environment, value)
                    self.assertTrue((state / coordination.PENDING).exists())
                    self.assertEqual([path.read_bytes() for path in paths], initial)
                    call_count = len(runtime.calls)
                    with self.assertRaisesRegex(ValueError, "operator recovery required"):
                        controller.deploy(self.environment, value)
                    self.assertEqual(len(runtime.calls), call_count)
                    self.assertFalse(any("start" in args for args in runtime.calls))

    def test_receipt_write_failure_preserves_marker_and_documents_partial_writes(self):
        for failed_write in (1, 2, 3):
            with self.subTest(write=failed_write), tempfile.TemporaryDirectory() as temporary:
                state = Path(temporary)
                value = manifest()
                runtime = self.runtime(value)
                previous = manifest()
                previous["source_sha"] = "f" * 40
                current_path = state / (self.environment + "-current.json")
                previous_path = state / (self.environment + "-previous.json")
                receipt_path = state / (self.environment + "-12345-2.json")
                current_path.write_bytes(controller.canonical(previous))
                original_write = controller.atomic_json
                writes = []
                def write(path, contents):
                    writes.append(path)
                    if len(writes) == failed_write:
                        raise OSError("synthetic receipt persistence failure")
                    original_write(path, contents)
                with (private_state(state),
                      patch.object(controller, "bundle_hashes", return_value=value["deployment_files"]),
                      patch.object(controller, "validate_environment_file"),
                      patch.object(controller.subprocess, "run", side_effect=runtime),
                      patch.object(controller, "smoke") as smoke,
                      patch.object(controller, "atomic_json", side_effect=write)):
                    with self.assertRaises(OSError):
                        controller.deploy(self.environment, value)
                    smoke.assert_called_once_with(controller.ORIGINS[self.environment],
                                                  staging=self.environment == "staging", login_requests=10)
                    self.assertTrue(runtime.started)
                    self.assertTrue((state / coordination.PENDING).exists())
                    self.assertEqual(json.loads(current_path.read_bytes()), value if failed_write == 3 else previous)
                    self.assertEqual(previous_path.exists(), failed_write > 1)
                    self.assertFalse(receipt_path.exists())

    def test_failed_start_migration_timeout_and_cancellation_keep_recovery_gate(self):
        for error in (None, subprocess.TimeoutExpired(["docker", "compose", "up"], 210),
                      KeyboardInterrupt(), SystemExit(143)):
            with self.subTest(error=type(error).__name__), tempfile.TemporaryDirectory() as temporary:
                state = Path(temporary)
                value = manifest()
                previous = copy.deepcopy(value)
                previous["source_sha"] = "f" * 40
                current_path = state / (self.environment + "-current.json")
                current_path.write_bytes(controller.canonical(previous))
                initial = current_path.read_bytes()
                runtime = self.runtime(value)
                runtime.up_code = 1
                runtime.up_error = error
                with (private_state(state),
                      patch.object(controller, "bundle_hashes", return_value=value["deployment_files"]),
                      patch.object(controller, "validate_environment_file"),
                      patch.object(controller.subprocess, "run", side_effect=runtime),
                      patch.object(controller.Commands, "diagnostics"),
                      patch.object(controller, "smoke")):
                    with self.assertRaises((ValueError, subprocess.TimeoutExpired, KeyboardInterrupt, SystemExit)):
                        controller.deploy(self.environment, value)
                    self.assertNotIn(OLD_BACKEND, runtime.records, "stopped old writer must not be brought back")
                    self.assertTrue((state / coordination.PENDING).exists())
                    self.assertEqual(current_path.read_bytes(), initial)
                    self.assertFalse((state / (self.environment + "-previous.json")).exists())
                    self.assertFalse((state / (self.environment + "-12345-2.json")).exists())
                    count = len(runtime.calls)
                    with self.assertRaisesRegex(ValueError, "operator recovery required"):
                        controller.deploy(self.environment, value)
                    self.assertEqual(len(runtime.calls), count)
                    # Simulate the separately approved operator recovery, never an
                    # automatic controller cleanup: after quiescence, archive/clear
                    # only the pending record while all existing lock inodes stay.
                    lock_inodes = {name: (state / name).stat().st_ino for name in coordination.LOCKS}
                    evidence = (state / coordination.PENDING).read_bytes()
                    with coordination.host_locks(state):
                        (state / "incident-pending.json").write_bytes(evidence)
                        (state / coordination.PENDING).unlink()
                        coordination.sync_state(state)
                    runtime.records.clear()  # separately establish candidate quiescence
                    runtime.up_error = None
                    runtime.up_code = 0
                    controller.deploy(self.environment, value)
                    self.assertFalse((state / coordination.PENDING).exists())
                    self.assertEqual(json.loads(current_path.read_bytes()), value)
                    self.assertEqual(json.loads((state / (self.environment + "-previous.json")).read_bytes()), previous)
                    self.assertTrue((state / (self.environment + "-12345-2.json")).exists())
                    self.assertEqual({name: (state / name).stat().st_ino for name in coordination.LOCKS}, lock_inodes)
                    self.assertEqual((state / "incident-pending.json").read_bytes(), evidence)


class ProductionReplacementTests(StagingReplacementTests):
    environment = "production"


if __name__ == "__main__":
    unittest.main()
