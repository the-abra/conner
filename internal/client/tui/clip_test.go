package tui

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m"
	if stripANSI(in) != "red" {
		t.Fatalf("%q", stripANSI(in))
	}
}

func TestLastVisibleLine(t *testing.T) {
	got := lastVisibleLine("  alice:\nhello world  \n")
	if got != "hello world" {
		t.Fatal(got)
	}
}

func TestShortHost(t *testing.T) {
	if shortHost("127.0.0.1:6666") != "127.0.0.1" {
		t.Fatal(shortHost("127.0.0.1:6666"))
	}
	h := shortHost("abcdefghijklmnopqrstuvwxyz.onion:6666")
	if !strings.Contains(h, "…onion") {
		t.Fatal(h)
	}
}

func TestFormatRoomTab(t *testing.T) {
	if formatRoomTab("ops", true, 3) != "[#ops (3)]" {
		t.Fatal(formatRoomTab("ops", true, 3))
	}
	if formatRoomTab("ops", false, 0) != "#ops" {
		t.Fatal(formatRoomTab("ops", false, 0))
	}
}

func TestRoomCycle(t *testing.T) {
	rooms := []string{"general", "ops", "intel"}
	if nextRoom(rooms, "general") != "ops" {
		t.Fatal("next")
	}
	if prevRoom(rooms, "general") != "intel" {
		t.Fatal("prev wrap")
	}
	if nextRoom(rooms, "intel") != "general" {
		t.Fatal("next wrap")
	}
}
