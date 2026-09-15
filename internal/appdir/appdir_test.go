package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCONNERHomeOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CONNER_HOME", dir)
	if Root() != dir {
		t.Fatalf("root=%s", Root())
	}
	if err := Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "identity")); err != nil {
		t.Fatal(err)
	}
	if IdentityKey("bob") != filepath.Join(dir, "identity", "identity_bob.key") {
		t.Fatal(IdentityKey("bob"))
	}
	if IdentityKeyWrap("bob") != filepath.Join(dir, "identity", "identity_bob.key.wrap") {
		t.Fatal(IdentityKeyWrap("bob"))
	}
	if _, err := os.Stat(filepath.Join(dir, "queue")); err != nil {
		t.Fatal(err)
	}
}
