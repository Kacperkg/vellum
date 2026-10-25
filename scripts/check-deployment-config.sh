#!/usr/bin/env bash
set -euo pipefail

# Synthetic values only. Never load a developer or EC2 environment file here.
export COMPOSE_PROJECT_NAME=vellum-config-check APP_ORIGIN=https://example.invalid
export IMAGE_PREFIX=vellum IMAGE_TAG=0123456789012345678901234567890123456789
export DB_NAME=vellum_test DB_USER=vellum_test DB_PASSWORD=synthetic-test-only
export JWT_SECRET=synthetic-config-validation-only TRUSTED_PROXY_CIDRS=172.18.255.254/32
export VELLUM_BACKEND_IMAGE=ghcr.io/kacperkg/vellum-backend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export VELLUM_FRONTEND_IMAGE=ghcr.io/kacperkg/vellum-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb

docker compose -f deploy/compose.yml config --quiet
docker compose -f deploy/compose.yml -f deploy/compose.registry.yml config --quiet
docker run --rm --network none \
  --mount "type=bind,src=$PWD/deploy/Caddyfile.example,dst=/etc/caddy/Caddyfile,readonly" \
  caddy:2-alpine caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
