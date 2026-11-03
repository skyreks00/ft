package commands

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skyreks00/ft/internal/gitx"
)

// gitEnv isolates git from the user's configuration.
func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "ft test")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "ft@example.com")
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSubmitGitHelpers(t *testing.T) {
	gitEnv(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	work := t.TempDir()
	git(t, work, "init", "--bare", "-q", "-b", "main", remote)
	git(t, work, "init", "-q", "-b", "dev")

	// Empty remote: no HEAD branch yet, so master.
	if b, err := remoteDefaultBranch(work, remote); err != nil || b != "master" {
		t.Fatalf("empty remote branch = %q, %v", b, err)
	}

	os.WriteFile(filepath.Join(work, "ft_strlen.c"), []byte("int ft_strlen(void);\n"), 0o644)
	git(t, work, "add", ".")
	git(t, work, "commit", "-q", "-m", "first")
	if n := dirtyFiles(work); n != 0 {
		t.Fatalf("clean tree has %d dirty files", n)
	}
	os.WriteFile(filepath.Join(work, "ft_strlen.c"), []byte("changed\n"), 0o644)
	os.WriteFile(filepath.Join(work, "new.c"), []byte("x\n"), 0o644)
	if n := dirtyFiles(work); n != 2 {
		t.Fatalf("dirtyFiles = %d, want 2", n)
	}

	if err := gitPush(work, remote, "main", io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := git(t, work, "--git-dir", remote, "log", "-1", "--format=%s", "main"); got != "first" {
		t.Fatalf("remote main = %q", got)
	}
	if b, err := remoteDefaultBranch(work, remote); err != nil || b != "main" {
		t.Fatalf("remote branch = %q, %v", b, err)
	}
	// ft never adds a remote.
	if r := git(t, work, "remote"); r != "" {
		t.Fatalf("remotes = %q", r)
	}

	if _, err := remoteDefaultBranch(work, filepath.Join(t.TempDir(), "missing.git")); err == nil {
		t.Fatal("unreachable remote: expected error")
	}
}

func TestTrackedAndExclude(t *testing.T) {
	gitEnv(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	os.WriteFile(filepath.Join(dir, ".ft.toml"), []byte("x"), 0o644)

	added, err := gitx.Exclude(dir, ".ft.toml")
	if err != nil || !added {
		t.Fatalf("gitExclude = %v, %v", added, err)
	}
	if added, _ := gitx.Exclude(dir, ".ft.toml"); added {
		t.Fatal("pattern added twice")
	}
	if st := git(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf(".ft.toml not ignored: %q", st)
	}
	if tracked(dir, ".ft.toml") {
		t.Fatal("untracked file reported as tracked")
	}
	git(t, dir, "add", "-f", ".ft.toml")
	if !tracked(dir, ".ft.toml") {
		t.Fatal("tracked file not detected")
	}

	if added, err := gitx.Exclude(t.TempDir(), ".ft.toml"); added || err != nil {
		t.Fatalf("outside a repo: %v, %v", added, err)
	}
}

// fakeNorminette installs a norminette that flags files containing BAD.
func fakeNorminette(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	script := `#!/bin/sh
s=0
for f in "$@"; do
  if grep -q BAD "$f"; then echo "$f: Error!"; echo "Error: TOO_MANY_LINES (line: 1, col: 1): Function has more than 25 lines"; s=1
  else echo "$f: OK!"; fi
done
exit $s
`
	if err := os.WriteFile(filepath.Join(bin, "norminette"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunNormChecksHead(t *testing.T) {
	gitEnv(t)
	fakeNorminette(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	os.WriteFile(filepath.Join(dir, "src", "ft_bad.c"), []byte("BAD\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "ft_ok.c"), []byte("int x;\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("BAD but not C\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "first")

	// Fixed in the working tree but not committed: HEAD still fails.
	os.WriteFile(filepath.Join(dir, "src", "ft_bad.c"), []byte("fixed\n"), 0o644)

	r, err := runNorm(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Ran || r.Files != 2 || len(r.Failed) != 1 || r.Failed[0] != "src/ft_bad.c" {
		t.Fatalf("got %+v", r)
	}
	if !strings.Contains(r.Report, "TOO_MANY_LINES") || strings.Contains(r.Report, "OK!") {
		t.Fatalf("report = %q", r.Report)
	}

	git(t, dir, "commit", "-q", "-am", "fix")
	if r, err := runNorm(dir); err != nil || len(r.Failed) != 0 || !r.Ran {
		t.Fatalf("after fix: %+v %v", r, err)
	}
}

func TestRunNormSkips(t *testing.T) {
	gitEnv(t)
	dir := t.TempDir()
	git(t, dir, "init", "-q")
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("print(1)\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "py")
	if r, err := runNorm(dir); err != nil || r.Ran {
		t.Fatalf("no C files: %+v %v", r, err)
	}

	os.WriteFile(filepath.Join(dir, "a.c"), []byte("int x;\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "c")
	gitDir := filepath.Dir(mustLookPath(t, "git"))
	t.Setenv("PATH", gitDir) // git, but no norminette in its folder
	if _, err := exec.LookPath("norminette"); err == nil {
		t.Skip("norminette installed next to git")
	}
	if _, err := runNorm(dir); err != errNoNorminette {
		t.Fatalf("err = %v, want errNoNorminette", err)
	}
}

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " not installed")
	}
	return p
}
