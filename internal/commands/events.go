package commands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/api"
)

// eventRow is an event with the user's subscription, for `--json`.
type eventRow struct {
	api.Event
	Joined bool `json:"joined"`
}

func newEventsCmd(a *app) *cobra.Command {
	var mine bool
	ls := func(cmd *cobra.Command, _ []string) error {
		rows, err := loadEvents(cmd.Context(), a, mine)
		if err != nil {
			return err
		}
		if a.flags.json {
			return a.printJSON(rows)
		}
		if len(rows) == 0 {
			fmt.Fprintln(a.out, "No upcoming events.")
			return nil
		}
		w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tWHEN\tEVENT\tSEATS\t")
		for _, r := range rows {
			joined := ""
			if r.Joined {
				joined = "✓ joined"
			}
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n", r.ID, slotRange(r.BeginAt, r.EndAt), truncate(r.Name, 44), seats(&r.Event), joined)
		}
		return w.Flush()
	}

	cmd := &cobra.Command{
		Use:     "events",
		Aliases: []string{"e"},
		Short:   "List, join and leave campus events",
		Args:    cobra.NoArgs,
		RunE:    ls,
	}
	cmd.PersistentFlags().BoolVar(&mine, "mine", false, "only events you joined")
	cmd.AddCommand(
		&cobra.Command{Use: "ls", Short: "List upcoming events of your campus", Args: cobra.NoArgs, RunE: ls},
		&cobra.Command{
			Use:               "join <id>",
			Aliases:           []string{"j"},
			ValidArgsFunction: completeEventID(a, false),
			Short:             "Subscribe to an event",
			Args:              cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return joinOrLeave(cmd, a, args[0], true)
			},
		},
		&cobra.Command{
			Use:               "leave <id>",
			Aliases:           []string{"l"},
			ValidArgsFunction: completeEventID(a, true),
			Short:             "Unsubscribe from an event",
			Args:              cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return joinOrLeave(cmd, a, args[0], false)
			},
		},
	)
	return cmd
}

func loadEvents(ctx context.Context, a *app, mine bool) ([]eventRow, error) {
	c, err := a.client(ctx)
	if err != nil {
		return nil, err
	}
	me, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}
	joined, err := c.MyEvents(ctx, me.ID)
	if err != nil {
		return nil, err
	}
	in := map[int]bool{}
	for _, e := range joined {
		in[e.ID] = true
	}
	events := joined
	if !mine {
		campus := me.PrimaryCampus()
		if campus == nil {
			return nil, errors.New("no campus on your profile")
		}
		cursusID := api.DefaultCursusID
		if cu := me.MainCursus(); cu != nil {
			cursusID = cu.Cursus.ID
		}
		if events, err = c.CampusEvents(ctx, campus.ID, cursusID); err != nil {
			return nil, err
		}
	}
	rows := make([]eventRow, len(events))
	for i, e := range events {
		rows[i] = eventRow{Event: e, Joined: in[e.ID]}
	}
	return rows, nil
}

func joinOrLeave(cmd *cobra.Command, a *app, arg string, join bool) error {
	id, err := strconv.Atoi(arg)
	if err != nil {
		return fmt.Errorf("invalid event id %q (see `ft events`)", arg)
	}
	ctx := cmd.Context()
	c, err := a.client(ctx)
	if err != nil {
		return err
	}
	me, err := c.Me(ctx)
	if err != nil {
		return err
	}
	ev, err := c.Event(ctx, id)
	if err != nil {
		return err
	}
	if !join {
		if err := c.LeaveEvent(ctx, id, me.ID); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Left %s.\n", ev.Name)
		return nil
	}
	if ev.Full() {
		return fmt.Errorf("%s is full (%s)", ev.Name, seats(ev))
	}
	if ev.ProhibitionOfCancellation != nil {
		if err := confirm(cmd, false, fmt.Sprintf("%s does not allow cancelling. Join anyway?", ev.Name)); err != nil {
			return err
		}
	}
	if err := c.JoinEvent(ctx, id, me.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Joined %s (%s, %s).\n", ev.Name, slotRange(ev.BeginAt, ev.EndAt), ev.Location)
	return nil
}

func seats(e *api.Event) string {
	if !e.Limited() {
		return strconv.Itoa(e.NbrSubscribers)
	}
	return fmt.Sprintf("%d/%d", e.NbrSubscribers, *e.MaxPeople)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
