package commands

import (
	"cmp"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/fuzzy"
	"github.com/swilmus/ft/internal/gitx"
)

func newEvalCmd(a *app) *cobra.Command {
	var history bool
	var last int
	ls := func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		c, err := a.client(ctx)
		if err != nil {
			return err
		}
		me, err := c.Me(ctx)
		if err != nil {
			return err
		}
		if history {
			return evalHistory(cmd, a, c, me, last)
		}
		ev, err := c.UpcomingEvaluations(ctx, me.Login)
		if err != nil {
			return err
		}
		if a.flags.json {
			return a.printJSON(ev)
		}
		if len(ev.AsCorrector)+len(ev.AsCorrected) == 0 {
			fmt.Fprintln(a.out, "No upcoming evaluations.")
			return nil
		}
		names := c.ProjectSlugs(ctx, me, ev)
		w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "WHEN\tROLE\tPROJECT\tWITH")
		for _, s := range ev.AsCorrector {
			fmt.Fprintf(w, "%s\tyou evaluate\t%s\t%s\n", evalWhen(&s), names[s.ProjectID()], logins(s.Correcteds))
		}
		for _, s := range ev.AsCorrected {
			who := s.Corrector.Login
			if who == "" {
				who = "(hidden until it starts)"
			}
			fmt.Fprintf(w, "%s\tyou are evaluated\t%s\t%s\n", evalWhen(&s), names[s.ProjectID()], who)
		}
		return w.Flush()
	}
	cmd := &cobra.Command{
		Use:     "eval",
		Aliases: []string{"ev"},
		Short:   "List your upcoming evaluations, as evaluator and as evaluated",
		Args:    cobra.NoArgs,
		RunE:    ls,
	}
	cmd.Flags().BoolVarP(&history, "history", "H", false, "show your last evaluations, given and received, with their comments")
	cmd.Flags().IntVarP(&last, "last", "n", 10, "with --history: how many on each side")
	cmd.AddCommand(&cobra.Command{Use: "ls", Short: cmd.Short, Args: cobra.NoArgs, RunE: ls}, newEvalCloneCmd(a))
	return cmd
}

// evalWindow is how far back an evaluation still counts as "current" for
// eval clone.
const evalWindow = 3 * time.Hour

func newEvalCloneCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "clone [id] [folder]",
		Aliases: []string{"cl"},
		Short:   "Clone the repository of the team you evaluate",
		Long: `Clone the repository of a team you evaluate: the evaluation in progress or
the next one, or the one with this id (see ft eval). The folder defaults to
./eval-<project>-<login>. Vogsphere usually opens the repository to the
evaluator when the evaluation starts.`,
		Args: cobra.MaximumNArgs(2),
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
			now := time.Now()
			evals, err := c.CorrectorEvaluations(ctx, me.Login, now.Add(-evalWindow), now.AddDate(0, 0, 14))
			if err != nil {
				return err
			}
			e, err := pickEvaluation(evals, args)
			if err != nil {
				return err
			}
			if e.Team == nil || e.Team.RepoURL == "" {
				return errors.New("the intra does not show this team's repository yet: try again when the evaluation starts")
			}
			names := c.ProjectSlugs(ctx, me, &api.Evaluations{AsCorrector: []api.ScaleTeam{*e}})
			dir := fmt.Sprintf("eval-%s-%s", fuzzy.ShortSlug(names[e.ProjectID()]), teamLogin(e))
			if len(args) > 1 {
				dir = args[1]
			}
			if empty, exists := dirState(dir); exists && !empty {
				return fmt.Errorf("%s already exists and is not empty", dir)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Evaluation %s, %s: cloning %s\n", evalWhen(e), names[e.ProjectID()], e.Team.RepoURL)
			if err := gitx.Clone(e.Team.RepoURL, dir, cmd.ErrOrStderr(), cmd.ErrOrStderr()); err != nil {
				if errors.Is(err, gitx.ErrKeyDenied) && e.BeginAt != nil && e.BeginAt.After(now) {
					return fmt.Errorf("vogsphere refused access: the evaluation starts at %s, the repository usually opens then", e.BeginAt.Local().Format("15:04"))
				}
				return err
			}
			fmt.Fprintf(a.out, "Cloned into %s.\nNext: cd %s\n", dir, dir)
			return nil
		},
	}
}

// pickEvaluation returns the evaluation with the id in args, or the one in
// progress, or the next one.
func pickEvaluation(evals []api.ScaleTeam, args []string) (*api.ScaleTeam, error) {
	if len(args) > 0 {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid evaluation id %q", args[0])
		}
		for i := range evals {
			if evals[i].ID == id {
				return &evals[i], nil
			}
		}
		return nil, fmt.Errorf("no evaluation %d that you give, in progress or upcoming", id)
	}
	if len(evals) == 0 {
		return nil, errors.New("no evaluation to give in progress or planned")
	}
	return &evals[0], nil // soonest first, the one in progress included
}

// teamLogin names the evaluated team by its leader (or first member), else
// by the login at the end of its repository name.
func teamLogin(e *api.ScaleTeam) string {
	if len(e.Correcteds) > 0 {
		return e.Correcteds[0].Login
	}
	if e.Team != nil {
		for _, u := range e.Team.Users {
			if u.Leader {
				return u.Login
			}
		}
		if i := strings.LastIndex(e.Team.RepoURL, "-"); i >= 0 {
			return e.Team.RepoURL[i+1:]
		}
	}
	return "team"
}

// evalHistory prints the user's last evaluations, received then given,
// with marks, flags and comments.
func evalHistory(cmd *cobra.Command, a *app, c *api.Client, me *api.User, n int) error {
	ev, err := c.PastEvaluations(cmd.Context(), me.Login, n)
	if err != nil {
		return err
	}
	if a.flags.json {
		return a.printJSON(ev)
	}
	names := c.ProjectSlugs(cmd.Context(), me, ev)
	section := func(title string, list []api.ScaleTeam, who func(*api.ScaleTeam) string) {
		fmt.Fprintln(a.out, title)
		if len(list) == 0 {
			fmt.Fprintln(a.out, "  none")
		}
		for i := range list {
			e := &list[i]
			mark := "—"
			if e.FinalMark != nil {
				mark = fmt.Sprint(*e.FinalMark)
			}
			if e.Flag != nil && e.Flag.Name != "" && e.Flag.Name != "Ok" {
				mark += " " + e.Flag.Name
			}
			fmt.Fprintf(a.out, "  %s  %-22s %-12s %s\n", evalWhen(e), names[e.ProjectID()], who(e), mark)
			for _, l := range evalComments(e, 72) {
				fmt.Fprintf(a.out, "      %s\n", l)
			}
		}
	}
	section("You were evaluated", ev.AsCorrected, func(e *api.ScaleTeam) string {
		return cmp.Or(e.Corrector.Login, "(hidden)")
	})
	fmt.Fprintln(a.out)
	section("You evaluated", ev.AsCorrector, func(e *api.ScaleTeam) string {
		return logins(e.Correcteds)
	})
	return nil
}

func evalWhen(s *api.ScaleTeam) string {
	if s.BeginAt == nil {
		return "unscheduled"
	}
	return s.BeginAt.Local().Format("Mon 02 Jan 15:04")
}

func logins(users api.Correcteds) string {
	if len(users) == 0 {
		return "(hidden until it starts)"
	}
	var out []string
	for _, u := range users {
		out = append(out, u.Login)
	}
	return strings.Join(out, ", ")
}
