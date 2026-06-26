import pytest

from keld.errors import KeldError
from keld.config.merge import (
    strip_keld_block, upsert_keld_block, has_keld_block, validate_toml,
    KELD_TOML_START, KELD_TOML_END,
)

BODY = '[otel]\nenvironment = "prod"\n'


def test_upsert_into_empty():
    out = upsert_keld_block("", BODY)
    assert KELD_TOML_START in out and KELD_TOML_END in out
    assert has_keld_block(out)
    validate_toml(out)


def test_upsert_preserves_user_content():
    user = '[user]\nkey = "val"\n'
    out = upsert_keld_block(user, BODY)
    assert '[user]' in out
    validate_toml(out)


def test_upsert_is_idempotent():
    once = upsert_keld_block("", BODY)
    twice = upsert_keld_block(once, BODY)
    assert once == twice


def test_strip_removes_block_only():
    user = '[user]\nkey = "val"\n'
    out = upsert_keld_block(user, BODY)
    stripped = strip_keld_block(out)
    assert not has_keld_block(stripped)
    assert '[user]' in stripped
    validate_toml(stripped)


def test_validate_toml_raises_on_duplicate_table():
    with pytest.raises(KeldError):
        validate_toml('[otel]\na=1\n[otel]\nb=2\n')


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
