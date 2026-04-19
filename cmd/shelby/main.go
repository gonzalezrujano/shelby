package main

import (
	"os"

	"shelby/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
