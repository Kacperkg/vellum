#!/usr/bin/env python3
"""Explicit synthetic PostgreSQL 17 drill with no AWS or production resources.

Pass a cached PostgreSQL 17 Alpine digest and a local backend image built from
the current checkout. The script creates and deletes only its unique test
containers. It is excluded from unittest discovery.
"""
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import secrets
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "deploy" / "recovery"))
sys.path.insert(0, str(ROOT / "deploy"))
import backup
import worker
from test_recovery_backup_interop import publish_and_verify_fixture


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--docker-host", default="unix:///var/run/docker.sock")
    parser.add_argument("--postgres-image", required=True)
    parser.add_argument("--backend-image", required=True)
    parser.add_argument("--emit-synthetic-evidence", action="store_true")
    args = parser.parse_args()
    if not re.fullmatch(r"unix:///[A-Za-z0-9_./-]+", args.docker_host):
        parser.error("only a local Unix Docker socket is allowed")
    if not re.fullmatch(r"(?:docker\.io/library/)?postgres@sha256:[a-f0-9]{64}", args.postgres_image):
        parser.error("a pinned PostgreSQL image is required")
    worker.DOCKER = docker = ["docker", "--host", args.docker_host]
    worker.CLEAN_ENV["PATH"] = "/opt/homebrew/bin:" + worker.CLEAN_ENV["PATH"]
    started = datetime.now(timezone.utc)
    job = secrets.token_hex(16)
    source = "vellum-recovery-test-source-" + job
    database = "recovery_fixture"
    bootstrap_role = "fixture_admin"
    password = secrets.token_hex(32)
    label = "org.vellum.recovery.source=" + job
    db_env = {"DB_HOST": "127.0.0.1", "DB_PORT": "5432", "DB_NAME": database,
              "DB_USER": "vellum_migrator", "DB_PASSWORD": password, "DB_SSLMODE": "disable"}

    def call(args, **kwargs):
        result = subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=kwargs.pop("timeout", 60), **kwargs)
        return result.stdout

    def pg(argv, **kwargs):
        return call([*docker, "exec", "-i", source, *argv], **kwargs)

    psql = ["psql", "-X", "-qAt", "-v", "ON_ERROR_STOP=1", "-U", bootstrap_role, "-d", database]

    try:
        call([*docker, "create", "--name", source, "--label=" + label,
              "--network=none", "--pull=never", "--log-driver=none", "--memory=1g",
              "--tmpfs=/var/lib/postgresql/data:rw,noexec,nosuid,size=512m",
              "--env=POSTGRES_DB=" + database, "--env=POSTGRES_USER=" + bootstrap_role,
              "--env=POSTGRES_PASSWORD=" + password,
              "--env=POSTGRES_INITDB_ARGS=--auth-host=scram-sha-256 --locale=C --encoding=UTF8",
              args.postgres_image])
        call([*docker, "start", source])
        for _ in range(25):
            try:
                pg(["pg_isready", "-h", "127.0.0.1", "-U", bootstrap_role, "-d", database])
                break
            except subprocess.CalledProcessError:
                time.sleep(1)
        else:
            raise AssertionError("synthetic source did not become ready")
        prepare = (ROOT / "deploy/recovery/runtime/prepare-roles.sql").read_bytes()
        pg([*psql, "-v", "expected_database=" + database], input=prepare)
        pg([*psql, "-c", "ALTER ROLE vellum_migrator PASSWORD '" + password + "'"])
        call([*docker, "run", "--rm", "--network=container:" + source, "--read-only", "--cap-drop=ALL",
              "--pull=never", "--log-driver=none",
              *("--env=" + key + "=" + value for key, value in db_env.items()),
              args.backend_image, "migrate"])
        insert = ("INSERT INTO public.users (id, first_name, last_name, email, password_hash) "
                  "VALUES (gen_random_uuid(), 'Recovery', 'Fixture', '%s', 'synthetic-only')")
        pg([*psql, "-c", insert % "before@example.invalid"])
        system_id, oid = pg([*psql, "-c", "SELECT system_identifier, oid FROM pg_control_system(), "
                            "pg_database WHERE datname = current_database()"]).decode().strip().split("|")
        expected_source = {"system_identifier": system_id, "database_oid": int(oid), "database_name": database}
        exporter = subprocess.Popen([*docker, "exec", "-i", source, *psql], stdin=subprocess.PIPE,
                                    stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        try:
            exporter.stdin.write(backup.SOURCE_QUERY)
            exporter.stdin.flush()
            snapshot = backup.snapshot_metadata(exporter.stdout.readline(), expected_source)
            pg([*psql, "-c", insert % "after@example.invalid"])
            archive = pg(["pg_dump", "--format=custom", "--snapshot=" + snapshot["snapshot_id"],
                          "-U", bootstrap_role, "-d", database])
        finally:
            exporter.stdin.close()
            exporter.wait(timeout=10)
        schema = pg(["pg_dump", "--schema-only", "--create", "--lock-wait-timeout=5s",
                     "--restrict-key=VellumReadinessSchemaFingerprintV1",
                     "-U", bootstrap_role, "-d", database])
        inspected = worker.strict_json(call([*docker, "image", "inspect", "--format", "{{json .}}",
                                            args.backend_image]))
        config = dict(inspected["Config"])
        labels = dict(config.get("Labels") or {})
        labels.pop("org.opencontainers.image.revision", None)
        config["Labels"] = labels
        profile = {
            "postgres_image": args.postgres_image,
            "postgres_image_id": call([*docker, "image", "inspect", "--format", "{{.Id}}",
                                       args.postgres_image]).decode().strip(),
            "postgres_version": pg([*psql, "-c", "SHOW server_version_num"]).decode().strip(),
            "postgres_role": bootstrap_role, "postgres_locale": "C",
            "source": expected_source, "backend_image": args.backend_image,
            "backend_runtime": {**{k: v for k, v in db_env.items() if k != "DB_PASSWORD"},
                                "DB_USER": "vellum_runtime", "PORT": "8080", "GIN_MODE": "release",
                                "JWT_EXPIRY": "15m", "JWT_REFRESH_EXPIRY": "24h"},
            "execution_sha256": worker.digest(worker.canonical({
                "architecture": inspected["Architecture"], "os": inspected["Os"],
                "rootfs": inspected["RootFS"], "config": config,
            })),
            "schema_sha256": worker.digest(schema),
        }

        class FixtureRunner(worker.Runner):
            checked_snapshot = False

            def call(self, args, label, **kwargs):
                if label == "restored database invariant check":
                    result = super().call(args + ["-c", "SELECT count(*) = 1 "
                                                  "AND bool_and(email = 'before@example.invalid') "
                                                  "FROM public.users"], "snapshot cut isolation check")
                    assert result.strip() == b"t", "post-snapshot insert appeared in restore"
                    self.checked_snapshot = True
                return super().call(args, label, **kwargs)

        with tempfile.TemporaryDirectory(prefix="vellum-recovery-test-") as scratch:
            path = Path(scratch) / "snapshot.dump"
            path.write_bytes(archive)
            cloud_work = Path(scratch) / "verified"
            cloud_work.mkdir()
            backup_projection, refs = publish_and_verify_fixture(
                path, snapshot, cloud_work, postgres_version=profile["postgres_version"])
            path = cloud_work / "archive.dump"
            profile["runtime_sha256"] = worker.digest(worker.canonical(profile["backend_runtime"]))
            runner = FixtureRunner(time.monotonic() + 300, scratch)
            assert worker.run_restore(runner, profile, path, job) == profile["schema_sha256"]
            assert runner.checked_snapshot, "snapshot isolation check did not run"
            contract = pg([*psql, "-c", "SELECT contract_sha256 FROM public.vellum_schema_migrations"]
                          ).decode().strip()
            candidate = {"image": inspected["Id"], "image_id": inspected["Id"],
                         "execution_sha256": profile["execution_sha256"], "migration_contract": contract}
            rehearsal = worker.run_restore(runner, profile, path, job, candidate)
            assert rehearsal["schema_before"] == rehearsal["schema_after"] == profile["schema_sha256"]
            assert rehearsal["data_before"] == rehearsal["data_after"]
            path.write_bytes(archive[:100])
            try:
                worker.run_restore(runner, profile, path, job)
            except worker.RecoveryError as error:
                assert str(error) == "transactional backup restore failed", str(error)
            else:
                raise AssertionError("truncated archive unexpectedly restored")
        if args.emit_synthetic_evidence:
            drill = worker.make_evidence("drill", profile, backup_projection, refs, started,
                                         profile["schema_sha256"])
            print(json.dumps({"synthetic": True, "drill_evidence": drill,
                              "rehearsal": rehearsal, "migration_contract": contract}))
        else:
            print("PASS: PostgreSQL 17 transactional restore, exported-snapshot isolation, schema and data checks, "
                  "migration-free baseline and candidate on read-only standby, truncated archive rejection, cleanup")
    finally:
        ids = call([*docker, "container", "ls", "--all", "--quiet", "--no-trunc",
                    "--filter", "label=" + label]).decode().split()
        assert len(ids) <= 1 and all(re.fullmatch(r"[a-f0-9]{64}", container) for container in ids)
        if ids:
            call([*docker, "rm", "-f", "--volumes", ids[0]])


if __name__ == "__main__":
    main()
