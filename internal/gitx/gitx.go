// Package gitx holds small git helpers.
package gitx

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Toplevel returns the root of the work tree containing dir, or "" when
// dir is not in a git repository.
func Toplevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Clone clones url into dir (which must not exist or be empty). The
// terminal is attached so ssh can ask about host keys or passphrases.
func Clone(url, dir string, stdout, stderr io.Writer) error {
	return Run(exec.Command("git", "clone", url, dir), "git clone", stdout, stderr)
}

// Run runs a git command that talks to a remote, with the terminal
// attached. A rejected SSH key is reported with what to do about it.
func Run(cmd *exec.Cmd, what string, stdout, stderr io.Writer) error {
	var seen strings.Builder
	cmd.Stdin, cmd.Stdout = os.Stdin, stdout
	cmd.Stderr = io.MultiWriter(stderr, &seen)
	if err := cmd.Run(); err != nil {
		if IsKeyDenied(seen.String()) {
			return ErrKeyDenied
		}
		return fmt.Errorf("%s failed: %w", what, err)
	}
	return nil
}

// ErrKeyDenied means the git server rejected the user's SSH key.
var ErrKeyDenied = errors.New(keyDeniedHelp())

// IsKeyDenied reports whether git/ssh output shows a rejected SSH key.
func IsKeyDenied(output string) bool {
	return strings.Contains(output, "Permission denied (publickey")
}

func keyDeniedHelp() string {
	msg := "vogsphere refused your SSH key: add your public key to your intra profile (Settings → SSH keys), wait a minute, then retry"
	home, err := os.UserHomeDir()
	if err != nil {
		return msg
	}
	for _, name := range []string{"id_ed25519.pub", "id_ecdsa.pub", "id_rsa.pub"} {
		p := filepath.Join(home, ".ssh", name)
		if _, err := os.Stat(p); err == nil {
			return msg + " (cat " + "~/.ssh/" + name + " to copy it)"
		}
	}
	return msg + " (no key yet: ssh-keygen -t ed25519)"
}

// Init creates a git repository in dir.
func Init(dir string) error {
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		return fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureRemote makes the repository in dir point to url: it keeps a remote
// that already does, else adds "origin" when that name is free, else
// "vogsphere". It returns the remote's name and whether it was added.
func EnsureRemote(dir, url string) (string, bool, error) {
	out, err := exec.Command("git", "-C", dir, "remote", "-v").Output()
	if err != nil {
		return "", false, fmt.Errorf("git remote: %w", err)
	}
	names := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if f[1] == url {
			return f[0], false, nil
		}
		names[f[0]] = true
	}
	name := "origin"
	if names[name] {
		name = "vogsphere"
	}
	if names[name] {
		return "", false, fmt.Errorf("remotes origin and vogsphere both exist and point elsewhere")
	}
	if out, err := exec.Command("git", "-C", dir, "remote", "add", name, url).CombinedOutput(); err != nil {
		return "", false, fmt.Errorf("git remote add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return name, true, nil
}

// Exclude adds pattern to the repository's local exclude file (never
// committed) if dir is inside a git work tree. It reports whether it added
// the pattern.
func Exclude(dir, pattern string) (bool, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) || errors.Is(err, exec.ErrNotFound) {
			return false, nil // not a git repository, or no git
		}
		return false, err
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l == pattern || l == "/"+pattern {
				f.Close()
				return false, nil
			}
		}
		f.Close()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "\n# ft project link\n%s\n", pattern); err != nil {
		return false, err
	}
	return true, nil
}
