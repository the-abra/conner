package logx

import (
	"log/slog"
	"os"
	"sync"
)

var (
	once sync.Once
	l    *slog.Logger
)

func L() *slog.Logger {
	once.Do(func() {
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})
		l = slog.New(h)
	})
	return l
}

func Info(msg string, args ...any)  { L().Info(msg, args...) }
func Warn(msg string, args ...any)  { L().Warn(msg, args...) }
func Error(msg string, args ...any) { L().Error(msg, args...) }
