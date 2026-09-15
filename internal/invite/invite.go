package invite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// Blob is a DNS-free join ticket: onion/host + optional room + hub fingerprint.
type Blob struct {
	V     int    `json:"v"`
	Onion string `json:"onion,omitempty"`
	Host  string `json:"host,omitempty"`
	Port  string `json:"port,omitempty"`
	Room  string `json:"room,omitempty"`
	FP    string `json:"fp,omitempty"`
	Tor   bool   `json:"tor"`
	PT    string `json:"pt,omitempty"` // "snowflake" | "system-tor" | ""
}

func Encode(b Blob) string {
	b.V = 1
	raw, _ := json.Marshal(b)
	return "conner://v1/" + base64.RawURLEncoding.EncodeToString(raw)
}

func Decode(s string) (Blob, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "conner://v1/") {
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(s, "conner://v1/"))
		if err != nil {
			return Blob{}, err
		}
		var b Blob
		if err := json.Unmarshal(raw, &b); err != nil {
			return Blob{}, err
		}
		return b, nil
	}
	if strings.Contains(s, ".onion") {
		host := s
		port := "6666"
		if h, p, ok := strings.Cut(s, ":"); ok {
			host, port = h, p
		}
		return Blob{V: 1, Onion: host, Port: port, Tor: true, Room: "general"}, nil
	}
	if strings.Contains(s, ":") {
		h, p, _ := strings.Cut(s, ":")
		return Blob{V: 1, Host: h, Port: p, Tor: false, Room: "general"}, nil
	}
	return Blob{}, fmt.Errorf("unrecognized invite: %s", s)
}

func (b Blob) DialAddr() string {
	port := b.Port
	if port == "" {
		port = "6666"
	}
	if b.Tor || b.Onion != "" {
		host := b.Onion
		if host == "" {
			host = b.Host
		}
		return host + ":" + port
	}
	return b.Host + ":" + port
}
