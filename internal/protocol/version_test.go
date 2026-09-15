package protocol

import "testing"

func TestCompatibleVersion(t *testing.T) {
	cases := []struct {
		a, b string
		ok   bool
	}{
		{"v3.0-ops", "v3.1-dev", true},
		{"v3", "v3.0-ops", true},
		{"v3nightly", "v3.0-ops", true},
		{"v2.9", "v3.0-ops", false},
		{"", "v3.0-ops", false},
		{"v3.0-ops", "", false},
		{"nope", "v3.0-ops", false},
	}
	for _, c := range cases {
		if CompatibleVersion(c.a, c.b) != c.ok {
			t.Errorf("CompatibleVersion(%q,%q) want %v", c.a, c.b, c.ok)
		}
	}
}

func TestMajorPrefix(t *testing.T) {
	if major("v3.0-ops") != "v3" {
		t.Fatal(major("v3.0-ops"))
	}
}
