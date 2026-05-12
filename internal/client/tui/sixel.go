package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// TryRenderSixel attempts to render an image using the external img2sixel command.
// If the command is missing or fails, it returns an empty string.
func TryRenderSixel(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return ""
	}

	cmd := exec.Command("img2sixel", "-w", "80", path)
	out, err := cmd.Output()
	if err != nil {
		return "" // Silently fail if not installed or fails
	}
	
	// Ensure there's a newline after the sixel sequence so it doesn't mess up Bubble Tea
	return string(out) + "\n"
}
