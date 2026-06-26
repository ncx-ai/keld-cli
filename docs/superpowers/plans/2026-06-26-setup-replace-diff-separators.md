# `signal setup` replace + `--diff` + dividers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "replace the conflicting section" option to the conflict prompt, gate the full unified diff behind a `--diff` flag (summary-only by default; replace always shows its diff), and give each tool's block a titled divider.

**Architecture:** A new `strip_toml_table` helper lets the Codex adapter resolve a conflict by surgically removing the user's `[otel]` table and inserting Keld's block. The conflict prompt returns skip/replace/abort. `_run_setup` renders a per-tool divider, shows diffs only when `--diff` (except replace, always), and on "replace" recomputes the plan via `apply(..., replace=True)`.

**Tech Stack:** Python 3.12, Typer, rich (`console.rule`), stdlib. No new dependencies.

## Global Constraints

- keld-cli repo. Activate the dev venv: `. .venv/bin/activate`; run tests with `python -m pytest`. No `uv`. No new dependencies.
- Conflict prompt returns `"skip" | "replace" | "abort"`. `--yes` auto-skips conflicts (never replaces). `--dry-run` reports conflicts (no prompt).
- `--diff` (default off): additive changes show the unified diff only when set; a **replace** always shows its diff; the per-tool summary always prints; conflict reasons always print.
- Replace is surgical: remove only the conflicting top-level table (`[otel]` for Codex), preserve the rest; the existing `backup_config` still backs up the original before writing.
- Each task: failing test → run (RED) → implement → run (GREEN) → commit. Use the code verbatim.

---

### Task 1: `strip_toml_table` helper

**Files:**
- Modify: `src/keld/config/merge.py`
- Test: `tests/config/test_merge_toml.py`

**Interfaces:**
- Produces: `merge.strip_toml_table(text: str, table: str) -> str` — removes the top-level `[table]` header + body and any `[table.sub]` subtables from raw TOML, preserving all other content; no-op if absent.

- [ ] **Step 1: Add the failing test**

Append to `tests/config/test_merge_toml.py`:
```python
def test_strip_toml_table_removes_table_and_subtables():
    from keld.config.merge import strip_toml_table
    text = (
        '[user]\nkeep = 1\n\n'
        '[otel]\nenvironment = "dev"\n\n'
        '[otel.exporter]\nx = 2\n\n'
        '[other]\ny = 3\n'
    )
    out = strip_toml_table(text, "otel")
    assert "[otel]" not in out
    assert "[otel.exporter]" not in out
    assert 'environment = "dev"' not in out
    assert "[user]" in out and "keep = 1" in out
    assert "[other]" in out and "y = 3" in out


def test_strip_toml_table_absent_is_noop():
    from keld.config.merge import strip_toml_table
    text = '[user]\nx = 1\n'
    assert strip_toml_table(text, "otel") == text
```

- [ ] **Step 2: Run test to verify it fails**

Run: `python -m pytest tests/config/test_merge_toml.py -v`
Expected: FAIL — `ImportError: cannot import name 'strip_toml_table'`.

- [ ] **Step 3: Implement**

Add to `src/keld/config/merge.py` (near the other TOML helpers):
```python
def strip_toml_table(text: str, table: str) -> str:
    """Remove a top-level [table] (and its [table.sub] subtables) from raw TOML
    text, preserving all other content. No-op if the table is absent.

    Walks lines tracking the current top-level table (first dotted segment of
    the most recent [header]/[[header]]); drops lines while it equals `table`.
    """
    out: list[str] = []
    dropping = False
    for line in text.splitlines(keepends=True):
        stripped = line.strip()
        if stripped.startswith("["):
            header = stripped.strip("[]").strip()
            top = header.split(".", 1)[0].strip().strip('"').strip("'")
            dropping = top == table
        if not dropping:
            out.append(line)
    return "".join(out)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `python -m pytest tests/config/test_merge_toml.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add src/keld/config/merge.py tests/config/test_merge_toml.py
git commit -m "feat: strip_toml_table helper"
```

---

### Task 2: `replace` kwarg on adapters + Codex replace path

**Files:**
- Modify: `src/keld/tools/base.py`, `src/keld/tools/claude.py`, `src/keld/tools/gemini.py`, `src/keld/tools/codex.py`
- Test: `tests/tools/test_codex.py`, `tests/tools/test_claude.py`, `tests/tools/test_gemini.py`

**Interfaces:**
- Consumes: `merge.strip_toml_table` (Task 1).
- Produces: `ToolAdapter.apply(self, current_text, params, *, replace: bool = False) -> Plan` (protocol + all three adapters). Codex: when `replace=True` and the normal merge would be invalid TOML, it strips the user's `[otel]` table, re-upserts, and returns a non-conflict plan (`summary` notes the replace); if even that is invalid it returns a conflict plan. Claude/Gemini accept `replace` and ignore it.

- [ ] **Step 1: Add the failing tests**

Append to `tests/tools/test_codex.py`:
```python
def test_apply_replace_resolves_conflict_and_preserves_other():
    import tomllib
    cfg = '[user]\nfoo = "bar"\n\n[otel]\nenvironment = "dev"\n'
    plan = CodexAdapter().apply(cfg, P, replace=True)
    assert plan.conflict is None
    assert plan.changed is True
    assert "# >>> keld" in plan.after_text          # Keld block inserted
    assert 'environment = "dev"' not in plan.after_text  # old [otel] removed
    assert "[user]" in plan.after_text and 'foo = "bar"' in plan.after_text  # preserved
    tomllib.loads(plan.after_text)                  # valid TOML (single [otel])


def test_apply_replace_without_conflict_is_normal():
    plan = CodexAdapter().apply(None, P, replace=True)
    assert plan.conflict is None and plan.changed is True
```

Append to `tests/tools/test_claude.py`:
```python
def test_apply_accepts_replace_kwarg_noop():
    a = ClaudeAdapter().apply(None, P)
    b = ClaudeAdapter().apply(None, P, replace=True)
    assert a.after_text == b.after_text
```

Append to `tests/tools/test_gemini.py`:
```python
def test_apply_accepts_replace_kwarg_noop():
    a = GeminiAdapter().apply(None, P)
    b = GeminiAdapter().apply(None, P, replace=True)
    assert a.after_text == b.after_text
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/tools/test_codex.py tests/tools/test_claude.py tests/tools/test_gemini.py -v`
Expected: FAIL — `apply()` got an unexpected keyword argument `replace`.

- [ ] **Step 3: Implement**

In `src/keld/tools/base.py`, update the protocol method signature:
```python
    def apply(self, current_text: str | None, params: SetupParams, *, replace: bool = False) -> Plan: ...
```

In `src/keld/tools/claude.py` and `src/keld/tools/gemini.py`, change each `apply` signature to accept and ignore `replace`:
```python
    def apply(self, current_text: str | None, params: SetupParams, *, replace: bool = False) -> Plan:
```
(The body is unchanged — `replace` is unused; JSON adapters don't hard-conflict.)

In `src/keld/tools/codex.py`, add the import and rewrite `apply`:
```python
from ..config.merge import (
    has_keld_block, strip_keld_block, strip_toml_table, upsert_keld_block, validate_toml,
)
```
```python
    def apply(self, current_text: str | None, params: SetupParams, *, replace: bool = False) -> Plan:
        body = t.codex_block_body(params, t.hook_command(str(hook_path())))
        after = upsert_keld_block(current_text, body)
        try:
            validate_toml(after)
        except KeldError as exc:
            if replace:
                stripped = strip_toml_table(current_text or "", "otel")
                after = upsert_keld_block(stripped, body)
                try:
                    validate_toml(after)
                except KeldError as exc2:
                    reason = (f"Keld couldn't replace the conflicting section in "
                              f"~/.codex/config.toml: {exc2}")
                    return Plan(
                        name=self.name, config_path=self.config_path(),
                        after_text=current_text or "", managed={}, summary=[],
                        changed=False, conflict=reason,
                    )
                return Plan(
                    name=self.name, config_path=self.config_path(), after_text=after,
                    managed={"block": True, "created": current_text is None},
                    summary=["replace your existing [otel] with Keld's [otel] + hooks block"],
                    changed=after != (current_text or ""),
                )
            reason = (f"your ~/.codex/config.toml can't be safely modified by Keld "
                      f"(it already defines conflicting settings, e.g. an [otel] table): {exc}")
            return Plan(
                name=self.name, config_path=self.config_path(),
                after_text=current_text or "", managed={}, summary=[],
                changed=False, conflict=reason,
            )
        return Plan(
            name=self.name, config_path=self.config_path(), after_text=after,
            managed={"block": True, "created": current_text is None},
            summary=["add [otel] + SessionStart/PreToolUse hooks block"],
            changed=after != (current_text or ""),
        )
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/tools/ -v`
Expected: PASS (new tests + existing adapter tests, including the existing codex conflict test which still gets a conflict plan when `replace` is not passed).

- [ ] **Step 5: Commit**

```bash
git add src/keld/tools/ tests/tools/
git commit -m "feat: replace kwarg on adapters; Codex surgical replace path"
```

---

### Task 3: `_run_setup` — dividers, `--diff`, three-way conflict + replace

**Files:**
- Modify: `src/keld/commands/setup.py`
- Test: `tests/commands/test_setup.py`

**Interfaces:**
- Consumes: `diffview.render`, `Plan`, `backup_config`, `write_atomic`, adapters' `apply(..., replace=True)` (Task 2), `console` (with `.rule`).
- Produces: `_default_resolve_conflict(adapter, plan) -> str` (`"skip"|"replace"|"abort"`). `_run_setup(..., show_diff: bool = False, resolve_conflict=None)`. `setup` CLI gains `--diff`.

- [ ] **Step 1: Replace the setup tests (RED)**

Replace the body of `tests/commands/test_setup.py` with:
```python
import json

import httpx
import pytest

from keld.api.client import AtlasClient, Onboarding
from keld.commands.setup import _run_setup
from keld.config.manifest import Manifest
from keld.paths import backups_dir, manifest_path
from keld.tools.base import SetupParams
from keld.tools.claude import ClaudeAdapter
from keld.tools.codex import CodexAdapter


def _client():
    return AtlasClient("https://atlas.keld.co",
                       transport=httpx.MockTransport(lambda r: httpx.Response(200, content=b"# hook\n")))


PARAMS = SetupParams(endpoint="https://ingest.keld.co", ingest_token="tok", actor="dg@keld.co")
OB = Onboarding(endpoint="https://ingest.keld.co", ingest_token="tok", actor="dg@keld.co")


def test_clean_tool_applies_and_backs_up(keld_home, monkeypatch, tmp_path):
    cfg = tmp_path / ".claude" / "settings.json"
    cfg.parent.mkdir(parents=True)
    cfg.write_text(json.dumps({"model": "opus"}) + "\n")
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: cfg)
    manifest = _run_setup([ClaudeAdapter()], PARAMS, _client(), OB, dry_run=False, yes=True)
    obj = json.loads(cfg.read_text())
    assert obj["env"]["OTEL_EXPORTER_OTLP_ENDPOINT"] == "https://ingest.keld.co"
    assert obj["model"] == "opus"
    bak = backups_dir() / "claude_code" / "settings.json"
    assert json.loads(bak.read_text()) == {"model": "opus"}
    assert manifest.tools["claude_code"].backup_path == str(bak)


def test_conflict_skip_applies_others(keld_home, monkeypatch, tmp_path):
    claude_cfg = tmp_path / ".claude" / "settings.json"
    codex_cfg = tmp_path / ".codex" / "config.toml"
    codex_cfg.parent.mkdir(parents=True)
    codex_cfg.write_text('[otel]\nenvironment = "dev"\n')
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: claude_cfg)
    monkeypatch.setattr(CodexAdapter, "config_path", lambda self: codex_cfg)
    manifest = _run_setup([ClaudeAdapter(), CodexAdapter()], PARAMS, _client(), OB,
                          dry_run=False, yes=False, confirm=lambda msg: True,
                          resolve_conflict=lambda adapter, plan: "skip")
    assert "claude_code" in manifest.tools and "codex" not in manifest.tools
    assert codex_cfg.read_text() == '[otel]\nenvironment = "dev"\n'


def test_conflict_replace_applies_and_preserves(keld_home, monkeypatch, tmp_path):
    codex_cfg = tmp_path / ".codex" / "config.toml"
    codex_cfg.parent.mkdir(parents=True)
    codex_cfg.write_text('[user]\nfoo = "bar"\n\n[otel]\nenvironment = "dev"\n')
    monkeypatch.setattr(CodexAdapter, "config_path", lambda self: codex_cfg)
    manifest = _run_setup([CodexAdapter()], PARAMS, _client(), OB,
                          dry_run=False, yes=False, confirm=lambda msg: True,
                          resolve_conflict=lambda adapter, plan: "replace")
    assert "codex" in manifest.tools
    text = codex_cfg.read_text()
    assert "# >>> keld" in text
    assert 'environment = "dev"' not in text       # old [otel] replaced
    assert 'foo = "bar"' in text                    # other config preserved
    bak = backups_dir() / "codex" / "config.toml"
    assert 'environment = "dev"' in bak.read_text()  # original backed up


def test_conflict_abort_writes_nothing(keld_home, monkeypatch, tmp_path):
    claude_cfg = tmp_path / ".claude" / "settings.json"
    codex_cfg = tmp_path / ".codex" / "config.toml"
    codex_cfg.parent.mkdir(parents=True)
    codex_cfg.write_text('[otel]\nx = 1\n')
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: claude_cfg)
    monkeypatch.setattr(CodexAdapter, "config_path", lambda self: codex_cfg)
    import typer
    with pytest.raises(typer.Exit) as exc_info:
        _run_setup([ClaudeAdapter(), CodexAdapter()], PARAMS, _client(), OB,
                   dry_run=False, yes=False, confirm=lambda msg: True,
                   resolve_conflict=lambda adapter, plan: "abort")
    assert exc_info.value.exit_code == 1
    assert not claude_cfg.exists()
    assert Manifest.load().tools == {}


def test_dry_run_writes_nothing(keld_home, monkeypatch, tmp_path):
    cfg = tmp_path / ".claude" / "settings.json"
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: cfg)
    _run_setup([ClaudeAdapter()], PARAMS, _client(), OB, dry_run=True, yes=True)
    assert not cfg.exists()
    assert not manifest_path().exists()


def test_decline_final_confirm_writes_nothing(keld_home, monkeypatch, tmp_path):
    cfg = tmp_path / ".claude" / "settings.json"
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: cfg)
    manifest = _run_setup([ClaudeAdapter()], PARAMS, _client(), OB,
                          dry_run=False, yes=False, confirm=lambda msg: False)
    assert not cfg.exists()
    assert manifest.tools == {}


def test_yes_auto_skips_conflict(keld_home, monkeypatch, tmp_path):
    claude_cfg = tmp_path / ".claude" / "settings.json"
    codex_cfg = tmp_path / ".codex" / "config.toml"
    codex_cfg.parent.mkdir(parents=True)
    codex_cfg.write_text('[otel]\nx = 1\n')
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: claude_cfg)
    monkeypatch.setattr(CodexAdapter, "config_path", lambda self: codex_cfg)
    manifest = _run_setup([ClaudeAdapter(), CodexAdapter()], PARAMS, _client(), OB,
                          dry_run=False, yes=True)
    assert "claude_code" in manifest.tools and "codex" not in manifest.tools


def test_diff_hidden_by_default_shown_with_flag(keld_home, monkeypatch, tmp_path, capsys):
    cfg = tmp_path / ".claude" / "settings.json"
    monkeypatch.setattr(ClaudeAdapter, "config_path", lambda self: cfg)
    _run_setup([ClaudeAdapter()], PARAMS, _client(), OB, dry_run=True, yes=True, show_diff=False)
    assert "@@" not in capsys.readouterr().out
    _run_setup([ClaudeAdapter()], PARAMS, _client(), OB, dry_run=True, yes=True, show_diff=True)
    assert "@@" in capsys.readouterr().out


def test_replace_always_shows_diff(keld_home, monkeypatch, tmp_path, capsys):
    codex_cfg = tmp_path / ".codex" / "config.toml"
    codex_cfg.parent.mkdir(parents=True)
    codex_cfg.write_text('[otel]\nx = 1\n')
    monkeypatch.setattr(CodexAdapter, "config_path", lambda self: codex_cfg)
    _run_setup([CodexAdapter()], PARAMS, _client(), OB, dry_run=False, yes=False,
               confirm=lambda msg: True, resolve_conflict=lambda adapter, plan: "replace",
               show_diff=False)
    assert "@@" in capsys.readouterr().out
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/commands/test_setup.py -v`
Expected: FAIL — `_run_setup` has no `show_diff` param; `resolve_conflict` returns a string now.

- [ ] **Step 3: Implement**

In `src/keld/commands/setup.py`, replace `_default_resolve_conflict` and `_run_setup` (leave the rest of imports; `setup` updated below):
```python
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
```

Then update the `setup` CLI function to add `--diff` and pass it. Add the option and the `show_diff=diff` argument:
```python
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/commands/test_setup.py -v`
Expected: PASS (10 tests).

- [ ] **Step 5: Run the full suite (no regressions)**

Run: `python -m pytest -q`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add src/keld/commands/setup.py tests/commands/test_setup.py
git commit -m "feat: setup replace option, --diff flag, per-tool dividers"
```

---

### Task 4: README + full green

**Files:**
- Modify: `README.md`
- Test: whole suite.

**Interfaces:** none new.

- [ ] **Step 1: Update the README**

In `README.md`, replace the existing interactive-setup paragraph (the one describing diffs/conflict/backups) with:
```markdown
`setup` is interactive. By default it prints a concise summary of the changes to
each config file; pass `--diff` to see the full unified diff. If a tool's config
already has settings Keld can't safely merge (e.g. Codex with its own `[otel]`
section), setup explains the conflict and lets you **[s]kip** that tool,
**[r]eplace** just the conflicting section with Keld's (the rest of your config
is preserved, and the diff is always shown for a replace), or **[a]bort**. Every
file Keld modifies is first copied to `~/.keld/backups/<tool>/`. Use `--dry-run`
to preview without writing and `--yes` to skip prompts (conflicts are
auto-skipped in that mode).
```

- [ ] **Step 2: Run the full suite + smoke**

Run: `python -m pytest -q`
Expected: PASS.
Then: `keld signal setup --help` shows `--diff` (and the other flags) with no traceback.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document setup --diff and replace option"
```

---

## Self-Review Notes (for the implementer)

- **Spec coverage:** §2 `--diff`/`show_diff` (T3); §3 dividers via `console.rule` (T3); §4 three-way `resolve_conflict` skip/replace/abort + `_default_resolve_conflict` prompt (T3); §5 `strip_toml_table` (T1) + Codex replace path + `replace` kwarg across adapters (T2); replace-always-diff (T3 conflict branch renders unconditionally); §6 flag interactions (T3 tests: dry-run/--yes/decline); §7 tests (each task); §8 out-of-scope respected (no `--replace` flag; JSON unchanged).
- **Type consistency:** `strip_toml_table(text, table) -> str` (T1) used by Codex (T2); `apply(..., *, replace=False)` consistent across base/claude/gemini/codex (T2) and called as `apply(before, params, replace=True)` in T3; `resolve_conflict(adapter, plan) -> str` returning "skip"/"replace"/"abort" consistent between `_default_resolve_conflict`, `_run_setup`, and the test lambdas; `_run_setup(..., show_diff=False)` matches the `setup` call `show_diff=diff`.
- **No placeholders:** every step has complete code.
