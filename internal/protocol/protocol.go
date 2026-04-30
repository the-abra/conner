package protocol

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type ChatMessage struct {
	Type      string `json:"type"`
	Content   string `json:"content"`
	Sender    string `json:"sender"`
	Timestamp string `json:"timestamp"`
	IsAdmin   bool   `json:"is_admin,omitempty"` // sender was an admin at send time
	Signature string `json:"signature,omitempty"`
}

func CreateMessage(msgType, content, sender string) ChatMessage {
	return ChatMessage{
		Type:      msgType,
		Content:   content,
		Sender:    sender,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	}
}

func (c *ChatMessage) ToJSON() (string, error) {
	b, err := json.Marshal(c)
	return string(b), err
}

func FromJSON(data string) (ChatMessage, error) {
	var msg ChatMessage
	err := json.Unmarshal([]byte(data), &msg)
	return msg, err
}

func SendFrame(conn io.Writer, payload []byte) error {
	n := len(payload)
	if n > 50*1024*1024 {
		return fmt.Errorf("payload too large: %d bytes (max 50 MB)", n)
	}
	// Convert to uint (same bit-width as int, no sign bit) then encode
	// as big-endian 4 bytes. After each right-shift the value fits in one
	// byte by construction, so the byte() truncations cannot overflow.
	un := uint(n)
	header := [4]byte{
		byte(un >> 24), // #nosec G115 — value ≤ 0xFF after 24-bit shift
		byte(un >> 16), // #nosec G115 — value ≤ 0xFF after 16-bit shift
		byte(un >> 8),  // #nosec G115 — value ≤ 0xFF after 8-bit shift
		byte(un & 0xFF),
	}
	if _, err := conn.Write(header[:]); err != nil {
		return err
	}
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func ReadFrame(conn io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	// Decode big-endian uint32 without a int→uint32 cast (no binary package needed)
	length := int(header[0])<<24 | int(header[1])<<16 | int(header[2])<<8 | int(header[3])

	if length > 50*1024*1024 {
		return nil, fmt.Errorf("frame too large: %d bytes (max 50 MB)", length)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
