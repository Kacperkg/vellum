#!/usr/bin/env python3
"""Disposable PG17 producer test. Run explicitly with a local Docker daemon."""

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

SPEC = importlib.util.spec_from_file_location('vellum_backup', Path(__file__).resolve().parents[1] / 'backup.py')
backup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(backup)


def verify_snapshot(work):
    context = os.environ.get('DOCKER_CONTEXT') or subprocess.check_output(
        ['docker', 'context', 'show'], text=True).strip()
    endpoint = os.environ.get('DOCKER_HOST') or subprocess.check_output(
        ['docker', 'context', 'inspect', context, '--format', '{{.Endpoints.docker.Host}}'], text=True).strip()
    if not endpoint.startswith('unix:///'):
        raise ValueError('snapshot integration requires a local Docker Unix socket')
    backup.BASE = work
    backup.CLEAN_ENV = {'PATH': os.environ['PATH'], 'DOCKER_HOST': endpoint, 'HOME': os.environ['HOME']}
    backup.check_space = lambda spool, new_dump=True: None  # producer storage limits have separate tests
    config = work / 'compose.json'
    config.write_text(json.dumps({'services': {'postgres': {
        'image': 'postgres:17-alpine', 'network_mode': 'none', 'mem_limit': '512m', 'cpus': 1,
        'environment': {'POSTGRES_DB': 'source_db', 'POSTGRES_USER': 'synthetic_owner',
                        'POSTGRES_HOST_AUTH_METHOD': 'trust'},
        'tmpfs': ['/var/lib/postgresql/data:rw,nosuid,size=384m'],
    }}}))
    compose = ['docker', 'compose', '-p', 'vellum-snapshot-test-' + uuid.uuid4().hex, '-f', str(config)]

    def psql(sql, database='source_db'):
        return backup.run([*compose, 'exec', '-T', 'postgres', 'psql', '-XAt', '-v', 'ON_ERROR_STOP=1',
                           '-U', 'synthetic_owner', '-d', database, '-c', sql], 'synthetic PostgreSQL command')

    try:
        backup.run([*compose, 'up', '-d', '--wait', '--wait-timeout', '45', '--no-build', '--pull', 'never'],
                   'synthetic PostgreSQL start')
        for attempt in range(20):
            try:
                # Use the network socket inside the isolated container to skip the temporary init server.
                backup.run([*compose, 'exec', '-T', 'postgres', 'pg_isready', '-h', '127.0.0.1',
                            '-U', 'synthetic_owner', '-d', 'source_db'], 'synthetic PostgreSQL readiness')
                break
            except backup.BackupError:
                if attempt == 19:
                    raise
                time.sleep(1)
        psql("CREATE TABLE ledger (entry text); INSERT INTO ledger VALUES ('before'); "
             "CREATE ROLE synthetic_reader; GRANT SELECT ON ledger TO synthetic_reader;")
        psql('CREATE DATABASE restored_db')
        row = psql("SELECT json_build_array(system_identifier::text, oid::bigint, current_database()) "
                   "FROM pg_control_system(), pg_database WHERE datname = current_database()")
        source = dict(zip(('system_identifier', 'database_oid', 'database_name'), json.loads(row), strict=True))
        wrong_source = dict(source, database_oid=source['database_oid'] + 1)
        try:
            with backup.exported_snapshot(compose, wrong_source):
                raise AssertionError('accepted wrong database OID')
        except backup.BackupError as error:
            if str(error) != 'unexpected PostgreSQL source identity':
                raise
        archive = work / 'snapshot.dump'
        with backup.exported_snapshot(compose, source) as snapshot:
            psql("INSERT INTO ledger VALUES ('after')")
            try:
                backup.dump(compose, work / 'wrong.dump', work,
                            dict(snapshot, snapshot_id='00000000-00000000-1'))
            except backup.BackupError as error:
                if str(error) != 'database dump failed':
                    raise
            else:
                raise AssertionError('pg_dump accepted nonexistent snapshot')
            backup.dump(compose, archive, work, snapshot)
        backup.verify_archive(compose, archive)
        with archive.open('rb') as source_file:
            subprocess.run([*compose, 'exec', '-T', 'postgres', 'pg_restore', '--exit-on-error',
                            '--single-transaction', '-U', 'synthetic_owner', '-d', 'restored_db'],
                           cwd=work, env=backup.CLEAN_ENV, stdin=source_file, timeout=60, check=True)
        assert psql('SELECT entry FROM ledger ORDER BY entry', 'source_db') == b'after\nbefore\n'
        assert psql('SELECT entry FROM ledger', 'restored_db') == b'before\n'
        assert psql("SELECT has_table_privilege('synthetic_reader', 'ledger', 'SELECT')", 'restored_db') == b't\n'
        print('PG17 source binding, snapshot exclusion, transaction restore and ACL preservation verified')
    finally:
        backup.run([*compose, 'down', '--timeout', '5'], 'synthetic PostgreSQL cleanup')


if __name__ == '__main__':
    with tempfile.TemporaryDirectory(prefix='vellum-snapshot-test-') as temporary:
        verify_snapshot(Path(temporary))
