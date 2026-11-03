package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/config"
	"github.com/swilmus/ft/internal/format"
	"github.com/swilmus/ft/internal/fuzzy"
	"github.com/swilmus/ft/internal/gitx"
)

func newLinkCmd(a *app) *cobra.Command {
	var here bool
	cmd := &cobra.Command{
		Use:               "link <project> [folder]",
		Aliases:           []string{"l"},
		ValidArgsFunction: completeProject(a, true),
		Short:             "Clone your team's repository for a project and link the folder",
		Long: `Create the project folder as a clone of your team's vogsphere repository
(./libft for libft by default) and link it to the project with a .ft.toml
file, which other commands use as context and which is never pushed.

With --here, link the current folder instead: ft adds the vogsphere remote
(origin, or vogsphere if origin is taken), running git init first if needed.

The project can be a slug (42cursus-libft), a short slug (libft), a name or
an id. You must be registered and have a team with a repository.`,
		Example: "  ft link libft\n  ft link get_next_line gnl\n  ft link libft --here",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if here && len(args) > 1 {
				return errors.New("give a folder or --here, not both")
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
			ref, err := c.ResolveProject(ctx, me, args[0])
			if err != nil {
				return err
			}
			embedded := me.ProjectsUser(ref.ID)
			if embedded == nil {
				return fmt.Errorf("you are not registered to %s yet: register on the intra (%s), then run `ft link` again", ref.Slug, api.ProjectPageURL(ref.Slug))
			}
			pu, err := c.ProjectsUser(ctx, embedded.ID)
			if err != nil {
				return err
			}
			team := pu.CurrentTeam()
			if team == nil || team.RepoURL == "" {
				return fmt.Errorf("%s has no repository yet: create and lock your team on the intra (%s)", ref.Slug, api.ProjectPageURL(ref.Slug))
			}

			dir := fuzzy.ShortSlug(ref.Slug)
			switch {
			case here:
				dir = "."
			case len(args) > 1:
				dir = args[1]
			}
			dir, err = filepath.Abs(dir)
			if err != nil {
				return err
			}
			action, err := prepareRepo(cmd, dir, team.RepoURL, here)
			if err != nil {
				return err
			}

			link := &config.Link{ProjectID: ref.ID, Slug: ref.Slug, Name: ref.Name, TeamID: team.ID}
			previous, _ := config.ReadLink(filepath.Join(dir, config.LinkFile))
			if err := config.WriteLink(dir, link); err != nil {
				return err
			}
			if _, err := gitx.Exclude(dir, config.LinkFile); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not add %s to .git/info/exclude: %v\n", config.LinkFile, err)
			}

			if a.flags.json {
				return a.printJSON(struct {
					*config.Link
					Dir  string `json:"dir"`
					Repo string `json:"repo_url"`
				}{link, dir, team.RepoURL})
			}
			if previous != nil && previous.ProjectID != link.ProjectID {
				fmt.Fprintf(a.out, "Relinked (was %s).\n", previous.Slug)
			}
			fmt.Fprintf(a.out, "%s\nLinked %s to %s (%s).\n", action, format.TildePath(dir), ref.Name, ref.Slug)
			if !here {
				if rel, err := filepath.Rel(mustGetwd(), dir); err == nil {
					fmt.Fprintf(a.out, "Next: cd %s\n", rel)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&here, "here", false, "link the current folder instead of cloning into a new one")
	return cmd
}

// prepareRepo makes dir a git repository pointing to repoURL and describes
// what it did. Without --here it clones into a new or empty folder, or
// reuses an existing clone; with --here it initializes and adds the remote.
func prepareRepo(cmd *cobra.Command, dir, repoURL string, here bool) (string, error) {
	top := gitx.Toplevel(dir)
	isRoot := top != "" && sameDir(top, dir)
	if top != "" && !isRoot {
		return "", fmt.Errorf("%s is inside the git repository %s: link its root, or a folder outside it", format.TildePath(dir), format.TildePath(top))
	}

	empty, exists := dirState(dir)
	switch {
	case isRoot:
		name, added, err := gitx.EnsureRemote(dir, repoURL)
		if err != nil {
			return "", err
		}
		if added {
			return fmt.Sprintf("Added remote %q → %s.", name, repoURL), nil
		}
		return fmt.Sprintf("Remote %q already points to vogsphere.", name), nil
	case here:
		if err := gitx.Init(dir); err != nil {
			return "", err
		}
		name, _, err := gitx.EnsureRemote(dir, repoURL)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Initialized git and added remote %q → %s.", name, repoURL), nil
	case exists && !empty:
		return "", fmt.Errorf("%s already exists and is not empty: run `ft link <project> --here` inside it, or choose another folder", format.TildePath(dir))
	}
	if err := gitx.Clone(repoURL, dir, cmd.ErrOrStderr(), cmd.ErrOrStderr()); err != nil {
		return "", err
	}
	return "Cloned " + repoURL + ".", nil
}

// dirState reports whether dir is empty and whether it exists.
func dirState(dir string) (empty, exists bool) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, false
	}
	return err == nil && len(entries) == 0, true
}

func sameDir(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
