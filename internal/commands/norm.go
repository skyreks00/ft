package commands

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// normResult is the outcome of running norminette on a repository.
type normResult struct {
	Ran    bool     // false when there was nothing to check or no norminette
	Files  int      // files checked
	Failed []string // files with errors
	Report string   // the error lines, without the OK ones
}

// maxNormLines bounds the report printed before a push.
const maxNormLines = 30

// runNorm runs norminette on the .c and .h files of HEAD, extracted to a
// temporary folder: what will be pushed, not the working tree.
func runNorm(top string) (*normResult, error) {
	all, err := gitOutput(top, "ls-tree", "-r", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	var list []string
	for f := range strings.SplitSeq(all, "\x00") {
		if strings.HasSuffix(f, ".c") || strings.HasSuffix(f, ".h") {
			list = append(list, f)
		}
	}
	r := &normResult{}
	if len(list) == 0 {
		return r, nil
	}
	bin, err := exec.LookPath("norminette")
	if err != nil {
		return r, errNoNorminette
	}
	tmp, err := os.MkdirTemp("", "ft-norm-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := extractHead(top, tmp, list); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, list...)
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return nil, err
	}
	r.Ran, r.Files = true, len(list)

	var report []string
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasSuffix(line, ": Error!"):
			r.Failed = append(r.Failed, strings.TrimSuffix(line, ": Error!"))
			report = append(report, line)
		case strings.HasSuffix(line, ": OK!"), strings.TrimSpace(line) == "":
		default:
			report = append(report, line)
		}
	}
	if len(report) > maxNormLines {
		more := len(report) - maxNormLines
		report = append(report[:maxNormLines], "… "+strconv.Itoa(more)+" more lines")
	}
	r.Report = strings.Join(report, "\n")
	return r, nil
}

// extractHead writes the HEAD version of files under dst.
func extractHead(top, dst string, files []string) error {
	cmd := exec.Command("git", append([]string{"-C", top, "archive", "--format=tar", "HEAD", "--"}, files...)...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	tr := tar.NewReader(out)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		path := filepath.Join(dst, filepath.FromSlash(h.Name))
		if !strings.HasPrefix(path, dst+string(filepath.Separator)) {
			return fmt.Errorf("unexpected path in git archive: %s", h.Name)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, tr)
		f.Close()
		if err != nil {
			return err
		}
	}
	return cmd.Wait()
}

var errNoNorminette = errors.New("norminette not found (pip install norminette): skipping the norm check")
