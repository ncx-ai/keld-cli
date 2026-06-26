from __future__ import annotations

from pathlib import Path

from .. import telemetry as t
from ..config.merge import (
    has_keld_block, strip_keld_block, strip_toml_table, upsert_keld_block, validate_toml,
)
from ..errors import KeldError
from ..paths import hook_path
from .base import Plan, SetupParams, ToolStatus


class CodexAdapter:
    name = "codex"
    display_name = "Codex"

    def config_path(self) -> Path:
        return Path.home() / ".codex" / "config.toml"

    def detect(self) -> bool:
        return self.config_path().parent.exists()

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

    def remove(self, current_text: str | None, managed: dict) -> Plan:
        after = strip_keld_block(current_text)
        return Plan(
            name=self.name, config_path=self.config_path(), after_text=after,
            managed=managed, summary=["remove Keld block"],
            changed=after != (current_text or ""),
        )

    def status(self, current_text: str | None, managed: dict | None) -> ToolStatus:
        configured = has_keld_block(current_text)
        return ToolStatus(
            name=self.name, installed=self.detect(), configured=configured,
            detail="configured" if configured else "not configured",
        )
