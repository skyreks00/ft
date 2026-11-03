// Package commands implements the ft CLI.
package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
	"golang.org/x/term"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/auth"
	"github.com/skyreks00/ft/internal/config"
	"github.com/skyreks00/ft/internal/slots42"
	"github.com/skyreks00/ft/internal/tui"
)

type globalFlags struct {
	json    bool
	noCache bool
}

// app holds what commands need; built lazily so `ft --help` works without
// any configuration.
type app struct {
	flags *globalFlags
	cfg   *config.Config
	out   io.Writer
}

// NewRoot returns the root command.
func NewRoot() *cobra.Command {
	flags := &globalFlags{}
	a := &app{flags: flags}

	root := &cobra.Command{
		Use:           "ft",
		Short:         "A fast terminal client for the 42 intra",
		Version:       config.BuildVersion(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			a.cfg = cfg
			a.out = cmd.OutOrStdout()
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) || !term.IsTerminal(int(os.Stdin.Fd())) {
				return cmd.Help()
			}
			return runTUI(cmd.Context(), a)
		},
	}
	root.PersistentFlags().BoolVar(&flags.json, "json", false, "machine-readable JSON output")
	defer hideAliases(root)
	defer withFlagCompletion(root)
	root.PersistentFlags().BoolVar(&flags.noCache, "no-cache", false, "bypass the local response cache")

	root.AddCommand(
		newLoginCmd(a),
		newLogoutCmd(a),
		newMeCmd(a),
		newProjectsCmd(a),
		newLinkCmd(a),
		newStatusCmd(a),
		newOpenCmd(a),
		newSubmitCmd(a),
		newCorrectCmd(a),
		newSlotsCmd(a),
		newEventsCmd(a),
		newEvalCmd(a),
		newUserCmd(a),
		newLogtimeCmd(a),
		newAPICmd(a),
		newSnapshotCmd(a),
	)
	return root
}

// aliasesBlock is the "Aliases:" section of cobra's usage template.
const aliasesBlock = `{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}`

// hideAliases keeps the short forms (ft e, ft sl a…) out of the help; they
// work when typed but are not advertised, like vim's.
func hideAliases(root *cobra.Command) {
	root.SetUsageTemplate(strings.Replace(root.UsageTemplate(), aliasesBlock, "", 1))
}

// tuiOptions gathers what the TUI needs, with the folder's link if any.
func tuiOptions(ctx context.Context, a *app) (tui.Options, error) {
	c, err := a.client(ctx)
	if err != nil {
		return tui.Options{}, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return tui.Options{}, err
	}
	link, dir, err := config.FindLink(cwd)
	if err != nil && !errors.Is(err, config.ErrNotLinked) {
		return tui.Options{}, err
	}
	exe, _ := os.Executable()
	o := tui.Options{Client: c, Exe: exe, Link: link, LinkDir: dir, SlotsURL: a.cfg.SlotsURL,
		RemindBefore: a.cfg.RemindBefore, NotifyDesktop: a.cfg.Notify}
	if sess, err := slots42.LoadSession(a.cfg.Dir); err == nil {
		o.Platform = slots42.New(sess)
	}
	return o, nil
}

// runTUI opens the full-screen interface.
func runTUI(ctx context.Context, a *app) error {
	o, err := tuiOptions(ctx, a)
	if err != nil {
		return err
	}
	return tui.Run(ctx, o)
}

// newSnapshotCmd prints one TUI frame without a terminal (hidden; for
// screenshots and debugging).
func newSnapshotCmd(a *app) *cobra.Command {
	var width, height int
	var view string
	cmd := &cobra.Command{
		Use:    "snapshot",
		Short:  "Print one frame of the TUI",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := tuiOptions(cmd.Context(), a)
			if err != nil {
				return err
			}
			frame, err := tui.Snapshot(cmd.Context(), o, width, height, view)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, frame)
			return nil
		},
	}
	cmd.Flags().IntVar(&width, "width", 110, "columns")
	cmd.Flags().IntVar(&height, "height", 32, "rows")
	cmd.Flags().StringVar(&view, "view", "", "upcoming, events, eval, logtime, user:<login> or palette:<input>")
	return cmd
}

func (a *app) broker() *auth.Broker {
	return &auth.Broker{URL: a.cfg.BrokerURL}
}

func (a *app) store() *auth.Store {
	return auth.NewStore(a.cfg.Dir)
}

func (a *app) cache() *api.FileCache {
	return api.NewFileCache(filepath.Join(a.cfg.Dir, "cache"))
}

// client returns an authenticated API client.
func (a *app) client(ctx context.Context) (*api.Client, error) {
	if err := a.cfg.Validate(); err != nil {
		return nil, err
	}
	ts, err := auth.NewTokenSource(ctx, a.store(), a.broker())
	if err != nil {
		return nil, err
	}
	hc := oauth2.NewClient(ctx, ts)
	hc.Timeout = 30 * time.Second
	c := api.New(a.cfg.APIBaseURL, hc, a.cache())
	c.NoCache = a.flags.noCache
	return c, nil
}

func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := NewRoot()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(root.ErrOrStderr(), "ft:", err)
		return 1
	}
	return 0
}
