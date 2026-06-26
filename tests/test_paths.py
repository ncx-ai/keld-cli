from keld import paths


def test_keld_home_honors_env(keld_home):
    assert paths.keld_home() == keld_home
    assert paths.auth_path().name == "auth.json"
    assert paths.manifest_path().name == "manifest.json"
    assert paths.hook_path().name == "keld-context.py"


def test_api_base_default(monkeypatch):
    monkeypatch.delenv("KELD_API_URL", raising=False)
    assert paths.api_base() == "https://atlas.keld.co"


def test_api_base_env_override(monkeypatch):
    monkeypatch.setenv("KELD_API_URL", "http://localhost:8000/")
    assert paths.api_base() == "http://localhost:8000"


def test_api_base_flag_override_wins_and_strips_slash(monkeypatch):
    # The --api-url override beats the env var, and a trailing slash is stripped.
    monkeypatch.setenv("KELD_API_URL", "https://atlas.keld.co")
    paths.set_api_base_override("http://localhost:9001/")
    assert paths.api_base() == "http://localhost:9001"
    assert paths.api_base_override() == "http://localhost:9001"


def test_clearing_override_falls_back(monkeypatch):
    monkeypatch.setenv("KELD_API_URL", "http://localhost:8000")
    paths.set_api_base_override("http://localhost:9001")
    paths.set_api_base_override(None)
    assert paths.api_base_override() is None
    assert paths.api_base() == "http://localhost:8000"
