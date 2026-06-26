from __future__ import annotations

import difflib
from pathlib import Path

from .console import console


def diff_lines(before: str | None, after: str, label: str) -> list[str]:
    return list(difflib.unified_diff(
        (before or "").splitlines(keepends=True),
        after.splitlines(keepends=True),
        fromfile=f"a/{label}", tofile=f"b/{label}",
    ))


def render(before: str | None, after: str, label: str | Path) -> None:
    for raw in diff_lines(before, after, str(label)):
        line = raw.rstrip("\n")
        if line.startswith("+") and not line.startswith("+++"):
            style = "green"
        elif line.startswith("-") and not line.startswith("---"):
            style = "red"
        elif line.startswith("@@"):
            style = "cyan"
        else:
            style = "dim"
        # markup=False: config lines contain brackets ([otel], JSON) that rich
        # would otherwise try to parse as markup.
        console.print(line, style=style, markup=False)
