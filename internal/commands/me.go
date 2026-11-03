package commands

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/format"

	"github.com/skyreks00/ft/internal/api"
)

func newMeCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show your profile, level, wallet, correction points and location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			u, err := c.Me(cmd.Context())
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(u)
			}
			printUser(a, u)
			return nil
		},
	}
}

func printUser(a *app, u *api.User) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	defer w.Flush()

	name := u.Login
	if u.DisplayName != "" {
		name = fmt.Sprintf("%s (%s)", u.DisplayName, u.Login)
	}
	fmt.Fprintf(w, "User\t%s\n", name)
	if camp := u.PrimaryCampus(); camp != nil {
		fmt.Fprintf(w, "Campus\t%s\n", camp.Name)
	}
	if cu := u.MainCursus(); cu != nil {
		fmt.Fprintf(w, "Level\t%.2f  (%s)\n", cu.Level, cu.Cursus.Name)
		if cu.BlackholedAt != nil {
			fmt.Fprintf(w, "Blackhole\t%s\n", format.DateWithDays(*cu.BlackholedAt))
		}
	}
	fmt.Fprintf(w, "Wallet\t%d ₳\n", u.Wallet)
	fmt.Fprintf(w, "Correction points\t%d\n", u.CorrectionPoint)
	loc := "not logged in"
	if u.Location != nil && *u.Location != "" {
		loc = *u.Location
	}
	fmt.Fprintf(w, "Location\t%s\n", loc)
}
