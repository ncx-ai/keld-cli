from __future__ import annotations

import typer


def uninstall(
    tool: str = typer.Option("", "--tool", help="Comma-separated tools to target."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
) -> None:
    """Remove Keld telemetry config and hook."""
    typer.echo("uninstall: not implemented")
