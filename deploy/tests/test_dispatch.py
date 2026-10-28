"""Synthetic SSM polling failures must never become successful release evidence."""
from contextlib import contextmanager
import os
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cd"))
import release
from test_release import manifest


COMMAND_ID = "12345678-1234-1234-1234-123456789abc"
INSTANCE = "i-1234567890abcdef0"
DOCUMENT_PREFIX = "arn:aws:ssm:eu-west-2:123456789012:document/VellumDeploy-"


def status(value, returncode=0, stderr=""):
    return subprocess.CompletedProcess([], returncode, value + "\n", stderr)


@contextmanager
def invocation(results, ticks, target):
    with tempfile.TemporaryDirectory() as tmp:
        source = Path(tmp) / "candidate.json"
        source.write_bytes(release.canonical(manifest(verified=target == "production")))
        output = Path(tmp) / "output"
        output.write_text("")
        environment = {
            "GITHUB_REPOSITORY": "kacperkg/vellum",
            "GITHUB_REF": "refs/heads/" + ("main" if target == "production" else "staging"),
            "SSM_INSTANCE_ID": INSTANCE,
            "SSM_DOCUMENT_ARN": DOCUMENT_PREFIX + target,
            "SSM_DOCUMENT_VERSION": "3",
            "SSM_DOCUMENT_SHA256": "e" * 64,
            "GITHUB_OUTPUT": str(output),
        }
        with (
            patch.dict(os.environ, environment, clear=True),
            patch.object(release.subprocess, "check_output", return_value=COMMAND_ID) as send,
            patch.object(release.subprocess, "run", side_effect=results) as poll,
            patch.object(release.time, "monotonic", side_effect=ticks),
            patch.object(release.time, "sleep") as sleep,
        ):
            yield types.SimpleNamespace(
                args=types.SimpleNamespace(manifest=source, environment=target),
                output=output, send=send, poll=poll, sleep=sleep,
            )


class DispatchFailureTests(unittest.TestCase):
    environment = "staging"

    def invocation(self, results, ticks):
        return invocation(results, ticks, self.environment)

    def assert_single_pinned_send(self, fixture):
        fixture.send.assert_called_once()
        command = fixture.send.call_args.args[0]
        self.assertEqual(command[:3], ["aws", "ssm", "send-command"])
        for option, expected in (
            ("--instance-ids", INSTANCE),
            ("--document-name", DOCUMENT_PREFIX + self.environment),
            ("--document-version", "3"),
            ("--document-hash", "e" * 64),
            ("--document-hash-type", "Sha256"),
        ):
            self.assertEqual(command[command.index(option) + 1], expected)
        self.assertFalse(fixture.send.call_args.kwargs.get("shell", False))
        for call in fixture.poll.call_args_list:
            command = call.args[0]
            self.assertEqual(command[:3], ["aws", "ssm", "get-command-invocation"])
            self.assertEqual(command[command.index("--command-id") + 1], COMMAND_ID)
            self.assertEqual(command[command.index("--instance-id") + 1], INSTANCE)
            self.assertFalse(call.kwargs.get("shell", False))

    def test_pending_statuses_timeout_without_success_or_automatic_retry(self):
        with self.invocation(
            [status("Pending"), status("InProgress"), status("Delayed")],
            [100, 101, 102, 103, 1001],
        ) as fixture:
            with self.assertRaisesRegex(ValueError, "timed out; remote command may still be running"):
                release.dispatch(fixture.args)
            self.assertEqual(fixture.output.read_text(), "")
            self.assertEqual(fixture.poll.call_count, 3)
            self.assertEqual(fixture.sleep.call_count, 3)
            self.assert_single_pinned_send(fixture)

    def test_terminal_failure_statuses_never_write_success(self):
        for value in ("Cancelled", "Cancelling", "TimedOut", "Failed", "Undeliverable", "Unknown"):
            with self.subTest(status=value), self.invocation([status(value)], [100, 101]) as fixture:
                with self.assertRaisesRegex(ValueError, "SSM deployment failed"):
                    release.dispatch(fixture.args)
                self.assertEqual(fixture.output.read_text(), "")
                fixture.sleep.assert_not_called()
                self.assert_single_pinned_send(fixture)

    def test_fatal_lookup_failure_preserves_uncertainty_even_with_success_stdout(self):
        with self.invocation([status("Success", returncode=1, stderr="AccessDeniedException")], [100, 101]) as fixture:
            with self.assertRaisesRegex(ValueError, "status lookup failed; remote command may still be running"):
                release.dispatch(fixture.args)
            self.assertEqual(fixture.output.read_text(), "")
            fixture.sleep.assert_not_called()
            self.assert_single_pinned_send(fixture)

    def test_transient_missing_invocation_can_succeed_without_resending(self):
        with self.invocation(
            [status("", returncode=1, stderr="InvocationDoesNotExist"), status("InProgress"), status("Success")],
            [100, 101, 102, 103],
        ) as fixture:
            release.dispatch(fixture.args)
            self.assertEqual(fixture.output.read_text(), "command_id=" + COMMAND_ID + "\n")
            self.assertEqual(fixture.poll.call_count, 3)
            self.assertEqual(fixture.sleep.call_count, 2)
            self.assert_single_pinned_send(fixture)

    def test_keyboard_interrupt_propagates_without_local_success_or_cancellation(self):
        with self.invocation([KeyboardInterrupt()], [100, 101]) as fixture:
            with self.assertRaises(KeyboardInterrupt):
                release.dispatch(fixture.args)
            self.assertEqual(fixture.output.read_text(), "")
            fixture.sleep.assert_not_called()
            self.assert_single_pinned_send(fixture)


class ProductionDispatchFailureTests(DispatchFailureTests):
    environment = "production"


class ApprovalModeDispatchTests(unittest.TestCase):
    def test_modes_arrive_as_production_only_data_under_existing_ssm_pins(self):
        for target, mode in (("staging", "standard"), ("production", "standard"),
                             ("production", "prelaunch")):
            with self.subTest(target=target, mode=mode), invocation([status("Success")], [100, 101], target) as f:
                f.args.approval_mode = mode
                release.dispatch(f.args)
                command = f.send.call_args.args[0]
                parameters = json.loads(command[command.index("--parameters") + 1])
                if target == "production":
                    self.assertEqual(parameters["ApprovalMode"], [mode])
                else:
                    self.assertNotIn("ApprovalMode", parameters)
                self.assertEqual(command[command.index("--document-version") + 1], "3")
                self.assertEqual(command[command.index("--document-hash") + 1], "e" * 64)

    def test_staging_and_invalid_mode_fail_before_aws(self):
        for target, mode in (("staging", "prelaunch"), ("production", ""),
                             ("production", "$(id)"), ("production", None)):
            with self.subTest(target=target, mode=mode), invocation([], [], target) as f:
                f.args.approval_mode = mode
                with self.assertRaises(ValueError):
                    release.dispatch(f.args)
                f.send.assert_not_called()


if __name__ == "__main__":
    unittest.main()
