package commands

import (
	"path/filepath"
	"testing"

	cursorstate "github.com/cotiq/sigvane-cli/internal/state"
)

func TestStateResetRemovesOnlySelectedSlug(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state", "state.json")

	if err := cursorstate.Save(statePath, cursorstate.File{
		"old-slug": {
			InboxID:    "00000000-0000-7000-8000-000000000001",
			LastItemID: "00000000-0000-7000-8000-000000000101",
		},
		"kept-slug": {
			InboxID:    "00000000-0000-7000-8000-000000000002",
			LastItemID: "00000000-0000-7000-8000-000000000202",
		},
	}); err != nil {
		t.Fatalf("state.Save returned error: %v", err)
	}

	stdout, stderr, err := executeCommand("state", "reset", "old-slug", "--state", statePath)
	if err != nil {
		t.Fatalf("state reset returned error: %v", err)
	}
	if stdout != "state reset: old-slug\n" {
		t.Fatalf("stdout = %q, want reset confirmation", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty output", stderr)
	}

	currentState, err := cursorstate.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if _, exists := currentState["old-slug"]; exists {
		t.Fatal("old-slug state should be removed")
	}
	if entry := currentState["kept-slug"]; entry.LastItemID != "00000000-0000-7000-8000-000000000202" {
		t.Fatalf("kept-slug entry = %#v, want unchanged", entry)
	}
}

func TestStateResetSucceedsWhenSlugIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "state", "state.json")

	stdout, stderr, err := executeCommand("state", "reset", "missing-slug", "--state", statePath)
	if err != nil {
		t.Fatalf("state reset returned error: %v", err)
	}
	if stdout != "state reset: missing-slug\n" {
		t.Fatalf("stdout = %q, want reset confirmation", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty output", stderr)
	}

	currentState, err := cursorstate.Load(statePath)
	if err != nil {
		t.Fatalf("state.Load returned error: %v", err)
	}
	if len(currentState) != 0 {
		t.Fatalf("state length = %d, want empty state", len(currentState))
	}
}
