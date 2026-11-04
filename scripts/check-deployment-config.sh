#!/usr/bin/env bash
set -euo pipefail

# Synthetic values only. Never load a developer or EC2 environment file here.
export COMPOSE_PROJECT_NAME=vellum-config-check APP_ORIGIN=https://example.invalid
export IMAGE_PREFIX=vellum IMAGE_TAG=0123456789012345678901234567890123456789
export DB_NAME=vellum_test DB_USER=vellum_test DB_PASSWORD=synthetic-test-only
export JWT_SECRET=synthetic-config-validation-only TRUSTED_PROXY_CIDRS=172.18.255.254/32
export VELLUM_BACKEND_IMAGE=ghcr.io/kacperkg/vellum-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export VELLUM_FRONTEND_IMAGE=ghcr.io/kacperkg/vellum-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

python3 - <<'PY'
import json
import os
from pathlib import Path
import subprocess
import tempfile

# All Compose calls use an allowlisted client environment and explicit env file:
# neither local .env files nor ambient application/COMPOSE_* settings may enter
# this synthetic check. Preserve only Docker client connection settings.
base = Path("deploy/compose.yml").resolve()
clean_env = {
    key: os.environ[key]
    for key in ("PATH", "HOME", "DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG")
    if key in os.environ
}
keys = (
    "IMAGE_PREFIX", "IMAGE_TAG", "DB_NAME", "DB_USER", "DB_PASSWORD",
    "JWT_SECRET", "TRUSTED_PROXY_CIDRS", "VELLUM_BACKEND_IMAGE",
    "VELLUM_FRONTEND_IMAGE",
)
synthetic_env = {
    **clean_env, **{key: os.environ[key] for key in keys},
    "COMPOSE_PROJECT_NAME": "vellum-config-check", "APP_ORIGIN": "https://example.invalid",
}
for files in ((base,), (base, base.with_name("compose.registry.yml"))):
    subprocess.run(
        ["docker", "compose", "--env-file", os.devnull,
         *[arg for path in files for arg in ("-f", str(path))], "config", "--quiet"],
        env=synthetic_env, check=True,
    )

# Relocation must preserve the entire model; remove application settings from
# the subprocess environment so the synthetic --env-file is actually tested.
with tempfile.TemporaryDirectory() as temporary:
    checkout = Path(temporary) / "checkout/deploy"
    managed = Path(temporary) / "etc/vellum-cd"
    for directory in (checkout, managed):
        directory.mkdir(parents=True)
    for environment in ("staging", "production"):
        project = "vellum-" + environment
        values = {key: os.environ[key] for key in keys}
        values.update(COMPOSE_PROJECT_NAME=project, APP_ORIGIN="https://example.invalid")
        data = "".join(f"{key}={value}\n" for key, value in values.items())
        models = []
        for directory in (checkout, managed):
            path = directory / (environment + ".env")
            path.write_text(data)
            path.chmod(0o600)
            result = subprocess.run(
                ["docker", "compose", "--project-name", project, "--env-file", str(path),
                 "-f", str(base), "-f", str(base.with_name("compose.registry.yml")),
                 "config", "--format", "json"],
                env=clean_env, capture_output=True, text=True, check=True,
            )
            models.append(json.loads(result.stdout))
        assert models[0] == models[1], "env-file relocation changes Compose configuration"
        assert models[1]["name"] == project
        assert models[1]["volumes"]["postgres_data"]["name"] == project + "_postgres_data"
print("Synthetic env-file relocation preserves both Compose projects and configurations.")
PY
docker run --rm --network none \
  --mount "type=bind,src=$PWD/deploy/Caddyfile.example,dst=/etc/caddy/Caddyfile,readonly" \
  caddy:2-alpine caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile

# Exercise POST/OPTIONS versus SPA GET dispatch using loopback-only fixtures.
python3 scripts/check-recovery-routing.py
