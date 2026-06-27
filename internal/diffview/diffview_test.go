package diffview

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/ncx-ai/keld-cli/internal/console"
)

func TestRenderShowsAddedLine(t *testing.T) {
	var buf bytes.Buffer
	orig := console.Out
	t.Cleanup(func() { console.Out = orig })
	console.Out = &buf
	color.NoColor = true
	before := "a\n"
	Render(&before, "a\nb\n", "f")
	if !strings.Contains(buf.String(), "+b") {
		t.Fatalf("diff missing added line:\n%s", buf.String())
	}
}

func TestRenderShowsRemovedLine(t *testing.T) {
	var buf bytes.Buffer
	orig := console.Out
	t.Cleanup(func() { console.Out = orig })
	console.Out = &buf
	color.NoColor = true
	before := "a\nb\n"
	Render(&before, "a\n", "f")
	if !strings.Contains(buf.String(), "-b") {
		t.Fatalf("diff missing removed line:\n%s", buf.String())
	}
}

func TestRenderShowsHunkHeader(t *testing.T) {
	var buf bytes.Buffer
	orig := console.Out
	t.Cleanup(func() { console.Out = orig })
	console.Out = &buf
	color.NoColor = true
	before := "a\n"
	Render(&before, "a\nb\n", "f")
	if !strings.Contains(buf.String(), "@@") {
		t.Fatalf("diff missing @@ hunk header:\n%s", buf.String())
	}
}

func TestRenderNilBeforeTreatedAsEmpty(t *testing.T) {
	var buf bytes.Buffer
	orig := console.Out
	t.Cleanup(func() { console.Out = orig })
	console.Out = &buf
	color.NoColor = true
	Render(nil, "hello\n", "cfg")
	if !strings.Contains(buf.String(), "+hello") {
		t.Fatalf("diff missing added line for nil before:\n%s", buf.String())
	}
}
