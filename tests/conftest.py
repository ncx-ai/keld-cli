import pytest

from keld import paths


@pytest.fixture(autouse=True)
def _reset_api_base_override():
    """The --api-url override is a process-global; reset it around every test
    so in-process CliRunner invocations don't leak it to later tests."""
    paths.set_api_base_override(None)
    yield
    paths.set_api_base_override(None)


@pytest.fixture
def keld_home(tmp_path, monkeypatch):
    home = tmp_path / "keld_home"
    monkeypatch.setenv("KELD_HOME", str(home))
    return home
