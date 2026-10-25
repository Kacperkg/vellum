"""Real process/flock regressions; no Docker, network, root or database required."""
from contextlib import ExitStack
import fcntl
import importlib.util
import multiprocessing
import os
from pathlib import Path
import signal
import stat
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

DEPLOY = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(DEPLOY / 'cd'))
import coordination
import controller
from test_release import manifest

spec = importlib.util.spec_from_file_location('coordinated_backup', DEPLOY / 'backup.py')
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)
CONFIG = {'bucket': 'synthetic-test', 'account_id': '123456789012', 'alert_topic_arn': None}


def fixture(state):
    """Only replace root-path/owner checks for private unprivileged temp fixtures."""
    stack = ExitStack()
    stack.enter_context(patch.object(coordination, 'validate_state'))
    stack.enter_context(patch.object(coordination, 'root_owned', lambda info: info.st_uid == os.getuid()))
    stack.enter_context(patch.object(backup, 'STATE', state))
    stack.enter_context(patch.object(controller, 'STATE', state))
    return stack


def run_operation(state, kind, environment, work):
    with fixture(state), ExitStack() as stack:
        if kind == 'maintenance':
            with coordination.operation(state, kind, environment):
                work()
        elif kind == 'deployment':
            value = manifest()
            stack.enter_context(patch.object(controller, 'bundle_hashes', return_value=value['deployment_files']))
            stack.enter_context(patch.object(controller, 'protected'))
            stack.enter_context(patch.object(controller, 'read_json', return_value={}))
            stack.enter_context(patch.object(controller, 'validate_approval'))
            commands = types.SimpleNamespace(apply=lambda _: work(), diagnostics=lambda: None)
            controller.deploy(environment, value, commands)
        else:
            stack.enter_context(patch.object(backup, 'trusted'))
            stack.enter_context(patch.object(backup, 'check_space'))
            stack.enter_context(patch.object(backup, 'BASE', DEPLOY))
            stack.enter_context(patch.object(backup, 'run', return_value=b'17.6'))
            def dump(compose, path, spool):
                work()
                path.write_bytes(b'synthetic archive')
            stack.enter_context(patch.object(backup, 'dump', side_effect=dump))
            stack.enter_context(patch.object(backup, 'verify_archive'))
            stack.enter_context(patch.object(backup, 'upload', side_effect=lambda c, k, p: {'key': k}))
            backup.backup(environment, CONFIG)


def holder(state, kind, environment, connection):
    def work():
        connection.send('entered')
        connection.recv()
    try:
        run_operation(state, kind, environment, work)
        connection.send('done')
    except BaseException as error:
        connection.send(type(error).__name__ + ': ' + str(error))
    finally:
        connection.close()


class CoordinationTests(unittest.TestCase):
    def test_real_backup_deployment_process_matrix(self):
        operations = [(kind, env) for kind in ('backup', 'deployment', 'maintenance') for env in ('staging', 'production')]
        for held_kind, held_env in operations:
            with self.subTest(holder=(held_kind, held_env)), tempfile.TemporaryDirectory() as tmp:
                state = Path(tmp)
                parent, child = multiprocessing.Pipe()
                process = multiprocessing.get_context('fork').Process(target=holder, args=(state, held_kind, held_env, child))
                process.start()
                child.close()
                try:
                    self.assertTrue(parent.poll(5), 'holder did not enter')
                    self.assertEqual(parent.recv(), 'entered')
                    for kind, env in operations:
                        reached = []
                        with self.subTest(contender=(kind, env)), self.assertRaisesRegex((ValueError, backup.BackupError), 'host operation lock busy'):
                            run_operation(state, kind, env, lambda: reached.append(True))
                        self.assertFalse(reached, 'contender executed dump/update')
                    parent.send('release')
                    self.assertTrue(parent.poll(5), 'holder did not finish')
                    self.assertEqual(parent.recv(), 'done')
                    process.join(5)
                    self.assertEqual(process.exitcode, 0)
                    self.assertFalse((state / coordination.PENDING).exists())
                    # All paths recover after a successful holder releases, not just same environment.
                    for kind, env in operations:
                        reached = []
                        run_operation(state, kind, env, lambda: reached.append(True))
                        self.assertEqual(reached, [True])
                finally:
                    if process.is_alive():
                        process.kill()
                        process.join(5)
                    parent.close()

    def test_legacy_environment_locks_and_partial_acquisition_release(self):
        for env in ('staging', 'production'):
            with self.subTest(env=env), tempfile.TemporaryDirectory() as tmp:
                state = Path(tmp)
                # A legacy controller holds ONLY its environment lock, possibly 0644.
                path = state / (env + '.lock')
                path.touch(mode=0o644)
                with path.open('a') as old:
                    fcntl.flock(old, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    for kind in ('backup', 'deployment'):
                        for target in ('staging', 'production'):
                            with self.assertRaisesRegex((ValueError, backup.BackupError), 'lock busy'):
                                run_operation(state, kind, target, lambda: self.fail('unsafe work'))
                    # Earlier locks must be released even when the last environment contends.
                    for name in coordination.LOCKS:
                        if name != env + '.lock':
                            with (state / name).open('a') as probe:
                                fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
                run_operation(state, 'backup', 'production', lambda: None)

    def test_old_backup_global_lock_blocks_new_deployments(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            with (state / 'backup.lock').open('a') as old:
                fcntl.flock(old, fcntl.LOCK_EX | fcntl.LOCK_NB)
                for env in ('staging', 'production'):
                    with self.assertRaisesRegex((ValueError, backup.BackupError), 'lock busy'):
                        run_operation(state, 'deployment', env, lambda: self.fail('unsafe update'))

    def test_failure_retains_latch_and_blocks_all_following_work(self):
        for kind in ('backup', 'deployment'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as tmp:
                state = Path(tmp)
                def fail():
                    raise RuntimeError('incomplete operation')
                with self.assertRaisesRegex(RuntimeError, 'incomplete'):
                    run_operation(state, kind, 'staging', fail)
                data = (state / coordination.PENDING).read_text()
                self.assertIn(kind, data)
                for next_kind in ('backup', 'deployment'):
                    for env in ('staging', 'production'):
                        with self.assertRaisesRegex((ValueError, backup.BackupError), 'operator recovery required'):
                            run_operation(state, next_kind, env, lambda: self.fail('stale work'))
                self.assertEqual((state / coordination.PENDING).read_text(), data)

    def test_signal_death_releases_flocks_but_not_recovery_gate(self):
        for kind in ('backup', 'deployment'):
            for sig in (signal.SIGTERM, signal.SIGKILL):
                with self.subTest(kind=kind, signal=sig), tempfile.TemporaryDirectory() as tmp:
                    state = Path(tmp)
                    parent, child = multiprocessing.Pipe()
                    process = multiprocessing.get_context('fork').Process(target=holder, args=(state, kind, 'production', child))
                    process.start()
                    child.close()
                    try:
                        self.assertTrue(parent.poll(5))
                        self.assertEqual(parent.recv(), 'entered')
                        os.kill(process.pid, sig)
                        process.join(5)
                        self.assertFalse(process.is_alive())
                        with fixture(state), coordination.host_locks(state):
                            self.assertTrue((state / coordination.PENDING).exists())
                        for contender in ('backup', 'deployment'):
                            with self.assertRaisesRegex((ValueError, backup.BackupError), 'operator recovery required'):
                                run_operation(state, contender, 'staging', lambda: self.fail('stale work'))
                    finally:
                        if process.is_alive():
                            process.kill()
                            process.join(5)
                        parent.close()

    def test_pending_broken_symlink_and_lock_inode_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            pending = state / coordination.PENDING
            pending.symlink_to(state / 'missing')
            with self.assertRaisesRegex((ValueError, backup.BackupError), 'operator recovery required'):
                run_operation(state, 'backup', 'production', lambda: self.fail('symlink accepted'))
            self.assertTrue(pending.is_symlink())
            pending.unlink()  # Synthetic test fixture only.
            before = {name: (state / name).stat().st_ino for name in coordination.LOCKS}
            run_operation(state, 'backup', 'production', lambda: None)
            self.assertEqual(before, {name: (state / name).stat().st_ino for name in coordination.LOCKS})

    def test_latch_durable_before_work_and_fsync_failure_blocks_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            events = []
            real_sync = coordination.sync_state
            def sync(path):
                real_sync(path)
                events.append('sync')
            def work():
                self.assertTrue((state / coordination.PENDING).exists())
                self.assertEqual(events, ['sync'])
            with patch.object(coordination, 'sync_state', side_effect=sync):
                run_operation(state, 'backup', 'production', work)
            self.assertEqual(events, ['sync', 'sync'])
            with patch.object(coordination, 'sync_state', side_effect=OSError('disk failure')):
                with self.assertRaisesRegex(OSError, 'disk failure'):
                    run_operation(state, 'deployment', 'staging', lambda: self.fail('work before durable guard'))
            self.assertTrue((state / coordination.PENDING).exists())

    def test_unsafe_lock_files_rejected(self):
        for kind in ('symlink', 'hardlink', 'fifo', 'writable', 'directory', 'owner'):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as tmp:
                state = Path(tmp)
                path = state / 'backup.lock'
                target = state / 'target'
                target.write_text('unchanged')
                if kind == 'symlink':
                    path.symlink_to(target)
                elif kind == 'hardlink':
                    os.link(target, path)
                elif kind == 'fifo':
                    os.mkfifo(path, 0o600)
                elif kind == 'directory':
                    path.mkdir()
                else:
                    path.touch(mode=0o600)
                    if kind == 'writable':
                        path.chmod(0o666)
                with fixture(state):
                    if kind == 'owner':
                        with patch.object(coordination, 'root_owned', return_value=False):
                            with self.assertRaisesRegex(ValueError, 'unsafe operation lock'):
                                with coordination.host_locks(state):
                                    self.fail('unsafe lock accepted')
                    else:
                        with self.assertRaises((ValueError, OSError)):
                            with coordination.host_locks(state):
                                self.fail('unsafe lock accepted')
                self.assertEqual(target.read_text(), 'unchanged')

    def test_base_exception_keeps_pending_and_releases_all_locks(self):
        with tempfile.TemporaryDirectory() as tmp:
            state = Path(tmp)
            with fixture(state):
                with self.assertRaises(KeyboardInterrupt):
                    with coordination.operation(state, 'maintenance', 'staging'):
                        raise KeyboardInterrupt
                with coordination.host_locks(state):
                    self.assertTrue((state / coordination.PENDING).exists())

    def test_backup_validates_library_before_import(self):
        with patch.object(backup, 'trusted', side_effect=backup.BackupError('unsafe library')):
            with self.assertRaisesRegex(backup.BackupError, 'unsafe library'):
                with backup.host_operation('staging'):
                    self.fail('unsafe import accepted')

    def test_state_rejects_untrusted_ancestors_and_symlinks(self):
        state = Path('/var/lib/vellum-cd')
        def safe(path):
            return types.SimpleNamespace(st_uid=0, st_mode=stat.S_IFDIR | (0o700 if path == state else 0o755))
        for unsafe_path in (*state.parents, state):
            for changes in ({'st_uid': 1000}, {'st_mode': stat.S_IFDIR | 0o777}, {'st_mode': stat.S_IFLNK | 0o755}):
                def metadata(path):
                    info = safe(path)
                    if path == unsafe_path:
                        info.__dict__.update(changes)
                    return info
                with self.subTest(path=unsafe_path, changes=changes), patch.object(Path, 'lstat', autospec=True, side_effect=metadata):
                    with self.assertRaisesRegex(ValueError, 'unsafe operation state'):
                        coordination.validate_state(state)
        with patch.object(Path, 'lstat', autospec=True, side_effect=safe):
            coordination.validate_state(state)


if __name__ == '__main__':
    unittest.main()
