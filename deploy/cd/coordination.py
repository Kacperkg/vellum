"""Host-wide advisory coordination for root-installed Vellum operations.

Keep backup.lock's inode/name for compatibility. Never unlink a flock file.
A durable pending record deliberately survives *any* incomplete operation: Docker
and database work can outlive the client and its kernel locks.
"""
from contextlib import contextmanager, ExitStack
from datetime import datetime, timezone
import fcntl
import json
import os
import stat

LOCKS = ('backup.lock', 'staging.lock', 'production.lock')
PENDING = 'operation-pending.json'


class CoordinationError(ValueError):
    """Fixed, non-sensitive messages safe for the backup journal/SNS path."""


def require(condition, message):
    if not condition:
        raise CoordinationError(message)


def root_owned(info):
    return info.st_uid == 0


def validate_state(state):
    require(state.is_absolute() and '..' not in state.parts, 'unsafe operation state path')
    for path in (*reversed(state.parents), state):
        info = path.lstat()
        require(stat.S_ISDIR(info.st_mode) and root_owned(info)
                and not info.st_mode & (0o077 if path == state else 0o022),
                'unsafe operation state directory')


@contextmanager
def lock(path):
    # NONBLOCK also prevents a malicious FIFO from hanging open before fstat.
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and root_owned(info)
                and info.st_nlink == 1 and not info.st_mode & 0o022,
                'unsafe operation lock file')
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise CoordinationError('host operation lock busy; backup/deployment/maintenance not started') from None
        yield
    finally:
        os.close(fd)


def sync_state(state):
    fd = os.open(state, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


@contextmanager
def host_locks(state):
    """One order, nonblocking, reverse release, including older env-only users."""
    validate_state(state)
    with ExitStack() as stack:
        for name in LOCKS:
            stack.enter_context(lock(state / name))
        yield


@contextmanager
def operation(state, kind, environment):
    require(kind in ('backup', 'deployment', 'maintenance'), 'unknown host operation')
    require(environment in ('staging', 'production'), 'unknown operation environment')
    with host_locks(state):
        pending = state / PENDING
        # O_EXCL rejects any existing entry, including dangling symlinks. No PID,
        # timestamp, reboot or unlocked flock is sufficient evidence to recover it.
        try:
            fd = os.open(pending, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        except FileExistsError:
            raise CoordinationError('unfinished host operation; operator recovery required') from None
        with os.fdopen(fd, 'w', encoding='utf-8') as output:
            json.dump({'format_version': 1, 'kind': kind, 'environment': environment,
                       'pid': os.getpid(), 'started_at': datetime.now(timezone.utc).isoformat()}, output)
            output.flush()
            os.fsync(output.fileno())
        sync_state(state)
        # Exceptions (including SIGTERM handling/SystemExit) intentionally retain
        # the record. Only a completely successful caller removes it, under locks.
        yield
        pending.unlink()
        sync_state(state)
