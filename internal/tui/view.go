package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/format"
	"github.com/skyreks00/ft/internal/fuzzy"
)

const (
	minWidth  = 60
	minHeight = 14
)

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return fmt.Sprintf("Terminal too small (%d×%d, need %d×%d). q to quit.", m.width, m.height, minWidth, minHeight)
	}
	leftW, projH, bodyH := m.layout()
	rightW := m.width - leftW

	left := lipgloss.JoinVertical(lipgloss.Left,
		m.viewProjects(leftW, projH),
		m.viewSlots(leftW, bodyH-projH),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, m.viewDetails(rightW, bodyH))
	if m.wildVisible() {
		body += "\n" + m.wildLine()
	}
	return body + "\n" + m.viewStatusBar()
}

// layout returns the left column width and the heights of the projects
// panel and of the body (everything but the status bar).
func (m model) layout() (leftW, projH, bodyH int) {
	bodyH = m.height - 1
	if m.wildVisible() {
		bodyH-- // the suggestions bar
	}
	return clamp(m.width*3/10, 26, 42), bodyH * 3 / 5, bodyH
}

// listStart is the first visible row of a list of rows lines whose cursor
// is at cur (the list scrolls to keep the cursor on screen).
func listStart(cur, rows int) int {
	return max(0, cur-rows+1)
}

func (m model) viewProjects(w, h int) string {
	inner, rows := w-2, h-2
	n := 0
	for _, r := range m.shown {
		if !r.header() {
			n++
		}
	}
	title := fmt.Sprintf("Projects %d", n)
	if m.filter != "" {
		title += " /" + m.filter
	}
	var lines []string
	switch {
	case m.me == nil:
		lines = []string{" " + m.spin.View() + " loading…"}
	case len(m.shown) == 0:
		lines = []string{sDim.Render(" no match")}
	}
	start := listStart(m.cursor, rows)
	for i := start; i < len(m.shown) && len(lines) < rows; i++ {
		var row string
		if r := m.shown[i]; r.header() {
			row = m.headerRow(r.group, inner)
		} else {
			row = m.projectRow(m.items[r.idx], inner)
		}
		if i == m.cursor {
			if m.focus == panelProjects {
				row = sSelected.Render(fit(ansi.Strip(row), inner))
			} else {
				row = sBold.Render(fit(ansi.Strip(row), inner))
			}
		}
		lines = append(lines, row)
	}
	return box(title, lines, w, h, m.focus == panelProjects)
}

// headerRow renders a group header: ▾ 42cursus      3
func (m model) headerRow(key string, inner int) string {
	arrow := "▾"
	if m.collapsed[key] && m.filter == "" {
		arrow = "▸"
	}
	count := strconv.Itoa(m.groupSize(key))
	if c, ok := isCircleKey(key); ok {
		if m.graph != nil {
			done, total := m.circleProgress(c)
			count = fmt.Sprintf("%d/%d", done, total)
		}
	}
	if key == upcomingGroup && m.curriculum == nil {
		count = ""
		if m.curriculumLoading {
			count = m.spin.View()
		}
	}
	return spread(sAccent.Render(" "+arrow+" ")+sAccent.Bold(true).Render(m.groupLabel(key)), sDim.Render(count+" "), inner)
}

func (m model) projectRow(it item, inner int) string {
	name := fuzzy.ShortSlug(it.ref.Slug)
	if m.link != nil && m.link.ProjectID == it.ref.ID {
		name += " ⌂"
	}
	right := ""
	switch {
	case it.mark != nil:
		right = strconv.Itoa(*it.mark) + " "
	case it.upcoming && it.xp > 0:
		right = strconv.Itoa(it.xp) + "xp "
	}
	return spread("   "+glyph(it.status, it.validated)+" "+name, sDim.Render(right), inner)
}

func glyph(status string, validated *bool) string {
	switch {
	case validated != nil && *validated:
		return sOK.Render("✓")
	case status == "finished":
		return sErr.Render("✗")
	case status == "in_progress":
		return sWarn.Render("●")
	case status == "waiting_for_correction":
		return sInfo.Render("◷")
	case status == api.NotRegistered:
		return sDim.Render("○")
	}
	return sDim.Render("·")
}

func (m model) viewSlots(w, h int) string {
	inner, rows := w-2, h-2
	ranges := m.slotRanges()
	var lines []string
	switch {
	case m.slotsErr != nil:
		lines = []string{sErr.Render(" " + m.slotsErr.Error())}
	case m.slots == nil && m.loading > 0:
		lines = []string{" " + m.spin.View() + " loading…"}
	case len(ranges) == 0 && m.platform == nil && api.SlotsPlatform(m.slotsURL, m.me) != "":
		host := strings.TrimPrefix(api.SlotsPlatform(m.slotsURL, m.me), "https://")
		lines = []string{sDim.Render(" managed on " + host), sDim.Render(" run ft slots auth")}
	case len(ranges) == 0:
		lines = []string{sDim.Render(" no upcoming slots")}
	}
	start := listStart(m.slotCur, rows)
	for i := start; i < len(ranges) && len(lines) < rows; i++ {
		r := ranges[i]
		state := sOK.Render("free")
		if r.Booked {
			state = sWarn.Render("booked")
		}
		if p, ok := m.pslots[r.ID()]; ok {
			tag := strings.ToUpper(p.Campus)
			if p.Remote {
				tag += "⇄"
			}
			state = sDim.Render(tag) + " " + state
		}
		when := r.BeginAt.Local().Format("Mon 02 15:04") + "–" + r.EndAt.Local().Format("15:04")
		row := spread(" "+when, state+" ", inner)
		if i == m.slotCur && m.focus == panelSlots {
			row = sSelected.Render(fit(ansi.Strip(row), inner))
		}
		lines = append(lines, row)
	}
	title := fmt.Sprintf("Slots %d", len(ranges))
	if m.platform != nil {
		title += " · " + strings.TrimPrefix(strings.TrimPrefix(m.platform.Session.BaseURL, "https://"), "slots.")
	}
	return box(title, lines, w, h, m.focus == panelSlots)
}

func (m model) viewDetails(w, h int) string {
	if m.help {
		return box("Help", helpLines(), w, h, true)
	}
	focused := m.focus == panelDetails
	switch m.view {
	case viewEvents:
		lines := m.eventLines(w - 2)
		start := clamp(m.evCur-(h-2)+10, 0, max(0, len(lines)-(h-2)))
		return box(m.viewTitle(), lines[start:], w, h, focused)
	case viewEvals, viewUser, viewLogtime:
		lines := m.evalLines(w - 2)
		switch m.view {
		case viewUser:
			lines = m.userLines()
		case viewLogtime:
			lines = m.logtimeLines(w - 2)
		}
		start := min(m.scroll, max(0, len(lines)-(h-2)))
		return box(m.viewTitle(), lines[start:], w, h, focused)
	}
	it := m.selected()
	if key, ok := m.selectedGroup(); ok {
		return box(m.groupLabel(key), m.groupLines(key), w, h, focused)
	}
	if it == nil {
		return box("", nil, w, h, m.focus == panelDetails)
	}
	title := it.ref.Name
	if title == "" {
		title = it.ref.Slug
	}
	lines := m.detailLines(it, w-2)
	start := min(m.scroll, max(0, len(lines)-(h-2)))
	return box(title, lines[start:], w, h, m.focus == panelDetails)
}

// groupLines describes a group header in the details panel.
func (m model) groupLines(key string) []string {
	if c, ok := isCircleKey(key); ok {
		done, total := m.circleProgress(c)
		lines := []string{fmt.Sprintf(" %d/%d validated", done, total)}
		if m.graph.Variant != "" {
			lines = append(lines, sDim.Render(" "+m.graph.Variant))
		}
		if m.curriculum == nil {
			lines = append(lines, "", " "+m.spin.View()+sDim.Render(" loading…"))
		}
		return lines
	}
	if key == upcomingGroup {
		return []string{sDim.Render(" Not started, by XP.")}
	}
	return []string{fmt.Sprintf(" %d projects", m.groupSize(key))}
}

func (m model) detailLines(it *item, inner int) []string {
	label := func(s string) string { return " " + sDim.Render(fmt.Sprintf("%-12s", s)) }
	var lines []string
	// The circle is the group on the left; only what it does not say.
	if len(it.tracks) > 0 {
		lines = append(lines, label("Tracks")+strings.Join(it.tracks, ", "))
	}
	if len(it.choice) > 1 {
		lines = append(lines, label("Choose one")+strings.Join(shortSlugs(it.choice), ", "))
	}
	if len(it.planet) > 1 {
		lines = append(lines, label("Planet")+strings.Join(shortSlugs(it.planet), ", "))
	}
	if it.upcoming {
		if it.xp > 0 {
			lines = append(lines, label("XP")+strconv.Itoa(it.xp))
		}
		if it.estimate != "" {
			lines = append(lines, label("Estimated")+it.estimate)
		}
		team := ""
		switch {
		case it.solo != nil && !*it.solo && it.maxPeople != nil && *it.maxPeople > 1:
			team = fmt.Sprintf("group, up to %d", *it.maxPeople)
		case it.solo != nil && !*it.solo:
			team = "group"
		case it.solo != nil:
			team = "solo"
		}
		if team != "" {
			lines = append(lines, label("Team")+team)
		}
		return append(lines, label("Status")+sDim.Render("not started"))
	}
	if m.link != nil && m.link.ProjectID == it.ref.ID {
		lines = append(lines, label("Folder")+format.TildePath(m.linkDir))
	}

	st, err := m.statuses[it.ref.ID], m.errs[it.ref.ID]
	switch {
	case err != nil:
		lines = append(lines, "", " "+sErr.Render(err.Error()))
	case st == nil && it.status == api.NotRegistered:
		lines = append(lines, label("Status")+sDim.Render("not registered"))
	case st == nil:
		lines = append(lines, " "+m.spin.View()+sDim.Render(" loading…"))
	default:
		lines = append(lines, statusLines(st, label, inner)...)
	}
	return lines
}

func statusLines(st *api.ProjectStatus, label func(string) string, inner int) []string {
	if !st.Registered() {
		return []string{label("Status") + sDim.Render("not registered")}
	}
	status := format.Status(st.Status)
	if st.FinalMark != nil {
		status += fmt.Sprintf("  mark %d", *st.FinalMark)
	}
	if st.Validated != nil {
		if *st.Validated {
			status = sOK.Render(status + " ✓")
		} else {
			status = sErr.Render(status + " ✗")
		}
	}
	lines := []string{label("Status") + status}

	if t := st.Team; t != nil {
		var members []string
		for _, u := range t.Users {
			if u.Leader {
				members = append(members, u.Login+"*")
			} else {
				members = append(members, u.Login)
			}
		}
		team := t.Name + sDim.Render(" · ") + strings.Join(members, ", ")
		var flags []string
		if t.Locked {
			flags = append(flags, "locked")
		}
		if t.Closed {
			flags = append(flags, "closed")
		}
		if len(flags) > 0 {
			team += sDim.Render("  [" + strings.Join(flags, ", ") + "]")
		}
		lines = append(lines, label("Team")+team)
		repo := t.RepoURL
		if repo == "" {
			repo = sDim.Render("not created yet")
		}
		lines = append(lines, label("Repo")+repo)
		if t.TerminatingAt != nil && !t.Closed {
			lines = append(lines, label("Deadline")+format.DateWithDays(*t.TerminatingAt))
		}
	}
	if st.BlackholedAt != nil {
		lines = append(lines, label("Blackhole")+format.DateWithDays(*st.BlackholedAt))
	}
	if st.RetriableAt != nil {
		lines = append(lines, label("Retry")+"after "+st.RetriableAt.Local().Format("2006-01-02 15:04"))
	}
	if st.Team == nil {
		return lines
	}

	done := st.EvaluationsDone()
	evals := fmt.Sprintf("%d done", done)
	if st.RequiredEvaluations > 0 {
		evals = fmt.Sprintf("%d/%d done", done, st.RequiredEvaluations)
	}
	lines = append(lines, label("Evaluations")+evals)
	for _, e := range st.Evaluations {
		when := sDim.Render("unscheduled     ")
		if e.BeginAt != nil {
			when = e.BeginAt.Local().Format("2006-01-02 15:04")
		}
		who := e.Corrector.Login
		if who == "" {
			who = "(hidden)"
		}
		result := sWarn.Render("pending")
		if e.Done() {
			result = "—"
			if e.FinalMark != nil {
				result = strconv.Itoa(*e.FinalMark)
			}
			if e.Flag != nil && e.Flag.Name != "" && e.Flag.Name != "Ok" {
				result += " " + sErr.Render(e.Flag.Name)
			}
		}
		lines = append(lines, fmt.Sprintf("   %s  %-10s %s", when, who, result))
		lines = append(lines, commentLines(&e, max(20, inner-10))...)
	}
	return lines
}

func helpLines() []string {
	rows := [][2]string{
		{"", "MOVE"},
		{"j / k", "down / up"},
		{"g / G", "top / bottom"},
		{"ctrl+d / ctrl+u", "half page down / up"},
		{"h / l, tab", "switch panel"},
		{"enter / space", "fold or unfold a group, or open the details"},
		{"/", "search projects (esc clears)"},
		{"esc", "back to the project"},
		{"?", "this help"},
		{"q", "quit"},
		{"", ""},
		{"", "COMMANDS  (:, tab completes)"},
		{":open", "the project's intra page"},
		{":submit [--dry-run] [--no-norm] [--branch b]", "push the linked folder (git, norminette)"},
		{":correct", "book an evaluation"},
		{":link <project> [--here]", "clone and link a project (--here: this folder)"},
		{":slot add <start> <end>", "e.g. :slot add tomorrow 14:00 +2h"},
		{":slot rm [id]", "the selected slot, or an id"},
		{":events", "campus events"},
		{":event join|leave [id]", "the selected event, or an id"},
		{":eval", "upcoming and recent evaluations, with comments"},
		{":eval clone [id]", "clone the repository of the team you evaluate"},
		{":logtime [login]", "time spent on campus, last two weeks"},
		{":user <login>", "a student's profile"},
		{":refresh", "reload"},
		{":quit", ""},
	}
	var lines []string
	for _, r := range rows {
		switch {
		case r[0] == "" && r[1] == "":
			lines = append(lines, "")
		case r[0] == "":
			lines = append(lines, " "+sAccent.Bold(true).Render(r[1]))
		default:
			lines = append(lines, " "+sKey.Render(fmt.Sprintf("%-44s", r[0]))+r[1])
		}
	}
	return append(lines, "", sDim.Render(" esc or ? to close"))
}

func (m model) viewStatusBar() string {
	badge := sBadge.Background(modeColors[m.mode]).Render(m.mode.String())
	if m.mode == modeConfirm {
		return fit(badge+" "+sBold.Render(m.confirmPrompt)+sDim.Render("  y / N"), m.width)
	}
	if m.mode != modeNormal {
		prompt := ":"
		if m.mode == modeSearch {
			prompt = "/"
		}
		return fit(badge+" "+prompt+m.input.View(), m.width)
	}

	left := badge
	if m.me != nil {
		left += " " + sBold.Render(m.me.Login)
		if cu := m.me.MainCursus(); cu != nil {
			left += sDim.Render(fmt.Sprintf("  Lvl %.2f", cu.Level))
		}
	}
	switch {
	case m.flash != "" && m.flashErr:
		left += "  " + sErr.Render(m.flash)
	case m.flash != "":
		left += "  " + sAccent.Render(m.flash)
	case m.loading > 0 || len(m.inflight) > 0:
		left += "  " + m.spin.View()
	}

	right := ""
	if m.c != nil {
		if q := m.c.Quota(); q.Known {
			right = sDim.Render(fmt.Sprintf("%d req/h ", q.HourlyRemaining))
		}
	}
	return spread(left, right, m.width)
}

func shortSlugs(slugs []string) []string {
	out := make([]string, len(slugs))
	for i, s := range slugs {
		out[i] = fuzzy.ShortSlug(s)
	}
	return out
}
