from __future__ import annotations

import os
from pathlib import Path

DEFAULT_API_URL = "https://atlas.keld.co"

# Process-level override for the Keld API base URL, set by the `--api-url` flag
# (e.g. `keld login --api-url http://localhost:8000` for local dev). Takes
# precedence over the KELD_API_URL env var and the default.
_api_base_override: str | None = None


def set_api_base_override(url: str | None) -> None:
    """Set (or clear) the API base URL override. A trailing slash is stripped."""
    global _api_base_override
    _api_base_override = url.rstrip("/") if url else None


def api_base_override() -> str | None:
    """The active `--api-url` override, or None."""
    return _api_base_override


def keld_home() -> Path:
    env = os.environ.get("KELD_HOME")
    return Path(env) if env else Path.home() / ".keld"


def auth_path() -> Path:
    return keld_home() / "auth.json"


def manifest_path() -> Path:
    return keld_home() / "manifest.json"


def hook_path() -> Path:
    return keld_home() / "keld-context.py"


def backups_dir() -> Path:
    return keld_home() / "backups"


def api_base() -> str:
    if _api_base_override is not None:
        return _api_base_override
    return os.environ.get("KELD_API_URL", DEFAULT_API_URL).rstrip("/")
