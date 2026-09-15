package vaultui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAndFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("hello-vault"), 0o600); err != nil {
		t.Fatal(err)
	}
	ents := List(dir)
	if len(ents) != 1 || ents[0].Name != "note.txt" || ents[0].Size != 11 {
		t.Fatalf("%+v", ents)
	}
	if FormatSize(500) != "500 B" {
		t.Fatal(FormatSize(500))
	}
	if TotalBytes(ents) != 11 {
		t.Fatal(TotalBytes(ents))
	}
	line := Line(ents[0], true, 80)
	if line[0] != '>' {
		t.Fatal(line)
	}
}
