// Command ft is a fast terminal client for the 42 intra.
package main

import (
	"os"

	"github.com/swilmus/ft/internal/commands"
)

func main() {
	os.Exit(commands.Execute())
}
