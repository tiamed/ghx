package main

import (
	"os"

	"ghx/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Options{}))
}
