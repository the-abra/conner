package protocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello-frame")
	if err := SendFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("%q", got)
	}
}

func TestFrameRejectsHuge(t *testing.T) {
	err := SendFrame(&bytes.Buffer{}, make([]byte, MaxFrame+1))
	if err == nil {
		t.Fatal("expected too large")
	}
}

func TestReadFrameRejectsHugeHeader(t *testing.T) {
	// 4-byte length = MaxFrame+1
	h := []byte{0, 0x80, 0x00, 0x01} // 8MiB+1
	_, err := ReadFrame(bytes.NewReader(h))
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestEmptyFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := SendFrame(&buf, nil); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %d", err, len(got))
	}
}

func TestEncodeDecodeMessage(t *testing.T) {
	m := CreateMessage("CHAT", "hi", "alice")
	if m.MessageId == "" || m.Timestamp == "" {
		t.Fatal("missing id/ts")
	}
	b, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.Sender != "alice" || out.Content != "hi" {
		t.Fatalf("%+v", out)
	}
}

func TestReadFrameTruncatedHeader(t *testing.T) {
	_, err := ReadFrame(strings.NewReader("xx"))
	if err == nil {
		t.Fatal("expected error")
	}
}
