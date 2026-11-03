package commands

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/api"
)

const maxSlotRows = 30

func newCorrectCmd(a *app) *cobra.Command {
	var list bool
	cmd := &cobra.Command{
		Use:               "correct [project]",
		Aliases:           []string{"c"},
		ValidArgsFunction: completeProject(a, false),
		Short:             "Book an evaluation for the linked project",
		Long: `Open the intra page where you book an evaluation slot for your team.
Booking through the API is not available to students, so this opens your
browser. --list shows the open slots the API reports instead: they come
from every campus and hide who opened them.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			me, err := c.Me(ctx)
			if err != nil {
				return err
			}
			link, _, err := linkedProject(ctx, c, me, args)
			if err != nil {
				return err
			}

			if list {
				slots, err := c.ProjectSlots(ctx, link.ProjectID)
				if err != nil {
					return err
				}
				if a.flags.json {
					return a.printJSON(slots)
				}
				printSlots(a, slots)
				return nil
			}

			embedded := me.ProjectsUser(link.ProjectID)
			if embedded == nil {
				return fmt.Errorf("not registered to %s: run `ft open`", link.Slug)
			}
			if platform := api.SlotsPlatform(a.cfg.SlotsURL, me); platform != "" {
				fmt.Fprintf(a.out, "Your campus books evaluations on %s.\n", platform)
				return openPage(a, platform)
			}
			pu, err := c.ProjectsUser(ctx, embedded.ID)
			if err != nil {
				return err
			}
			team := pu.CurrentTeam()
			if team == nil {
				return fmt.Errorf("no team for %s yet: create it on the intra (%s)", link.Slug, api.ProjectPageURL(link.Slug))
			}
			if team.RepoURL != "" && !team.Closed {
				fmt.Fprintln(a.out, "Tip: push your work first with `ft submit`.")
			}
			return openPage(a, api.SlotsPageURL(link.Slug, team.ID))
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "list open slots from the API (all campuses) instead of opening the intra")
	return cmd
}

// printSlots prints how many slots start at each time, soonest first.
func printSlots(a *app, slots []api.Slot) {
	if len(slots) == 0 {
		fmt.Fprintln(a.out, "No open slots.")
		return
	}
	type row struct {
		at    time.Time
		count int
	}
	var rows []row
	for _, s := range slots {
		if n := len(rows); n > 0 && rows[n-1].at.Equal(s.BeginAt) {
			rows[n-1].count++
			continue
		}
		rows = append(rows, row{at: s.BeginAt})
		rows[len(rows)-1].count = 1
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	for _, r := range rows[:min(maxSlotRows, len(rows))] {
		fmt.Fprintf(w, "%s\t%d slot(s)\n", r.at.Local().Format("Mon 02 Jan 15:04"), r.count)
	}
	w.Flush()
	if len(rows) > maxSlotRows {
		fmt.Fprintf(a.out, "… %d more start times\n", len(rows)-maxSlotRows)
	}
	fmt.Fprintln(a.out, "Note: these are open slots from every campus, and evaluators are hidden. Book with `ft correct`.")
}
