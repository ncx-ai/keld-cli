from typer.testing import CliRunner
from keld.cli import app


def test_help_lists_subcommands():
    result = CliRunner().invoke(app, ["--help"])
    assert result.exit_code == 0
    for cmd in ["login", "logout", "whoami", "setup", "status", "doctor", "uninstall"]:
        assert cmd in result.output
