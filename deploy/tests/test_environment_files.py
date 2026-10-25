"""Linux ownership fixtures: do not rely on the test host's UID or /etc symlink."""

from pathlib import Path
import stat
import subprocess
import sys
import types
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "cd"))
import controller


DIRECTORY = Path("/etc/vellum-cd")
PARENTS = (Path("/"), Path("/etc"), DIRECTORY)


def metadata(path):
    return types.SimpleNamespace(
        st_uid=0,
        st_mode=(stat.S_IFDIR | 0o755) if path in PARENTS else (stat.S_IFREG | 0o600),
        st_nlink=1,
    )


def commands():
    return controller.Commands(
        "staging",
        {
            "source_sha": "a" * 40,
            "images": {name: "sha256:" + "b" * 64 for name in ("backend", "frontend")},
        },
    )


class EnvironmentFileTests(unittest.TestCase):
    def test_fixed_paths_check_every_component_without_resolving_symlinks(self):
        for environment in ("staging", "production"):
            with (
                self.subTest(environment=environment),
                patch.object(Path, "lstat", autospec=True, side_effect=metadata) as check,
                patch.object(
                    Path, "resolve", side_effect=AssertionError("must not resolve")
                ),
            ):
                path = DIRECTORY / (environment + ".env")
                self.assertEqual(controller.validate_environment_file(environment), path)
                self.assertEqual(
                    [c.args[0] for c in check.call_args_list], [*PARENTS, path]
                )

    def test_rejects_unsafe_ownership_permissions_and_type_of_every_parent(self):
        mutations = (
            {"st_uid": 1000},
            {"st_mode": stat.S_IFDIR | 0o775},
            {"st_mode": stat.S_IFDIR | 0o757},
            {"st_mode": stat.S_IFDIR | 0o1777},
            {"st_mode": stat.S_IFLNK | 0o777},
            {"st_mode": stat.S_IFREG | 0o755},
        )
        for parent in PARENTS:
            for mutation in mutations:
                def unsafe(path):
                    info = metadata(path)
                    if path == parent:
                        info.__dict__.update(mutation)
                    return info

                with (
                    self.subTest(parent=parent, mutation=mutation),
                    patch.object(Path, "lstat", autospec=True, side_effect=unsafe),
                ):
                    with self.assertRaisesRegex(ValueError, "environment parents"):
                        controller.validate_environment_file("staging")

    def test_rejects_nonprivate_nonroot_symlink_hardlink_and_special_file(self):
        mutations = (
            {"st_uid": 1000},
            {"st_mode": stat.S_IFREG | 0o640},
            {"st_mode": stat.S_IFREG | 0o604},
            {"st_mode": stat.S_IFREG | 0o620},
            {"st_mode": stat.S_IFREG | 0o700},
            {"st_mode": stat.S_IFREG | 0o4600},
            {"st_mode": stat.S_IFLNK | 0o600},
            {"st_mode": stat.S_IFDIR | 0o600},
            {"st_mode": stat.S_IFIFO | 0o600},
            {"st_nlink": 2},
        )
        for mutation in mutations:
            def unsafe(path):
                info = metadata(path)
                if path == DIRECTORY / "staging.env":
                    info.__dict__.update(mutation)
                return info

            with (
                self.subTest(mutation=mutation),
                patch.object(Path, "lstat", autospec=True, side_effect=unsafe),
                patch.object(controller.subprocess, "run") as run,
            ):
                cmd = commands()
                with self.assertRaisesRegex(ValueError, "environment file"):
                    cmd.run("validation", cmd.compose + ["config", "--quiet"])
                run.assert_not_called()

    def test_accepts_root_read_only_file(self):
        def read_only(path):
            info = metadata(path)
            if path == DIRECTORY / "staging.env":
                info.st_mode = stat.S_IFREG | 0o400
            return info

        with patch.object(Path, "lstat", autospec=True, side_effect=read_only):
            controller.validate_environment_file("staging")

    def test_missing_environment_file_has_no_checkout_fallback(self):
        def missing(path):
            if path == DIRECTORY / "staging.env":
                raise FileNotFoundError("missing managed env")
            return metadata(path)

        with (
            patch.object(Path, "lstat", autospec=True, side_effect=missing) as check,
            patch.object(controller.subprocess, "run") as run,
        ):
            cmd = commands()
            with self.assertRaises(FileNotFoundError):
                cmd.run("validation", cmd.compose + ["config", "--quiet"])
            run.assert_not_called()
            self.assertEqual(
                [c.args[0] for c in check.call_args_list],
                [*PARENTS, DIRECTORY / "staging.env"],
            )

    def test_unknown_environment_cannot_choose_an_arbitrary_file(self):
        for environment in ("../production", "/tmp/secret", "", "MAIN"):
            with self.subTest(environment=environment):
                with self.assertRaisesRegex(
                    ValueError, "unknown deployment environment"
                ):
                    controller.validate_environment_file(environment)
                with self.assertRaisesRegex(
                    ValueError, "unknown deployment environment"
                ):
                    controller.Commands(environment, {})

    def test_every_compose_invocation_revalidates_and_uses_only_managed_file(self):
        with (
            patch.object(
                controller,
                "validate_environment_file",
                side_effect=[DIRECTORY / "staging.env", ValueError("permissions changed")],
            ) as check,
            patch.object(
                controller.subprocess,
                "run",
                return_value=subprocess.CompletedProcess([], 0, "", ""),
            ) as run,
        ):
            cmd = commands()
            cmd.run("first", cmd.compose + ["config", "--quiet"])
            with self.assertRaisesRegex(ValueError, "permissions changed"):
                cmd.run("second", cmd.compose + ["up", "backend", "frontend"])
            self.assertEqual(check.call_count, 2)
            self.assertEqual(run.call_count, 1)
            argv = run.call_args.args[0]
            self.assertEqual(
                argv[argv.index("--env-file") + 1], str(DIRECTORY / "staging.env")
            )
            self.assertEqual(argv[argv.index("--project-name") + 1], "vellum-staging")


if __name__ == "__main__":
    unittest.main()
