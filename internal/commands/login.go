package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/auth"
	"github.com/swilmus/ft/internal/config"
	"github.com/swilmus/ft/internal/slots42"
)

func newLoginCmd(a *app) *cobra.Command {
	var noBrowser, probePKCE bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to the 42 intra in your browser",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.cfg.Validate(); err != nil {
				return err
			}
			if err := a.cfg.EnsureDir(); err != nil {
				return err
			}
			tok, err := auth.Login(cmd.Context(), auth.LoginOptions{
				ClientID:     a.cfg.ClientID,
				APIBaseURL:   a.cfg.APIBaseURL,
				Scopes:       config.Scopes,
				RedirectURIs: a.cfg.RedirectURIs,
				Broker:       a.broker(),
				Out:          cmd.ErrOrStderr(),
				NoBrowser:    noBrowser,
				ProbePKCE:    probePKCE,
			})
			if err != nil {
				return err
			}
			if err := a.store().Save(tok); err != nil {
				return fmt.Errorf("saving token: %w", err)
			}
			// A new session may be a different user: drop cached responses.
			_ = a.cache().Clear()

			c, err := a.client(cmd.Context())
			if err != nil {
				return err
			}
			me, err := c.Me(cmd.Context())
			if err != nil {
				return fmt.Errorf("logged in, but fetching your profile failed: %w", err)
			}
			fmt.Fprintf(a.out, "Logged in as %s.\n", me.Login)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the login URL instead of opening a browser")
	cmd.Flags().BoolVar(&probePKCE, "probe-pkce", false, "check whether 42 accepts a secret-less PKCE exchange")
	_ = cmd.Flags().MarkHidden("probe-pkce")
	return cmd
}

func newLogoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored session and cached data",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.store().Delete(); err != nil {
				return err
			}
			if err := a.cache().Clear(); err != nil {
				return err
			}
			if err := slots42.DeleteSession(a.cfg.Dir); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Logged out.")
			return nil
		},
	}
}
