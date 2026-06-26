from __future__ import annotations

import typer

from .commands import login as login_cmd
from .commands import setup as setup_cmd
from .commands import status as status_cmd
from .commands import uninstall as uninstall_cmd
from .console import console_err
from .errors import KeldError

app = typer.Typer(no_args_is_help=True, add_completion=False, help="Keld CLI")

# Top-level auth commands — shared across all product groups.
app.command()(login_cmd.login)
app.command()(login_cmd.logout)
app.command()(login_cmd.whoami)

# `keld atlas <cmd>` — Keld Atlas telemetry onboarding for local tools.
atlas_app = typer.Typer(
    no_args_is_help=True,
    help="Set up Keld Atlas telemetry for your local AI coding tools.",
)
atlas_app.command()(setup_cmd.setup)
atlas_app.command()(status_cmd.status)
atlas_app.command("doctor")(status_cmd.doctor)
atlas_app.command()(uninstall_cmd.uninstall)
app.add_typer(atlas_app, name="atlas")


def main() -> None:
    try:
        app()
    except KeldError as exc:
        console_err.print(f"[bold red]Error:[/] {exc}")
        raise SystemExit(1)
