package commands

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/config"
	"github.com/skyreks00/ft/internal/format"
	"github.com/skyreks00/ft/internal/fuzzy"
	"github.com/skyreks00/ft/internal/slots42"
)

// Shell completion. Cobra does not run PersistentPreRunE for completion
// requests, so these load the configuration themselves, and they rely on
// the response cache to stay fast.

type completeFunc = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)

const noFiles = cobra.ShellCompDirectiveNoFileComp

// completionClient returns a client and profile for a completion request,
// bounded by a short timeout; callers must call cancel.
func (a *app) completionClient(cmd *cobra.Command) (ctx context.Context, cancel func(), c *api.Client, me *api.User, ok bool) {
	ctx = cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
	if a.cfg == nil {
		cfg, err := config.Load()
		if err != nil {
			return ctx, cancel, nil, nil, false
		}
		a.cfg = cfg
	}
	c, err := a.client(ctx)
	if err != nil {
		return ctx, cancel, nil, nil, false
	}
	me, err = c.Me(ctx)
	return ctx, cancel, c, me, err == nil
}

// completeProject offers the user's projects (short slugs) as the first
// argument; when dirs is set, folders are offered for the second.
func completeProject(a *app, dirs bool) completeFunc {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			if dirs && len(args) == 1 {
				if here, _ := cmd.Flags().GetBool("here"); here {
					return nil, noFiles // --here takes no folder
				}
				// A folder, or an option (--here): offered together.
				return append(localFlags(cmd, prefix), subdirs(prefix)...), noFiles
			}
			return nil, noFiles
		}
		_, cancel, _, me, ok := a.completionClient(cmd)
		defer cancel()
		if !ok {
			return nil, noFiles
		}
		var out []string
		for _, pu := range me.ProjectsUsers {
			if pu.Project.ParentID != nil {
				continue
			}
			short := fuzzy.ShortSlug(pu.Project.Slug)
			if strings.HasPrefix(short, prefix) {
				out = append(out, short+"\t"+pu.Project.Name+" ("+format.Status(pu.Status)+")")
			}
		}
		return out, noFiles
	}
}

// completeSlotID offers the user's slot ids, from the campus platform when
// connected.
func completeSlotID(a *app) completeFunc {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, noFiles
		}
		ctx, cancel, c, me, ok := a.completionClient(cmd)
		defer cancel()
		if !ok {
			return nil, noFiles
		}
		var out []string
		add := func(id int, begin, end time.Time) {
			if s := strconv.Itoa(id); strings.HasPrefix(s, prefix) {
				out = append(out, s+"\t"+slotRange(begin, end))
			}
		}
		if api.SlotsPlatform(a.cfg.SlotsURL, me) != "" {
			sess, err := slots42.LoadSession(a.cfg.Dir)
			if err != nil {
				return nil, noFiles
			}
			now := time.Now()
			slots, err := slots42.New(sess).MySlots(ctx, now, now.Add(14*24*time.Hour))
			if err != nil {
				return nil, noFiles
			}
			for _, s := range slots {
				if !s.Reserved {
					add(s.ID, s.Start, s.End)
				}
			}
			return out, noFiles
		}
		slots, err := c.MySlots(ctx)
		if err != nil {
			return nil, noFiles
		}
		for _, r := range api.GroupSlots(slots) {
			if !r.Booked {
				add(r.ID(), r.BeginAt, r.EndAt)
			}
		}
		return out, noFiles
	}
}

// completeEventID offers campus events (or only joined ones, for leave).
func completeEventID(a *app, joinedOnly bool) completeFunc {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, noFiles
		}
		ctx, cancel, _, _, ok := a.completionClient(cmd)
		defer cancel()
		if !ok {
			return nil, noFiles
		}
		rows, err := loadEvents(ctx, a, joinedOnly)
		if err != nil {
			return nil, noFiles
		}
		var out []string
		for _, r := range rows {
			if s := strconv.Itoa(r.ID); strings.HasPrefix(s, prefix) {
				out = append(out, fmt.Sprintf("%s\t%s, %s", s, r.Name, r.BeginAt.Local().Format("Mon 02 Jan 15:04")))
			}
		}
		return out, noFiles
	}
}

// withFlagCompletion makes Tab offer a command's options once it has no
// argument to complete, without typing "-" first (like the TUI palette).
func withFlagCompletion(root *cobra.Command) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			orig := sub.ValidArgsFunction
			sub.ValidArgsFunction = func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
				if orig != nil {
					if comps, dir := orig(cmd, args, prefix); len(comps) > 0 || dir&cobra.ShellCompDirectiveFilterDirs != 0 {
						return comps, dir
					}
				}
				return localFlags(cmd, prefix), noFiles
			}
			walk(sub)
		}
	}
	walk(root)
}

// localFlags lists a command's own options not given yet, as "--name\tusage".
func localFlags(cmd *cobra.Command, prefix string) []string {
	var out []string
	cmd.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
		name := "--" + f.Name
		if f.Hidden || f.Name == "help" || f.Changed || !strings.HasPrefix(name, prefix) {
			return
		}
		out = append(out, name+"\t"+f.Usage)
	})
	return out
}

// subdirs lists the folders matching a path prefix, with a trailing slash.
func subdirs(prefix string) []string {
	dir, base := filepath.Split(prefix)
	entries, err := os.ReadDir(cmp.Or(dir, "."))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), base) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, dir+e.Name()+"/")
		}
	}
	return out
}
