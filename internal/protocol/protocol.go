package protocol

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"conner/internal/config"

	"google.golang.org/protobuf/proto"
)

const MaxFrame = 8 * 1024 * 1024

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func CreateMessage(msgType, content, sender string) *ChatMessage {
	return &ChatMessage{
		Type:      msgType,
		Content:   content,
		Sender:    sender,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		MessageId: generateID(),
	}
}

func (c *ChatMessage) Encode() ([]byte, error) {
	return proto.Marshal(c)
}

func Decode(data []byte) (*ChatMessage, error) {
	msg := &ChatMessage{}
	err := proto.Unmarshal(data, msg)
	return msg, err
}

func Marshal(m proto.Message) ([]byte, error) {
	return proto.Marshal(m)
}

func UnmarshalKE(data []byte) (*KeyExchange, error) {
	m := &KeyExchange{}
	return m, proto.Unmarshal(data, m)
}

func UnmarshalHello(data []byte) (*ClientHello, error) {
	m := &ClientHello{}
	return m, proto.Unmarshal(data, m)
}

func UnmarshalOK(data []byte) (*HandshakeOk, error) {
	if len(data) < 2 || data[0] != hsOK {
		return nil, fmt.Errorf("not HandshakeOk")
	}
	m := &HandshakeOk{}
	return m, proto.Unmarshal(data[1:], m)
}

const (
	hsOK  byte = 1
	hsErr byte = 2
)

func UnmarshalErr(data []byte) (*HandshakeErr, error) {
	if len(data) < 2 || data[0] != hsErr {
		return nil, fmt.Errorf("not an error frame")
	}
	m := &HandshakeErr{}
	if err := proto.Unmarshal(data[1:], m); err != nil {
		return nil, err
	}
	if m.Reason == "" {
		return nil, fmt.Errorf("not an error frame")
	}
	return m, nil
}

func SendHandshakeErr(conn io.Writer, reason string) error {
	b, err := proto.Marshal(&HandshakeErr{Reason: reason})
	if err != nil {
		return err
	}
	return SendFrame(conn, append([]byte{hsErr}, b...))
}

func CompatibleVersion(clientVer, serverVer string) bool {
	if clientVer == "" || serverVer == "" {
		return false
	}
	return major(clientVer) == major(serverVer)
}

func major(v string) string {
	n := 0
	for n < len(v) && (v[n] == 'v' || v[n] == 'V' || (v[n] >= '0' && v[n] <= '9')) {
		n++
	}
	if n == 0 {
		return v
	}
	return v[:n]
}

func SendHandshakeOK(conn io.Writer, vaultPort int, approved bool) error {
	b, err := proto.Marshal(&HandshakeOk{VaultPort: int32(vaultPort), Version: config.Version, Approved: approved})
	if err != nil {
		return err
	}
	return SendFrame(conn, append([]byte{hsOK}, b...))
}

func LooksLikeTextHandshake(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	return b[0] >= 0x20 && b[0] < 0x7f
}

func SendFrame(conn io.Writer, payload []byte) error {
	n := len(payload)
	if n > MaxFrame {
		return fmt.Errorf("payload too large: %d bytes (max %d)", n, MaxFrame)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(n))
	if _, err := conn.Write(header[:]); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

func ReadFrame(conn io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > MaxFrame {
		return nil, fmt.Errorf("frame too large: %d bytes (max %d)", length, MaxFrame)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
