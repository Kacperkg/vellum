#!/usr/bin/env python3
"""Explicit local-only synthetic drill; never loaded by unittest discovery.

Requires Docker Desktop, builds the current backend, creates a uniquely named
private project, and removes only that project's disposable resources afterward.
No AWS calls or host production configuration. Run from any directory.
"""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from unittest.mock import patch
import uuid

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('backup', ROOT / 'deploy/backup.py')
backup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(backup)
DOCKER = ['docker', '--context', 'desktop-linux']


def command(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, timeout=600, **kwargs).stdout


def main():
    project = 'vellum-backup-drill-' + uuid.uuid4().hex[:12]
    image = project + ':synthetic'
    endpoint = command(DOCKER + ['context', 'inspect', 'desktop-linux', '--format',
                                '{{.Endpoints.docker.Host}}']).decode().strip()
    assert endpoint.startswith('unix://'), 'only a local Unix Docker socket is allowed'
    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix=project) as directory:
        root = Path(directory)
        environment = {
            'DB_HOST': 'postgres', 'DB_PORT': '5432', 'DB_USER': 'drill',
            'DB_PASSWORD': 'synthetic-drill-only', 'DB_NAME': 'source', 'DB_SSLMODE': 'disable',
            'JWT_SECRET': 'synthetic-drill-signing-key', 'JWT_EXPIRY': '15m',
            'JWT_REFRESH_EXPIRY': '24h', 'PORT': '8080', 'BIND_HOST': '0.0.0.0',
        }
        app = {'image': image, 'environment': environment, 'mem_limit': '256m',
               'cpus': 1, 'read_only': True, 'tmpfs': ['/tmp:size=16m'],
               'depends_on': {'postgres': {'condition': 'service_healthy'}}}
        restored = {**app, 'environment': {**environment, 'DB_NAME': 'restored'}}
        model = {'services': {
            'postgres': {'image': 'postgres:17-alpine', 'mem_limit': '512m', 'cpus': 1,
                         'environment': {'POSTGRES_USER': 'drill', 'POSTGRES_DB': 'source',
                                         'POSTGRES_PASSWORD': 'synthetic-drill-only'},
                         'tmpfs': ['/var/lib/postgresql/data:size=256m'],
                         'healthcheck': {'test': ['CMD', 'pg_isready', '-U', 'drill', '-d', 'source'],
                                         'interval': '1s', 'timeout': '3s', 'retries': 60}},
            'backend': app, 'restored': restored},
            'networks': {'default': {'internal': True}}}
        compose_file = root / 'compose.json'
        compose_file.write_text(json.dumps(model))
        compose = DOCKER + ['compose', '-p', project, '-f', str(compose_file)]

        def sql(database, statement):
            return command(compose + ['exec', '-T', 'postgres', 'psql', '-X', '-v',
                                      'ON_ERROR_STOP=1', '-U', 'drill', '-d', database, '-At'],
                           input=statement.encode()).decode().strip()

        def request(service, path, body=None, token=None):
            args = compose + ['exec', '-T', service, 'wget', '-qO-',
                              '--header', 'Content-Type: application/json']
            if token:
                args += ['--header', 'Authorization: Bearer ' + token]
            if body is not None:
                args += ['--post-data', json.dumps(body)]
            return json.loads(command(args + ['http://127.0.0.1:8080' + path]))

        def wait_app(service):
            for attempt in range(60):
                try:
                    request(service, '/ping')
                    return
                except subprocess.CalledProcessError:
                    time.sleep(1)
            raise RuntimeError('synthetic backend did not become ready')

        try:
            command(DOCKER + ['build', '-q', '-f', str(ROOT / 'backend/Dockerfile.prod'),
                              '-t', image, str(ROOT / 'backend')])
            command(compose + ['up', '-d', '--wait', '--wait-timeout', '120', 'postgres', 'backend'])
            wait_app('backend')
            user = {'first_name': 'Backup', 'last_name': 'Drill', 'email': 'drill@example.test',
                    'password': 'synthetic-drill-password'}
            request('backend', '/register', user)
            login = request('backend', '/login', {'email': user['email'], 'password': user['password']})
            token = login['token'] if 'token' in login else login['access_token']
            request('backend', '/api/accounts', {'name': 'Synthetic Savings'}, token)
            before = request('backend', '/api/accounts', token=token)
            command(compose + ['stop', 'backend'])
            expected = sql('source', 'SELECT count(*) FROM users; SELECT count(*) FROM accounts; '
                                    'SELECT count(*) FROM categories;')
            archive = root / 'synthetic.dump'
            clean = {'PATH': os.environ['PATH'], 'HOME': os.environ['HOME']}
            # Only host paths/ownership/disk gates are bypassed for this non-root fixture.
            # Real PostgreSQL dump/timeout/check/restore and application code all execute.
            with patch.object(backup, 'BASE', root), patch.object(backup, 'CLEAN_ENV', clean), \
                    patch.object(backup, 'check_space'):
                backup.dump(compose, archive, root)
                backup.verify_archive(compose, archive)
            digest = backup.sha256_file(archive)
            copied = root / 'download-simulation.dump'
            copied.write_bytes(archive.read_bytes())
            assert backup.sha256_file(copied) == digest
            command(compose + ['exec', '-T', 'postgres', 'createdb', '-U', 'drill', '-O', 'drill', 'restored'])
            restore_args = compose + ['exec', '-T', 'postgres', 'pg_restore', '--exit-on-error',
                                      '--single-transaction', '--no-owner', '--no-acl',
                                      '-U', 'drill', '-d', 'restored']
            command(restore_args, input=copied.read_bytes())
            assert sql('restored', 'SELECT count(*) FROM users; SELECT count(*) FROM accounts; '
                                   'SELECT count(*) FROM categories;') == expected
            assert sql('restored', 'SELECT count(*) FROM accounts a LEFT JOIN users u ON u.id=a.user_id '
                                   'WHERE u.id IS NULL;') == '0'
            assert sql('restored', 'SELECT count(*) FROM pg_constraint WHERE NOT convalidated;') == '0'
            command(compose + ['up', '-d', '--no-deps', 'restored'])
            wait_app('restored')
            login = request('restored', '/login', {'email': user['email'], 'password': user['password']})
            token = login['token'] if 'token' in login else login['access_token']
            assert request('restored', '/api/accounts', token=token) == before
            # A truncated archive must fail a real transactional restore.
            command(compose + ['exec', '-T', 'postgres', 'createdb', '-U', 'drill', 'corrupt'])
            broken = [*restore_args[:-1], 'corrupt']
            try:
                command(broken, input=archive.read_bytes()[:100])
                raise AssertionError('corrupt restore unexpectedly succeeded')
            except subprocess.CalledProcessError:
                pass
            version = sql('source', 'SHOW server_version;')
            pg_id = command(compose + ['ps', '-q', 'postgres']).decode().strip()
            pg_digest = command(DOCKER + ['inspect', pg_id, '--format', '{{.Image}}']).decode().strip()
            print(json.dumps({'result': 'PASS', 'postgres_version': version, 'image_id': pg_digest,
                              'archive_bytes': archive.stat().st_size, 'archive_sha256': digest,
                              'checks': ['real pg_dump', 'archive list', 'SHA256', 'transactional restore',
                                         'counts and constraints before startup', 'login and accounts after startup',
                                         'corrupt restore rejected'],
                              'elapsed_seconds': round(time.monotonic() - started, 1),
                              's3_round_trip': False}, indent=2))
        finally:
            command(compose + ['down', '--volumes', '--remove-orphans'])
            command(DOCKER + ['image', 'rm', image])


if __name__ == '__main__':
    main()
