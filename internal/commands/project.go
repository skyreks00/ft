package commands

import (
	"context"
	"os"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/config"
)

// linkedProject returns the project named by args[0], or the one linked to
// the current folder. dir is the folder holding .ft.toml ("" for args).
func linkedProject(ctx context.Context, c *api.Client, me *api.User, args []string) (link *config.Link, dir string, err error) {
	if len(args) > 0 {
		ref, err := c.ResolveProject(ctx, me, args[0])
		if err != nil {
			return nil, "", err
		}
		return &config.Link{ProjectID: ref.ID, Slug: ref.Slug, Name: ref.Name}, "", nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}
	return config.FindLink(cwd)
}
