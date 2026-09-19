package main

import (
	"os"

	"github.com/bharm16/readmit/internal/cli"
	"github.com/bharm16/readmit/internal/engine"
)

func main() {
	if err := cli.Execute(engine.Version(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
