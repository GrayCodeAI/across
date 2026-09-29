package main

import (
	"os"

	"github.com/graycodeai/across/internal/adapter"
)

func main() {
	os.Exit(adapter.Run("codex", os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
