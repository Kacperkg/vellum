#!/usr/bin/env python3
"""Exercise the reviewed Caddy routes locally; no real hosts or application data."""
import pathlib
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid

root = pathlib.Path(__file__).resolve().parents[1]
source = (root / 'deploy/Caddyfile.example').read_text()
for original, replacement in (
    ('staging.vellum.kacpergajdarski.com {', 'http://staging.test:8080 {'),
    ('\nvellum.kacpergajdarski.com {', '\nhttp://production.test:8080 {'),
    ('vellum-production-backend:8080', '127.0.0.1:18081'),
    ('vellum-staging-backend:8080', '127.0.0.1:18081'),
    ('vellum-production-frontend:8080', '127.0.0.1:18082'),
    ('vellum-staging-frontend:8080', '127.0.0.1:18082'),
):
    if source.count(original) != (2 if "-backend:8080" in original else 1):
        raise SystemExit(f'Unexpected Caddy fixture structure: {original}')
    source = source.replace(original, replacement)
source += '\nhttp://:18081 {\n respond "backend {http.request.method}"\n}\n'
source += '\nhttp://:18082 {\n respond "frontend {http.request.method}"\n}\n'
name = 'vellum-recovery-routing-' + uuid.uuid4().hex
with tempfile.TemporaryDirectory(prefix='vellum-recovery-routing-') as directory:
    config = pathlib.Path(directory) / 'Caddyfile'
    config.write_text(source)
    try:
        subprocess.run(['docker', 'run', '--detach', '--rm', '--name', name,
                        '--publish', '127.0.0.1::8080', '--mount',
                        f'type=bind,src={config},dst=/etc/caddy/Caddyfile,readonly',
                        'caddy:2-alpine'], check=True, capture_output=True, text=True)
        binding = subprocess.check_output(['docker', 'port', name, '8080/tcp'], text=True).strip()
        port = binding.rsplit(':', 1)[1]
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(host, method, path):
            req = urllib.request.Request(f'http://127.0.0.1:{port}{path}', method=method,
                                         headers={'Host': host})
            with opener.open(req, timeout=3) as response:
                return response.read().decode(), response.headers

        for attempt in range(50):
            try:
                request('production.test', 'GET', '/login')
                break
            except (OSError, urllib.error.URLError):
                time.sleep(.1)
        else:
            raise SystemExit('Local Caddy fixture did not become ready')
        for host in ('production.test', 'staging.test'):
            for path in ('/login', '/register', '/refresh', '/logout', '/forgot-password', '/reset-password'):
                for method in ('GET', 'POST', 'OPTIONS'):
                    body, headers = request(host, method, path)
                    expected = ('frontend' if method == 'GET' else 'backend') + ' ' + method
                    assert body == expected, (host, method, path, body)
                    if path in ('/forgot-password', '/reset-password'):
                        assert headers.get('Cache-Control') == 'no-store', (path, headers)
                        assert headers.get('Referrer-Policy') == 'no-referrer', (path, headers)
            body, _ = request(host, 'GET', '/api/accounts')
            assert body == 'backend GET', body
        print('Caddy: both hosts route auth POST/OPTIONS, SPA GET, API and recovery privacy headers correctly.')
    finally:
        subprocess.run(['docker', 'rm', '--force', name], capture_output=True, check=False)
