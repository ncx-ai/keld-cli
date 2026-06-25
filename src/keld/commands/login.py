from __future__ import annotations

import typer


def login(no_login: bool = typer.Option(False, "--no-login", help="Fail instead of opening a browser.")) -> None:
    """Authenticate to Keld."""
    typer.echo("login: not implemented")


def logout() -> None:
    """Remove stored credentials."""
    typer.echo("logout: not implemented")


def whoami() -> None:
    """Show the logged-in principal."""
    typer.echo("whoami: not implemented")
