package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptReaderRoundTrip(t *testing.T) {
	key := GenerateRandomKey()
	src := bytes.Repeat([]byte("conner-file-payload-"), 4000) // > one chunk
	var buf bytes.Buffer
	if err := EncryptReaderTo(&buf, bytes.NewReader(src), key, "fid"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := DecryptReaderTo(&out, &buf, key, "fid"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), src) {
		t.Fatalf("len %d vs %d", out.Len(), len(src))
	}
	buf.Reset()
	_ = EncryptReaderTo(&buf, bytes.NewReader(src), key, "fid")
	if err := DecryptReaderTo(ioDiscard{}, &buf, key, "other"); err == nil {
		t.Fatal("expected aad fail")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
