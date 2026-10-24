#!/usr/bin/env bash
set -euo pipefail

# Build synthetic contexts with the real ignore rules. No real configuration,
# credentials, or application records are read or copied by this check.
script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repository_directory=$(dirname "$script_directory")
temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/vellum-docker-context.XXXXXXXX")
trap 'rm -rf -- "$temporary_directory"' EXIT

printf 'FROM scratch\nCOPY . /\n' > "$temporary_directory/Dockerfile"

for context in backend frontend; do
  fixture="$temporary_directory/$context"
  output="$temporary_directory/output-$context"
  mkdir -p "$fixture/nested" "$fixture/secrets" "$fixture/.aws" "$fixture/.ssh" \
    "$fixture/.git" "$fixture/node_modules" "$fixture/tmp" "$fixture/coverage"
  cp "$repository_directory/$context/.dockerignore" "$fixture/.dockerignore"
  printf 'source marker\n' > "$fixture/source.txt"
  printf 'PLACEHOLDER=example-only\n' > "$fixture/.env.example"
  printf 'PLACEHOLDER=nested-example-only\n' > "$fixture/nested/.env.example"

  excluded_files=(
    .env .env.local nested/.env.production nested/production.env nested/private.key nested/certificate.pem
    nested/credentials.json secrets/token.txt .aws/credentials .ssh/id_rsa
    .git/config .DS_Store node_modules/dependency.js tmp/build.out coverage/result.out
  )
  # The frontend also produces browser and bundle artifacts.
  if [[ "$context" == frontend ]]; then
    mkdir -p "$fixture/dist" "$fixture/playwright-report" "$fixture/test-results"
    excluded_files+=(dist/index.html playwright-report/index.html test-results/trace.zip)
  fi
  for filename in "${excluded_files[@]}"; do
    [[ -n "$filename" ]] || continue
    printf 'HARMLESS_CONTEXT_SENTINEL\n' > "$fixture/$filename"
  done

  docker build --quiet --file "$temporary_directory/Dockerfile" \
    --output "type=local,dest=$output" "$fixture"

  for filename in source.txt .env.example nested/.env.example; do
    if [[ ! -f "$output/$filename" ]]; then
      printf 'Expected allowed %s fixture to survive: %s\n' "$context" "$filename" >&2
      exit 1
    fi
  done
  for filename in "${excluded_files[@]}"; do
    [[ -n "$filename" ]] || continue
    if [[ -e "$output/$filename" ]]; then
      printf 'Unsafe %s build context includes: %s\n' "$context" "$filename" >&2
      exit 1
    fi
  done
  printf '%s Docker context excludes environment files, credentials, and generated output.\n' "$context"
done
