package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// errCancelled is returned when the user answers no.
var errCancelled = errors.New("cancelled")

// confirm asks a yes/no question on stderr and returns errCancelled unless
// the answer is yes. It refuses to guess when stdin is not a terminal:
// scripts must pass --yes.
func confirm(cmd *cobra.Command, yes bool, prompt string) error {
	if yes {
		return nil
	}
	if f, ok := cmd.InOrStdin().(*os.File); ok && !term.IsTerminal(int(f.Fd())) {
		return errors.New("stdin is not a terminal: pass --yes to confirm")
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s [y/N] ", prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "o", "oui":
		return nil
	}
	return errCancelled
}
