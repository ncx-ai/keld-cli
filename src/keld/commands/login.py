from __future__ import annotations

import typer

from ..auth.device_flow import require_auth
from ..auth.store import clear_auth, load_auth
from ..config.manifest import Manifest
from ..console import console, fail
from ..paths import set_api_base_override


def login(
    no_login: bool = typer.Option(False, "--no-login",
                                  help="Fail instead of opening a browser."),
    api_url: str = typer.Option(None, "--api-url", metavar="URL",
                                help="Target a different Keld API base URL "
                                     "(e.g. http://localhost:8000) for local dev."),
) -> None:
    """Authenticate to Keld."""
    if api_url:
        set_api_base_override(api_url)
    auth = require_auth(no_login=no_login)
    console.print(f"Logged in as [bold]{auth.principal}[/] (org: {auth.org})")


def logout() -> None:
    """Remove stored credentials."""
    if clear_auth():
        console.print("Logged out.")
    else:
        console.print("Not logged in.")


def whoami() -> None:
    """Show the logged-in principal."""
    auth = load_auth()
    if auth is None:
        fail("not logged in (run `keld login`)")
    endpoint = Manifest.load().endpoint
    suffix = f" · endpoint {endpoint}" if endpoint else ""
    console.print(f"[bold]{auth.principal}[/] · org {auth.org} · {auth.api_url}{suffix}")
