package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSidecarBinPathEnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "custom-sidecar")
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KELD_SIDECAR_BIN", p)
	got, ok := sidecarBinPath()
	if !ok || got != p {
		t.Fatalf("env override: got %q,%v want %q,true", got, ok, p)
	}
}

func TestSidecarBinPathEnvMissingFileIgnored(t *testing.T) {
	t.Setenv("KELD_SIDECAR_BIN", filepath.Join(t.TempDir(), "nope"))
	// No sibling binary in the test's exec dir, so expect not-found.
	if _, ok := sidecarBinPath(); ok {
		t.Fatal("nonexistent env path should not resolve")
	}
}

func TestSidecarBinPathBesideExecutable(t *testing.T) {
	os.Unsetenv("KELD_SIDECAR_BIN")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "keld-agent-sidecar"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sib := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(sib); err == nil {
		t.Skip("a real sidecar sits beside the test binary; skip synthetic check")
	}
	// Create it beside the test executable, assert resolution, then clean up.
	if err := os.WriteFile(sib, []byte("x"), 0o755); err != nil {
		t.Skipf("cannot write beside test exe (%v); environment-limited", err)
	}
	defer os.Remove(sib)
	got, ok := sidecarBinPath()
	if !ok || got != sib {
		t.Fatalf("beside-exe: got %q,%v want %q,true", got, ok, sib)
	}
}
