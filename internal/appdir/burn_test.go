package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBurnRemovesHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONNER_HOME", dir)
	if err := Ensure(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "identity", "x")
	if err := os.WriteFile(marker, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Burn(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("home still exists: %v", err)
	}
}
