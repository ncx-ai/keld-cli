package provision

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

type fakeFetcher struct{ content []byte }

func (f fakeFetcher) Fetch(_ context.Context, dest string) error {
	return os.WriteFile(filepath.Join(dest, "model.safetensors"), f.content, 0o644)
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestEnsureModelFetchesVerifiesInstalls(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	content := []byte("weights")
	if err := EnsureModel(t.Context(), dir, sha(content), fakeFetcher{content}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "model.safetensors")); err != nil {
		t.Fatal("model not installed")
	}
}

func TestEnsureModelShaMismatchDoesNotInstall(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	err := EnsureModel(t.Context(), dir, sha([]byte("expected")), fakeFetcher{[]byte("actual")})
	if err == nil {
		t.Fatal("want sha-mismatch error")
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Fatal("nothing should be installed on mismatch")
	}
}

func TestEnsureModelSkipsWhenPresentValid(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gliner2")
	content := []byte("weights")
	if err := EnsureModel(t.Context(), dir, sha(content), fakeFetcher{content}); err != nil {
		t.Fatal(err)
	}
	// second call with a fetcher that would error must NOT be invoked
	if err := EnsureModel(t.Context(), dir, sha(content), errFetcher{}); err != nil {
		t.Fatalf("present+valid should skip fetch: %v", err)
	}
}

type errFetcher struct{}

func (errFetcher) Fetch(context.Context, string) error { return os.ErrPermission }
