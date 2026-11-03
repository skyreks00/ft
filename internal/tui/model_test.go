package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/config"
)

const meJSON = `{"id":1,"login":"swilmus",
"cursus_users":[{"level":4.2,"cursus":{"id":21,"slug":"42cursus","name":"42cursus"}},{"level":9.7,"cursus":{"id":9,"slug":"c-piscine","name":"C Piscine"}}],
"campus":[{"id":99,"name":"Nowhere"}],
"projects_users":[
 {"id":10,"status":"finished","final_mark":100,"validated?":true,"marked_at":"2026-08-01T00:00:00Z","cursus_ids":[9],"project":{"id":1,"slug":"c-piscine-c-00","name":"C Piscine C 00"}},
 {"id":11,"status":"in_progress","cursus_ids":[21],"project":{"id":1314,"slug":"42cursus-libft","name":"Libft"}},
 {"id":12,"status":"finished","final_mark":0,"validated?":false,"marked_at":"2026-09-01T00:00:00Z","cursus_ids":[9],"project":{"id":2,"slug":"c-piscine-rush-00","name":"Rush 00"}},
 {"id":13,"status":"finished","cursus_ids":[9],"project":{"id":3,"slug":"child","name":"Child","parent_id":2}}]}`

// testModel uses a campus without a holy graph table; see graphModel.
func testModel(t *testing.T, link *config.Link) model {
	t.Helper()
	return testModelJSON(t, meJSON, link)
}

func testModelJSON(t *testing.T, js string, link *config.Link) model {
	t.Helper()
	var me api.User
	if err := json.Unmarshal([]byte(js), &me); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), Options{Link: link, LinkDir: "/home/swilmus/gnl"})
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m = update(t, m, meMsg{me: &me})
	m = update(t, m, slotsMsg{slots: []api.Slot{{ID: 1, BeginAt: time.Now().Add(time.Hour), EndAt: time.Now().Add(2 * time.Hour)}}})
	return m
}

func update(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// command runs ":line" and returns the model and the command it produced.
func command(t *testing.T, m model, line string) (model, tea.Cmd) {
	t.Helper()
	m = update(t, m, key(":"))
	m = typeKeys(t, m, line)
	next, cmd := m.Update(key("enter"))
	return next.(model), cmd
}

func typeKeys(t *testing.T, m model, s string) model {
	for _, r := range s {
		m = update(t, m, key(string(r)))
	}
	return m
}

// slugs lists the visible project rows (headers as "[label]").
func slugs(m model) []string {
	var out []string
	for _, r := range m.shown {
		if r.header() {
			out = append(out, "["+m.groupLabel(r.group)+"]")
		} else {
			out = append(out, m.items[r.idx].ref.Slug)
		}
	}
	return out
}

func TestGroupsAndLinkedProject(t *testing.T) {
	m := testModel(t, &config.Link{ProjectID: 1327, Slug: "42cursus-get_next_line", Name: "get_next_line"})
	got := strings.Join(slugs(m), ",")
	want := "[42cursus],42cursus-libft,42cursus-get_next_line,[Upcoming],[C Piscine]"
	if got != want {
		t.Fatalf("rows = %s, want %s (main cursus open, others folded, sub-projects hidden)", got, want)
	}
	if it := m.selected(); it == nil || it.ref.Slug != "42cursus-get_next_line" {
		t.Fatalf("linked project not preselected: %+v", it)
	}

	m.cursor = 4 // the C Piscine header (Piscine modules is empty, hidden)
	m = update(t, m, key("enter"))
	got = strings.Join(slugs(m), ",")
	if !strings.HasSuffix(got, "[C Piscine],c-piscine-rush-00,c-piscine-c-00") {
		t.Fatalf("expanded piscine = %s (latest marked first)", got)
	}
	m = update(t, m, key("enter"))
	if strings.Contains(strings.Join(slugs(m), ","), "rush") {
		t.Fatal("enter did not fold the group back")
	}
}

func TestUpcoming(t *testing.T) {
	m := testModel(t, nil)
	m.cursor = 2 // the Upcoming header
	next, cmd := m.Update(key("enter"))
	m = next.(model)
	if cmd == nil || !m.curriculumLoading {
		t.Fatal("expanding Upcoming did not load the curriculum")
	}
	solo, four := false, 4
	m = update(t, m, curriculumMsg{projects: []api.Project{
		{ID: 1314, Slug: "42cursus-libft", Difficulty: 462},
		{ID: 1471, Slug: "42cursus-push_swap", Name: "push_swap", Difficulty: 1855},
		{ID: 1331, Slug: "42cursus-minishell", Name: "minishell", Difficulty: 2814,
			Sessions: []api.ProjectSession{{Solo: &solo, MaxPeople: &four}}},
		{ID: 2000, Slug: "python-module-00", Difficulty: 70},
	}})
	got := strings.Join(slugs(m), ",")
	if got != "[42cursus],42cursus-libft,[Upcoming],42cursus-push_swap,42cursus-minishell,[Piscine modules],[C Piscine]" {
		t.Fatalf("rows = %s (started projects not repeated, XP order)", got)
	}
	m = update(t, m, key("j"))
	m = update(t, m, key("j"))
	v := ansi.Strip(m.View())
	for _, want := range []string{"2814xp", "minishell", "group, up to 4", "not started"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
}

func TestNavigationAndFocus(t *testing.T) {
	m := testModel(t, nil)
	if m.cursor != 1 {
		t.Fatalf("initial cursor = %d, want the first project", m.cursor)
	}
	m = update(t, m, key("j"))
	m = update(t, m, key("j"))
	if m.cursor != 3 {
		t.Fatalf("cursor = %d", m.cursor)
	}
	m = update(t, m, key("G"))
	m = update(t, m, key("j"))
	if m.cursor != len(m.shown)-1 {
		t.Fatalf("cursor past end: %d", m.cursor)
	}
	m = update(t, m, key("g"))
	if m.cursor != 0 {
		t.Fatalf("g: cursor = %d", m.cursor)
	}
	m = update(t, m, key("l"))
	if m.focus != panelDetails {
		t.Fatal("l did not focus details")
	}
	m = update(t, m, key("h"))
	m = update(t, m, key("tab"))
	if m.focus != panelSlots {
		t.Fatalf("tab focus = %d", m.focus)
	}
}

func TestSearch(t *testing.T) {
	m := testModel(t, nil)
	m = update(t, m, key("/"))
	if m.mode != modeSearch {
		t.Fatal("/ did not enter search mode")
	}
	m = typeKeys(t, m, "rush")
	if got := slugs(m); len(got) != 1 || got[0] != "c-piscine-rush-00" {
		t.Fatalf("filtered = %v (search must look into folded groups)", got)
	}
	m = update(t, m, key("enter"))
	if m.mode != modeNormal || m.filter != "rush" {
		t.Fatalf("enter: mode=%v filter=%q", m.mode, m.filter)
	}
	m = update(t, m, key("esc"))
	if m.filter != "" || len(m.shown) != 4 {
		t.Fatalf("esc did not clear the filter: %q %d", m.filter, len(m.shown))
	}
}

func TestPalette(t *testing.T) {
	m := testModel(t, nil)
	m = update(t, m, key(":"))
	m = typeKeys(t, m, "q")
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal(":q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal(":q did not quit")
	}

	m = update(t, m, key(":"))
	m = typeKeys(t, m, "nope")
	m = update(t, m, key("enter"))
	if !m.flashErr || !strings.Contains(m.flash, "unknown command") {
		t.Fatalf("flash = %q", m.flash)
	}

}

func TestSubmitNeedsLinkedProject(t *testing.T) {
	m := testModel(t, nil)
	m, _ = command(t, m, "submit")
	if !m.flashErr || !strings.Contains(m.flash, "linked folder") {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestViewFitsTerminal(t *testing.T) {
	m := testModel(t, &config.Link{ProjectID: 1314, Slug: "42cursus-libft", Name: "Libft"})
	m = update(t, m, statusMsg{id: 1314, s: &api.ProjectStatus{
		Project: api.ProjectRef{ID: 1314, Slug: "42cursus-libft", Name: "Libft"}, Status: "in_progress",
		Team: &api.Team{Name: "swilmus's group", Locked: true, RepoURL: "git@vogsphere-v2.42belgium.be:vogsphere/" + strings.Repeat("x", 120),
			Users: []api.TeamUser{{Login: "swilmus", Leader: true}}},
		RequiredEvaluations: 3,
	}})
	for _, size := range [][2]int{{100, 30}, {60, 14}, {220, 60}} {
		m = update(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, help := range []bool{false, true} {
			m.help = help
			lines := strings.Split(m.View(), "\n")
			if len(lines) != size[1] {
				t.Fatalf("%v help=%v: %d lines", size, help, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != size[0] {
					t.Fatalf("%v help=%v: line %d has width %d: %q", size, help, i, w, ansi.Strip(l))
				}
			}
		}
	}
	m.help = false
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	v := ansi.Strip(m.View())
	for _, want := range []string{"Projects 1", "42cursus", "Upcoming", "Slots 1", "Libft", "0/3 done", "NORMAL", "swilmus", "Lvl 4.20", "⌂"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	m = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(m.View(), "too small") {
		t.Fatal("tiny terminal not handled")
	}
}

func TestEventsViewAndConfirm(t *testing.T) {
	m := testModel(t, nil)
	m, _ = command(t, m, "events")
	if m.view != viewEvents || m.focus != panelDetails {
		t.Fatalf(":events: view=%d focus=%d", m.view, m.focus)
	}
	m = update(t, m, eventsMsg{events: []api.Event{
		{ID: 1, Name: "Workshop Go", BeginAt: time.Now().Add(24 * time.Hour), EndAt: time.Now().Add(25 * time.Hour)},
		{ID: 2, Name: "Movie night", BeginAt: time.Now().Add(48 * time.Hour), EndAt: time.Now().Add(50 * time.Hour)},
	}, joined: map[int]bool{2: true}})
	m = update(t, m, key("j"))
	if m.evCur != 1 {
		t.Fatalf("evCur = %d", m.evCur)
	}
	m, _ = command(t, m, "event leave") // the selected event
	if m.mode != modeConfirm || !strings.Contains(m.confirmPrompt, "Leave Movie night") {
		t.Fatalf("mode=%v prompt=%q", m.mode, m.confirmPrompt)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "CONFIRM") || !strings.Contains(v, "y / N") {
		t.Fatal("confirm prompt not shown")
	}
	m = update(t, m, key("n"))
	if m.mode != modeNormal || m.flash != "Cancelled" || m.confirmCmd != nil {
		t.Fatalf("n: mode=%v flash=%q", m.mode, m.flash)
	}

	m, _ = command(t, m, "event join 1")
	next, cmd := m.Update(key("y"))
	if m = next.(model); m.mode != modeNormal || cmd == nil {
		t.Fatal("y did not run the join")
	}
	m = update(t, m, key("esc"))
	if m.view != viewProject {
		t.Fatal("esc did not go back to the project")
	}
}

func TestSlotCommands(t *testing.T) {
	m := testModel(t, nil)
	m.slotsURL = "intra" // the test campus (12) would use its platform
	m, cmd := command(t, m, "slot add 14:10 15:00")
	m = update(t, m, cmd())
	if !m.flashErr || !strings.Contains(m.flash, "quarter hour") {
		t.Fatalf("flash = %q", m.flash)
	}

	m, _ = command(t, m, "slot rm") // the selected slot
	if m.mode != modeConfirm || !strings.Contains(m.confirmPrompt, "Remove slot") {
		t.Fatalf(":slot rm: mode=%v prompt=%q", m.mode, m.confirmPrompt)
	}

	for in, want := range map[string][2]string{
		"14:00 16:00":        {"14:00", "16:00"},
		"tomorrow 9:00 +2h":  {"tomorrow 9:00", "+2h"},
		"mon 9:00 tue 10:00": {"mon 9:00", "tue 10:00"},
	} {
		s, e, ok := splitRange(strings.Fields(in))
		if !ok || s != want[0] || e != want[1] {
			t.Errorf("splitRange(%q) = %q %q %v", in, s, e, ok)
		}
	}
	if _, _, ok := splitRange([]string{"14:00"}); ok {
		t.Error("one word accepted")
	}
}

func TestMouse(t *testing.T) {
	m := testModel(t, nil) // 100×30: left column 30 wide, projects panel 17 high
	click := func(x, y int) tea.MouseMsg {
		return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	}
	m = update(t, m, click(5, 3)) // third row of the projects panel
	if m.cursor != 2 || m.focus != panelProjects {
		t.Fatalf("click: cursor=%d focus=%d", m.cursor, m.focus)
	}
	m = update(t, m, tea.MouseMsg{X: 5, Y: 3, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.cursor != 1 {
		t.Fatalf("wheel up: cursor=%d", m.cursor)
	}
	m = update(t, m, click(60, 5))
	if m.focus != panelDetails {
		t.Fatalf("click right: focus=%d", m.focus)
	}
	m = update(t, m, click(5, 18)) // first slot row (panel border at y=17)
	if m.focus != panelSlots || m.slotCur != 0 {
		t.Fatalf("click slot: focus=%d cur=%d", m.focus, m.slotCur)
	}
	m.slotCur = 5
	m = update(t, m, click(5, 18))
	if m.slotCur != 0 {
		t.Fatalf("click did not select the first slot: %d", m.slotCur)
	}
	m = update(t, m, click(5, 29)) // status bar: ignored
	if m.focus != panelSlots {
		t.Fatal("status bar click changed focus")
	}
}

func TestPaletteSuggestions(t *testing.T) {
	m := testModel(t, nil)
	m = update(t, m, key(":"))
	if len(m.wild) != len(commandHelp) {
		t.Fatalf("empty input: %d suggestions", len(m.wild))
	}
	m = typeKeys(t, m, "li")
	if len(m.wild) != 1 || m.wild[0].value != "link" {
		t.Fatalf("li: %+v", m.wild)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "clone & link a project") {
		t.Fatal("suggestions bar not shown")
	}
	// A lone suggestion is accepted, then its arguments are offered.
	m = update(t, m, key("tab"))
	if m.input.Value() != "link " || len(m.wild) != 3 {
		t.Fatalf("after tab: %q %+v", m.input.Value(), m.wild)
	}
	m = update(t, m, key("tab"))
	first := m.input.Value()
	m = update(t, m, key("tab"))
	if first == m.input.Value() || !strings.HasPrefix(first, "link ") {
		t.Fatalf("tab does not cycle: %q then %q", first, m.input.Value())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.input.Value() != first {
		t.Fatalf("shift+tab: %q, want %q", m.input.Value(), first)
	}

	// Event ids load on demand.
	m = update(t, m, key("esc"))
	m = update(t, m, key(":"))
	m = typeKeys(t, m, "event join ")
	if !m.eventsRequested || len(m.wild) != 0 {
		t.Fatalf("events not requested: %v %+v", m.eventsRequested, m.wild)
	}
	m = update(t, m, eventsMsg{events: []api.Event{{ID: 45091, Name: "Go workshop"}, {ID: 45409, Name: "Chess"}}, joined: map[int]bool{45409: true}})
	if len(m.wild) != 1 || m.wild[0].value != "45091" {
		t.Fatalf("join suggestions: %+v", m.wild)
	}

	// The layout keeps the screen size with the bar shown.
	for _, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != m.width {
			t.Fatalf("line width %d: %q", w, ansi.Strip(l))
		}
	}
	if n := len(strings.Split(m.View(), "\n")); n != m.height {
		t.Fatalf("%d lines, want %d", n, m.height)
	}
}

func TestSlotsPlatformWithoutSession(t *testing.T) {
	m := testModel(t, nil)
	m.slotsURL = "https://slots.example" // a platform, but no session stored
	m, _ = command(t, m, "slot add 14:00 16:00")
	if m.mode != modeNormal || !m.flashErr || !strings.Contains(m.flash, "ft slots auth") {
		t.Fatalf(":slot add: mode=%v flash=%q", m.mode, m.flash)
	}
}

// graphModel uses Belgium (campus 12), for which ft has the holy graph.
func graphModel(t *testing.T, link *config.Link) model {
	t.Helper()
	return testModelJSON(t, strings.Replace(meJSON, `{"id":99,"name":"Nowhere"}`, `{"id":12,"name":"Belgium"}`, 1), link)
}

func TestCircles(t *testing.T) {
	m := graphModel(t, &config.Link{ProjectID: 1327, Slug: "42cursus-get_next_line", Name: "get_next_line"})
	got := strings.Join(slugs(m), ",")
	// libft (circle 0) is in progress: circles 0 and 1 are unfolded.
	want := "[Circle 0],42cursus-libft,[Circle 1],42cursus-get_next_line,[Circle 2],[Circle 3],[Circle 4],[Circle 5],[Circle 6],[After the common core],[C Piscine]"
	if got != want {
		t.Fatalf("rows =\n %s\nwant\n %s", got, want)
	}
	if !m.curriculumLoading {
		t.Fatal("the circles' upcoming projects were not requested")
	}
	m = update(t, m, curriculumMsg{projects: []api.Project{
		{ID: 1314, Slug: "42cursus-libft", Difficulty: 462},
		{ID: 1994, Slug: "born2beroot", Difficulty: 577},
		{ID: 1316, Slug: "42cursus-ft_printf", Difficulty: 882},
		{ID: 1327, Slug: "42cursus-get_next_line", Difficulty: 882},
		{ID: 1471, Slug: "42cursus-push_swap", Difficulty: 1855},
		{ID: 1330, Slug: "libasm", Difficulty: 966},
	}})
	got = strings.Join(slugs(m), ",")
	want = "[Circle 0],42cursus-libft,[Circle 1],born2beroot,42cursus-ft_printf,42cursus-get_next_line,[Circle 2],[Circle 3]"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("rows =\n %s\nwant prefix\n %s (circle 1 by XP, linked one in place)", got, want)
	}
	v := ansi.Strip(m.View())
	for _, s := range []string{"Circle 0", "0/1", "0/3", "0/7"} {
		if !strings.Contains(v, s) {
			t.Errorf("view lacks %q", s)
		}
	}

	m.cursor = 6 // [Circle 2]
	m = update(t, m, key("enter"))
	if !strings.Contains(strings.Join(slugs(m), ","), "[Circle 2],42cursus-push_swap") {
		t.Fatalf("circle 2 did not unfold: %v", slugs(m))
	}
	m = update(t, m, key("j"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Circle 2") || !strings.Contains(v, "1855") {
		t.Fatal("details lack the circle or XP of push_swap")
	}
}

func TestCircles42Next(t *testing.T) {
	var me api.User
	js := strings.Replace(meJSON, `{"id":99,"name":"Nowhere"}`, `{"id":12,"name":"Belgium"}`, 1)
	if err := json.Unmarshal([]byte(js), &me); err != nil {
		t.Fatal(err)
	}
	m := newModel(context.Background(), Options{})
	m = update(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	m = update(t, m, meMsg{me: &me, quests: []string{"common-core", "42next"}})
	if m.graph == nil || m.graph.Variant != "42next" {
		t.Fatalf("graph = %+v", m.graph)
	}
	m = update(t, m, curriculumMsg{projects: []api.Project{
		{ID: 1314, Slug: "42cursus-libft"},
		{ID: 1316, Slug: "42cursus-ft_printf", Difficulty: 882},
		{ID: 1327, Slug: "42cursus-get_next_line", Difficulty: 882},
		{ID: 2687, Slug: "42next-push_swap", Difficulty: 1855},
		{ID: 2686, Slug: "42next-born2beroot", Difficulty: 577},
		{ID: 2743, Slug: "agent-smith", Difficulty: 10042},
	}})
	got := strings.Join(slugs(m), ",")
	want := "[Circle 0],42cursus-libft,[Circle 1],42cursus-ft_printf,42cursus-get_next_line,42next-push_swap,[Circle 2]"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("rows =\n %s\nwant prefix\n %s", got, want)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "0/3") || !strings.Contains(v, "0/16") {
		t.Fatalf("circle counts missing:\n%s", v)
	}
}

func TestOldSlugTakesItsPlace(t *testing.T) {
	var me api.User
	json.Unmarshal([]byte(`{"login":"x","campus":[{"id":12}],"cursus_users":[{"cursus":{"id":21,"name":"42cursus"}}],
		"projects_users":[{"status":"finished","validated?":true,"cursus_ids":[21],"project":{"id":1471,"slug":"42cursus-push_swap","name":"push_swap"}}]}`), &me)
	tab := api.Graph(12, 21, []string{"42next"})
	items := buildItems(&me, nil, []api.Project{{ID: 2687, Slug: "42next-push_swap"}}, tab)
	if len(items) != 1 || items[0].ref.Slug != "42cursus-push_swap" || items[0].circle != 1 {
		t.Fatalf("items = %+v (the validated old push_swap stands for 42next-push_swap on circle 1)", items)
	}
}

func TestNoActionHints(t *testing.T) {
	m := testModel(t, &config.Link{ProjectID: 1314, Slug: "42cursus-libft", Name: "Libft"})
	v := ansi.Strip(m.View())
	for _, hint := range []string{"? help", ": command", "o open", "s submit", "esc back", "to open one", "Slug "} {
		if strings.Contains(v, hint) {
			t.Errorf("view repeats %q", hint)
		}
	}
	m = update(t, m, key("?"))
	if v := ansi.Strip(m.View()); !strings.Contains(v, "COMMANDS") {
		t.Fatal("? no longer shows the keys and commands")
	}
}

func TestNoActionKeys(t *testing.T) {
	m := testModel(t, &config.Link{ProjectID: 1314, Slug: "42cursus-libft", Name: "Libft"})
	for _, k := range []string{"o", "s", "c", "r", "e", "v", "a", "d", "x", "p"} {
		next, cmd := m.Update(key(k))
		n := next.(model)
		if cmd != nil || n.mode != modeNormal || n.view != viewProject || n.flash != "" {
			t.Errorf("key %q still does something (mode=%v view=%d flash=%q)", k, n.mode, n.view, n.flash)
		}
	}
}

func TestAliases(t *testing.T) {
	m := testModel(t, nil)
	m.slotsURL = "intra"

	m, _ = command(t, m, "e")
	if m.view != viewEvents {
		t.Fatal(":e did not open the events")
	}
	m = update(t, m, eventsMsg{events: []api.Event{{ID: 7, Name: "Go workshop"}}, joined: map[int]bool{}})
	m, _ = command(t, m, "e j 7")
	if m.mode != modeConfirm || !strings.Contains(m.confirmPrompt, "Join Go workshop") {
		t.Fatalf(":e j 7: mode=%v prompt=%q", m.mode, m.confirmPrompt)
	}
	m = update(t, m, key("n"))

	m, _ = command(t, m, "ev")
	if m.view != viewEvals {
		t.Fatal(":ev did not open the evaluations")
	}
	m, cmd := command(t, m, "sl a 14:10 15:00")
	m = update(t, m, cmd())
	if !strings.Contains(m.flash, "quarter hour") {
		t.Fatalf(":sl a: flash = %q", m.flash)
	}
	m, _ = command(t, m, "l")
	if !strings.Contains(m.flash, "usage: :link") {
		t.Fatalf(":l: flash = %q", m.flash)
	}
	m, _ = command(t, m, "zz")
	if !strings.Contains(m.flash, "unknown command :zz") {
		t.Fatalf(":zz: flash = %q", m.flash)
	}

	// Suggestions show full names only; typed short forms still complete
	// their arguments.
	m = update(t, m, key(":"))
	m = typeKeys(t, m, "e")
	if len(m.wild) != 2 || m.wild[0].value != "event" || m.wild[1].value != "eval" {
		t.Fatalf(":e suggestions = %+v (full names only)", m.wild)
	}
	for _, c := range m.wild {
		if strings.Contains(c.desc, ":e") {
			t.Fatalf("suggestion mentions a short form: %+v", c)
		}
	}
	m = typeKeys(t, m, " ")
	if len(m.wild) != 2 || m.wild[0].value != "join" {
		t.Fatalf(":e  suggestions = %+v", m.wild)
	}
	m = typeKeys(t, m, "j ")
	if len(m.wild) != 1 || m.wild[0].value != "7" {
		t.Fatalf(":e j suggestions = %+v", m.wild)
	}
}

func TestHelpWithoutAliases(t *testing.T) {
	m := testModel(t, nil)
	m = update(t, m, tea.WindowSizeMsg{Width: 120, Height: 50})
	m = update(t, m, key("?"))
	v := ansi.Strip(m.View())
	if !strings.Contains(v, ":events") || strings.Contains(v, "[e]") || strings.Contains(v, ":sl ") {
		t.Fatalf("help shows short forms:\n%s", v)
	}
}

func TestOptionSuggestions(t *testing.T) {
	m := testModel(t, nil)
	m = update(t, m, key(":"))
	m = typeKeys(t, m, "link libft ")
	if len(m.wild) != 1 || m.wild[0].value != "--here" {
		t.Fatalf(":link libft  suggestions = %+v", m.wild)
	}
	m = typeKeys(t, m, "--here ")
	if len(m.wild) != 0 {
		t.Fatalf("--here suggested twice: %+v", m.wild)
	}
	m = update(t, m, key("esc"))

	m = update(t, m, key(":"))
	m = typeKeys(t, m, "submit ")
	if len(m.wild) != 3 {
		t.Fatalf(":submit  suggestions = %+v", m.wild)
	}
	m = typeKeys(t, m, "--n")
	if len(m.wild) != 1 || m.wild[0].value != "--no-norm" {
		t.Fatalf(":submit --n suggestions = %+v", m.wild)
	}
	m = typeKeys(t, m, "o-norm --branch ")
	if len(m.wild) != 0 {
		t.Fatalf("after --branch, a value is expected: %+v", m.wild)
	}
	m = update(t, m, key("esc"))

	m = update(t, m, key(":"))
	m = typeKeys(t, m, "l li")
	if len(m.wild) == 0 || m.wild[0].value != "libft" {
		t.Fatalf(":l li suggestions = %+v (projects before options)", m.wild)
	}
}

func TestReminders(t *testing.T) {
	m := testModel(t, nil)
	m.remindBefore = 15 * time.Minute
	now := time.Now()
	soon, later := now.Add(10*time.Minute), now.Add(2*time.Hour)
	comment := "Clean code"
	m.evals = &api.Evaluations{
		AsCorrector: []api.ScaleTeam{{ID: 1, BeginAt: &soon, Team: &api.Team{ProjectID: 1314}, Correcteds: api.Correcteds{{Login: "neric"}}}},
		AsCorrected: []api.ScaleTeam{{ID: 2, BeginAt: &later, Comment: &comment}},
	}
	m.evalNames = map[int]string{1314: "42cursus-libft"}
	m.myEvents = []api.Event{{ID: 9, Name: "Go workshop", Location: "Belfius room", BeginAt: now.Add(5 * time.Minute)}}

	due := m.dueReminders(now)
	if len(due) != 2 {
		t.Fatalf("due = %+v (evaluation in 10 min and event in 5 min, not the one in 2h)", due)
	}
	cmd := m.checkReminders(now)
	if cmd == nil || !strings.Contains(m.flash, "In ") || !m.reminded["eval:1"] || !m.reminded["event:9"] {
		t.Fatalf("flash=%q reminded=%v", m.flash, m.reminded)
	}
	if again := m.dueReminders(now.Add(time.Minute)); len(again) != 0 {
		t.Fatalf("announced twice: %+v", again)
	}
	m.remindBefore = 0
	if c := m.startReminders(); c != nil {
		t.Fatal("reminders started while disabled")
	}
}

func TestEvaluationCommentsAndLogtime(t *testing.T) {
	m := testModel(t, &config.Link{ProjectID: 1314, Slug: "42cursus-libft", Name: "Libft"})
	done := time.Now().Add(-time.Hour)
	mark, comment, feedback := 100, "Great work, clean Makefile", "Thorough evaluator"
	m = update(t, m, statusMsg{id: 1314, s: &api.ProjectStatus{
		Project: api.ProjectRef{ID: 1314, Slug: "42cursus-libft", Name: "Libft"}, Status: "finished",
		Team:        &api.Team{Name: "g"},
		Evaluations: []api.ScaleTeam{{BeginAt: &done, FilledAt: &done, FinalMark: &mark, Comment: &comment, Feedback: &feedback, Corrector: api.Corrector{Login: "neric"}}},
	}})
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "“Great work, clean Makefile”") || !strings.Contains(v, "↳ “Thorough evaluator”") {
		t.Fatalf("comments missing:\n%s", v)
	}

	m, _ = command(t, m, "lt")
	if m.view != viewLogtime || m.logtimeLogin != "swilmus" {
		t.Fatalf(":lt: view=%d login=%q", m.view, m.logtimeLogin)
	}
	m = update(t, m, logtimeMsg{login: "swilmus", days: map[string]time.Duration{api.DayKey(time.Now()): 5*time.Hour + 12*time.Minute}})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "5h12") || !strings.Contains(v, "█") || !strings.Contains(v, "week of") {
		t.Fatalf("logtime view:\n%s", v)
	}
}

func TestEvalClone(t *testing.T) {
	m := testModel(t, nil) // no ft binary in tests: execFT reports it
	m, _ = command(t, m, "eval clone")
	if !strings.Contains(m.flash, "ft binary") {
		t.Fatalf(":eval clone did not try to run ft eval clone: %q", m.flash)
	}
	m = update(t, m, key(":"))
	m = typeKeys(t, m, "eval ")
	if len(m.wild) != 1 || m.wild[0].value != "clone" {
		t.Fatalf(":eval suggestions = %+v", m.wild)
	}
}
