package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
)

func TestRuleContainsTitle(t *testing.T) {
	var buf bytes.Buffer
	Out = &buf
	color.NoColor = true
	Rule("Claude Code · /x")
	if !strings.Contains(buf.String(), "Claude Code · /x") {
		t.Fatalf("rule missing title: %q", buf.String())
	}
}
