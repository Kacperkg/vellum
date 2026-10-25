#!/bin/sh
set -eu
# Install this wrapper AND backup.py as root-owned files; never sudo-run checkout code.
exec /usr/bin/python3 -I /usr/local/lib/vellum-cd/backup.py "$@"
