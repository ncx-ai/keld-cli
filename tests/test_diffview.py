from keld import diffview


def test_diff_lines_shows_added_line():
    lines = diffview.diff_lines('{\n}\n', '{\n  "x": 1\n}\n', "settings.json")
    body = "".join(lines)
    assert '+  "x": 1' in body
    assert any(l.startswith("@@") for l in lines)


def test_diff_lines_new_file_diffs_against_empty():
    lines = diffview.diff_lines(None, "hello\n", "f")
    body = "".join(lines)
    assert "+hello" in body


def test_render_smoke(capsys):
    diffview.render(None, "hello\n", "f")  # must not raise, prints something
    assert "hello" in capsys.readouterr().out
