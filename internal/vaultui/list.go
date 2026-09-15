package vaultui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Name    string
	Size    int64
	ModTime time.Time
	Dir     bool
}

func List(dir string) []Entry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Entry
	for _, e := range ents {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
			Dir:     e.IsDir(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out
}

func FormatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	}
}

func TotalBytes(ents []Entry) int64 {
	var n int64
	for _, e := range ents {
		n += e.Size
	}
	return n
}

func Line(e Entry, selected bool, width int) string {
	mark := "  "
	if selected {
		mark = "> "
	}
	kind := "file"
	if e.Dir {
		kind = "dir "
	}
	name := e.Name
	meta := fmt.Sprintf("%s  %8s  %s", kind, FormatSize(e.Size), e.ModTime.Format("15:04 02 Jan"))
	if width > 28 && len(name)+len(meta)+4 > width {
		keep := width - len(meta) - 7
		if keep < 8 {
			keep = 8
		}
		if len(name) > keep {
			name = name[:keep-1] + "…"
		}
	}
	return mark + name + strings.Repeat(" ", 1) + meta
}

func SafeJoin(dir, name string) string {
	return filepath.Join(dir, filepath.Base(name))
}
