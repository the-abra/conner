package protocol

import (
	"bytes"
	"testing"
)

func TestHandshakeFrames(t *testing.T) {
	ke := &KeyExchange{X25519Pub: []byte{1, 2, 3}, Nonce: []byte{4}, PowDifficulty: 16, Version: "v"}
	b, err := Marshal(ke)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnmarshalKE(b)
	if err != nil || got.PowDifficulty != 16 {
		t.Fatalf("%v %+v", err, got)
	}

	var buf bytes.Buffer
	if err := SendHandshakeOK(&buf, 6667, true); err != nil {
		t.Fatal(err)
	}
	frame, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := UnmarshalOK(frame)
	if err != nil || ok.VaultPort != 6667 || !ok.Approved {
		t.Fatalf("%v %+v", err, ok)
	}

	buf.Reset()
	if err := SendHandshakeErr(&buf, "banned: x"); err != nil {
		t.Fatal(err)
	}
	frame, _ = ReadFrame(&buf)
	herr, err := UnmarshalErr(frame)
	if err != nil || herr.Reason != "banned: x" {
		t.Fatal(err)
	}
	if _, err := UnmarshalErr(b); err == nil {
		t.Fatal("ke should not parse as err")
	}
}
