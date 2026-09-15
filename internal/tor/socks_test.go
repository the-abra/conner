package tor

import "testing"

func TestNormalizeSocks(t *testing.T) {
	if normalizeSocks("127.0.0.1:19050") != "127.0.0.1:19050" {
		t.Fatal(normalizeSocks("127.0.0.1:19050"))
	}
	if normalizeSocks(":9050") != "127.0.0.1:9050" {
		t.Fatal(normalizeSocks(":9050"))
	}
	if normalizeSocks("9050") != "127.0.0.1:9050" {
		t.Fatal(normalizeSocks("9050"))
	}
}
