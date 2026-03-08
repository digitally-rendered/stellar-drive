// Command stellar-drive is the CLI entry point for the stellar-drive framework.
package main

import (
	"os"

	"github.com/digitally-rendered/stellar-drive/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
