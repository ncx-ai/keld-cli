// internal/config/writer_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicAndDeleteIfEmpty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	if err := WriteAtomic(p, "{\n  \"a\": 1\n}\n", false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("content %q", b)
	}
	deleted, err := DeleteIfEmpty(p, "{}")
	if err != nil || !deleted {
		t.Fatalf("expected delete, got %v %v", deleted, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
}

func TestWriteAtomicBackupOneTime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := WriteAtomic(p, "original", false); err != nil {
		t.Fatal(err)
	}

	// First write with backup=true on the existing file: .keld.bak captures original.
	if err := WriteAtomic(p, "newer", true); err != nil {
		t.Fatal(err)
	}
	bak := p + ".keld.bak"
	b, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("expected .keld.bak: %v", err)
	}
	if string(b) != "original" {
		t.Fatalf("backup content %q, want %q", b, "original")
	}

	// Second write with backup=true: .keld.bak must be unchanged (one-time guard).
	if err := WriteAtomic(p, "newest", true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(bak)
	if string(b) != "original" {
		t.Fatalf("backup changed to %q, want %q (one-time)", b, "original")
	}
}

func TestBackupConfigPreservesMode(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(src, []byte("orig"), 0o600); err != nil {
		t.Fatal(err)
	}
	// WriteFile is subject to umask; force the source mode explicitly.
	if err := os.Chmod(src, 0o600); err != nil {
		t.Fatal(err)
	}

	dest, err := BackupConfig(src, "claude_code")
	if err != nil || dest == "" {
		t.Fatalf("backup failed: %v %v", dest, err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("backup mode %o, want 0600", got)
	}
}

func TestBackupConfigOneTime(t *testing.T) {
	t.Setenv("KELD_HOME", t.TempDir())
	dir := t.TempDir()
	src := filepath.Join(dir, "settings.json")
	os.WriteFile(src, []byte("orig"), 0o644)
	first, err := BackupConfig(src, "claude_code")
	if err != nil || first == "" {
		t.Fatalf("first backup failed: %v %v", first, err)
	}
	second, _ := BackupConfig(src, "claude_code")
	if second != "" {
		t.Fatal("second backup should be no-op")
	}
}
