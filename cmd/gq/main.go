package main

import (
	"os"

	"github.com/javirub/gq/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
