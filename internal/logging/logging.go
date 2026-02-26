package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

var Logger *slog.Logger

func Init(level string, filePath string) error {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	var w io.Writer = os.Stderr
	if filePath != "" {
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		w = io.MultiWriter(os.Stderr, f)
	}
	Logger = slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl}))
	slog.SetDefault(Logger)
	return nil
}
