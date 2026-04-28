package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePathUsesOverrideAndXDGFallback(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "custom-state.json")

	path, err := ResolvePath(overridePath)
	if err != nil {
		t.Fatalf("ResolvePath with override returned error: %v", err)
	}
	if path != overridePath {
		t.Fatalf("override path = %q, want %q", path, overridePath)
	}

	t.Setenv("XDG_STATE_HOME", filepath.Join(tempDir, "xdg-state"))
	path, err = ResolvePath("")
	if err != nil {
		t.Fatalf("ResolvePath with XDG_STATE_HOME returned error: %v", err)
	}
	if path != filepath.Join(tempDir, "xdg-state", "sigvane", "state.json") {
		t.Fatalf("xdg state path = %q", path)
	}
}

func TestSaveCreatesParentDirectoryAndRoundTripsState(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "nested", "sigvane", "state.json")
	expected := File{
		"github-repo": {
			InboxID:    "00000000-0000-7000-8000-000000000001",
			LastItemID: "00000000-0000-7000-8000-0000000000f3",
		},
	}

	if err := Save(statePath, expected); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Dir(statePath)); err != nil {
		t.Fatalf("state directory was not created: %v", err)
	}

	actual, err := Load(statePath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	entry, ok := actual["github-repo"]
	if !ok {
		t.Fatal("missing github-repo entry after round trip")
	}
	if entry != expected["github-repo"] {
		t.Fatalf("state entry = %#v, want %#v", entry, expected["github-repo"])
	}
}

func TestLoadReturnsEmptyStateWhenFileDoesNotExist(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "missing.json")

	state, err := Load(statePath)
	if err != nil {
		t.Fatalf("Load returned error for missing file: %v", err)
	}
	if len(state) != 0 {
		t.Fatalf("state length = %d, want 0", len(state))
	}
}
