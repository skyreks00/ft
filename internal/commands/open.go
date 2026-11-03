package commands

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/format"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/browser"
)

func newOpenCmd(a *app) *cobra.Command {
	var yes, web bool
	cmd := &cobra.Command{
		Use:               "open [project]",
		Aliases:           []string{"o"},
		ValidArgsFunction: completeProject(a, false),
		Short:             "Register to the linked project",
		Long: `Register to the linked project (or the one given).
If the intra refuses registration through the API, the project page is
opened in your browser instead. Teams are created and invited on the intra.`,
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
			page := api.ProjectPageURL(link.Slug)
			if web {
				return openPage(a, page)
			}

			if pu := me.ProjectsUser(link.ProjectID); pu != nil {
				fmt.Fprintf(a.out, "Already registered to %s: %s.\n", link.Slug, format.Status(pu.Status))
				return nil
			}
			if err := confirm(cmd, yes, fmt.Sprintf("Register %s to %s?", me.Login, link.Slug)); err != nil {
				return err
			}

			pu, err := c.Register(ctx, link.ProjectID, me.ID)
			var apiErr *api.Error
			if errors.As(err, &apiErr) && !a.flags.json {
				fmt.Fprintf(cmd.ErrOrStderr(), "The intra refused registration through the API (%v).\n", err)
				return openPage(a, page)
			}
			if err != nil {
				return err
			}
			if a.flags.json {
				return a.printJSON(pu)
			}
			fmt.Fprintf(a.out, "Registered to %s: %s.\n", link.Slug, format.Status(pu.Status))
			fmt.Fprintf(a.out, "Create or join a team on the intra if needed: %s\n", page)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&web, "web", false, "open the project page on the intra instead")
	return cmd
}

func openPage(a *app, u string) error {
	fmt.Fprintf(a.out, "Opening %s\n", u)
	if err := browser.Open(u); err != nil {
		return fmt.Errorf("could not open a browser: %w", err)
	}
	return nil
}
