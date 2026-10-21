package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

type Config struct {
	Level      string
	File       bool
	Dir        string
	Component  string
	MaxSizeMB  int
	MaxBackups int
}

// Setup 初始化日志, 返回 logger 与关闭函数.
func Setup(cfg Config) (*slog.Logger, func(), error) {
	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}
	console := slog.NewTextHandler(os.Stderr, opts)

	var handlers []slog.Handler
	handlers = append(handlers, console)

	var file *lumberjack.Logger
	if cfg.File {
		if cfg.Dir == "" {
			return nil, nil, fmt.Errorf("log dir is empty")
		}
		if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
			return nil, nil, err
		}
		maxSize := cfg.MaxSizeMB
		if maxSize <= 0 {
			maxSize = 50
		}
		maxBackups := cfg.MaxBackups
		if maxBackups <= 0 {
			maxBackups = 14
		}
		file = &lumberjack.Logger{
			Filename:   filepath.Join(cfg.Dir, "dormwatt.log"),
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			LocalTime:  true,
		}
		handlers = append(handlers, slog.NewJSONHandler(file, opts))
	}

	logger := slog.New(fanout{handlers: handlers})
	if cfg.Component != "" {
		logger = logger.With("component", cfg.Component)
	}
	cleanup := func() {
		if file != nil {
			_ = file.Close()
		}
	}
	return logger, cleanup, nil
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type fanout struct {
	handlers []slog.Handler
}

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, r slog.Record) error {
	var first error
	for _, h := range f.handlers {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return fanout{handlers: next}
}

func (f fanout) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(f.handlers))
	for i, h := range f.handlers {
		next[i] = h.WithGroup(name)
	}
	return fanout{handlers: next}
}

// Discard 返回一个不输出日志的 logger, 用于测试.
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
