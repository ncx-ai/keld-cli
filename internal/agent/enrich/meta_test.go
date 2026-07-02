package enrich

import "testing"

func TestPreamble(t *testing.T) {
	got := Meta{Repo: "keld/atlas", Tool: "Claude Code"}.Preamble()
	want := "[Context — repository: keld/atlas; tool: Claude Code]\nTask: "
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if empty := (Meta{}).Preamble(); empty != "[Context — repository: none]\nTask: " {
		t.Fatalf("empty repo should say none, got %q", empty)
	}
}
