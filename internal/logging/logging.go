// Package logging provides minimal structured logging for Across.
// Logs go to stderr by default; ACROSS_LOG=file redirects to home/logs/.
package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Logger is a minimal levelled logger.
type Logger struct {
	out io.Writer
}

// New returns a logger writing to stderr, or to home/logs/across.log when
// ACROSS_LOG=file.
func New(home string) *Logger {
	if os.Getenv("ACROSS_LOG") == "file" && home != "" {
		p := filepath.Join(home, "logs", "across.log")
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			return &Logger{out: f}
		}
	}
	return &Logger{out: os.Stderr}
}

func (l *Logger) log(level, msg string) {
	fmt.Fprintf(l.out, "%s %-5s %s\n", time.Now().UTC().Format(time.RFC3339), level, msg)
}

// Info logs informational messages.
func (l *Logger) Info(msg string) { l.log("INFO", msg) }

// Warn logs warnings.
func (l *Logger) Warn(msg string) { l.log("WARN", msg) }

// Error logs errors.
func (l *Logger) Error(msg string) { l.log("ERROR", msg) }
