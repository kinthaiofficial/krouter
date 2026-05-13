// Package logging configures structured logging.
//
// Logs are written to:
//   - macOS:   ~/.kinthai/logs/daemon.log
//   - Linux:   ~/.kinthai/logs/daemon.log
//   - Windows: %LOCALAPPDATA%\kinthai\logs\daemon.log
//
// Format: JSON lines with timestamp, level, module, message, structured fields.
// Rotation: 10 MB per file, keep 5 files.
//
// CRITICAL: NEVER log API Keys, even at debug level. Filter incoming headers.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger is the structured logger interface.
type Logger interface {
	Debug(msg string, fields ...any)
	Info(msg string, fields ...any)
	Warn(msg string, fields ...any)
	Error(msg string, fields ...any)
}

type contextKey struct{}

type slogLogger struct {
	l *slog.Logger
}

// New creates a logger writing to the standard log path.
// level must be "debug", "info", "warn", or "error" (case-insensitive).
func New(level string) Logger {
	return NewWithWriter(level, defaultWriter())
}

// NewWithWriter creates a logger writing to w.
// Intended for testing; production code should use New.
func NewWithWriter(level string, w io.Writer) Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: parseSlogLevel(level)})
	return &slogLogger{l: slog.New(h)}
}

// FromContext returns the logger embedded in ctx, or a default info-level logger.
func FromContext(ctx context.Context) Logger {
	if l, ok := ctx.Value(contextKey{}).(Logger); ok {
		return l
	}
	return New("info")
}

// WithContext returns a new context carrying l.
func WithContext(ctx context.Context, l Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// SanitizeHeader returns "[REDACTED]" for sensitive header names; value otherwise.
// Sensitive: x-api-key, authorization. Never logs these values.
func SanitizeHeader(name, value string) string {
	switch strings.ToLower(name) {
	case "x-api-key", "authorization":
		return "[REDACTED]"
	}
	return value
}

func (l *slogLogger) Debug(msg string, fields ...any) { l.l.Debug(msg, fields...) }
func (l *slogLogger) Info(msg string, fields ...any)  { l.l.Info(msg, fields...) }
func (l *slogLogger) Warn(msg string, fields ...any)  { l.l.Warn(msg, fields...) }
func (l *slogLogger) Error(msg string, fields ...any) { l.l.Error(msg, fields...) }

func defaultWriter() io.Writer {
	logPath := defaultLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return os.Stderr
	}
	return &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // MB
		MaxBackups: 5,
		Compress:   false,
	}
}

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "kinthai", "logs", "daemon.log")
	}
	return filepath.Join(home, ".kinthai", "logs", "daemon.log")
}

func parseSlogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
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
