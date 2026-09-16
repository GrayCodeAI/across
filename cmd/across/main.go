package main

import (
	"os"

	"github.com/graycodeai/across/internal/cli"
	"github.com/graycodeai/across/internal/config"
	"github.com/graycodeai/across/internal/logging"
)

func main() {
	home := config.DefaultHome()
	for i, a := range os.Args {
		if a == "--home" && i+1 < len(os.Args) {
			home = os.Args[i+1]
		}
	}
	log := logging.New(home)
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}
