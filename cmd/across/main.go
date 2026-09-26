package main

import (
	"fmt"
	"os"

	"github.com/graycodeai/across/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		err = cli.WrapError(err)
		fmt.Fprintln(os.Stderr, cli.FormatError(err))
		os.Exit(cli.ExitCode(err))
	}
}
