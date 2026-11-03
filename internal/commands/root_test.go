package commands

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/skyreks00/ft/internal/api"
)

func TestAliases(t *testing.T) {
	root := NewRoot()
	for alias, want := range map[string]string{"e": "events", "ev": "eval", "l": "link", "sl": "slots",
		"u": "user", "o": "open", "s": "submit", "c": "correct", "p": "projects", "st": "status"} {
		cmd, _, err := root.Find([]string{alias})
		if err != nil || cmd.Name() != want {
			t.Errorf("ft %s → %v %v, want %s", alias, cmd.Name(), err, want)
		}
	}
	for args, want := range map[string]string{"e j": "join", "e l": "leave", "sl a": "add", "sl r": "rm", "sl del": "rm"} {
		cmd, _, err := root.Find(strings.Fields(args))
		if err != nil || cmd.Name() != want {
			t.Errorf("ft %s → %v %v, want %s", args, cmd.Name(), err, want)
		}
	}
}

func TestAliasesHiddenFromHelp(t *testing.T) {
	root := NewRoot()
	events, _, _ := root.Find([]string{"events"})
	var out bytes.Buffer
	events.SetOut(&out)
	if err := events.Usage(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Aliases") {
		t.Fatalf("help shows aliases:\n%s", out.String())
	}
}

func TestAliasesNotCompleted(t *testing.T) {
	root := NewRoot()
	root.SetArgs([]string{"__complete", "e"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "events\t") || strings.Contains(got, "\ne\t") || strings.Contains(got, "ev\t") {
		t.Fatalf("completion for \"e\" = %q (full names only)", got)
	}
}

// complete runs a shell completion request and returns its lines.
func complete(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRoot()
	root.SetArgs(append([]string{"__complete"}, args...))
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestOptionsOnTab(t *testing.T) {
	t.Chdir(t.TempDir())
	os.Mkdir("gnl", 0o755)
	cases := []struct {
		args      []string
		want, not []string
	}{
		{[]string{"submit", ""}, []string{"--dry-run\t", "--no-norm\t", "--branch\t"}, []string{"--help", "--json"}},
		{[]string{"submit", "--dry-run", ""}, []string{"--no-norm\t"}, []string{"--dry-run\t"}},
		{[]string{"link", "libft", ""}, []string{"--here\t", "gnl/"}, nil},
		{[]string{"link", "libft", "--here", ""}, nil, []string{"gnl/", "--here"}},
		{[]string{"projects", ""}, []string{"--upcoming\t", "--after\t"}, nil},
		{[]string{"slots", "add", ""}, []string{"--campus\t", "--inter-campus\t"}, nil},
	}
	for _, c := range cases {
		got := complete(t, c.args...)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("ft %v<Tab> lacks %q:\n%s", c.args, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got, n) {
				t.Errorf("ft %v<Tab> offers %q:\n%s", c.args, n, got)
			}
		}
	}
}

func TestPickEvaluation(t *testing.T) {
	soon, later := time.Now().Add(-30*time.Minute), time.Now().Add(2*time.Hour)
	evals := []api.ScaleTeam{
		{ID: 1, BeginAt: &soon, Team: &api.Team{RepoURL: "git@vog:repo-7637854-neric", Users: []api.TeamUser{{Login: "neric", Leader: true}}}},
		{ID: 2, BeginAt: &later, Team: &api.Team{RepoURL: "git@vog:repo-7637452-oservaye"}},
	}
	if e, err := pickEvaluation(evals, nil); err != nil || e.ID != 1 {
		t.Fatalf("default = %+v %v, want the one in progress", e, err)
	}
	if e, err := pickEvaluation(evals, []string{"2"}); err != nil || e.ID != 2 {
		t.Fatalf("by id = %+v %v", e, err)
	}
	if _, err := pickEvaluation(evals, []string{"9"}); err == nil {
		t.Fatal("unknown id accepted")
	}
	if _, err := pickEvaluation(nil, nil); err == nil {
		t.Fatal("nothing to evaluate accepted")
	}
	if got := teamLogin(&evals[0]); got != "neric" {
		t.Errorf("teamLogin = %q", got)
	}
	if got := teamLogin(&evals[1]); got != "oservaye" {
		t.Errorf("teamLogin from repo = %q", got)
	}
}
