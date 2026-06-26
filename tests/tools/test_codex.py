import tomllib

from keld.tools.base import SetupParams
from keld.tools.codex import CodexAdapter

P = SetupParams(endpoint="https://ingest.keld.co", ingest_token="tok", actor="dg@keld.co")


def test_apply_to_empty():
    plan = CodexAdapter().apply(None, P)
    parsed = tomllib.loads(plan.after_text)
    assert parsed["otel"]["environment"] == "prod"
    assert "keld-context.py" in plan.after_text
    assert plan.managed == {"block": True, "created": True}


def test_round_trip_preserves_user_toml():
    user = '[user]\nfoo = "bar"\n'
    applied = CodexAdapter().apply(user, P)
    assert '[user]' in applied.after_text
    removed = CodexAdapter().remove(applied.after_text, applied.managed)
    assert tomllib.loads(removed.after_text) == {"user": {"foo": "bar"}}


def test_apply_idempotent():
    first = CodexAdapter().apply(None, P)
    second = CodexAdapter().apply(first.after_text, P)
    assert first.after_text == second.after_text


def test_apply_conflict_returns_conflict_plan_not_raises():
    plan = CodexAdapter().apply('[otel]\nenvironment = "dev"\n', P)
    assert plan.conflict is not None
    assert "otel" in plan.conflict.lower()
    assert plan.changed is False


def test_status():
    plan = CodexAdapter().apply(None, P)
    assert CodexAdapter().status(plan.after_text, plan.managed).configured is True
    assert CodexAdapter().status("[user]\nx=1\n", None).configured is False


def test_apply_replace_resolves_conflict_and_preserves_other():
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
