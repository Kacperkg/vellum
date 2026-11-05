#!/usr/bin/env python3
"""Root-only UNIX relay to the IAM-authenticated immutable issuer Lambda."""

import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import tempfile

STATE = Path("/var/lib/vellum-cd")
CONFIG = Path("/etc/vellum-cd/recovery-relay.json")
LIMIT = 32768


def protected(path):
    info = path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0
            or info.st_nlink != 1 or info.st_mode & 0o077):
        raise ValueError("unsafe relay credential or configuration")


def read_request(connection):
    data = bytearray()
    connection.settimeout(5)
    while len(data) <= LIMIT:
        chunk = connection.recv(4096)
        if not chunk:
            break
        data.extend(chunk)
    if not 0 < len(data) <= LIMIT:
        raise ValueError("invalid relay request size")
    value = json.loads(data)
    if set(value) != {"method", "request"} or value["method"] not in ("admit", "sign"):
        raise ValueError("invalid relay request")
    return bytes(data)


def invoke(payload, config):
    with tempfile.TemporaryDirectory(dir=STATE, prefix=".relay-") as temp:
        base = Path(temp)
        (base / "request.json").write_bytes(payload)
        result = subprocess.run(
            ["/usr/local/bin/aws", "lambda", "invoke", "--function-name", config["function"],
             "--region", "eu-west-2", "--cli-binary-format", "raw-in-base64-out",
             "--payload", f"fileb://{base / 'request.json'}", "--cli-connect-timeout", "5",
             "--cli-read-timeout", "22", str(base / "response.json")],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=28,
            env={"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/root",
                 "AWS_CONFIG_FILE": config["aws_config"], "AWS_PROFILE": "vellum-recovery-relay",
                 "AWS_SHARED_CREDENTIALS_FILE": "/dev/null", "AWS_EC2_METADATA_DISABLED": "true",
                 "AWS_MAX_ATTEMPTS": "1"},
        )
        if result.returncode:
            raise ValueError("issuer invocation failed")
        with (base / "response.json").open("rb") as result_file:
            data = result_file.read(LIMIT + 1)
        if not 0 < len(data) <= LIMIT:
            raise ValueError("invalid issuer response size")
        return data


def main():
    protected(CONFIG)
    info = STATE.lstat()
    if os.geteuid() != 0 or not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
        raise ValueError("unsafe relay state directory")
    config = json.loads(CONFIG.read_bytes())
    if set(config) != {"function", "aws_config"} or not re.fullmatch(
        r"arn:aws:lambda:eu-west-2:[0-9]{12}:function:vellum-production-host:[1-9][0-9]*", config["function"]
    ) or config["aws_config"] != "/etc/vellum-cd/recovery-relay-aws.conf":
        raise ValueError("invalid private issuer configuration")
    protected(Path(config["aws_config"]))
    # A root-owned systemd socket is passed as FD 3. The dedicated AWS profile
    # uses a protected Roles Anywhere certificate, never the application role.
    with socket.socket(fileno=3) as listener:
        while True:
            connection, _ = listener.accept()
            with connection:
                try:
                    connection.sendall(invoke(read_request(connection), config))
                except (OSError, ValueError, subprocess.SubprocessError, TypeError):
                    try:
                        connection.sendall(b'{"error":"private recovery issuer unavailable"}')
                    except OSError:
                        pass


if __name__ == "__main__":
    main()
