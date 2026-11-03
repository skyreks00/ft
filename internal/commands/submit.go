package commands

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/config"
	"github.com/skyreks00/ft/internal/gitx"
)

func newSubmitCmd(a *app) *cobra.Command {
	var yes, dryRun, noNorm bool
	var branch string
	cmd := &cobra.Command{
		Use:     "submit",
		Aliases: []string{"s"},
		Short:   "Push the current commit to the team's vogsphere repository",
		Long: `Push HEAD to the linked project's repository, fetched from the API.
Only committed work is pushed.

When the commit has .c or .h files and norminette is installed, ft checks
them first and asks before pushing code that fails the norm (with --yes it
refuses; pass --no-norm to skip the check).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			link, linkDir, err := config.FindLink(cwd)
			if err != nil {
				return err
			}
			top, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
			if err != nil {
				return errors.New("not a git repository: run `git init`, then commit your work")
			}
			if tracked(linkDir, config.LinkFile) {
				return fmt.Errorf("%s is committed: run `git rm --cached %s` and commit, so it is not pushed", config.LinkFile, config.LinkFile)
			}
			head, err := gitOutput(top, "log", "-1", "--format=%h %s")
			if err != nil {
				return errors.New("nothing to push: make a first commit")
			}

			c, err := a.client(ctx)
			if err != nil {
				return err
			}
			me, err := c.Me(ctx)
			if err != nil {
				return err
			}
			embedded := me.ProjectsUser(link.ProjectID)
			if embedded == nil {
				return fmt.Errorf("not registered to %s: register on the intra (%s)", link.Slug, api.ProjectPageURL(link.Slug))
			}
			pu, err := c.ProjectsUser(ctx, embedded.ID)
			if err != nil {
				return err
			}
			team := pu.CurrentTeam()
			if team == nil || team.RepoURL == "" {
				return fmt.Errorf("%s has no repository yet: create and lock your team on the intra (%s)", link.Slug, api.ProjectPageURL(link.Slug))
			}

			if branch == "" {
				if branch, err = remoteDefaultBranch(top, team.RepoURL); err != nil {
					return err
				}
			}
			fmt.Fprintf(a.out, "Repo    %s\nCommit  %s\nBranch  %s\n", team.RepoURL, head, branch)
			if n := dirtyFiles(top); n > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %d uncommitted change(s) will NOT be pushed\n", n)
			}
			if team.Closed {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: this team is closed; evaluations use the repository as it was when it closed")
			}
			prompt := "Push to vogsphere?"
			if !noNorm {
				failed, err := checkNorm(cmd, a, top)
				if err != nil {
					return err
				}
				if failed > 0 {
					if yes && !dryRun {
						return fmt.Errorf("%d file(s) fail the norm: fix them, or pass --no-norm", failed)
					}
					prompt = fmt.Sprintf("%d file(s) fail the norm. Push anyway?", failed)
				}
			}
			if dryRun {
				return nil
			}
			if err := confirm(cmd, yes, prompt); err != nil {
				return err
			}
			if err := gitPush(top, team.RepoURL, branch, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Pushed. Book evaluations with `ft correct`.")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be pushed, then stop")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "remote branch (default: the repository's default branch, else master)")
	cmd.Flags().BoolVar(&noNorm, "no-norm", false, "skip the norminette check")
	return cmd
}

// checkNorm prints the norminette report for HEAD and returns how many
// files fail. A missing norminette is a warning, not an error.
func checkNorm(cmd *cobra.Command, a *app, top string) (int, error) {
	r, err := runNorm(top)
	if errors.Is(err, errNoNorminette) {
		fmt.Fprintln(cmd.ErrOrStderr(), "note:", err)
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("norminette: %w", err)
	}
	if !r.Ran {
		return 0, nil
	}
	if len(r.Failed) == 0 {
		fmt.Fprintf(a.out, "Norm    OK (%d files)\n", r.Files)
		return 0, nil
	}
	fmt.Fprintf(a.out, "Norm    %d/%d files fail\n%s\n", len(r.Failed), r.Files, r.Report)
	return len(r.Failed), nil
}

func gitOutput(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func tracked(dir, file string) bool {
	return exec.Command("git", "-C", dir, "ls-files", "--error-unmatch", file).Run() == nil
}

func dirtyFiles(dir string) int {
	out, err := gitOutput(dir, "status", "--porcelain")
	if err != nil || out == "" {
		return 0
	}
	return strings.Count(out, "\n") + 1
}

// remoteDefaultBranch asks the remote for its HEAD branch. An empty
// repository (the usual state before a first push) defaults to master.
// stdin is passed through so ssh can ask about host keys or passphrases.
func remoteDefaultBranch(dir, repoURL string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "ls-remote", "--symref", repoURL, "HEAD")
	cmd.Stdin = os.Stdin
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if gitx.IsKeyDenied(msg) {
			return "", gitx.ErrKeyDenied
		}
		hint := "is your SSH key added on the intra?"
		if strings.Contains(msg, "Host key verification failed") {
			hint = "unknown host key: run `ssh -T " + sshHost(repoURL) + "` once to accept it"
		}
		return "", fmt.Errorf("cannot reach %s (%s):\n%s", repoURL, hint, msg)
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		if ref, ok := strings.CutPrefix(sc.Text(), "ref: refs/heads/"); ok {
			if name, _, ok := strings.Cut(ref, "\t"); ok {
				return name, nil
			}
		}
	}
	return "master", nil
}

// sshHost returns "git@host" from an scp-like URL "git@host:path".
func sshHost(repoURL string) string {
	host, _, _ := strings.Cut(repoURL, ":")
	return host
}

func gitPush(dir, repoURL, branch string, stdout, stderr interface{ Write([]byte) (int, error) }) error {
	return gitx.Run(exec.Command("git", "-C", dir, "push", repoURL, "HEAD:refs/heads/"+branch), "git push", stdout, stderr)
}
