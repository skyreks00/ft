package tui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/skyreks00/ft/internal/api"
	"github.com/skyreks00/ft/internal/format"
	"github.com/skyreks00/ft/internal/slots42"
)

// rightView is what the details panel shows.
type rightView int

const (
	viewProject rightView = iota
	viewEvents
	viewEvals
	viewUser
	viewLogtime
)

type (
	eventsMsg struct {
		events []api.Event
		joined map[int]bool
		err    error
	}
	evalsMsg struct {
		evals *api.Evaluations // upcoming
		past  *api.Evaluations // the last few, with comments
		names map[int]string
		err   error
	}
	logtimeMsg struct {
		login string
		days  map[string]time.Duration
		err   error
	}
	userMsg struct {
		user *api.User
		last *api.Location
		err  error
	}
	// doneMsg reports a write; reload names what to fetch again.
	doneMsg struct {
		text   string
		err    error
		reload string // "slots" or "events"
	}
)

// --- loaders --------------------------------------------------------------

func (m model) loadEvents(fresh bool) tea.Cmd {
	ctx, c, me := m.reqCtx(fresh), m.c, m.me
	return func() tea.Msg {
		if me == nil {
			return eventsMsg{err: fmt.Errorf("profile not loaded yet")}
		}
		campus := me.PrimaryCampus()
		if campus == nil {
			return eventsMsg{err: fmt.Errorf("no campus on your profile")}
		}
		cursusID := api.DefaultCursusID
		if cu := me.MainCursus(); cu != nil {
			cursusID = cu.Cursus.ID
		}
		events, err := c.CampusEvents(ctx, campus.ID, cursusID)
		if err != nil {
			return eventsMsg{err: err}
		}
		mine, err := c.MyEvents(ctx, me.ID)
		if err != nil {
			return eventsMsg{err: err}
		}
		joined := map[int]bool{}
		for _, e := range mine {
			joined[e.ID] = true
		}
		return eventsMsg{events: events, joined: joined}
	}
}

func (m model) loadEvals(fresh bool) tea.Cmd {
	ctx, c, me := m.reqCtx(fresh), m.c, m.me
	return func() tea.Msg {
		if me == nil {
			return evalsMsg{err: fmt.Errorf("profile not loaded yet")}
		}
		ev, err := c.UpcomingEvaluations(ctx, me.Login)
		if err != nil {
			return evalsMsg{err: err}
		}
		past, err := c.PastEvaluations(ctx, me.Login, pastEvals)
		if err != nil {
			return evalsMsg{err: err}
		}
		all := &api.Evaluations{
			AsCorrector: append(append([]api.ScaleTeam{}, ev.AsCorrector...), past.AsCorrector...),
			AsCorrected: append(append([]api.ScaleTeam{}, ev.AsCorrected...), past.AsCorrected...),
		}
		return evalsMsg{evals: ev, past: past, names: c.ProjectSlugs(ctx, me, all)}
	}
}

// pastEvals is how many past evaluations per side the eval view shows.
const pastEvals = 5

// logtimeDays is the span of the logtime view.
const logtimeDays = 14

func (m model) loadLogtime(login string) tea.Cmd {
	ctx, c := m.ctx, m.c
	return func() tea.Msg {
		now := time.Now()
		from := format.WeekStart(now).AddDate(0, 0, -7)
		locs, err := c.Locations(ctx, login, from.AddDate(0, 0, -1), now)
		if err != nil {
			return logtimeMsg{login: login, err: err}
		}
		return logtimeMsg{login: login, days: api.Logtime(locs, from, now, now)}
	}
}

func (m model) loadUser(login string) tea.Cmd {
	ctx, c := m.ctx, m.c
	return func() tea.Msg {
		u, err := c.User(ctx, login)
		if api.IsNotFound(err) {
			return userMsg{err: fmt.Errorf("no user %q", login)}
		}
		if err != nil {
			return userMsg{err: err}
		}
		last, err := c.LastLocation(ctx, u.Login)
		return userMsg{user: u, last: last, err: err}
	}
}

// --- writes ---------------------------------------------------------------

func (m model) addSlot(start, end string) tea.Cmd {
	begin, finish, err := format.ParseRange(start, end, time.Now())
	if err != nil {
		return func() tea.Msg { return doneMsg{err: err} }
	}
	ctx, c, me := m.ctx, m.c, m.me
	if p := m.platform; p != nil {
		return func() tea.Msg {
			prefs, err := p.Prefs(ctx)
			if err == nil {
				err = p.Create(ctx, prefs, begin, finish)
			}
			if err != nil {
				return doneMsg{err: err, reload: "slots"}
			}
			where := slots42.CampusName(prefs.Campus)
			if prefs.Remote {
				where += ", inter-campus"
			}
			return doneMsg{text: "Opened " + slotLabel(begin, finish) + " (" + where + ")", reload: "slots"}
		}
	}
	return func() tea.Msg {
		if _, err := c.CreateSlot(ctx, me.ID, begin, finish); err != nil {
			return doneMsg{err: err, reload: "slots"}
		}
		return doneMsg{text: "Opened " + slotLabel(begin, finish), reload: "slots"}
	}
}

func (m model) removeSlot(r api.SlotRange) tea.Cmd {
	ctx, c := m.ctx, m.c
	if p := m.platform; p != nil {
		var targets []slots42.Slot
		for _, id := range r.IDs {
			if s, ok := m.pslots[id]; ok {
				targets = append(targets, s)
			}
		}
		return func() tea.Msg {
			prefs, err := p.Prefs(ctx)
			if err != nil {
				return doneMsg{err: err, reload: "slots"}
			}
			for _, s := range targets {
				if err := p.Delete(ctx, prefs, s); err != nil {
					return doneMsg{err: err, reload: "slots"}
				}
			}
			return doneMsg{text: "Removed " + slotLabel(r.BeginAt, r.EndAt), reload: "slots"}
		}
	}
	return func() tea.Msg {
		for _, id := range r.IDs {
			if err := c.DeleteSlot(ctx, id); err != nil {
				return doneMsg{err: err, reload: "slots"}
			}
		}
		return doneMsg{text: "Removed " + slotLabel(r.BeginAt, r.EndAt), reload: "slots"}
	}
}

func (m model) toggleEvent(e api.Event, join bool) tea.Cmd {
	ctx, c, me := m.ctx, m.c, m.me
	return func() tea.Msg {
		if join {
			if err := c.JoinEvent(ctx, e.ID, me.ID); err != nil {
				return doneMsg{err: err, reload: "events"}
			}
			return doneMsg{text: "Joined " + e.Name, reload: "events"}
		}
		if err := c.LeaveEvent(ctx, e.ID, me.ID); err != nil {
			return doneMsg{err: err, reload: "events"}
		}
		return doneMsg{text: "Left " + e.Name, reload: "events"}
	}
}

// askConfirm switches to CONFIRM mode; y runs cmd.
func (m *model) askConfirm(prompt string, cmd tea.Cmd) {
	m.mode = modeConfirm
	m.confirmPrompt = prompt
	m.confirmCmd = cmd
}

// --- palette commands -----------------------------------------------------

// slotCommand handles `:slot add <start> <end>` and `:slot rm [id]`.
func (m model) slotCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.focus = panelSlots
		return m, nil
	}
	switch args[0] {
	case "add":
		if platform := api.SlotsPlatform(m.slotsURL, m.me); platform != "" && m.platform == nil {
			return m, m.setFlash(true, "run `ft slots auth` once to manage %s slots from ft", platform)
		}
		start, end, ok := splitRange(args[1:])
		if !ok {
			return m, m.setFlash(true, `usage: :slot add <start> <end>, e.g. :slot add tomorrow 14:00 +2h`)
		}
		return m, m.addSlot(start, end)
	case "rm", "del", "delete":
		r, ok := m.selectedRange()
		if len(args) > 1 {
			id, err := strconv.Atoi(args[1])
			if err != nil {
				return m, m.setFlash(true, "invalid slot id %q", args[1])
			}
			r, ok = findSlotRange(m.slotRanges(), id)
		}
		if !ok {
			return m, m.setFlash(true, "no such slot")
		}
		return m.confirmRemove(r)
	}
	return m, m.setFlash(true, "usage: :slot add <start> <end> | :slot rm [id]")
}

// splitRange splits "tomorrow 9:00 +2h" into start and end: the start may
// take two words (day and time), the end one or two.
func splitRange(f []string) (string, string, bool) {
	switch len(f) {
	case 2:
		return f[0], f[1], true
	case 3:
		return f[0] + " " + f[1], f[2], true
	case 4:
		return f[0] + " " + f[1], f[2] + " " + f[3], true
	}
	return "", "", false
}

func (m model) confirmRemove(r api.SlotRange) (tea.Model, tea.Cmd) {
	if r.Booked {
		return m, m.setFlash(true, "this slot is booked: cancel the evaluation on the intra")
	}
	m.askConfirm("Remove slot "+slotLabel(r.BeginAt, r.EndAt)+"?", m.removeSlot(r))
	return m, nil
}

// eventCommand handles `:event join|leave <id>` and `:events`.
func (m model) eventCommand(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		return m.showView(viewEvents)
	}
	if len(args) > 2 || (args[0] != "join" && args[0] != "leave") {
		return m, m.setFlash(true, "usage: :event join|leave [id]")
	}
	if len(args) == 1 {
		// The event selected in the events view.
		if m.view != viewEvents || m.evCur >= len(m.events) {
			return m, m.setFlash(true, "usage: :event %s <id> (or select one in :events)", args[0])
		}
		return m.confirmEvent(m.events[m.evCur], args[0] == "join")
	}
	id, err := strconv.Atoi(args[1])
	if err != nil {
		return m, m.setFlash(true, "invalid event id %q", args[1])
	}
	for _, e := range m.events {
		if e.ID == id {
			return m.confirmEvent(e, args[0] == "join")
		}
	}
	return m, m.setFlash(true, "event %d is not in the list (:events loads them)", id)
}

func (m model) confirmEvent(e api.Event, join bool) (tea.Model, tea.Cmd) {
	switch {
	case join && m.joined[e.ID]:
		return m, m.setFlash(false, "Already joined %s", e.Name)
	case !join && !m.joined[e.ID]:
		return m, m.setFlash(false, "Not subscribed to %s", e.Name)
	case join && e.Full():
		return m, m.setFlash(true, "%s is full", e.Name)
	}
	prompt := "Leave " + e.Name + "?"
	if join {
		prompt = "Join " + e.Name + " (" + slotLabel(e.BeginAt, e.EndAt) + ")?"
		if e.ProhibitionOfCancellation != nil {
			prompt = "Join " + e.Name + "? Cancelling is not allowed."
		}
	}
	m.askConfirm(prompt, m.toggleEvent(e, join))
	return m, nil
}

// showView switches the details panel, loading its data the first time.
func (m model) showView(v rightView) (tea.Model, tea.Cmd) {
	m.view = v
	m.focus = panelDetails
	m.scroll = 0
	switch {
	case v == viewEvents && m.events == nil:
		m.loading++
		return m, m.loadEvents(false)
	case v == viewEvals && m.evals == nil:
		m.loading++
		return m, m.loadEvals(false)
	}
	return m, nil
}

// --- slots helpers --------------------------------------------------------

func (m model) slotRanges() []api.SlotRange {
	return api.GroupSlots(m.slots)
}

func (m model) selectedRange() (api.SlotRange, bool) {
	rs := m.slotRanges()
	if m.slotCur < 0 || m.slotCur >= len(rs) {
		return api.SlotRange{}, false
	}
	return rs[m.slotCur], true
}

func findSlotRange(rs []api.SlotRange, id int) (api.SlotRange, bool) {
	for _, r := range rs {
		if slices.Contains(r.IDs, id) {
			return r, true
		}
	}
	return api.SlotRange{}, false
}

func slotLabel(b, e time.Time) string {
	b, e = b.Local(), e.Local()
	if b.YearDay() == e.YearDay() {
		return b.Format("Mon 02 Jan 15:04") + "–" + e.Format("15:04")
	}
	return b.Format("Mon 02 15:04") + " – " + e.Format("Mon 02 15:04")
}

// --- rendering ------------------------------------------------------------

func (m model) viewTitle() string {
	switch m.view {
	case viewEvents:
		return fmt.Sprintf("Events %d", len(m.events))
	case viewEvals:
		return "Evaluations"
	case viewUser:
		if m.user != nil {
			return "@" + m.user.Login
		}
		return "User"
	case viewLogtime:
		if m.logtimeLogin != "" && (m.me == nil || m.logtimeLogin != m.me.Login) {
			return "Logtime @" + m.logtimeLogin
		}
		return "Logtime"
	}
	return ""
}

// eventLines renders the events list; the selected row is highlighted.
func (m model) eventLines(inner int) []string {
	switch {
	case m.eventsErr != nil:
		return []string{" " + sErr.Render(m.eventsErr.Error())}
	case m.events == nil:
		return []string{" " + m.spin.View() + " loading…"}
	case len(m.events) == 0:
		return []string{sDim.Render(" no upcoming events")}
	}
	lines := []string{}
	for i, e := range m.events {
		mark := "  "
		if m.joined[e.ID] {
			mark = sOK.Render("✓ ")
		}
		seats := strconv.Itoa(e.NbrSubscribers)
		if e.Limited() {
			seats += "/" + strconv.Itoa(*e.MaxPeople)
		}
		left := " " + mark + e.BeginAt.Local().Format("Mon 02 Jan 15:04") + "  " + e.Name
		row := spread(left, sDim.Render(seats+" "), inner)
		if i == m.evCur {
			row = sSelected.Render(fit(ansi.Strip(row), inner))
		}
		lines = append(lines, row)
	}
	if m.evCur >= 0 && m.evCur < len(m.events) {
		e := m.events[m.evCur]
		lines = append(lines, "", " "+sBold.Render(e.Name)+sDim.Render("  #"+strconv.Itoa(e.ID)), " "+sDim.Render(slotLabel(e.BeginAt, e.EndAt)+" · "+e.Location+" · "+e.Kind))
		for _, l := range wrap(stripMarkdown(e.Description), inner-2, 6) {
			lines = append(lines, " "+l)
		}

	}
	return lines
}

func (m model) evalLines(inner int) []string {
	switch {
	case m.evalsErr != nil:
		return []string{" " + sErr.Render(m.evalsErr.Error())}
	case m.evals == nil:
		return []string{" " + m.spin.View() + " loading…"}
	}
	lines := []string{" " + sAccent.Bold(true).Render("You evaluate")}
	if len(m.evals.AsCorrector) == 0 {
		lines = append(lines, sDim.Render("   nothing planned"))
	}
	for _, s := range m.evals.AsCorrector {
		who := "(hidden until it starts)"
		if len(s.Correcteds) > 0 {
			var ls []string
			for _, u := range s.Correcteds {
				ls = append(ls, u.Login)
			}
			who = strings.Join(ls, ", ")
		}
		lines = append(lines, fmt.Sprintf("   %s  %-22s %s", evalTime(&s), m.evalNames[s.ProjectID()], who))
	}
	lines = append(lines, "", " "+sAccent.Bold(true).Render("You are evaluated"))
	if len(m.evals.AsCorrected) == 0 {
		lines = append(lines, sDim.Render("   nothing planned"))
	}
	for _, s := range m.evals.AsCorrected {
		who := s.Corrector.Login
		if who == "" {
			who = "(hidden until it starts)"
		}
		lines = append(lines, fmt.Sprintf("   %s  %-22s %s", evalTime(&s), m.evalNames[s.ProjectID()], who))
	}
	if m.pastEvals != nil {
		lines = append(lines, "", " "+sAccent.Bold(true).Render("Recent"))
		recent := append(append([]api.ScaleTeam{}, m.pastEvals.AsCorrected...), m.pastEvals.AsCorrector...)
		sort.SliceStable(recent, func(i, j int) bool {
			return timeOrZero(recent[i].BeginAt).After(timeOrZero(recent[j].BeginAt))
		})
		for i := range recent {
			e := &recent[i]
			role, who := "evaluated by", e.Corrector.Login
			if who == m.me.Login {
				role, who = "you evaluated", logins(e.Correcteds)
			}
			mark := ""
			if e.FinalMark != nil {
				mark = strconv.Itoa(*e.FinalMark)
			}
			lines = append(lines, fmt.Sprintf("   %s  %-22s %s %s  %s", evalTime(e), m.evalNames[e.ProjectID()], sDim.Render(role), who, mark))
			lines = append(lines, commentLines(e, max(20, inner-10))...)
		}
	}
	return lines
}

// commentLines renders an evaluation's comment and feedback, dimmed.
func commentLines(e *api.ScaleTeam, width int) []string {
	var out []string
	for _, c := range []struct {
		text  *string
		label string
	}{{e.Comment, "“"}, {e.Feedback, "↳ “"}} {
		if c.text == nil || strings.TrimSpace(*c.text) == "" {
			continue
		}
		ls := format.Wrap(*c.text, width, 3)
		ls[0] = c.label + ls[0]
		ls[len(ls)-1] += "”"
		for _, l := range ls {
			out = append(out, "      "+sDim.Render(l))
		}
	}
	return out
}

func logins(users api.Correcteds) string {
	if len(users) == 0 {
		return "(hidden)"
	}
	var ls []string
	for _, u := range users {
		ls = append(ls, u.Login)
	}
	return strings.Join(ls, ", ")
}

// logtimeLines draws the last two weeks as bars, with week totals.
func (m model) logtimeLines(inner int) []string {
	switch {
	case m.logtimeErr != nil:
		return []string{" " + sErr.Render(m.logtimeErr.Error())}
	case m.logtime == nil:
		return []string{" " + m.spin.View() + sDim.Render(" loading…")}
	}
	now := time.Now()
	monday := format.WeekStart(now)
	barW := max(10, inner-28)
	var lines []string
	for _, start := range []time.Time{monday.AddDate(0, 0, -7), monday} {
		var total time.Duration
		for d := 0; d < 7; d++ {
			day := start.AddDate(0, 0, d)
			if day.After(now) {
				break
			}
			v := m.logtime[api.DayKey(day)]
			total += v
			n := int(float64(barW) * min(1, v.Hours()/12))
			bar := sAccent.Render(strings.Repeat("█", n)) + sDim.Render(strings.Repeat("·", barW-n))
			lines = append(lines, fmt.Sprintf(" %s  %s %6s", day.Format("Mon 02"), bar, format.Hours(v)))
		}
		lines = append(lines, sDim.Render(fmt.Sprintf(" week of %s: %s", start.Format("02 Jan"), format.Hours(total))), "")
	}
	return lines
}

func evalTime(s *api.ScaleTeam) string {
	if s.BeginAt == nil {
		return "unscheduled     "
	}
	return s.BeginAt.Local().Format("Mon 02 Jan 15:04")
}

func (m model) userLines() []string {
	switch {
	case m.userErr != nil:
		return []string{" " + sErr.Render(m.userErr.Error())}
	case m.user == nil:
		return []string{" " + m.spin.View() + " loading…"}
	}
	u := m.user
	label := func(s string) string { return " " + sDim.Render(fmt.Sprintf("%-12s", s)) }
	lines := []string{label("Name") + u.DisplayName}
	if camp := u.PrimaryCampus(); camp != nil {
		lines = append(lines, label("Campus")+camp.Name)
	}
	if cu := u.MainCursus(); cu != nil {
		lines = append(lines, label("Level")+fmt.Sprintf("%.2f  %s", cu.Level, sDim.Render(cu.Cursus.Name)))
		if cu.BlackholedAt != nil {
			lines = append(lines, label("Blackhole")+format.DateWithDays(*cu.BlackholedAt))
		}
	}
	lines = append(lines, label("Wallet")+fmt.Sprintf("%d ₳", u.Wallet), label("Corrections")+strconv.Itoa(u.CorrectionPoint))
	switch {
	case u.Location != nil && *u.Location != "":
		lines = append(lines, label("Location")+sOK.Render("● "+*u.Location))
	case m.userLast != nil && m.userLast.EndAt != nil:
		lines = append(lines, label("Location")+sDim.Render("offline · last seen "+m.userLast.EndAt.Local().Format("Mon 02 Jan 15:04")+" on "+m.userLast.Host))
	default:
		lines = append(lines, label("Location")+sDim.Render("offline"))
	}
	return lines
}

// stripMarkdown drops the emphasis and heading marks event descriptions use.
func stripMarkdown(s string) string {
	return strings.NewReplacer("**", "", "*", "", "__", "", "# ", "", "#", "").Replace(s)
}

// wrap splits text into at most n lines of width w.
func wrap(text string, w, n int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) > w {
			lines = append(lines, line)
			if len(lines) == n {
				lines[n-1] += " …"
				return lines
			}
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
