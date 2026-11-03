package commands

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/format"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/config"
)

func newStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:               "status [project]",
		Aliases:           []string{"st"},
		ValidArgsFunction: completeProject(a, false),
		Short:             "Show the linked project's state, team, repo and evaluations",
		Args:              cobra.MaximumNArgs(1),
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
			link, dir, err := linkedProject(ctx, c, me, args)
			if err != nil {
				return err
			}

			r, err := c.ProjectStatus(ctx, me, api.ProjectRef{ID: link.ProjectID, Slug: link.Slug, Name: link.Name})
			if err != nil {
				return err
			}
			// Keep the cached team id in .ft.toml current (e.g. after a retry).
			if dir != "" && r.Team != nil && r.Team.ID != link.TeamID {
				link.TeamID = r.Team.ID
				_ = config.WriteLink(dir, link)
			}

			if a.flags.json {
				return a.printJSON(r)
			}
			printStatus(a, r)
			return nil
		},
	}
}

func printStatus(a *app, r *api.ProjectStatus) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	defer w.Flush()

	name := r.Project.Slug
	if r.Project.Name != "" {
		name = fmt.Sprintf("%s (%s)", r.Project.Name, r.Project.Slug)
	}
	fmt.Fprintf(w, "Project\t%s\n", name)
	if !r.Registered() {
		fmt.Fprintf(w, "Status\tnot registered (run `ft open`)\n")
		return
	}
	status := format.Status(r.Status)
	if r.FinalMark != nil {
		status += fmt.Sprintf("  mark %d", *r.FinalMark)
		if r.Validated != nil && *r.Validated {
			status += " ✓"
		} else if r.Validated != nil {
			status += " ✗"
		}
	}
	fmt.Fprintf(w, "Status\t%s\n", status)

	if t := r.Team; t != nil {
		var members []string
		for _, u := range t.Users {
			if u.Leader {
				members = append(members, u.Login+"*")
			} else {
				members = append(members, u.Login)
			}
		}
		var flags []string
		if t.Locked {
			flags = append(flags, "locked")
		}
		if t.Closed {
			flags = append(flags, "closed")
		}
		team := fmt.Sprintf("%s: %s", t.Name, strings.Join(members, ", "))
		if len(flags) > 0 {
			team += "  [" + strings.Join(flags, ", ") + "]"
		}
		fmt.Fprintf(w, "Team\t%s\n", team)
		repo := t.RepoURL
		if repo == "" {
			repo = "not created yet"
		}
		fmt.Fprintf(w, "Repo\t%s\n", repo)
		if t.TerminatingAt != nil && !t.Closed {
			fmt.Fprintf(w, "Deadline\t%s\n", format.DateWithDays(*t.TerminatingAt))
		}
	}
	if r.BlackholedAt != nil {
		fmt.Fprintf(w, "Blackhole\t%s\n", format.DateWithDays(*r.BlackholedAt))
	}
	if r.RetriableAt != nil {
		fmt.Fprintf(w, "Retry\tafter %s\n", r.RetriableAt.Local().Format("2006-01-02 15:04"))
	}

	if r.Team == nil {
		return
	}
	done := r.EvaluationsDone()
	evals := fmt.Sprintf("%d done", done)
	if r.RequiredEvaluations > 0 {
		evals = fmt.Sprintf("%d/%d done", done, r.RequiredEvaluations)
	}
	if pending := len(r.Evaluations) - done; pending > 0 {
		evals += fmt.Sprintf(", %d scheduled", pending)
	}
	fmt.Fprintf(w, "Evaluations\t%s\n", evals)
	for _, e := range r.Evaluations {
		fmt.Fprintf(w, "\t  %s\n", evalLine(&e))
		for _, l := range evalComments(&e, 72) {
			fmt.Fprintf(w, "\t      %s\n", l)
		}
	}
}

// evalComments wraps an evaluation's comment (from the evaluator) and
// feedback (about the evaluator), quoted.
func evalComments(e *api.ScaleTeam, width int) []string {
	var out []string
	for _, c := range []struct {
		text  *string
		label string
	}{{e.Comment, "“"}, {e.Feedback, "↳ feedback: “"}} {
		if c.text == nil || strings.TrimSpace(*c.text) == "" {
			continue
		}
		lines := format.Wrap(*c.text, width, 4)
		lines[0] = c.label + lines[0]
		lines[len(lines)-1] += "”"
		out = append(out, lines...)
	}
	return out
}

func evalLine(e *api.ScaleTeam) string {
	when := "unscheduled"
	if e.BeginAt != nil {
		when = e.BeginAt.Local().Format("2006-01-02 15:04")
	}
	who := e.Corrector.Login
	if who == "" {
		who = "(hidden)"
	}
	result := "pending"
	if e.Done() {
		result = "—"
		if e.FinalMark != nil {
			result = strconv.Itoa(*e.FinalMark)
		}
		if e.Flag != nil && e.Flag.Name != "" && e.Flag.Name != "Ok" {
			result += " " + e.Flag.Name
		}
	}
	return fmt.Sprintf("%s  %-10s %s", when, who, result)
}
