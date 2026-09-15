package tor

import "strings"

func normalizeSocks(v string) string {
	v = strings.TrimPrefix(v, "unix:")
	if strings.HasPrefix(v, "[") {
		return v
	}
	if !strings.Contains(v, ":") {
		return "127.0.0.1:" + v
	}
	if strings.HasPrefix(v, ":") {
		return "127.0.0.1" + v
	}
	return v
}
