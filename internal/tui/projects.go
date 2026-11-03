package tui

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/config"
	"github.com/swilmus/ft/internal/fuzzy"
)

// The projects panel groups projects by cursus. When ft has the holy graph
// of the user's campus and main cursus (api.Circles), that cursus is shown
// circle by circle, upcoming projects included; otherwise its upcoming
// projects get their own group (by XP), loaded when first expanded. The
// other cursus (piscines…) come last, folded.

const (
	upcomingGroup = "upcoming"
	modulesGroup  = "modules" // optional piscines' modules, without a graph
)

type group struct {
	key   string
	label string
}

// row is a line of the projects panel: a group header (idx < 0) or an item.
type row struct {
	group string
	idx   int
}

func (r row) header() bool { return r.idx < 0 }

type curriculumMsg struct {
	projects []api.Project
	err      error
}

func cursusKey(id int) string { return "c" + strconv.Itoa(id) }
func circleKey(c int) string  { return "circle" + strconv.Itoa(c) }

func isCircleKey(key string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(key, "circle"))
	return n, err == nil && strings.HasPrefix(key, "circle")
}

func mainCursusID(me *api.User) int {
	if cu := me.MainCursus(); cu != nil {
		return cu.Cursus.ID
	}
	return api.DefaultCursusID
}

// graphFor returns the holy graph of the user's campus and main cursus, in
// the curriculum variant of their quests, or nil.
func graphFor(me *api.User, quests []string) *api.CircleTable {
	campusID := 0
	if c := me.PrimaryCampus(); c != nil {
		campusID = c.ID
	}
	return api.Graph(campusID, mainCursusID(me), quests)
}

// validatedFn reports whether the user validated a project, by slug.
func validatedFn(me *api.User) func(string) bool {
	ok := map[string]bool{}
	for _, pu := range me.ProjectsUsers {
		if pu.Validated != nil && *pu.Validated {
			ok[pu.Project.Slug] = true
		}
	}
	return func(slug string) bool { return ok[slug] }
}

// buildGroups orders the groups: the main cursus (by circle, or plus its
// upcoming projects), then the other cursus.
func buildGroups(me *api.User, t *api.CircleTable) []group {
	mainID := mainCursusID(me)
	names := map[int]string{}
	for _, cu := range me.CursusUsers {
		names[cu.Cursus.ID] = cu.Cursus.Name
	}
	label := func(id int) string {
		if n := names[id]; n != "" {
			return n
		}
		return "Other"
	}
	var groups []group
	if t != nil {
		for c := 0; c <= api.AfterCore; c++ {
			groups = append(groups, group{circleKey(c), api.CircleName(c)})
		}
		groups = append(groups, group{cursusKey(mainID), label(mainID) + " · off the graph"})
	} else {
		groups = append(groups, group{cursusKey(mainID), label(mainID)}, group{upcomingGroup, "Upcoming"}, group{modulesGroup, "Piscine modules"})
	}
	for _, cu := range me.CursusUsers {
		if cu.Cursus.ID != mainID {
			groups = append(groups, group{cursusKey(cu.Cursus.ID), label(cu.Cursus.ID)})
		}
	}
	return append(groups, group{cursusKey(0), "Other"})
}

// buildItems lists the user's projects (without sub-projects) by cursus or
// circle, the linked one, and the curriculum projects not started yet.
func buildItems(me *api.User, link *config.Link, curriculum []api.Project, t *api.CircleTable) []item {
	mainID := mainCursusID(me)
	known := map[int]bool{}
	for _, cu := range me.CursusUsers {
		known[cu.Cursus.ID] = true
	}
	campusID := 0
	if c := me.PrimaryCampus(); c != nil {
		campusID = c.ID
	}
	// place puts a main-cursus project on its circle when the graph has it;
	// an older slug (e.g. 42cursus-push_swap for 42next-push_swap) takes the
	// place of the project it counts for.
	covered := map[string]bool{}
	place := func(it *item) {
		it.circle = -1
		if info, ok := t.Lookup(it.ref.Slug); ok {
			it.group, it.circle, it.tracks = circleKey(info.Circle), info.Circle, info.Tracks
			it.choice, it.planet = info.Choice, info.Planet
			if info.Primary != "" {
				covered[info.Primary] = true
			}
		}
	}
	// enrich adds what only the curriculum knows (XP, estimate, team size).
	byID := map[int]*api.Project{}
	for i := range curriculum {
		byID[curriculum[i].ID] = &curriculum[i]
	}
	enrich := func(it *item) {
		p := byID[it.ref.ID]
		if p == nil {
			return
		}
		it.xp = p.Difficulty
		if s := p.SessionFor(campusID, mainID); s != nil {
			it.solo, it.maxPeople = s.Solo, s.MaxPeople
			if s.EstimateTime != nil {
				it.estimate = *s.EstimateTime
			}
		}
	}

	var items []item
	seen := map[int]bool{}
	for _, pu := range me.ProjectsUsers {
		if pu.Project.ParentID != nil {
			continue
		}
		seen[pu.Project.ID] = true
		it := item{
			ref: pu.Project, status: pu.Status, validated: pu.Validated, mark: pu.FinalMark, markedAt: pu.MarkedAt,
			group: cursusKey(projectCursus(pu.CursusIDs, mainID, known)), circle: -1,
		}
		if it.group == cursusKey(mainID) {
			place(&it)
		}
		enrich(&it)
		items = append(items, it)
	}
	if link != nil && !seen[link.ProjectID] {
		seen[link.ProjectID] = true
		it := item{
			ref:    api.ProjectRef{ID: link.ProjectID, Slug: link.Slug, Name: link.Name},
			status: api.NotRegistered, group: cursusKey(mainID),
		}
		place(&it)
		enrich(&it)
		items = append(items, it)
	}
	for _, p := range curriculum {
		if seen[p.ID] || covered[p.Slug] {
			continue
		}
		it := item{
			ref:    api.ProjectRef{ID: p.ID, Slug: p.Slug, Name: p.Name},
			status: api.NotRegistered, group: upcomingGroup, upcoming: true,
		}
		if t != nil {
			place(&it)
		} else {
			it.circle = -1
			if p.OptionalModule() {
				it.group = modulesGroup
			}
		}
		enrich(&it)
		items = append(items, it)
	}

	// Order: by group, then within a circle by track (after the common
	// core) and XP; within a cursus, active work first, then the latest
	// marked; upcoming projects (without a graph) keep the curriculum order.
	rank := map[string]int{}
	for i, g := range buildGroups(me, t) {
		rank[g.key] = i
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := &items[i], &items[j]
		if rank[a.group] != rank[b.group] {
			return rank[a.group] < rank[b.group]
		}
		switch {
		case a.circle >= 0:
			if ta, tb := firstTrack(a), firstTrack(b); ta != tb {
				return ta < tb
			}
			if ea, eb := isExam(a), isExam(b); ea != eb {
				return eb // a circle's exam comes after its projects
			}
			if a.xp != b.xp {
				return a.xp < b.xp
			}
			return a.ref.Slug < b.ref.Slug
		case a.upcoming:
			return false
		}
		pa, pb := priority(a.status), priority(b.status)
		if pa != pb {
			return pa < pb
		}
		return timeOrZero(a.markedAt).After(timeOrZero(b.markedAt))
	})
	return items
}

func isExam(it *item) bool { return strings.HasPrefix(it.ref.Slug, "exam-") }

func firstTrack(it *item) string {
	if len(it.tracks) == 0 {
		return "\uffff" // untracked last
	}
	return it.tracks[0]
}

// projectCursus picks the group of a registration: the main cursus if it
// belongs to it, else its first known cursus, else 0 ("Other").
func projectCursus(ids []int, mainID int, known map[int]bool) int {
	if slices.Contains(ids, mainID) {
		return mainID
	}
	for _, id := range ids {
		if known[id] {
			return id
		}
	}
	return 0
}

func priority(status string) int {
	switch status {
	case "in_progress":
		return 0
	case "waiting_for_correction":
		return 1
	case "creating_group", "searching_a_group":
		return 2
	case api.NotRegistered:
		return 3
	case "finished":
		return 5
	}
	return 4
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// rebuild recomputes items and rows, keeping the selected project.
func (m *model) rebuild() {
	var keep int
	if it := m.selected(); it != nil {
		keep = it.ref.ID
	}
	m.graph = graphFor(m.me, m.quests)
	m.groups = buildGroups(m.me, m.graph)
	m.items = buildItems(m.me, m.link, m.curriculum, m.graph)
	m.applyFilter()
	if keep != 0 {
		m.selectProject(keep)
	}
}

// applyFilter computes the panel rows: grouped with headers, or a flat
// ranked list (across collapsed groups) while searching.
func (m *model) applyFilter() {
	m.shown = m.shown[:0]
	if m.filter != "" {
		fi := make([]fuzzy.Item, len(m.items))
		for i, it := range m.items {
			fi[i] = fuzzy.Item{Slug: it.ref.Slug, Name: it.ref.Name}
		}
		for _, match := range fuzzy.Rank(m.filter, fi) {
			m.shown = append(m.shown, row{group: m.items[match.Index].group, idx: match.Index})
		}
	} else {
		for _, g := range m.groups {
			_, isCircle := isCircleKey(g.key)
			if m.groupSize(g.key) == 0 && g.key != upcomingGroup && !isCircle {
				continue
			}
			m.shown = append(m.shown, row{group: g.key, idx: -1})
			if m.collapsed[g.key] {
				continue
			}
			for i, it := range m.items {
				if it.group == g.key {
					m.shown = append(m.shown, row{group: g.key, idx: i})
				}
			}
		}
	}
	m.cursor = clamp(m.cursor, 0, len(m.shown)-1)
}

// firstProjectRow is the first row showing a project, or 0.
func (m model) firstProjectRow() int {
	for i, r := range m.shown {
		if !r.header() {
			return i
		}
	}
	return 0
}

// circleProgress is "done/total" for a circle group.
func (m model) circleProgress(c int) (done, total int) {
	return m.graph.Progress(c, validatedFn(m.me))
}

// groupDone counts the validated projects of a group.
func (m model) groupDone(key string) int {
	n := 0
	for _, it := range m.items {
		if it.group == key && it.validated != nil && *it.validated {
			n++
		}
	}
	return n
}

func (m model) groupSize(key string) int {
	n := 0
	for _, it := range m.items {
		if it.group == key {
			n++
		}
	}
	return n
}

func (m model) groupLabel(key string) string {
	for _, g := range m.groups {
		if g.key == key {
			return g.label
		}
	}
	return key
}

func (m model) selected() *item {
	if m.cursor < 0 || m.cursor >= len(m.shown) || m.shown[m.cursor].header() {
		return nil
	}
	return &m.items[m.shown[m.cursor].idx]
}

// selectedGroup returns the group key when the cursor is on a header.
func (m model) selectedGroup() (string, bool) {
	if m.cursor < 0 || m.cursor >= len(m.shown) || !m.shown[m.cursor].header() {
		return "", false
	}
	return m.shown[m.cursor].group, true
}

// selectProject moves the cursor to a project, expanding its group.
func (m *model) selectProject(id int) {
	for _, it := range m.items {
		if it.ref.ID == id && m.collapsed[it.group] && m.filter == "" {
			m.collapsed[it.group] = false
			m.applyFilter()
			break
		}
	}
	for i, r := range m.shown {
		if !r.header() && m.items[r.idx].ref.ID == id {
			m.cursor = i
			return
		}
	}
}

// toggleGroup collapses or expands a group, loading the curriculum the
// first time Upcoming opens.
func (m *model) toggleGroup(key string) tea.Cmd {
	m.collapsed[key] = !m.collapsed[key]
	m.applyFilter()
	for i, r := range m.shown {
		if r.header() && r.group == key {
			m.cursor = i
		}
	}
	if key == upcomingGroup && !m.collapsed[key] && m.curriculum == nil && !m.curriculumLoading {
		return m.loadCurriculum()
	}
	return nil
}

func (m *model) loadCurriculum() tea.Cmd {
	if m.me == nil {
		return nil
	}
	m.curriculumLoading = true
	m.loading++
	ctx, c, me := m.ctx, m.c, m.me
	quests := m.quests
	return func() tea.Msg { return fetchCurriculum(ctx, c, me, quests) }
}

func fetchCurriculum(ctx context.Context, c *api.Client, me *api.User, quests []string) curriculumMsg {
	campusID := 0
	if camp := me.PrimaryCampus(); camp != nil {
		campusID = camp.ID
	}
	ps, err := c.Curriculum(ctx, mainCursusID(me), campusID, quests)
	return curriculumMsg{ps, err}
}
