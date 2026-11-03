// Command ft is a fast terminal client for the 42 intra.
package main

import (
	"os"

	"github.com/skyreks00/ft/internal/commands"
)

func main() {
	os.Exit(commands.Execute())
}
