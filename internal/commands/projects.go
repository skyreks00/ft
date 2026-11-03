package commands

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/format"
	"github.com/skyreks00/ft/internal/fuzzy"
)

func newProjectsCmd(a *app) *cobra.Command {
	var upcoming, all, modules, after bool
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"p"},
		Short:   "List your projects by cursus, or the upcoming ones",
		Long: `List your projects grouped by cursus, main cursus first.

When ft knows the holy graph of your campus (42 Belgium), the main cursus is
shown circle by circle with every project, started or not; --after adds
the projects after the common core, by specialization track.

--upcoming lists the projects of your campus' main cursus you have not
started, by XP (the API has no holy graph; XP roughly follows its circles).
Exams, sub-projects and retired variants are left out, and so are the
optional piscines' modules unless --modules is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			me, err := c.Me(ctx)
			if err != nil {
				return err
			}
			// Quests pick the holy graph variant (e.g. 42next).
			quests, _ := c.QuestSlugs(ctx, me.Login)
			if upcoming {
				return listUpcoming(cmd, a, c, me, quests, modules)
			}
			if t := userGraph(me, quests); t != nil && !a.flags.json {
				if err := listCircles(cmd, a, c, me, t, quests, after); err != nil {
					return err
				}
				if !all {
					return nil
				}
				fmt.Fprintln(a.out)
				return listByCursus(a, me, true, false)
			}
			return listByCursus(a, me, all, true)
		},
	}
	cmd.Flags().BoolVarP(&upcoming, "upcoming", "u", false, "projects of your cursus you have not started, by XP")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include the other cursus (piscines…)")
	cmd.Flags().BoolVar(&modules, "modules", false, "with --upcoming: include the optional piscines' modules")
	cmd.Flags().BoolVar(&after, "after", false, "with the holy graph: also list the projects after the common core")
	return cmd
}

// listByCursus prints the projects per cursus; withMain includes the main
// cursus (already shown by circle otherwise).
func listByCursus(a *app, me *api.User, all, withMain bool) error {
	mainID := api.DefaultCursusID
	if cu := me.MainCursus(); cu != nil {
		mainID = cu.Cursus.ID
	}
	order := []api.Cursus{}
	for _, cu := range me.CursusUsers {
		if cu.Cursus.ID == mainID {
			if withMain {
				order = append([]api.Cursus{cu.Cursus}, order...)
			}
		} else if all {
			order = append(order, cu.Cursus)
		}
	}
	byCursus := map[int][]api.ProjectsUser{}
	for _, pu := range me.ProjectsUsers {
		if pu.Project.ParentID != nil {
			continue
		}
		id := 0
		if slices.Contains(pu.CursusIDs, mainID) {
			id = mainID
		} else if len(pu.CursusIDs) > 0 {
			id = pu.CursusIDs[0]
		}
		byCursus[id] = append(byCursus[id], pu)
	}
	if a.flags.json {
		out := map[string][]api.ProjectsUser{}
		for _, cu := range order {
			out[cu.Slug] = byCursus[cu.ID]
		}
		return a.printJSON(out)
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	for i, cu := range order {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", cu.Name)
		pus := byCursus[cu.ID]
		sort.SliceStable(pus, func(i, j int) bool { return pus[i].Status != "finished" && pus[j].Status == "finished" })
		for _, pu := range pus {
			mark := ""
			if pu.FinalMark != nil {
				mark = fmt.Sprint(*pu.FinalMark)
			}
			fmt.Fprintf(w, "  %s %s\t%s\t%s\n", statusGlyph(&pu), fuzzy.ShortSlug(pu.Project.Slug), format.Status(pu.Status), mark)
		}
	}
	if withMain && !all && len(me.CursusUsers) > 1 {
		fmt.Fprintln(w, "\n(--all for the other cursus, --upcoming for what comes next)")
	}
	return w.Flush()
}

func userGraph(me *api.User, quests []string) *api.CircleTable {
	campusID := 0
	if c := me.PrimaryCampus(); c != nil {
		campusID = c.ID
	}
	mainID := api.DefaultCursusID
	if cu := me.MainCursus(); cu != nil {
		mainID = cu.Cursus.ID
	}
	return api.Graph(campusID, mainID, quests)
}

// registrations maps a project slug to the user's registration, including
// through an older slug that counts for it on the graph.
func registrations(me *api.User, t *api.CircleTable) map[string]*api.ProjectsUser {
	m := map[string]*api.ProjectsUser{}
	for i := range me.ProjectsUsers {
		pu := &me.ProjectsUsers[i]
		m[pu.Project.Slug] = pu
		if info, ok := t.Lookup(pu.Project.Slug); ok && info.Primary != "" {
			m[info.Primary] = pu
		}
	}
	return m
}

// listCircles prints the main cursus circle by circle, with the user's
// state on each project.
func listCircles(cmd *cobra.Command, a *app, c *api.Client, me *api.User, t *api.CircleTable, quests []string, after bool) error {
	campus := me.PrimaryCampus()
	mainID := api.DefaultCursusID
	if cu := me.MainCursus(); cu != nil {
		mainID = cu.Cursus.ID
	}
	ps, err := c.Curriculum(cmd.Context(), mainID, campus.ID, quests)
	if err != nil {
		return err
	}
	regs := registrations(me, t)
	validated := func(slug string) bool {
		pu := regs[slug]
		return pu != nil && pu.Validated != nil && *pu.Validated
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	circle, track := -1, ""
	hidden := 0
	for _, p := range ps {
		if *p.Circle == api.AfterCore && !after {
			hidden++
			continue
		}
		if *p.Circle != circle {
			circle = *p.Circle
			if circle > 0 {
				fmt.Fprintln(w, "\t\t") // keeps the columns aligned across circles
			}
			if circle == api.AfterCore {
				fmt.Fprintf(w, "%s\t\t\n", api.CircleName(circle))
			} else {
				done, total := t.Progress(circle, validated)
				fmt.Fprintf(w, "%s\t%d/%d\t\n", api.CircleName(circle), done, total)
			}
		}
		if circle == api.AfterCore {
			tr := "Other"
			if len(p.Tracks) > 0 {
				tr = p.Tracks[0]
			}
			if tr != track {
				track = tr
				fmt.Fprintf(w, " %s\t\t\n", track)
			}
		}
		glyph, state := "○", ""
		if pu := regs[p.Slug]; pu != nil {
			glyph, state = statusGlyph(pu), format.Status(pu.Status)
			if pu.FinalMark != nil {
				state += fmt.Sprintf(" %d", *pu.FinalMark)
			}
		}
		name := fuzzy.ShortSlug(p.Slug)
		if info, _ := t.Lookup(p.Slug); len(info.Choice) > 1 {
			var others []string
			for _, o := range info.Choice {
				if o != p.Slug {
					others = append(others, fuzzy.ShortSlug(o))
				}
			}
			name += " (or " + strings.Join(others, ", ") + ")"
		}
		xp := ""
		if p.Difficulty > 0 {
			xp = fmt.Sprintf("%dxp", p.Difficulty)
		}
		fmt.Fprintf(w, "  %s %s\t%s\t%s\n", glyph, name, state, xp)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if hidden > 0 {
		fmt.Fprintf(a.out, "\n%s: %d projects (--after to list them)\n", api.CircleName(api.AfterCore), hidden)
	}
	variant := ""
	if t.Variant != "" {
		variant = ", curriculum " + t.Variant
	}
	fmt.Fprintf(a.out, "(holy graph of %s%s; --all for your other cursus)\n", t.Generated, variant)
	return nil
}

func listUpcoming(cmd *cobra.Command, a *app, c *api.Client, me *api.User, quests []string, modules bool) error {
	campus := me.PrimaryCampus()
	if campus == nil {
		return fmt.Errorf("no campus on your profile")
	}
	mainID := api.DefaultCursusID
	if cu := me.MainCursus(); cu != nil {
		mainID = cu.Cursus.ID
	}
	ps, err := c.Curriculum(cmd.Context(), mainID, campus.ID, quests)
	if err != nil {
		return err
	}
	t := userGraph(me, quests)
	regs := registrations(me, t)
	var next []api.Project
	for _, p := range ps {
		// On a graph, modules are part of it (42next's Python planets).
		if regs[p.Slug] == nil && (modules || t != nil || !p.OptionalModule()) {
			next = append(next, p)
		}
	}
	if a.flags.json {
		return a.printJSON(next)
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "XP\tPROJECT\tESTIMATED\tTEAM")
	for _, p := range next {
		est, team := "", ""
		if s := p.SessionFor(campus.ID, mainID); s != nil {
			if s.EstimateTime != nil {
				est = *s.EstimateTime
			}
			switch {
			case s.Solo != nil && *s.Solo:
				team = "solo"
			case s.MaxPeople != nil && *s.MaxPeople > 1:
				team = fmt.Sprintf("up to %d", *s.MaxPeople)
			case s.Solo != nil:
				team = "group"
			}
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", p.Difficulty, fuzzy.ShortSlug(p.Slug), est, team)
	}
	return w.Flush()
}

func statusGlyph(pu *api.ProjectsUser) string {
	switch {
	case pu.Validated != nil && *pu.Validated:
		return "✓"
	case pu.Status == "finished":
		return "✗"
	case pu.Status == "in_progress":
		return "●"
	}
	return "·"
}
