package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/format"
	"github.com/swilmus/ft/internal/slots42"
)

func newSlotsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "slots",
		Aliases: []string{"sl"},
		Short:   "List, open and remove your evaluation slots",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listSlots(cmd, a)
		},
	}
	cmd.AddCommand(
		newSlotsAuthCmd(a),
		&cobra.Command{
			Use:   "ls",
			Short: "List your upcoming slots",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return listSlots(cmd, a)
			},
		},
		newSlotsAddCmd(a),
		&cobra.Command{
			Use:               "rm <id>",
			Aliases:           []string{"r", "del"},
			ValidArgsFunction: completeSlotID(a),
			Short:             "Remove a slot (the whole range shown by `ft slots`)",
			Args:              cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				id, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid slot id %q", args[0])
				}
				ctx := cmd.Context()
				c, err := a.client(ctx)
				if err != nil {
					return err
				}
				if p, ok, err := platformClient(cmd, a, c); err != nil {
					return err
				} else if ok {
					return removePlatformSlot(cmd, a, p, id)
				}
				slots, err := c.MySlots(api.WithNoCache(ctx))
				if err != nil {
					return err
				}
				r, ok := findRange(api.GroupSlots(slots), id)
				if !ok {
					return fmt.Errorf("no upcoming slot with id %d (see `ft slots`)", id)
				}
				if r.Booked {
					return errors.New("this slot is booked: an evaluation is planned, cancel it on the intra")
				}
				for _, chunk := range r.IDs {
					if err := c.DeleteSlot(ctx, chunk); err != nil {
						return err
					}
				}
				fmt.Fprintf(a.out, "Removed %s.\n", slotRange(r.BeginAt, r.EndAt))
				return nil
			},
		},
	)
	return cmd
}

func listSlots(cmd *cobra.Command, a *app) error {
	c, err := a.client(cmd.Context())
	if err != nil {
		return err
	}
	if me, err := c.Me(cmd.Context()); err == nil && api.SlotsPlatform(a.cfg.SlotsURL, me) != "" {
		sess, err := slots42.LoadSession(a.cfg.Dir)
		if err == nil {
			return listPlatformSlots(cmd, a, slots42.New(sess))
		}
		if !errors.Is(err, slots42.ErrNoSession) {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), "Tip: run `ft slots auth` to list your slots from the campus platform here.")
	}
	slots, err := c.MySlots(cmd.Context())
	if err != nil {
		return err
	}
	ranges := api.GroupSlots(slots)
	if a.flags.json {
		return a.printJSON(ranges)
	}
	platform := ""
	if me, err := c.Me(cmd.Context()); err == nil {
		platform = api.SlotsPlatform(a.cfg.SlotsURL, me)
	}
	if len(ranges) == 0 {
		if platform != "" {
			fmt.Fprintf(a.out, "No intra slots. Your campus manages slots on %s (`ft slots add` opens it).\n", platform)
		} else {
			fmt.Fprintln(a.out, "No upcoming slots. Open one with `ft slots add 14:00 16:00`.")
		}
		return nil
	}
	if platform != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: these are intra slots; your campus deletes them and uses %s.\n", platform)
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tWHEN\tSTATE")
	for _, r := range ranges {
		state := "free"
		if r.Booked {
			state = "booked"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", r.ID(), slotRange(r.BeginAt, r.EndAt), state)
	}
	return w.Flush()
}

// listPlatformSlots prints the user's slots on the campus platform for the
// two weeks it lets you plan.
func listPlatformSlots(cmd *cobra.Command, a *app, p *slots42.Client) error {
	now := time.Now()
	slots, err := p.MySlots(cmd.Context(), now, now.Add(14*24*time.Hour))
	if err != nil {
		return err
	}
	if a.flags.json {
		return a.printJSON(slots)
	}
	host := strings.TrimPrefix(p.Session.BaseURL, "https://")
	if len(slots) == 0 {
		fmt.Fprintf(a.out, "No upcoming slots on %s.\n", host)
		return nil
	}
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tWHEN\tCAMPUS\tSTATE")
	for _, s := range slots {
		campus := slots42.CampusName(s.Campus)
		if s.Remote {
			campus += " (inter-campus)"
		}
		state := "free"
		if s.Reserved {
			state = "booked"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", s.ID, slotRange(s.Start, s.End), campus, state)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "From %s (unofficial).\n", host)
	return nil
}

func newSlotsAddCmd(a *app) *cobra.Command {
	var campus string
	var remote bool
	cmd := &cobra.Command{
		Use:     "add <start> <end>",
		Aliases: []string{"a"},
		Short:   "Open a slot",
		Long: `Open an evaluation slot, in local time, on quarter hours.
start: 14:00 (today, or tomorrow if past), "tomorrow 14:00", "wed 14:00", "2026-11-03 14:00"
end:   16:00, +2h, +90m, or a full date

On a campus slots platform (after ` + "`ft slots auth`" + `), the slot uses your
campus and inter-campus settings from the platform unless --campus or
--inter-campus is given.`,
		Example: "  ft slots add 14:00 16:00\n  ft slots add \"tomorrow 9:00\" +2h --inter-campus=false",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			begin, end, err := format.ParseRange(args[0], args[1], time.Now())
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			p, ok, err := platformClient(cmd, a, c)
			if err != nil {
				return err
			}
			if !ok {
				// Intra slots (campuses without a platform).
				me, err := c.Me(ctx)
				if err != nil {
					return err
				}
				created, err := c.CreateSlot(ctx, me.ID, begin, end)
				if err != nil {
					return err
				}
				if a.flags.json {
					return a.printJSON(api.GroupSlots(created))
				}
				fmt.Fprintf(a.out, "Opened %s.\n", slotRange(begin, end))
				return nil
			}

			prefs, err := p.Prefs(ctx)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("campus") {
				prefs.Campus = campus
			}
			if cmd.Flags().Changed("inter-campus") {
				prefs.Remote = remote
			}
			if err := p.Create(ctx, prefs, begin, end); err != nil {
				return err
			}
			where := slots42.CampusName(prefs.Campus)
			if prefs.Remote {
				where += ", inter-campus"
			}
			fmt.Fprintf(a.out, "Opened %s (%s) on %s.\n", slotRange(begin, end), where, strings.TrimPrefix(p.Session.BaseURL, "https://"))
			return nil
		},
	}
	cmd.Flags().StringVar(&campus, "campus", "", "campus of the slot: bx (Brussels) or anr (Antwerp)")
	cmd.Flags().BoolVar(&remote, "inter-campus", false, "allow an evaluation with the other campus")
	return cmd
}

// platformClient returns the campus slots platform client when the user's
// campus has one (ok is false for intra campuses). It fails when the
// campus has a platform but `ft slots auth` was never run.
func platformClient(cmd *cobra.Command, a *app, c *api.Client) (*slots42.Client, bool, error) {
	me, err := c.Me(cmd.Context())
	if err != nil {
		return nil, false, err
	}
	platform := api.SlotsPlatform(a.cfg.SlotsURL, me)
	if platform == "" {
		return nil, false, nil
	}
	sess, err := slots42.LoadSession(a.cfg.Dir)
	if errors.Is(err, slots42.ErrNoSession) {
		return nil, false, fmt.Errorf("your campus manages slots on %s: run `ft slots auth` to manage them from ft", platform)
	}
	if err != nil {
		return nil, false, err
	}
	return slots42.New(sess), true, nil
}

func removePlatformSlot(cmd *cobra.Command, a *app, p *slots42.Client, id int) error {
	ctx := cmd.Context()
	now := time.Now()
	slots, err := p.MySlots(ctx, now, now.Add(14*24*time.Hour))
	if err != nil {
		return err
	}
	for _, s := range slots {
		if s.ID != id {
			continue
		}
		prefs, err := p.Prefs(ctx)
		if err != nil {
			return err
		}
		if err := p.Delete(ctx, prefs, s); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Removed %s.\n", slotRange(s.Start, s.End))
		return nil
	}
	return fmt.Errorf("no upcoming slot with id %d (see `ft slots`)", id)
}

func newSlotsAuthCmd(a *app) *cobra.Command {
	var remove bool
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Connect ft to your campus slots platform with your browser session",
		Long: `Store your browser session for the campus slots platform (e.g.
slots.42belgium.be), so ft can read your slots there. This is unofficial: it
uses the platform's web endpoints and breaks if the site changes or the
session expires. The cookie is stored in the config directory (0600).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remove {
				if err := slots42.DeleteSession(a.cfg.Dir); err != nil {
					return err
				}
				fmt.Fprintln(a.out, "Removed the slots platform session.")
				return nil
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
			platform := api.SlotsPlatform(a.cfg.SlotsURL, me)
			if platform == "" {
				return errors.New("your campus manages slots on the intra: nothing to connect")
			}
			host := strings.TrimPrefix(platform, "https://")
			fmt.Fprintf(cmd.ErrOrStderr(), `1. Open %s in your browser and log in.
2. Open the developer tools (F12) → Storage (Firefox) or Application (Chrome)
   → Cookies → %s.
3. Copy the value of the "sessionid" cookie and paste it below (hidden).

Cookie: `, platform, host)
			pasted, err := readSecret(cmd)
			fmt.Fprintln(cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			cookie, err := slots42.CookieHeader(pasted)
			if err != nil {
				return err
			}
			sess := &slots42.Session{BaseURL: platform, Cookie: cookie, SavedAt: time.Now()}
			if err := slots42.New(sess).Check(ctx); err != nil {
				if errors.Is(err, slots42.ErrExpired) {
					return fmt.Errorf("%s rejected this cookie: copy the sessionid of %s while logged in", host, host)
				}
				return err
			}
			if err := sess.Save(a.cfg.Dir); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Connected to %s. `ft slots`, `ft slots add` and `ft slots rm` now use it.\n", host)
			return nil
		},
	}
	cmd.Flags().BoolVar(&remove, "remove", false, "forget the stored session")
	return cmd
}

// readSecret reads one line without echo when stdin is a terminal.
func readSecret(cmd *cobra.Command) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		return string(b), err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return line, nil
}

func findRange(ranges []api.SlotRange, id int) (api.SlotRange, bool) {
	for _, r := range ranges {
		if slices.Contains(r.IDs, id) {
			return r, true
		}
	}
	return api.SlotRange{}, false
}

// slotRange formats "Tue 03 Nov 14:00–16:00".
func slotRange(begin, end time.Time) string {
	b, e := begin.Local(), end.Local()
	if b.YearDay() == e.YearDay() {
		return b.Format("Mon 02 Jan 15:04") + "–" + e.Format("15:04")
	}
	return b.Format("Mon 02 Jan 15:04") + " – " + e.Format("Mon 02 Jan 15:04")
}
