"""Exercise the installed backup producer and worker protocol without AWS."""

import base64
from datetime import datetime, timezone
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from test_recovery_worker import FakeCloud, worker


SPEC = importlib.util.spec_from_file_location('backup_producer', Path(__file__).resolve().parents[1] / 'backup.py')
producer = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(producer)
TARGET = {'bucket': 'synthetic-backups', 'account_id': '123456789012'}


class VersionedBackupFixture(FakeCloud):
    def aws(self, args, label, **options):
        def argument(flag):
            return args[args.index(flag) + 1]

        assert argument('--bucket') == TARGET['bucket']
        assert argument('--expected-bucket-owner') == TARGET['account_id']
        operation = args[1]
        if operation == 'get-bucket-versioning':
            return {'Status': 'Enabled'}
        if operation == 'get-bucket-location':
            return super().aws(args, label, **options)
        key = argument('--key')
        if operation == 'put-object':
            assert argument('--if-none-match') == '*'
            assert argument('--server-side-encryption') == 'AES256'
            assert argument('--checksum-algorithm') == 'SHA256'
            assert all(stored_key != key for stored_key, _ in self.objects), 'attempted conditional overwrite'
            data = Path(argument('--body')).read_bytes()
            assert argument('--checksum-sha256') == base64.b64encode(bytes.fromhex(worker.digest(data))).decode()
            version = f'synthetic-version-{len(self.objects) + 1}'
            self.add(key, version, data)
            return {'VersionId': version}
        if operation == 'head-object' and not any(arg.startswith('--version-id=') for arg in args):
            # The producer reads the current version. The worker always supplies a version.
            version, = [v for stored_key, v in self.objects if stored_key == key]
            args = [*args, '--version-id=' + version]
        return super().aws(args, label, **options)


def publish_and_verify_fixture(archive, snapshot, work, *, postgres_version, backup_contract='1' * 64):
    """Return worker evidence and refs, leaving the verified dump at work/archive.dump."""
    openssl = shutil.which('openssl')
    assert openssl is not None, 'OpenSSL 3 is required for the synthetic fixture'
    work = Path(work)
    archive = Path(archive)
    assert work.is_dir() and not any(work.iterdir()), 'worker fixture directory must be empty'
    real_run = producer.run
    storage = VersionedBackupFixture()
    profile = {**TARGET, 'source': snapshot['source'], 'postgres_version': postgres_version,
               'producer_key_id': 'synthetic-producer', 'backup_contract': backup_contract}

    with tempfile.TemporaryDirectory(prefix='vellum-backup-producer-') as temporary:
        root = Path(temporary)
        env = {'PATH': str(Path(openssl).parent) + os.pathsep + os.defpath, 'HOME': temporary,
               'AWS_EC2_METADATA_DISABLED': 'true'}

        def fixture_key(name):
            private, public = root / f'{name}-private.pem', root / f'{name}-public.pem'
            subprocess.run([openssl, 'genpkey', '-algorithm', 'ED25519', '-out', private],
                           env=env, check=True, capture_output=True)
            subprocess.run([openssl, 'pkey', '-in', private, '-pubout', '-out', public],
                           env=env, check=True, capture_output=True)
            return private, public

        private, public = fixture_key('producer')
        _, unrelated_public = fixture_key('unrelated')
        assert public.read_bytes() != unrelated_public.read_bytes(), 'producer and unrelated fixture share a key'
        provenance = {'signing_key': str(private), 'public_key': str(public), 'key_id': profile['producer_key_id']}

        def local_openssl(args, label, timeout=60):
            assert args[0] == '/usr/bin/openssl', 'fixture can only run the producer OpenSSL commands'
            return real_run([openssl, *args[1:]], label, timeout)

        with patch.object(producer, 'BASE', root), patch.object(producer, 'CLEAN_ENV', env), \
             patch.object(producer, 'trusted'), patch.object(producer, 'run', side_effect=local_openssl), \
             patch.object(producer, 'aws', side_effect=lambda _config, args, label: storage.aws(args, label)), \
             patch.object(worker, 'CLEAN_ENV', env):
            producer.check_bucket(TARGET, publishing=True)
            producer.publish_backup('production', TARGET, root, archive, snapshot, provenance)
            marker, = [(key, version) for key, version in storage.objects if key.startswith('production/completed/')]
            store = worker.BackupStore(storage, profile)
            store.check_region()
            now = datetime.now(timezone.utc)
            untrusted = root / 'untrusted'
            untrusted.mkdir()
            try:
                worker.verify_backup(store, storage, profile, *marker, unrelated_public, untrusted, now)
            except worker.RecoveryError as error:
                if 'producer signature' not in str(error):
                    raise
            else:
                raise AssertionError('worker accepted the unrelated fixture key')
            evidence, refs = worker.verify_backup(store, storage, profile, *marker, public, work, now)
        assert evidence['archive_sha256'] == producer.sha256_file(archive)
        assert producer.sha256_file(work / 'archive.dump') == evidence['archive_sha256']
        assert refs['completion']['version_id'] == marker[1]
        return evidence, refs


class BackupWorkerInteropTest(unittest.TestCase):
    def test_actual_producer_output_round_trips_through_worker(self):
        snapshot = {
            'source': {'system_identifier': '7253446013106688026', 'database_oid': 16385,
                       'database_name': 'synthetic'},
            'snapshot_id': '00000003-0000004B-1',
            'snapshot_started_at': producer.now(),
            'postgres_version': '17.8',
        }
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source, work = root / 'source.dump', root / 'worker'
            source.write_bytes(b'PGDMP synthetic exact producer archive')
            work.mkdir()
            evidence, refs = publish_and_verify_fixture(source, snapshot, work, postgres_version='170008')
            self.assertEqual((work / 'archive.dump').read_bytes(), source.read_bytes())
            self.assertEqual(evidence['archive_bytes'], source.stat().st_size)
            self.assertEqual(set(refs), {'completion', 'manifest', 'archive'})
            self.assertEqual(len({ref['version_id'] for ref in refs.values()}), 3)
