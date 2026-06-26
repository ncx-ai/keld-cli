import pytest
from typer.testing import CliRunner
from keld.cli import app


def test_help_lists_top_level_commands():
    result = CliRunner().invoke(app, ["--help"])
    assert result.exit_code == 0
    # Auth commands are top-level; Atlas onboarding lives under the `atlas` group.
    for cmd in ["login", "logout", "whoami", "atlas"]:
        assert cmd in result.output


def test_atlas_group_lists_subcommands():
    result = CliRunner().invoke(app, ["atlas", "--help"])
    assert result.exit_code == 0
    for cmd in ["setup", "status", "doctor", "uninstall"]:
        assert cmd in result.output


def test_main_handles_keld_error(monkeypatch, capsys):
    import keld.cli as cli
    from keld.errors import KeldError

    def boom():
        raise KeldError("boom message")

    monkeypatch.setattr(cli, "app", boom)
    with pytest.raises(SystemExit) as exc:
        cli.main()
    assert exc.value.code == 1
    assert "boom message" in capsys.readouterr().err
