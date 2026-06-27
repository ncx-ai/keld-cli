package cli

import (
	"bytes"
	"testing"

	"github.com/ncx-ai/keld-cli/internal/config"
	"github.com/ncx-ai/keld-cli/internal/console"
	"github.com/ncx-ai/keld-cli/internal/tools"
)

func TestDoctorReportsDrift(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())

	// Build a manifest that records a tool entry but whose config file doesn't
	// exist (simulating drift: manifest says configured, reality says otherwise).
	manifest := &config.Manifest{
		Tools: map[string]config.ToolManifest{
			"claude_code": {
				Name:       "claude_code",
				ConfigPath: "/nonexistent/path/settings.json",
				Managed:    map[string]any{},
			},
		},
	}
	if err := manifest.Save(); err != nil {
		t.Fatalf("saving manifest: %v", err)
	}

	// Capture console output.
	var buf bytes.Buffer
	orig := console.Out
	console.Out = &buf
	defer func() { console.Out = orig }()

	// The real ClaudeAdapter.Status will return not-installed/not-configured
	// when the config file is absent, which satisfies the drift condition.
	adapter, err := tools.Get("claude_code")
	if err != nil {
		t.Fatalf("get adapter: %v", err)
	}
	st := adapter.Status(nil, map[string]any{})
	if st.Configured {
		t.Skip("ClaudeAdapter reports configured with nil config — skip drift test")
	}

	cmd := newDoctorCmd()
	err = cmd.RunE(cmd, nil)
	if err == nil {
		t.Error("doctor should return an error when problems are found")
	}

	out := buf.String()
	if out == "" {
		t.Error("doctor should print problem output")
	}
	// The output should contain a drift message for Claude Code.
	if !bytes.Contains([]byte(out), []byte("claude")) && !bytes.Contains([]byte(out), []byte("Claude")) {
		t.Errorf("expected drift message mentioning Claude Code; got: %s", out)
	}
}
