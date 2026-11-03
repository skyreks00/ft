package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// LinkFile is the per-folder project link file.
const LinkFile = ".ft.toml"

// ErrNotLinked is returned when no .ft.toml is found.
var ErrNotLinked = errors.New("this folder is not linked to a project: run `ft link <project>`")

// Link ties a folder to a 42 project.
type Link struct {
	ProjectID int    `toml:"project_id"`
	Slug      string `toml:"slug"`
	Name      string `toml:"name,omitempty"`
	TeamID    int    `toml:"team_id,omitempty"`
}

// FindLink looks for .ft.toml in dir and its parents. It returns the link
// and the directory that holds it.
func FindLink(dir string) (*Link, string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	for {
		l, err := ReadLink(filepath.Join(dir, LinkFile))
		if err == nil {
			return l, dir, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, "", ErrNotLinked
		}
		dir = parent
	}
}

// ReadLink parses a .ft.toml file.
func ReadLink(path string) (*Link, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var l Link
	if err := toml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if l.ProjectID == 0 || l.Slug == "" {
		return nil, fmt.Errorf("%s: project_id and slug are required (run `ft link <project>` again)", path)
	}
	return &l, nil
}

// WriteLink writes dir/.ft.toml.
func WriteLink(dir string, l *Link) error {
	var buf bytes.Buffer
	buf.WriteString("# Written by `ft link`. Do not commit it to your project repository.\n")
	if err := toml.NewEncoder(&buf).Encode(l); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, LinkFile), buf.Bytes(), 0o644)
}
