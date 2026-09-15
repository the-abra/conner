package crypto

import (
	"bytes"
	"testing"
)

func TestFileContentKeyStable(t *testing.T) {
	a := FileContentKey("tok", "a.bin")
	b := FileContentKey("tok", "a.bin")
	if !bytes.Equal(a, b) || len(a) != 32 {
		t.Fatal("stable 32-byte key")
	}
	c := FileContentKey("tok", "b.bin")
	if bytes.Equal(a, c) {
		t.Fatal("file id must bind")
	}
	d := FileContentKey("other", "a.bin")
	if bytes.Equal(a, d) {
		t.Fatal("token must bind")
	}
}

func TestEncryptEmptyStream(t *testing.T) {
	key := GenerateRandomKey()
	var buf bytes.Buffer
	if err := EncryptReaderTo(&buf, bytes.NewReader(nil), key, "x"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := DecryptReaderTo(&out, &buf, key, "x"); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal(out.Len())
	}
}
