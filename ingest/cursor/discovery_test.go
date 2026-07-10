package cursor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceFolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(path, []byte(`{"folder":"file:///Users/dev/myrepo"}`), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := workspaceFolder(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/Users/dev/myrepo" {
		t.Errorf("workspaceFolder = %q, want /Users/dev/myrepo", got)
	}
}

func TestWorkspaceFolderMissing(t *testing.T) {
	if _, err := workspaceFolder(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing workspace.json")
	}
}
