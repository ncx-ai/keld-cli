from typer.testing import CliRunner

from keld.auth.store import AuthData, save_auth
from keld.cli import app

runner = CliRunner()


def test_status_shows_auth_and_tools(keld_home):
    save_auth(AuthData(access_token="t", principal="dg@keld.co", org="Keld",
                       api_url="https://atlas.keld.co"))
    result = runner.invoke(app, ["status"])
    assert result.exit_code == 0
    assert "dg@keld.co" in result.output
    for name in ["Claude Code", "Codex", "Gemini CLI"]:
        assert name in result.output


def test_status_not_logged_in(keld_home):
    result = runner.invoke(app, ["status"])
    assert result.exit_code == 0
    assert "not logged in" in result.output.lower()


def test_doctor_clean_when_nothing_configured(keld_home):
    result = runner.invoke(app, ["doctor"])
    # nothing configured, nothing broken → exit 0
    assert result.exit_code == 0
