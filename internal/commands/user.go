package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/api"
)

func newUserCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "user <login>",
		Aliases: []string{"u"},
		Short:   "Show a student's profile, level and location",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			u, err := c.User(ctx, args[0])
			if api.IsNotFound(err) {
				return fmt.Errorf("no user %q", args[0])
			}
			if err != nil {
				return err
			}
			last, err := c.LastLocation(ctx, u.Login)
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(struct {
					User         *api.User     `json:"user"`
					LastLocation *api.Location `json:"last_location"`
				}{u, last})
			}
			printUser(a, u)
			if last != nil && last.EndAt != nil {
				fmt.Fprintf(a.out, "Last seen          %s on %s (%s ago)\n",
					last.EndAt.Local().Format("Mon 02 Jan 15:04"), last.Host, ago(*last.EndAt))
			}
			return nil
		},
	}
}

// ago formats a duration since t as "3h", "2d"...
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
