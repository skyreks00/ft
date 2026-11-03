package commands

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/format"
)

func newLogtimeCmd(a *app) *cobra.Command {
	var weeks int
	cmd := &cobra.Command{
		Use:     "logtime [login]",
		Aliases: []string{"lt"},
		Short:   "Show the time spent on campus per day and week",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			login := ""
			if len(args) > 0 {
				login = args[0]
			} else {
				me, err := c.Me(ctx)
				if err != nil {
					return err
				}
				login = me.Login
			}
			now := time.Now()
			from := format.WeekStart(now).AddDate(0, 0, -7*(weeks-1))
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
			if monthStart.Before(from) {
				from = monthStart
			}
			locs, err := c.Locations(ctx, login, from.AddDate(0, 0, -1), now)
			if err != nil {
				return err
			}
			days := api.Logtime(locs, from, now, now)
			if a.flags.json {
				out := map[string]string{}
				for d, v := range days {
					out[d] = format.Hours(v)
				}
				return a.printJSON(out)
			}
			printLogtime(a, days, now, weeks)
			return nil
		},
	}
	cmd.Flags().IntVarP(&weeks, "weeks", "w", 4, "number of weeks to show")
	return cmd
}

func printLogtime(a *app, days map[string]time.Duration, now time.Time, weeks int) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(w, "week of\tMon\tTue\tWed\tThu\tFri\tSat\tSun\ttotal\t")
	start := format.WeekStart(now).AddDate(0, 0, -7*(weeks-1))
	for wk := 0; wk < weeks; wk++ {
		monday := start.AddDate(0, 0, 7*wk)
		fmt.Fprint(w, monday.Format("02 Jan"))
		var total time.Duration
		for d := 0; d < 7; d++ {
			day := monday.AddDate(0, 0, d)
			v := days[api.DayKey(day)]
			total += v
			cell := "·"
			switch {
			case day.After(now):
				cell = ""
			case v > 0:
				cell = format.Hours(v)
			}
			fmt.Fprintf(w, "\t%s", cell)
		}
		fmt.Fprintf(w, "\t%s\t\n", format.Hours(total))
	}
	w.Flush()
	var month time.Duration
	for d, v := range days {
		if t, err := time.ParseInLocation("2006-01-02", d, time.Local); err == nil && t.Month() == now.Month() && t.Year() == now.Year() {
			month += v
		}
	}
	fmt.Fprintf(a.out, "\nToday %s · this month %s\n", format.Hours(days[api.DayKey(now)]), format.Hours(month))
}
