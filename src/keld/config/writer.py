from __future__ import annotations

import os
import shutil
import tempfile
from pathlib import Path

from ..paths import backups_dir


def write_atomic(path: Path, text: str, *, backup: bool) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if backup and path.exists():
        bak = path.with_name(path.name + ".keld.bak")
        if not bak.exists():
            shutil.copy2(path, bak)
    fd, tmp = tempfile.mkstemp(dir=path.parent, prefix=".keld-", suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as fh:
            fh.write(text)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)


def delete_if_empty(path: Path, text: str) -> bool:
    if text.strip() in ("", "{}"):
        if path.exists():
            path.unlink()
        return True
    return False


def backup_config(path: Path, tool_name: str) -> Path | None:
    """Copy `path` into ~/.keld/backups/<tool_name>/ before Keld modifies it.

    One-time: if a backup already exists there it is preserved (keeps the
    pristine pre-Keld copy across re-runs). Returns the backup path, or None
    if the source doesn't exist or a backup already exists.
    """
    if not path.exists():
        return None
    dest = backups_dir() / tool_name / path.name
    if dest.exists():
        return None
    dest.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, dest)
    return dest
