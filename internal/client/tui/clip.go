package tui

import (
	"fmt"
	"strings"

	"conner/internal/clipx"

	tea "github.com/charmbracelet/bubbletea"
)

func sanitizeClip(s string) string   { return clipx.Sanitize(s) }
func writeClipboard(s string) error  { return clipx.Write(s) }
func readClipboard() (string, error) { return clipx.Read() }

func lastVisibleLine(rendered string) string {
	plain := stripANSI(rendered)
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return ""
	}
	lines := strings.Split(plain, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		s := strings.TrimSpace(lines[i])
		if s != "" {
			return s
		}
	}
	return ""
}

func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) {
				c := s[i]
				i++
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func shortcutConsumed(msg tea.Msg) bool {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch km.String() {
	case "ctrl+y", "ctrl+shift+c", "ctrl+v", "ctrl+u", "ctrl+k", "ctrl+l", "ctrl+g", "ctrl+s",
		"ctrl+p", "ctrl+f", "ctrl+o", "ctrl+n", "ctrl+h", "alt+right", "alt+left", "ctrl+right", "ctrl+left",
		"up", "down", "pgup", "pgdown", "f1":
		return true
	}
	return false
}

func nextRoom(rooms []string, current string) string {
	if len(rooms) == 0 {
		return current
	}
	for i, r := range rooms {
		if r == current {
			return rooms[(i+1)%len(rooms)]
		}
	}
	return rooms[0]
}

func shortHost(addr string) string {
	host := addr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		host = addr[:i]
	}
	if strings.HasSuffix(host, ".onion") && len(host) > 16 {
		return host[:8] + "…onion"
	}
	return host
}

func formatRoomTab(name string, current bool, unread int) string {
	label := "#" + name
	if unread > 0 {
		label = fmt.Sprintf("#%s (%d)", name, unread)
	}
	if current {
		return "[" + label + "]"
	}
	return label
}

func prevRoom(rooms []string, current string) string {
	if len(rooms) == 0 {
		return current
	}
	for i, r := range rooms {
		if r == current {
			if i == 0 {
				return rooms[len(rooms)-1]
			}
			return rooms[i-1]
		}
	}
	return rooms[0]
}
