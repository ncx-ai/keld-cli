package config

import "testing"

func TestManifestRoundTrip(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	ep := "https://atlas.keld.co"
	m := &Manifest{Endpoint: &ep, Tools: map[string]ToolManifest{}}
	m.Tools["claude_code"] = ToolManifest{
		Name: "claude_code", ConfigPath: "/x/settings.json",
		Managed: map[string]any{"created": true},
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if got.Endpoint == nil || *got.Endpoint != ep {
		t.Fatalf("endpoint lost: %v", got.Endpoint)
	}
	if _, ok := got.Tools["claude_code"]; !ok {
		t.Fatal("tool lost")
	}
}

func TestLoadManifestMissingReturnsEmpty(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	m, err := LoadManifest()
	if err != nil || len(m.Tools) != 0 {
		t.Fatalf("expected empty manifest, got %v %v", m, err)
	}
}
