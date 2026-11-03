package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectsUserAndCurrentTeam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/projects_users/42" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"id":42,"status":"in_progress","current_team_id":2,"project":{"id":1314,"slug":"42cursus-libft"},
			"teams":[{"id":1,"created_at":"2026-01-01T00:00:00Z"},
			         {"id":2,"created_at":"2025-01-01T00:00:00Z","locked?":true,"repo_url":"git@vogsphere:repo","project_session_id":3802,
			          "users":[{"login":"swilmus","leader":true}]}]}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	pu, err := c.ProjectsUser(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	team := pu.CurrentTeam()
	if team == nil || team.ID != 2 || !team.Locked || team.RepoURL != "git@vogsphere:repo" || !team.Users[0].Leader {
		t.Fatalf("CurrentTeam = %+v", team)
	}
	pu.CurrentTeamID = nil
	if team := pu.CurrentTeam(); team.ID != 1 {
		t.Fatalf("fallback team = %d, want most recent (1)", team.ID)
	}
}

func TestRequiredEvaluations(t *testing.T) {
	var p Project
	json.Unmarshal([]byte(`{"project_sessions":[{"id":1,"scales":[{"correction_number":2}]},
		{"id":3802,"scales":[{"correction_number":5,"is_primary":false},{"correction_number":3,"is_primary":true}]}]}`), &p)
	if s := p.Session(3802); s == nil || s.RequiredEvaluations() != 3 {
		t.Fatalf("primary scale not used: %+v", s)
	}
	if s := p.Session(1); s.RequiredEvaluations() != 2 {
		t.Fatal("first scale not used as fallback")
	}
	if p.Session(9) != nil {
		t.Fatal("unknown session found")
	}
}

func TestScaleTeamCorrectorForms(t *testing.T) {
	var evals []ScaleTeam
	err := json.Unmarshal([]byte(`[{"id":1,"corrector":{"id":7,"login":"neric"},"filled_at":"2026-08-05T14:04:10.342Z","final_mark":85},
		{"id":2,"corrector":"invisible","filled_at":null}]`), &evals)
	if err != nil {
		t.Fatal(err)
	}
	if evals[0].Corrector.Login != "neric" || !evals[0].Done() {
		t.Fatalf("got %+v", evals[0])
	}
	if evals[1].Corrector.Login != "" || evals[1].Done() {
		t.Fatalf("got %+v", evals[1])
	}
}

func TestIsForbiddenAndNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/projects/hidden" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := newTestClient(srv, nil)
	if _, err := c.Project(context.Background(), "hidden"); !IsForbidden(err) || IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.Project(context.Background(), "nope"); !IsNotFound(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestCurriculum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/cursus/21/projects" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `[
{"id":1,"slug":"42cursus-push_swap","difficulty":1855,"campus":[{"id":99}],"project_sessions":[{"campus_id":99,"cursus_id":21}]},
{"id":2,"slug":"42cursus-libft","difficulty":462,"campus":[{"id":99}],"project_sessions":[{"campus_id":null,"cursus_id":21}]},
{"id":3,"slug":"exam-rank-02","exam":true,"difficulty":0,"campus":[{"id":99}],"project_sessions":[{"campus_id":99}]},
{"id":4,"slug":"42next-push_swap","difficulty":1855,"campus":[{"id":99}],"project_sessions":[{"campus_id":99}]},
{"id":5,"slug":"paris-only","difficulty":900,"campus":[{"id":1}],"project_sessions":[{"campus_id":1}]},
{"id":6,"slug":"child","difficulty":500,"parent":{"id":2},"campus":[{"id":99}],"project_sessions":[{"campus_id":99}]},
{"id":7,"slug":"other-campus-session","difficulty":700,"campus":[{"id":99}],"project_sessions":[{"campus_id":1,"cursus_id":21}]},
{"id":8,"slug":"call-me-maybe","difficulty":1764,"campus":[{"id":99}],"project_sessions":[{"campus_id":null}]},
{"id":9,"slug":"42cursus-call-me-maybe","difficulty":1764,"campus":[{"id":99}],"project_sessions":[{"campus_id":99,"cursus_id":21}]}]`)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv, nil)
	got, err := c.Curriculum(context.Background(), 21, 99, nil) // a campus without a holy graph table
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, p := range got {
		slugs = append(slugs, p.Slug)
	}
	if fmt.Sprint(slugs) != "[42cursus-libft 42cursus-call-me-maybe 42cursus-push_swap]" {
		t.Fatalf("got %v", slugs)
	}
}

func TestOptionalModule(t *testing.T) {
	for slug, want := range map[string]bool{
		"python-module-00": true, "data-science-2": true, "unity-0-the-basics-unity-tools": true,
		"django-0-initiation": true, "cybersecurity-arachnida-web": true, "piscine-object-module-00-encapsulation": true,
		"cpp-module-00": false, "42cursus-push_swap": false, "born2beroot": false, "ft_transcendence": false,
	} {
		if got := (&Project{Slug: slug}).OptionalModule(); got != want {
			t.Errorf("%s: OptionalModule = %v, want %v", slug, got, want)
		}
	}
}

func TestGraphCampus(t *testing.T) {
	tab := Graph(12, 21, []string{"common-core"})
	if tab == nil || tab.Variant != "" {
		t.Fatalf("campus graph = %+v", tab)
	}
	for slug, want := range map[string]int{"42cursus-libft": 0, "born2beroot": 1, "42cursus-push_swap": 2, "42cursus-minishell": 3,
		"cpp-module-04": 4, "cpp-module-05": 5, "ft_transcendence": 6, "libasm": AfterCore} {
		if info, ok := tab.Lookup(slug); !ok || info.Circle != want {
			t.Errorf("%s: circle %+v %v, want %d", slug, info, ok, want)
		}
	}
	if info, _ := tab.Lookup("42cursus-corewar"); len(info.Tracks) != 3 || info.Tracks[0] != "Algo & AI & Data" {
		t.Errorf("corewar tracks = %v", info.Tracks)
	}
	if Graph(1, 21, nil) != nil {
		t.Error("graph for a campus ft has none for")
	}
	if len(circlesErrors) > 0 {
		t.Errorf("table errors: %v", circlesErrors)
	}
}

func TestGraph42Next(t *testing.T) {
	tab := Graph(12, 21, []string{"common-core", "42next"})
	if tab == nil || tab.Variant != "42next" {
		t.Fatalf("42next graph = %+v", tab)
	}
	for slug, want := range map[string]int{"42cursus-libft": 0, "42next-push_swap": 1, "42cursus-ft_printf": 1,
		"42next-born2beroot": 2, "python-module-07": 2, "a-maze-ing": 2, "codexion": 3, "pac-man": 4, "netpractice": 4,
		"tree_nity": 5, "inception": 5, "ft_transcendence": 6, "libasm": AfterCore} {
		if info, ok := tab.Lookup(slug); !ok || info.Circle != want {
			t.Errorf("%s: circle %+v %v, want %d", slug, info, ok, want)
		}
	}
	if info, _ := tab.Lookup("42cursus-push_swap"); info.Circle != 1 || info.Primary != "42next-push_swap" {
		t.Errorf("old push_swap = %+v, want counted for 42next-push_swap", info)
	}
	if _, ok := tab.Lookup("cpp-module-00"); ok {
		t.Error("42Next has no cpp modules in its core")
	}

	validated := map[string]bool{"42cursus-libft": true, "42cursus-ft_printf": true, "42cursus-get_next_line": true, "42cursus-push_swap": true}
	v := func(s string) bool { return validated[s] }
	if done, total := tab.Progress(1, v); done != 3 || total != 3 {
		t.Errorf("circle 1 = %d/%d (old push_swap counts)", done, total)
	}
	if c := tab.Current(v); c != 2 {
		t.Errorf("current = %d, want 2", c)
	}
	if _, total := tab.Progress(5, v); total != 4 {
		t.Errorf("circle 5 total = %d, want 4 (agent-smith or tree_nity counts once)", total)
	}
	validated["tree_nity"] = true
	if done, _ := tab.Progress(5, v); done != 1 {
		t.Errorf("circle 5 done = %d, want 1", done)
	}
	if _, total := tab.Progress(2, v); total != 3+13 {
		t.Errorf("circle 2 total = %d, want 16 (born2beroot, exam, a-maze-ing, 13 modules)", total)
	}
}

func TestCurriculumByCircle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
{"id":1337,"slug":"ft_transcendence","difficulty":24360},
{"id":1330,"slug":"libasm","difficulty":966},
{"id":1471,"slug":"42cursus-push_swap","difficulty":1855},
{"id":2687,"slug":"42next-push_swap","difficulty":1855},
{"id":1320,"slug":"exam-rank-02","exam":true,"difficulty":0},
{"id":2004,"slug":"pipex","difficulty":1142},
{"id":2690,"slug":"python-module-00","difficulty":70},
{"id":1314,"slug":"42cursus-libft","difficulty":462}]`)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv, nil)
	slugs := func(ps []Project) string {
		var out []string
		for _, p := range ps {
			out = append(out, fmt.Sprintf("%d:%s", *p.Circle, p.Slug))
		}
		return fmt.Sprint(out)
	}

	got, err := c.Curriculum(context.Background(), 21, 12, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[0:42cursus-libft 2:pipex 2:42cursus-push_swap 2:exam-rank-02 6:ft_transcendence 7:libasm]"; slugs(got) != want {
		t.Errorf("campus graph:\n got  %s\n want %s", slugs(got), want)
	}

	got, err = c.Curriculum(context.Background(), 21, 12, []string{"42next"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[0:42cursus-libft 1:42next-push_swap 2:python-module-00 6:ft_transcendence 7:libasm]"; slugs(got) != want {
		t.Errorf("42next:\n got  %s\n want %s (old slugs not listed twice)", slugs(got), want)
	}
}

func TestQuestSlugs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"quest_id":37,"quest":{"slug":"common-core"}},{"quest_id":128,"quest":{"slug":"42next"}}]`)
	}))
	defer srv.Close()
	c, _ := newTestClient(srv, nil)
	got, err := c.QuestSlugs(context.Background(), "swilmus")
	if err != nil || fmt.Sprint(got) != "[common-core 42next]" {
		t.Fatalf("got %v %v", got, err)
	}
}
