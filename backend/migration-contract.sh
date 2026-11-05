#!/bin/sh
# Run from the backend directory. File names and file digests are both bound.
set -eu
export LC_ALL=C
sha256sum go.mod go.sum internal/models/*.go internal/database/database.go \
    internal/database/migrate.go internal/database/controlled_migration.go \
    internal/database/runtime_policy.sql internal/database/runtime_grants.sql \
    internal/database/runtime_policy.go \
    migration-contract.sh > "${1:?Pass an output file for the contract input}"
contract=$(sha256sum "$1")
printf '%s\n' "${contract%% *}"
