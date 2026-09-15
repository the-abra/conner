package filesync

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher prefers fsnotify and falls back to a short poll if the OS watch fails.
type Watcher struct {
	dir      string
	interval time.Duration
	stop     chan struct{}
	once     sync.Once
}

func New(dir string, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = time.Second
	}
	return &Watcher{dir: dir, interval: interval, stop: make(chan struct{})}
}

func (w *Watcher) Events(emit func(name string, info os.FileInfo)) {
	if w.watchFS(emit) {
		return
	}
	w.poll(emit)
}

func (w *Watcher) watchFS(emit func(name string, info os.FileInfo)) bool {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return false
	}
	defer fw.Close()
	if err := fw.Add(w.dir); err != nil {
		return false
	}
	// Initial scan so existing files are offered once.
	w.scan(map[string]time.Time{}, emit)

	for {
		select {
		case <-w.stop:
			return true
		case ev, ok := <-fw.Events:
			if !ok {
				return true
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			info, err := os.Stat(ev.Name)
			if err != nil {
				continue
			}
			emit(ev.Name, info)
		case <-fw.Errors:
			// keep running; next event or stop
		}
	}
}

func (w *Watcher) poll(emit func(name string, info os.FileInfo)) {
	seen := map[string]time.Time{}
	tick := time.NewTicker(w.interval)
	defer tick.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-tick.C:
			w.scan(seen, emit)
		}
	}
}

func (w *Watcher) scan(seen map[string]time.Time, emit func(name string, info os.FileInfo)) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		mt := info.ModTime()
		if last, ok := seen[name]; ok && !mt.After(last) {
			continue
		}
		seen[name] = mt
		emit(filepath.Join(w.dir, name), info)
	}
}

func (w *Watcher) Stop() {
	w.once.Do(func() { close(w.stop) })
}
