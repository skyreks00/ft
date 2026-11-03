package gitx

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitEnv(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCloneAndToplevel(t *testing.T) {
	gitEnv(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	run(t, root, "init", "--bare", "-q", remote)

	dst := filepath.Join(root, "libft")
	if err := Clone(remote, dst, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := run(t, dst, "remote", "get-url", "origin"); got != remote {
		t.Fatalf("origin = %q", got)
	}
	if top := Toplevel(filepath.Join(dst)); !sameDir(t, top, dst) {
		t.Fatalf("Toplevel = %q, want %q", top, dst)
	}
	if top := Toplevel(t.TempDir()); top != "" {
		t.Fatalf("Toplevel outside a repo = %q", top)
	}
	if err := Clone(filepath.Join(root, "missing.git"), filepath.Join(root, "x"), io.Discard, io.Discard); err == nil {
		t.Fatal("cloning a missing repo must fail")
	}
}

func TestEnsureRemote(t *testing.T) {
	gitEnv(t)
	dir := t.TempDir()
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	const vog = "git@vogsphere.example:team-1"

	name, added, err := EnsureRemote(dir, vog)
	if err != nil || name != "origin" || !added {
		t.Fatalf("first = %q %v %v", name, added, err)
	}
	name, added, err = EnsureRemote(dir, vog)
	if err != nil || name != "origin" || added {
		t.Fatalf("again = %q %v %v", name, added, err)
	}

	other := t.TempDir()
	Init(other)
	run(t, other, "remote", "add", "origin", "git@github.com:me/libft.git")
	name, added, err = EnsureRemote(other, vog)
	if err != nil || name != "vogsphere" || !added {
		t.Fatalf("origin taken = %q %v %v", name, added, err)
	}
	if got := run(t, other, "remote", "get-url", "origin"); got != "git@github.com:me/libft.git" {
		t.Fatalf("existing origin changed: %q", got)
	}
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra == rb
}

func TestRunReportsDeniedKey(t *testing.T) {
	denied := exec.Command("sh", "-c", "echo 'git@vogsphere: Permission denied (publickey).' >&2; exit 128")
	var shown strings.Builder
	err := Run(denied, "git clone", io.Discard, &shown)
	if err != ErrKeyDenied || !strings.Contains(err.Error(), "SSH keys") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(shown.String(), "Permission denied") {
		t.Fatal("git output not passed through")
	}
	other := exec.Command("sh", "-c", "echo 'fatal: repository not found' >&2; exit 128")
	if err := Run(other, "git clone", io.Discard, io.Discard); err == ErrKeyDenied || !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("other failure: %v", err)
	}
}
