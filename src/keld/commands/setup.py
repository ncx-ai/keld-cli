from __future__ import annotations

import typer


def setup(
    tool: str = typer.Option("", "--tool", help="Comma-separated tools to target."),
    dry_run: bool = typer.Option(False, "--dry-run", help="Show changes without writing."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
    no_login: bool = typer.Option(False, "--no-login", help="Fail instead of opening a browser."),
) -> None:
    """Configure detected tools for Keld telemetry."""
    typer.echo("setup: not implemented")
