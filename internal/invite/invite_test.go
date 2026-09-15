package invite

import "testing"

func TestTableDecode(t *testing.T) {
	in := Blob{Onion: "abc.onion", Port: "6666", Tor: true, Room: "ops", FP: "deadbeef", PT: "system-tor"}
	s := Encode(in)
	out, err := Decode(s)
	if err != nil {
		t.Fatal(err)
	}
	if out.Onion != in.Onion || out.Room != "ops" || out.FP != "deadbeef" || out.PT != "system-tor" || !out.Tor {
		t.Fatalf("%+v", out)
	}
	if Dial := out.DialAddr(); Dial != "abc.onion:6666" {
		t.Fatal(Dial)
	}

	cases := []struct {
		in   string
		tor  bool
		addr string
		fail bool
	}{
		{"10.0.0.5:6666", false, "10.0.0.5:6666", false},
		{"xyz.onion:9051", true, "xyz.onion:9051", false},
		{"xyz.onion", true, "xyz.onion:6666", false},
		{"not-an-invite", false, "", true},
		{"", false, "", true},
	}
	for _, c := range cases {
		b, err := Decode(c.in)
		if c.fail {
			if err == nil {
				t.Errorf("%q expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if b.Tor != c.tor || b.DialAddr() != c.addr {
			t.Errorf("%q -> tor=%v addr=%s", c.in, b.Tor, b.DialAddr())
		}
	}
}
