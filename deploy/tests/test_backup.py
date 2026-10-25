"""Synthetic failure-path tests: no Docker, AWS, secrets or production access."""
import base64
from contextlib import nullcontext
from datetime import datetime, timedelta, timezone
import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location('vellum_backup', Path(__file__).resolve().parents[1] / 'backup.py')
backup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(backup)
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'cd'))
import coordination

CONFIG = {'bucket': 'synthetic-vellum-test', 'account_id': '123456789012', 'alert_topic_arn': None}


def root_info(path):
    mode = stat.S_IFREG | 0o600 if path.suffix else stat.S_IFDIR | 0o755
    return types.SimpleNamespace(st_uid=0, st_mode=mode, st_nlink=1, st_size=128)


class PathSecurityTests(unittest.TestCase):
    def test_checks_every_parent_without_resolving(self):
        path = Path('/etc/vellum-cd/staging.env')
        with patch.object(Path, 'lstat', autospec=True, side_effect=root_info) as check:
            with patch.object(Path, 'resolve', side_effect=AssertionError('must not follow links')):
                backup.trusted(path, private=True)
        self.assertEqual([c.args[0] for c in check.call_args_list],
                         [Path('/'), Path('/etc'), Path('/etc/vellum-cd'), path])

    def test_rejects_each_untrusted_parent(self):
        path = Path('/etc/vellum-cd/staging.env')
        for parent in path.parents:
            for change in ({'st_uid': 1000}, {'st_mode': stat.S_IFDIR | 0o777},
                           {'st_mode': stat.S_IFLNK | 0o777}):
                def unsafe(item):
                    info = root_info(item)
                    if item == parent:
                        info.__dict__.update(change)
                    return info
                with self.subTest(parent=parent, change=change):
                    with patch.object(Path, 'lstat', autospec=True, side_effect=unsafe):
                        with self.assertRaises(backup.BackupError):
                            backup.trusted(path, private=True)

    def test_rejects_unsafe_secret_files(self):
        path = Path('/etc/vellum-cd/staging.env')
        changes = ({'st_uid': 1000}, {'st_mode': stat.S_IFREG | 0o640},
                   {'st_mode': stat.S_IFLNK | 0o600}, {'st_mode': stat.S_IFIFO | 0o600},
                   {'st_mode': stat.S_IFDIR | 0o600}, {'st_nlink': 2})
        for change in changes:
            def unsafe(item):
                info = root_info(item)
                if item == path:
                    info.__dict__.update(change)
                return info
            with self.subTest(change=change):
                with patch.object(Path, 'lstat', autospec=True, side_effect=unsafe):
                    with self.assertRaises(backup.BackupError):
                        backup.trusted(path, private=True)

    def test_rejects_relative_and_parent_paths(self):
        for path in ('relative.env', '/etc/../tmp/test.env'):
            with self.subTest(path=path):
                with self.assertRaises(backup.BackupError):
                    backup.trusted(Path(path))

    def test_clean_subprocess_environment_drops_hostile_overrides(self):
        hostile = {'DOCKER_HOST': 'tcp://attacker:2375', 'DOCKER_CONTEXT': 'attacker',
                   'AWS_ENDPOINT_URL': 'https://attacker.invalid', 'AWS_ACCESS_KEY_ID': 'secret',
                   'AWS_PROFILE': 'attacker', 'COMPOSE_FILE': '/tmp/attacker.yml', 'DB_NAME': 'wrong'}
        with patch.dict(os.environ, hostile):
            with patch.object(backup.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, b'ok')) as run:
                self.assertEqual(backup.run(['docker', 'version'], 'test'), b'ok')
        env = run.call_args.kwargs['env']
        self.assertEqual(env['DOCKER_HOST'], 'unix:///var/run/docker.sock')
        for name in hostile.keys() - {'DOCKER_HOST'}:
            self.assertNotIn(name, env)
        self.assertEqual(env['AWS_SHARED_CREDENTIALS_FILE'], '/dev/null')
        self.assertEqual(run.call_args.kwargs['stderr'], subprocess.DEVNULL)

    def test_environment_argument_allowlist_before_any_commands(self):
        for args in ([], ['../production'], ['MAIN'], ['staging', 's3://other'],
                     ['production', '--check-freshness', 'extra']):
            with self.subTest(args=args):
                with patch.object(backup.os, 'geteuid', return_value=0), \
                     patch.object(backup, '__file__', str(backup.BASE / 'backup.py')), \
                     patch.object(sys, 'argv', ['backup.sh', *args]), \
                     patch.object(backup, 'run') as run:
                    with self.assertRaises(backup.BackupError):
                        backup.main()
                    run.assert_not_called()

    def test_valid_environment_and_mode_dispatch_without_database_access(self):
        for environment in ('staging', 'production'):
            for freshness in (False, True):
                argv = ['backup.sh', environment] + (['--check-freshness'] if freshness else [])
                with self.subTest(environment=environment, freshness=freshness):
                    with patch.object(backup.os, 'geteuid', return_value=0), \
                         patch.object(backup, '__file__', str(backup.BASE / 'backup.py')), \
                         patch.object(sys, 'argv', argv), patch.object(backup.os, 'umask'), \
                         patch.object(backup.signal, 'signal'), patch.object(backup, 'trusted'), \
                         patch.object(backup, 'load_config', return_value=CONFIG), \
                         patch.object(backup, 'backup') as create, \
                         patch.object(backup, 'check_freshness') as check:
                        backup.main()
                        (check if freshness else create).assert_called_once_with(environment, CONFIG)
                        (create if freshness else check).assert_not_called()

    def test_config_only_allows_fixed_schema_and_account_bound_topic(self):
        for change in ({'bucket': 's3://other'}, {'bucket': '127.0.0.1'},
                       {'account_id': 'wrong'}, {'alert_topic_arn': 'arn:aws:sns:us-east-1:123456789012:test'},
                       {'alert_topic_arn': 'arn:aws:sns:eu-west-2:999999999999:test'}, {'endpoint': 'https://other'}):
            config = dict(CONFIG, **change)
            with self.subTest(change=change):
                with patch.object(backup, 'trusted'), \
                     patch.object(Path, 'stat', return_value=types.SimpleNamespace(st_size=100)), \
                     patch.object(Path, 'read_text', return_value=json.dumps(config)):
                    with self.assertRaises(backup.BackupError):
                        backup.load_config()


class ResourceAndCommandTests(unittest.TestCase):
    def test_sanitized_subprocess_failure_and_timeout(self):
        for effect in (subprocess.CompletedProcess([], 1, b'sensitive'),
                       subprocess.TimeoutExpired(['secret'], 60), OSError('secret')):
            with self.subTest(effect=type(effect).__name__):
                kwargs = {'side_effect': effect} if isinstance(effect, Exception) else {'return_value': effect}
                with patch.object(backup.subprocess, 'run', **kwargs):
                    with self.assertRaises(backup.BackupError) as result:
                        backup.run(['test'], 'safe step')
                    self.assertNotIn('secret', str(result.exception))

    def test_aws_retries_are_bounded(self):
        with patch.object(backup, 'run', side_effect=backup.BackupError('safe')) as run, \
             patch.object(backup.time, 'sleep') as sleep:
            with self.assertRaises(backup.BackupError):
                backup.aws(CONFIG, ['s3api', 'head-object'], 'head')
        self.assertEqual(run.call_count, 3)
        self.assertEqual([c.args[0] for c in sleep.call_args_list], [1, 2])
        self.assertTrue(all(c.kwargs['timeout'] == 60 for c in run.call_args_list))

    def test_disk_cap_and_reserve_fail_before_dump(self):
        ample = types.SimpleNamespace(total=20 * 1024**3, free=10 * 1024**3)
        with patch.object(backup, 'spool_bytes', return_value=backup.MAX_SPOOL), \
             patch.object(backup.shutil, 'disk_usage', return_value=ample):
            with self.assertRaisesRegex(backup.BackupError, 'cap'):
                backup.check_space(Path('/ignored'))
        with patch.object(backup, 'spool_bytes', return_value=0), \
             patch.object(backup.shutil, 'disk_usage', return_value=types.SimpleNamespace(total=20 * 1024**3, free=backup.RESERVE)):
            with self.assertRaisesRegex(backup.BackupError, 'reserve'):
                backup.check_space(Path('/ignored'))

    def test_dump_disk_exhaustion_cancels_container_before_terminating_client(self):
        process = Mock(returncode=None)
        process.poll.return_value = None
        with tempfile.TemporaryDirectory() as temporary:
            with patch.object(backup.subprocess, 'Popen', return_value=process), \
                 patch.object(backup, 'check_space', side_effect=backup.BackupError('disk reserve')), \
                 patch.object(backup, 'cancel_dump') as cancel, \
                 patch.object(backup.os, 'killpg') as kill:
                with self.assertRaisesRegex(backup.BackupError, 'disk reserve'):
                    backup.dump(['docker', 'compose'], Path(temporary) / 'partial.dump', Path(temporary))
                cancel.assert_called_once()
                self.assertIn('/tmp/vellum-backup-', cancel.call_args.args[1])
                kill.assert_called_once_with(process.pid, backup.signal.SIGTERM)
                process.wait.assert_called_once_with(timeout=5)

    def test_real_flock_contends_for_controller_and_global_paths(self):
        actual_fstat = os.fstat
        def root_fstat(fd):
            info = actual_fstat(fd)
            return types.SimpleNamespace(st_uid=0, st_mode=info.st_mode, st_nlink=info.st_nlink)
        with tempfile.TemporaryDirectory() as temporary, patch.object(backup.os, 'fstat', side_effect=root_fstat):
            for name in ('production.lock', 'backup.lock'):
                path = Path(temporary) / name
                with coordination.lock(path):
                    with self.assertRaisesRegex(ValueError, 'lock busy'):
                        with coordination.lock(path):
                            self.fail('contended lock unexpectedly acquired')
                with coordination.lock(path):
                    pass

    def test_existing_controller_lock_with_default_0644_mode_is_compatible(self):
        actual_fstat = os.fstat
        def root_fstat(fd):
            info = actual_fstat(fd)
            return types.SimpleNamespace(st_uid=0, st_mode=info.st_mode, st_nlink=info.st_nlink)
        with tempfile.TemporaryDirectory() as temporary, patch.object(backup.os, 'fstat', side_effect=root_fstat):
            path = Path(temporary) / 'production.lock'
            path.touch(mode=0o644)
            path.chmod(0o644)
            with coordination.lock(path):
                pass
            path.chmod(0o666)
            with self.assertRaisesRegex(ValueError, 'unsafe operation lock'):
                with coordination.lock(path):
                    self.fail('world-writable lock accepted')

    def test_lock_rejects_symlink_without_touching_target(self):
        with tempfile.TemporaryDirectory() as temporary:
            target = Path(temporary) / 'target'
            target.write_text('untouched')
            link = Path(temporary) / 'lock'
            link.symlink_to(target)
            with self.assertRaises(OSError):
                with coordination.lock(link):
                    self.fail('symlink accepted')
            self.assertEqual(target.read_text(), 'untouched')

    def test_corrupt_archive_check_fails(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / 'bad.dump'
            archive.write_bytes(b'not an archive')
            with patch.object(backup.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1)):
                with self.assertRaisesRegex(backup.BackupError, 'archive list'):
                    backup.verify_archive(['docker', 'compose'], archive)

    def test_pg_dump_failure_is_not_valid_artifact(self):
        process = Mock(returncode=1)
        process.poll.return_value = 1
        with tempfile.TemporaryDirectory() as temporary:
            with patch.object(backup.subprocess, 'Popen', return_value=process), \
                 patch.object(backup, 'cancel_dump') as cancel:
                with self.assertRaisesRegex(backup.BackupError, 'database dump failed'):
                    backup.dump(['docker', 'compose'], Path(temporary) / 'failed.dump', Path(temporary))
                cancel.assert_called_once()

    def test_unconfirmed_container_cancellation_holds_lock_for_bound(self):
        with patch.object(backup, 'run', side_effect=backup.BackupError('daemon unavailable')), \
             patch.object(backup.time, 'sleep') as sleep:
            backup.cancel_dump(['docker', 'compose'], '/tmp/vellum-backup-synthetic')
        self.assertGreaterEqual(sum(c.args[0] for c in sleep.call_args_list), 905)


class UploadTests(unittest.TestCase):
    def test_upload_checks_sse_size_and_whole_object_checksum(self):
        digest = 'a' * 64
        expected = {'ContentLength': 12, 'ServerSideEncryption': 'AES256',
                    'ChecksumSHA256': base64.b64encode(bytes.fromhex(digest)).decode()}
        with patch.object(backup, 'aws', return_value=expected) as aws:
            backup.verify_object(CONFIG, 'staging/runs/run.dump', 12, digest)
        self.assertIn('--expected-bucket-owner', aws.call_args.args[1])
        self.assertIn('--checksum-mode', aws.call_args.args[1])
        for change in ({'ContentLength': 13}, {'ServerSideEncryption': 'aws:kms'}, {'ChecksumSHA256': 'wrong'}):
            with self.subTest(change=change), patch.object(backup, 'aws', return_value=dict(expected, **change)):
                with self.assertRaisesRegex(backup.BackupError, 'mismatch'):
                    backup.verify_object(CONFIG, 'staging/runs/run.dump', 12, digest)

    def test_single_put_never_overwrites_and_verifies_before_return(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / 'dump'
            archive.write_bytes(b'synthetic')
            with patch.object(backup, 'aws', return_value={}) as aws, \
                 patch.object(backup, 'verify_object') as verify:
                reference = backup.upload(CONFIG, 'staging/runs/unique.dump', archive)
        argv = aws.call_args.args[1]
        self.assertEqual(argv[argv.index('--if-none-match') + 1], '*')
        self.assertEqual(argv[argv.index('--server-side-encryption') + 1], 'AES256')
        self.assertEqual(reference['size'], 9)
        verify.assert_called_once_with(CONFIG, reference['key'], 9, reference['sha256'])


    def test_lost_put_response_requires_matching_existing_object(self):
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / 'dump'
            archive.write_bytes(b'synthetic')
            with patch.object(backup, 'aws', side_effect=backup.BackupError('lost response')), \
                 patch.object(backup, 'verify_object') as verify:
                reference = backup.upload(CONFIG, 'staging/runs/unique.dump', archive)
                self.assertEqual(reference['size'], 9)
                verify.assert_called_once()
            with patch.object(backup, 'aws', side_effect=backup.BackupError('lost response')), \
                 patch.object(backup, 'verify_object', side_effect=backup.BackupError('mismatch')):
                with self.assertRaisesRegex(backup.BackupError, 'mismatch'):
                    backup.upload(CONFIG, 'staging/runs/unique.dump', archive)


class BackupFlowTests(unittest.TestCase):
    def run_backup(self, root, upload, dump_failure=False, archive_failure=False):
        def synthetic_dump(compose, archive, spool):
            archive.write_bytes(b'PGDMP synthetic archive')
            if dump_failure:
                raise backup.BackupError('database dump failed')
        def synthetic_run(args, label, timeout=60):
            return b'17.6\n' if label == 'PostgreSQL version' else b''
        with patch.object(backup, 'STATE', root), patch.object(backup, 'trusted'), \
             patch.object(backup, 'host_operation', side_effect=lambda environment: nullcontext()), \
             patch.object(backup, 'check_space'), patch.object(backup, 'run', side_effect=synthetic_run), \
             patch.object(backup, 'dump', side_effect=synthetic_dump), \
             patch.object(backup, 'verify_archive', side_effect=backup.BackupError('corrupt') if archive_failure else None), \
             patch.object(backup, 'upload', side_effect=upload):
            backup.backup('production', CONFIG)

    def test_no_marker_on_dump_or_archive_failure_and_partial_cleanup(self):
        for dump_failure, archive_failure in ((True, False), (False, True)):
            with self.subTest(dump=dump_failure, archive=archive_failure), tempfile.TemporaryDirectory() as temporary:
                upload = Mock()
                with self.assertRaises(backup.BackupError):
                    self.run_backup(Path(temporary), upload, dump_failure, archive_failure)
                upload.assert_not_called()
                self.assertEqual(list((Path(temporary) / 'backups').iterdir()), [])

    def test_failed_upload_preserves_complete_dump_and_never_marks_success(self):
        for fail_at in (0, 1):
            with self.subTest(fail_at=fail_at), tempfile.TemporaryDirectory() as temporary:
                calls = []
                def upload(config, key, path):
                    calls.append(key)
                    if len(calls) > fail_at:
                        raise backup.BackupError('upload failed')
                    return {'key': key, 'size': path.stat().st_size, 'sha256': backup.sha256_file(path)}
                with self.assertRaises(backup.BackupError):
                    self.run_backup(Path(temporary), upload)
                self.assertTrue(all('/completed/' not in key for key in calls))
                self.assertEqual(len(list((Path(temporary) / 'backups').glob('*/database.dump'))), 1)

    def test_success_marker_is_last_and_cleanup_preserves_older_failed_copy(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            old = root / 'backups' / 'production-old'
            old.mkdir(parents=True)
            (old / 'database.dump').write_bytes(b'last usable prior copy')
            calls = []
            def upload(config, key, path):
                calls.append((key, path.read_bytes()))
                return {'key': key, 'size': path.stat().st_size, 'sha256': backup.sha256_file(path)}
            self.run_backup(root, upload)
            self.assertEqual(len(calls), 3)
            self.assertIn('/runs/', calls[0][0])
            self.assertIn('/completed/', calls[-1][0])
            marker = json.loads(calls[-1][1])
            self.assertEqual(marker['archive']['key'], calls[0][0])
            self.assertEqual(marker['manifest']['key'], calls[1][0])
            self.assertTrue((old / 'database.dump').exists())
            self.assertEqual(list((root / 'backups').iterdir()), [old])


class FreshnessTests(unittest.TestCase):
    def marker(self, age=0):
        snapshot = datetime.now(timezone.utc) - timedelta(seconds=age)
        run_id = snapshot.strftime('%Y%m%dT%H%M%SZ-') + 'a' * 32
        base = 'production/runs/' + run_id
        return run_id, {'format_version': 1, 'environment': 'production',
                        'snapshot_started_at': snapshot.isoformat(),
                        'archive': {'key': base + '.dump', 'size': 10, 'sha256': 'a' * 64},
                        'manifest': {'key': base + '.json', 'size': 20, 'sha256': 'b' * 64}}

    def run_freshness(self, age=0, tamper=None):
        run_id, marker = self.marker(age)
        if tamper:
            tamper(marker)
        def aws(config, args, label):
            if label == 'backup freshness listing':
                return {'Contents': [{'Key': 'production/completed/' + run_id + '.json', 'Size': 512}]}
            Path(args[-1]).write_text(json.dumps(marker))
            return {}
        with patch.object(backup, 'aws', side_effect=aws), patch.object(backup, 'verify_object') as verify:
            backup.check_freshness('production', CONFIG)
            return verify.call_count

    def test_fresh_backup_verifies_both_objects(self):
        self.assertEqual(self.run_freshness(age=60), 2)

    def test_26_hour_limit_uses_snapshot_not_upload_time(self):
        with self.assertRaisesRegex(backup.BackupError, 'stale'):
            self.run_freshness(age=backup.MAX_AGE + 10)
        with self.assertRaisesRegex(backup.BackupError, 'stale'):
            self.run_freshness(age=-60)

    def test_missing_or_truncated_listing_fails(self):
        for listing in ({}, {'IsTruncated': True}):
            with self.subTest(listing=listing), patch.object(backup, 'aws', return_value=listing):
                with self.assertRaises(backup.BackupError):
                    backup.check_freshness('production', CONFIG)

    def test_cross_environment_reference_fails(self):
        with self.assertRaisesRegex(backup.BackupError, 'key mismatch'):
            self.run_freshness(tamper=lambda marker: marker['archive'].update(key='staging/runs/wrong.dump'))


if __name__ == '__main__':
    unittest.main()
