package rooms

import "testing"

func TestEnsureAndEpoch(t *testing.T) {
	r := NewRegistry()
	if r.Get(Default) == nil || r.Get("nope") != nil {
		t.Fatal("get")
	}
	ops := r.Ensure("ops")
	if ops.Epoch != 1 {
		t.Fatal(ops.Epoch)
	}
	if r.BumpEpoch("ops") != 2 {
		t.Fatal("bump")
	}
	if r.Ensure("ops").Epoch != 2 {
		t.Fatal("ensure must not reset")
	}
	if r.Ensure("").ID != Default {
		t.Fatal("empty")
	}
	if n := len(r.List()); n < 2 {
		t.Fatalf("list %d", n)
	}
}
