package main

import (
	"os"

	"github.com/bharm16/readmit/internal/cli"
)

var version = "dev"

func main() {
	if err := cli.Execute(version, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
