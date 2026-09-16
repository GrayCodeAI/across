package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

func execBounded(path string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, args...)
	var buf bytes.Buffer
	c.Stdout = &limitedWriter{W: &buf, N: 1 << 20}
	c.Stderr = &limitedWriter{W: &buf, N: 1 << 20}
	// do not leak full env secrets beyond minimal: pass through but document
	err := c.Run()
	out := buf.String()
	if len(out) > 1<<20 {
		out = out[:1<<20] + "\n[TRUNCATED]"
	}
	return out, err
}

type limitedWriter struct {
	W io.Writer
	N int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.N <= 0 {
		return len(p), nil
	}
	if len(p) > l.N {
		p = p[:l.N]
	}
	n, err := l.W.Write(p)
	l.N -= n
	_ = err
	return len(p), nil
}

var _ = fmt.Sprint
