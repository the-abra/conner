package clipx

import (
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/atotto/clipboard"
)

const MaxRunes = 32 * 1024

func Sanitize(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	if utf8.RuneCountInString(s) > MaxRunes {
		r := []rune(s)
		s = string(r[:MaxRunes])
	}
	return s
}

func Write(s string) error {
	s = Sanitize(s)
	if err := clipboard.WriteAll(s); err == nil {
		return nil
	}
	return writeFallback(s)
}

func Read() (string, error) {
	s, err := clipboard.ReadAll()
	if err == nil {
		return Sanitize(s), nil
	}
	return readFallback()
}

func writeFallback(s string) error {
	for _, name := range []string{"wl-copy", "pbcopy"} {
		if _, err := exec.LookPath(name); err != nil {
			continue
		}
		cmd := exec.Command(name)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return os.ErrNotExist
}

func readFallback() (string, error) {
	if _, err := exec.LookPath("wl-paste"); err == nil {
		out, err := exec.Command("wl-paste").Output()
		if err == nil {
			return Sanitize(string(out)), nil
		}
	}
	if _, err := exec.LookPath("pbpaste"); err == nil {
		out, err := exec.Command("pbpaste").Output()
		if err == nil {
			return Sanitize(string(out)), nil
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
		if err == nil {
			return Sanitize(string(out)), nil
		}
	}
	return "", os.ErrNotExist
}
