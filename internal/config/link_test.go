package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinkRoundTripAndParentLookup(t *testing.T) {
	root := t.TempDir()
	want := &Link{ProjectID: 1314, Slug: "42cursus-libft", Name: "Libft", TeamID: 7755866}
	if err := WriteLink(root, want); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "src", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, dir, err := FindLink(sub)
	if err != nil {
		t.Fatal(err)
	}
	if *got != *want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if dir != root {
		t.Fatalf("dir = %s, want %s", dir, root)
	}
}

func TestFindLinkNotLinked(t *testing.T) {
	if _, _, err := FindLink(t.TempDir()); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("err = %v, want ErrNotLinked", err)
	}
}

func TestReadLinkErrors(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]string{
		"invalid toml":    "project_id = [",
		"missing slug":    "project_id = 1",
		"missing project": `slug = "libft"`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(name, " ", "_"))
			os.WriteFile(p, []byte(content), 0o644)
			if _, err := ReadLink(p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestReadLinkHandWritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), LinkFile)
	os.WriteFile(p, []byte("# mine\nproject_id = 1314\nslug = \"42cursus-libft\"\n"), 0o644)
	l, err := ReadLink(p)
	if err != nil {
		t.Fatal(err)
	}
	if l.ProjectID != 1314 || l.TeamID != 0 {
		t.Fatalf("got %+v", l)
	}
}
