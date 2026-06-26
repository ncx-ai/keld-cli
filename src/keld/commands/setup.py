from __future__ import annotations

import typer

from .. import diffview
from ..api.client import AtlasClient, Onboarding
from ..auth.device_flow import require_auth
from ..config.manifest import Manifest, ToolManifest
from ..config.writer import backup_config, write_atomic
from ..console import console
from ..errors import KeldError
from ..hook import install_hook
from ..paths import api_base_override, set_api_base_override
from ..tools.base import Plan, SetupParams
from ..tools.registry import select_adapters


def _default_resolve_conflict(adapter, plan) -> str:
    """Prompt for a conflicted tool. Returns "skip", "replace", or "abort"."""
    choice = typer.prompt(
        f"{adapter.display_name}: [s]kip this tool, [r]eplace the conflicting "
        f"section, or [a]bort everything?",
        default="s",
    ).strip().lower()[:1]
    return {"s": "skip", "r": "replace", "a": "abort"}.get(choice, "skip")


def _run_setup(adapters, params: SetupParams, client: AtlasClient, ob: Onboarding,
               *, dry_run: bool, yes: bool, show_diff: bool = False,
               confirm=typer.confirm, resolve_conflict=None) -> Manifest:
    if resolve_conflict is None:
        resolve_conflict = _default_resolve_conflict

    approved = []  # list[tuple[adapter, Plan]]
    for adapter in adapters:
        path = adapter.config_path()
        before = path.read_text() if path.exists() else None
        console.rule(f"[bold]{adapter.display_name}[/] · {path}")
        try:
            plan = adapter.apply(before, params)
        except KeldError as exc:
            plan = Plan(name=adapter.name, config_path=path, after_text=before or "",
                        managed={}, summary=[], changed=False, conflict=str(exc))

        if plan.conflict:
            console.print(f"  [yellow]conflict:[/] {plan.conflict}")
            if dry_run:
                console.print("  [dim](dry-run: would be skipped)[/]")
                continue
            if yes:
                console.print("  [yellow]skipped[/] (--yes)")
                continue
            choice = resolve_conflict(adapter, plan)
            if choice == "abort":
                console.print("Aborted.")
                raise typer.Exit(code=1)
            if choice == "replace":
                plan = adapter.apply(before, params, replace=True)
                if plan.conflict:
                    console.print(f"  [yellow]can't replace:[/] {plan.conflict}")
                    console.print("  [yellow]skipped[/]")
                    continue
                diffview.render(before, plan.after_text, plan.config_path)  # replace: always
                for line in plan.summary:
                    console.print(f"  [dim]{line}[/]")
                approved.append((adapter, plan))
                continue
            console.print("  [yellow]skipped[/]")
            continue

        if not plan.changed:
            console.print("  already configured — no changes")
            continue

        if show_diff:
            diffview.render(before, plan.after_text, plan.config_path)
        for line in plan.summary:
            console.print(f"  [dim]{line}[/]")
        approved.append((adapter, plan))

    console.print("\n[bold]Hook[/] · keld-context.py → ~/.keld")

    if dry_run:
        console.print("\n[dim]--dry-run: no changes written.[/]")
        return Manifest.load()
    if not approved:
        console.print("\nNothing to apply.")
        return Manifest.load()
    if not yes and not confirm(f"Apply {len(approved)} change(s)?"):
        console.print("Aborted.")
        return Manifest.load()

    manifest = Manifest(endpoint=ob.endpoint, actor=ob.actor)
    manifest.hook = install_hook(client, ob)
    for adapter, plan in approved:
        backup = backup_config(plan.config_path, adapter.name)
        if backup:
            console.print(f"  [dim]backed up {plan.config_path} → {backup}[/]")
        write_atomic(plan.config_path, plan.after_text, backup=False)
        manifest.tools[adapter.name] = ToolManifest(
            name=adapter.name, config_path=str(plan.config_path),
            managed=plan.managed, backup_path=str(backup) if backup else None)
        console.print(f"  [green]✓[/] {adapter.display_name}")

    manifest.save()
    console.print("\nSetup complete. Restart any running sessions to pick up the new config.")
    return manifest


def setup(
    tool: str = typer.Option("", "--tool", help="Comma-separated tools to target."),
    dry_run: bool = typer.Option(False, "--dry-run", help="Show changes without writing."),
    diff: bool = typer.Option(False, "--diff", help="Show full unified diffs (default: summary only)."),
    yes: bool = typer.Option(False, "--yes", "-y", help="Skip confirmation."),
    no_login: bool = typer.Option(False, "--no-login", help="Fail instead of opening a browser."),
    api_url: str = typer.Option(None, "--api-url", metavar="URL",
                                help="Target a different Keld API base URL "
                                     "(e.g. http://localhost:8000) for local dev."),
) -> None:
    """Configure detected tools for Keld telemetry."""
    if api_url:
        set_api_base_override(api_url)
    auth = require_auth(no_login=no_login)
    client = AtlasClient(api_base_override() or auth.api_url, token=auth.access_token)
    ob = client.onboarding()
    names = [t.strip() for t in tool.split(",") if t.strip()] or None
    adapters = select_adapters(names)
    if not adapters:
        console.print("No supported tools detected. Use --tool to target one explicitly.")
        raise typer.Exit(code=0)
    params = SetupParams(endpoint=ob.endpoint, ingest_token=ob.ingest_token, actor=ob.actor)
    _run_setup(adapters, params, client, ob, dry_run=dry_run, yes=yes, show_diff=diff)
