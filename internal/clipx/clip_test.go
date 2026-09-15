package clipx

import "testing"

func TestSanitize(t *testing.T) {
	if Sanitize("a\x00b") != "ab" {
		t.Fatal("nul")
	}
	r := make([]rune, MaxRunes+3)
	for i := range r {
		r[i] = 'x'
	}
	if len([]rune(Sanitize(string(r)))) != MaxRunes {
		t.Fatal("cap")
	}
}
