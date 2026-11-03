package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func quietCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	return c
}

func TestPrepareRepo(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := filepath.Join(root, "team.git")
	git(t, root, "init", "--bare", "-q", remote)

	// New folder: cloned, origin points to vogsphere.
	libft := filepath.Join(root, "libft")
	msg, err := prepareRepo(quietCmd(), libft, remote, false)
	if err != nil || !strings.HasPrefix(msg, "Cloned") {
		t.Fatalf("clone: %q %v", msg, err)
	}
	if got := git(t, libft, "remote", "get-url", "origin"); got != remote {
		t.Fatalf("origin = %q", got)
	}

	// Same folder again: the existing clone is reused.
	msg, err = prepareRepo(quietCmd(), libft, remote, false)
	if err != nil || !strings.Contains(msg, "already points") {
		t.Fatalf("reuse: %q %v", msg, err)
	}

	// Existing empty folder: cloned into.
	empty := filepath.Join(root, "empty")
	os.Mkdir(empty, 0o755)
	if _, err := prepareRepo(quietCmd(), empty, remote, false); err != nil {
		t.Fatalf("empty folder: %v", err)
	}

	// Non-empty, non-git folder: refused without --here.
	busy := filepath.Join(root, "busy")
	os.Mkdir(busy, 0o755)
	os.WriteFile(filepath.Join(busy, "main.c"), []byte("int main(void){}\n"), 0o644)
	if _, err := prepareRepo(quietCmd(), busy, remote, false); err == nil || !strings.Contains(err.Error(), "--here") {
		t.Fatalf("busy folder: %v", err)
	}

	// --here on it: git init + origin, files untouched.
	msg, err = prepareRepo(quietCmd(), busy, remote, true)
	if err != nil || !strings.Contains(msg, "Initialized") {
		t.Fatalf("--here: %q %v", msg, err)
	}
	if got := git(t, busy, "remote", "get-url", "origin"); got != remote {
		t.Fatalf("--here origin = %q", got)
	}
	if _, err := os.Stat(filepath.Join(busy, "main.c")); err != nil {
		t.Fatal("--here touched existing files")
	}

	// A subfolder of another repository is refused.
	sub := filepath.Join(libft, "src")
	os.Mkdir(sub, 0o755)
	if _, err := prepareRepo(quietCmd(), sub, remote, true); err == nil || !strings.Contains(err.Error(), "inside the git repository") {
		t.Fatalf("nested: %v", err)
	}
}
