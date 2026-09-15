package filesync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherSeesNewFile(t *testing.T) {
	dir := t.TempDir()
	w := New(dir, 50*time.Millisecond)
	got := make(chan string, 4)
	go w.Events(func(name string, info os.FileInfo) {
		select {
		case got <- filepath.Base(name):
		default:
		}
	})
	defer w.Stop()

	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case name := <-got:
		if name != "hello.txt" {
			t.Fatalf("got %s", name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for watch event")
	}
}
