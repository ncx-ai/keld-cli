from keld.config.writer import write_atomic, delete_if_empty


def test_write_creates_file_and_dirs(tmp_path):
    target = tmp_path / "sub" / "settings.json"
    write_atomic(target, '{"a": 1}\n', backup=False)
    assert target.read_text() == '{"a": 1}\n'


def test_backup_made_once(tmp_path):
    target = tmp_path / "settings.json"
    target.write_text("original\n")
    write_atomic(target, "v2\n", backup=True)
    bak = tmp_path / "settings.json.keld.bak"
    assert bak.read_text() == "original\n"
    write_atomic(target, "v3\n", backup=True)
    assert bak.read_text() == "original\n"  # not overwritten


def test_delete_if_empty(tmp_path):
    target = tmp_path / "settings.json"
    target.write_text("{}\n")
    assert delete_if_empty(target, "{}\n") is True
    assert not target.exists()
    other = tmp_path / "x.json"
    other.write_text('{"a":1}\n')
    assert delete_if_empty(other, '{"a": 1}\n') is False


def test_backup_config_central_one_time(keld_home, tmp_path):
    from keld.config.writer import backup_config
    from keld.paths import backups_dir

    cfg = tmp_path / ".claude" / "settings.json"
    cfg.parent.mkdir(parents=True)
    cfg.write_text('{"a": 1}\n')

    dest = backup_config(cfg, "claude_code")
    assert dest == backups_dir() / "claude_code" / "settings.json"
    assert dest.read_text() == '{"a": 1}\n'

    # one-time: a second call does not clobber and returns None
    cfg.write_text('{"a": 2}\n')
    assert backup_config(cfg, "claude_code") is None
    assert dest.read_text() == '{"a": 1}\n'


def test_backup_config_missing_source_returns_none(keld_home, tmp_path):
    from keld.config.writer import backup_config
    assert backup_config(tmp_path / "nope.json", "codex") is None
