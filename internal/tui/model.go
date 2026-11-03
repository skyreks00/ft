// Package tui is ft's full-screen interface (Bubble Tea, Elm architecture).
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/browser"
	"github.com/swilmus/ft/internal/config"
	"github.com/swilmus/ft/internal/slots42"
)

// Options configures the TUI.
type Options struct {
	Client  *api.Client
	Exe     string       // the ft binary, run for actions that need the terminal
	Link    *config.Link // project linked to the current folder, or nil
	LinkDir string
	// SlotsURL overrides the campus slots platform (FT_SLOTS_URL).
	SlotsURL string
	// Platform reads slots from the campus platform (`ft slots auth`), or nil.
	Platform *slots42.Client
	// RemindBefore announces evaluations and events this long before they
	// start (0 disables reminders); NotifyDesktop also sends them to the
	// desktop.
	RemindBefore  time.Duration
	NotifyDesktop bool
}

// Run starts the TUI and blocks until the user quits.
func Run(ctx context.Context, o Options) error {
	_, err := tea.NewProgram(newModel(ctx, o), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx)).Run()
	return err
}

type panel int

const (
	panelProjects panel = iota
	panelSlots
	panelDetails
)

type mode int

const (
	modeNormal mode = iota
	modeCommand
	modeSearch
	modeConfirm
)

func (m mode) String() string {
	return [...]string{"NORMAL", "COMMAND", "SEARCH", "CONFIRM"}[m]
}

// Delays: wait for the cursor to rest before fetching details, to spare the
// shared rate limit while scrolling.
const (
	selectDelay = 200 * time.Millisecond
	flashTTL    = 5 * time.Second
	autoRefresh = 5 * time.Minute
)

// item is a row of the projects panel.
type item struct {
	ref       api.ProjectRef
	status    string
	validated *bool
	mark      *int
	markedAt  *time.Time
	group     string // see projects.go
	circle    int    // holy graph circle, -1 when unknown
	tracks    []string
	choice    []string // choose one of these projects
	planet    []string // the modules of its planet

	// curriculum projects not started yet
	upcoming  bool
	xp        int
	estimate  string
	solo      *bool
	maxPeople *int
}

type (
	meMsg struct {
		me     *api.User
		quests []string
		err    error
	}
	slotsMsg struct {
		slots    []api.Slot
		err      error
		platform map[int]slots42.Slot
	}
	statusMsg struct {
		id  int
		s   *api.ProjectStatus
		err error
	}
	selectTickMsg struct{ seq int }
	refreshMsg    struct{}
	flashClearMsg struct{ seq int }
	execDoneMsg   struct {
		label     string
		projectID int
		err       error
	}
)

type model struct {
	ctx      context.Context
	c        *api.Client
	exe      string
	link     *config.Link
	linkDir  string
	slotsURL string
	platform *slots42.Client

	me                *api.User
	items             []item
	groups            []group
	shown             []row // panel rows after grouping or the search filter
	cursor            int   // index into shown
	collapsed         map[string]bool
	curriculum        []api.Project
	curriculumLoading bool
	quests            []string         // the user's quests; pick the graph variant
	graph             *api.CircleTable // nil without a holy graph
	slots             []api.Slot
	pslots            map[int]slots42.Slot // platform slots by id, when connected
	slotsErr          error
	slotCur           int
	statuses          map[int]*api.ProjectStatus
	errs              map[int]error
	inflight          map[int]bool
	loading           int // pending me/slots requests
	selSeq            int
	scroll            int // details panel offset

	view      rightView
	events    []api.Event
	joined    map[int]bool
	eventsErr error
	evCur     int
	evals     *api.Evaluations
	evalNames map[int]string
	evalsErr  error
	user      *api.User
	userLast  *api.Location
	userErr   error

	confirmPrompt string
	confirmCmd    tea.Cmd

	// reminders (reminders.go)
	remindBefore  time.Duration
	notifyDesktop bool
	reminded      map[string]bool
	myEvents      []api.Event

	// past evaluations and logtime views
	pastEvals    *api.Evaluations
	logtime      map[string]time.Duration
	logtimeLogin string
	logtimeErr   error

	// command palette suggestions (wildmenu.go)
	wild            []candidate
	wildIdx         int
	wildBase        string
	eventsRequested bool

	focus  panel
	mode   mode
	input  textinput.Model
	filter string
	spin   spinner.Model
	help   bool

	flash    string
	flashErr bool
	flashSeq int

	width, height int
}

func newModel(ctx context.Context, o Options) model {
	in := textinput.New()
	in.Prompt = ""
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sAccent))
	return model{
		ctx:           ctx,
		c:             o.Client,
		exe:           o.Exe,
		link:          o.Link,
		linkDir:       o.LinkDir,
		slotsURL:      o.SlotsURL,
		remindBefore:  o.RemindBefore,
		notifyDesktop: o.NotifyDesktop,
		reminded:      map[string]bool{},
		platform:      o.Platform,
		statuses:      map[int]*api.ProjectStatus{},
		errs:          map[int]error{},
		inflight:      map[int]bool{},
		// Upcoming loads on demand; other cursus (piscines) start folded.
		collapsed: map[string]bool{upcomingGroup: true, modulesGroup: true},
		input:     in,
		spin:      sp,
		loading:   2,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.loadMe(false), m.loadSlots(false), m.spin.Tick, scheduleRefresh())
}

func scheduleRefresh() tea.Cmd {
	return tea.Tick(autoRefresh, func(time.Time) tea.Msg { return refreshMsg{} })
}

// --- data loading ---------------------------------------------------------

func (m model) reqCtx(fresh bool) context.Context {
	if fresh {
		return api.WithNoCache(m.ctx)
	}
	return m.ctx
}

func (m model) loadMe(fresh bool) tea.Cmd {
	ctx := m.reqCtx(fresh)
	return func() tea.Msg {
		me, err := m.c.Me(ctx)
		if err != nil {
			return meMsg{err: err}
		}
		// Quests pick the holy graph variant; without them, the campus graph.
		quests, _ := m.c.QuestSlugs(ctx, me.Login)
		return meMsg{me: me, quests: quests}
	}
}

// loadSlots reads the campus platform when connected, else the intra.
func (m model) loadSlots(fresh bool) tea.Cmd {
	ctx := m.reqCtx(fresh)
	if p := m.platform; p != nil {
		return func() tea.Msg {
			now := time.Now()
			ps, err := p.MySlots(ctx, now, now.Add(14*24*time.Hour))
			if err != nil {
				return slotsMsg{err: err}
			}
			byID := map[int]slots42.Slot{}
			slots := make([]api.Slot, len(ps))
			for i, s := range ps {
				byID[s.ID] = s
				slots[i] = api.Slot{ID: s.ID, BeginAt: s.Start, EndAt: s.End}
				if s.Reserved {
					slots[i].ScaleTeam = &struct {
						ID int `json:"id"`
					}{}
				}
			}
			return slotsMsg{slots: slots, platform: byID}
		}
	}
	return func() tea.Msg {
		s, err := m.c.MySlots(ctx)
		return slotsMsg{slots: s, err: err}
	}
}

func (m *model) loadStatus(ref api.ProjectRef, fresh bool) tea.Cmd {
	if m.me == nil || m.inflight[ref.ID] {
		return nil
	}
	m.inflight[ref.ID] = true
	ctx, me := m.reqCtx(fresh), m.me
	return func() tea.Msg {
		s, err := m.c.ProjectStatus(ctx, me, ref)
		return statusMsg{ref.ID, s, err}
	}
}

// selectionChanged schedules a details fetch once the cursor rests.
func (m *model) selectionChanged() tea.Cmd {
	m.selSeq++
	m.scroll = 0
	it := m.selected()
	if it == nil || it.upcoming || m.statuses[it.ref.ID] != nil {
		return nil
	}
	seq := m.selSeq
	return tea.Tick(selectDelay, func(time.Time) tea.Msg { return selectTickMsg{seq} })
}

func (m *model) setFlash(err bool, format string, args ...any) tea.Cmd {
	m.flash = fmt.Sprintf(format, args...)
	m.flashErr = err
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(flashTTL, func(time.Time) tea.Msg { return flashClearMsg{seq} })
}

// --- update ---------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case meMsg:
		m.loading = max(0, m.loading-1)
		if msg.err != nil {
			return m, m.setFlash(true, "%v", msg.err)
		}
		first := m.me == nil
		m.me, m.quests = msg.me, msg.quests
		var load, remind tea.Cmd
		if first {
			remind = m.startReminders()
			mainKey := cursusKey(mainCursusID(m.me))
			t := graphFor(m.me, m.quests)
			cur := 0
			if t != nil {
				cur = t.Current(validatedFn(m.me))
				load = m.loadCurriculum() // the circles' upcoming projects
			}
			for _, g := range buildGroups(m.me, t) {
				switch c, isCircle := isCircleKey(g.key); {
				case isCircle:
					// Unfold the circle in progress and the next one.
					m.collapsed[g.key] = c != cur && c != cur+1
				case g.key != mainKey && g.key != upcomingGroup && g.key != modulesGroup:
					m.collapsed[g.key] = true
				}
			}
		}
		m.rebuild()
		switch {
		case first && m.link != nil:
			m.selectProject(m.link.ProjectID)
		case first:
			m.cursor = m.firstProjectRow()
		}
		return m, tea.Batch(m.selectionChanged(), load, remind)

	case myEventsMsg:
		m.loading = max(0, m.loading-1)
		if msg.err == nil {
			m.myEvents = msg.events
		}
		return m, nil

	case remindTickMsg:
		return m, tea.Batch(scheduleRemindTick(), m.checkReminders(time.Now()))

	case remindReloadMsg:
		m.loading += 2
		return m, tea.Batch(scheduleRemindReload(), m.loadEvals(true), m.loadMyEvents(true))

	case logtimeMsg:
		m.loading = max(0, m.loading-1)
		m.logtime, m.logtimeLogin, m.logtimeErr = msg.days, msg.login, msg.err
		return m, nil

	case curriculumMsg:
		m.loading = max(0, m.loading-1)
		m.curriculumLoading = false
		if msg.err != nil {
			return m, m.setFlash(true, "upcoming projects: %v", msg.err)
		}
		m.curriculum = msg.projects
		m.rebuild()
		return m, nil

	case slotsMsg:
		m.loading = max(0, m.loading-1)
		m.slots, m.slotsErr, m.pslots = msg.slots, msg.err, msg.platform
		m.slotCur = clamp(m.slotCur, 0, len(m.slotRanges())-1)
		return m, nil

	case eventsMsg:
		m.loading = max(0, m.loading-1)
		m.eventsErr = msg.err
		if msg.err == nil {
			m.events, m.joined = msg.events, msg.joined
			m.evCur = clamp(m.evCur, 0, len(m.events)-1)
		}
		if m.mode == modeCommand {
			return m, m.refreshWild()
		}
		return m, nil

	case evalsMsg:
		m.loading = max(0, m.loading-1)
		m.evalsErr = msg.err
		if msg.err == nil {
			m.evals, m.evalNames, m.pastEvals = msg.evals, msg.names, msg.past
		}
		return m, nil

	case userMsg:
		m.loading = max(0, m.loading-1)
		m.user, m.userLast, m.userErr = msg.user, msg.last, msg.err
		return m, nil

	case doneMsg:
		var flash, reload tea.Cmd
		if msg.err != nil {
			flash = m.setFlash(true, "%v", msg.err)
		} else {
			flash = m.setFlash(false, "%s", msg.text)
		}
		switch msg.reload {
		case "slots":
			m.loading++
			reload = m.loadSlots(true)
		case "events":
			m.loading++
			reload = m.loadEvents(true)
		}
		return m, tea.Batch(flash, reload)

	case statusMsg:
		delete(m.inflight, msg.id)
		if msg.err != nil {
			m.errs[msg.id] = msg.err
		} else {
			delete(m.errs, msg.id)
			m.statuses[msg.id] = msg.s
		}
		return m, nil

	case selectTickMsg:
		if it := m.selected(); msg.seq == m.selSeq && it != nil {
			return m, m.loadStatus(it.ref, false)
		}
		return m, nil

	case flashClearMsg:
		if msg.seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil

	case execDoneMsg:
		var flash tea.Cmd
		if msg.err != nil {
			flash = m.setFlash(true, "ft %s did not complete", msg.label)
		} else {
			flash = m.setFlash(false, "ft %s done", msg.label)
		}
		delete(m.statuses, msg.projectID)
		m.loading++
		cmds := []tea.Cmd{flash, m.loadMe(true)}
		if it := m.selected(); it != nil && it.ref.ID == msg.projectID {
			cmds = append(cmds, m.loadStatus(it.ref, true))
		}
		return m, tea.Batch(cmds...)

	case refreshMsg:
		// Quiet background refresh: slots and the selected project only.
		cmds := []tea.Cmd{scheduleRefresh()}
		if m.mode == modeNormal {
			m.loading++
			cmds = append(cmds, m.loadSlots(true))
			if it := m.selected(); it != nil {
				cmds = append(cmds, m.loadStatus(it.ref, true))
			}
		}
		return m, tea.Batch(cmds...)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch m.mode {
	case modeCommand, modeSearch:
		return m.handleInputKey(k)
	case modeConfirm:
		m.mode = modeNormal
		cmd := m.confirmCmd
		m.confirmCmd = nil
		if k.String() == "y" || k.String() == "Y" {
			return m, cmd
		}
		return m, m.setFlash(false, "Cancelled")
	}
	if m.help {
		switch k.String() {
		case "q":
			return m, tea.Quit
		case "?", "esc", "enter":
			m.help = false
		}
		return m, nil
	}

	switch k.String() {
	case "q":
		return m, tea.Quit
	case "?":
		m.help = true
	case "esc":
		if m.view != viewProject {
			m.view = viewProject
			m.scroll = 0
			return m, nil
		}
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
			return m, m.selectionChanged()
		}
	case ":":
		return m.startInput(modeCommand, "")
	case "/":
		return m.startInput(modeSearch, m.filter)
	case "tab":
		m.focus = (m.focus + 1) % 3
	case "shift+tab":
		m.focus = (m.focus + 2) % 3
	case "h", "left":
		if m.focus == panelDetails {
			m.focus = panelProjects
		}
	case "enter", " ":
		if key, ok := m.selectedGroup(); ok && m.focus == panelProjects {
			return m, m.toggleGroup(key)
		}
		if k.String() == " " {
			return m, nil
		}
		if m.focus == panelProjects {
			m.view = viewProject
		}
		m.focus = panelDetails
	case "l", "right":
		m.focus = panelDetails
	case "j", "down":
		return m.move(1)
	case "k", "up":
		return m.move(-1)
	case "g", "home":
		return m.move(-1 << 20)
	case "G", "end":
		return m.move(1 << 20)
	case "ctrl+d":
		return m.move(10)
	case "ctrl+u":
		return m.move(-10)
	}
	// Everything else is a `:` command.
	return m, nil
}

// handleMouse scrolls the panel under the pointer with the wheel and
// selects a row with a left click.
func (m model) handleMouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal || m.help || m.width == 0 {
		return m, nil
	}
	leftW, projH, bodyH := m.layout()
	if e.Y >= bodyH {
		return m, nil // status bar
	}
	p := panelDetails
	switch {
	case e.X < leftW && e.Y < projH:
		p = panelProjects
	case e.X < leftW:
		p = panelSlots
	}
	switch e.Button {
	case tea.MouseButtonWheelUp:
		m.focus = p
		return m.move(-1)
	case tea.MouseButtonWheelDown:
		m.focus = p
		return m.move(1)
	case tea.MouseButtonLeft:
		if e.Action != tea.MouseActionPress {
			return m, nil
		}
	default:
		return m, nil
	}
	m.focus = p
	switch p {
	case panelProjects:
		row := listStart(m.cursor, projH-2) + e.Y - 1
		if e.Y > 0 && row < len(m.shown) {
			if m.shown[row].header() {
				m.cursor = row
				return m, m.toggleGroup(m.shown[row].group)
			}
			if row != m.cursor {
				m.cursor = row
				m.view = viewProject
				return m, m.selectionChanged()
			}
		}
	case panelSlots:
		row := listStart(m.slotCur, bodyH-projH-2) + e.Y - projH - 1
		if e.Y > projH && row < len(m.slotRanges()) {
			m.slotCur = row
		}
	}
	return m, nil
}

func (m model) move(delta int) (tea.Model, tea.Cmd) {
	switch m.focus {
	case panelProjects:
		prev := m.cursor
		m.cursor = clamp(m.cursor+delta, 0, len(m.shown)-1)
		if m.cursor != prev {
			m.view = viewProject
			return m, m.selectionChanged()
		}
	case panelSlots:
		m.slotCur = clamp(m.slotCur+delta, 0, len(m.slotRanges())-1)
	case panelDetails:
		if m.view == viewEvents {
			m.evCur = clamp(m.evCur+delta, 0, len(m.events)-1)
		} else {
			m.scroll = max(0, m.scroll+delta)
		}
	}
	return m, nil
}

func (m model) startInput(md mode, value string) (tea.Model, tea.Cmd) {
	m.mode = md
	m.input.SetValue(value)
	m.input.CursorEnd()
	cmds := []tea.Cmd{m.input.Focus()}
	if md == modeCommand {
		cmds = append(cmds, m.refreshWild())
	}
	return m, tea.Batch(cmds...)
}

func (m model) handleInputKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		if m.mode == modeSearch {
			m.filter = ""
			m.applyFilter()
		}
		m.mode = modeNormal
		m.wild = nil
		m.input.Blur()
		return m, m.selectionChanged()
	case tea.KeyEnter:
		line := strings.TrimSpace(m.input.Value())
		md := m.mode
		m.mode = modeNormal
		m.wild = nil
		m.input.Blur()
		if md == modeSearch {
			m.focus = panelProjects
			return m, m.selectionChanged()
		}
		return m.runCommand(line)
	case tea.KeyTab, tea.KeyShiftTab:
		if m.mode == modeCommand {
			step := 1
			if k.Type == tea.KeyShiftTab {
				step = -1
			}
			return m, m.cycleWild(step)
		}
	}
	var cmd tea.Cmd
	before := m.input.Value()
	m.input, cmd = m.input.Update(k)
	if m.mode == modeCommand && m.input.Value() != before {
		cmd = tea.Batch(cmd, m.refreshWild())
	}
	if m.mode == modeSearch && m.input.Value() != m.filter {
		m.filter = m.input.Value()
		m.applyFilter()
		m.focus = panelProjects
	}
	return m, cmd
}

// --- actions --------------------------------------------------------------

func (m *model) refresh() tea.Cmd {
	m.statuses = map[int]*api.ProjectStatus{}
	m.errs = map[int]error{}
	m.loading += 2
	cmds := []tea.Cmd{m.loadMe(true), m.loadSlots(true), m.setFlash(false, "Refreshing…")}
	if m.events != nil {
		m.loading++
		cmds = append(cmds, m.loadEvents(true))
	}
	if m.evals != nil {
		m.loading++
		cmds = append(cmds, m.loadEvals(true))
	}
	if it := m.selected(); it != nil {
		cmds = append(cmds, m.loadStatus(it.ref, true))
	}
	return tea.Batch(cmds...)
}

// actionOpen opens the selected project's intra page.
func (m *model) actionOpen() tea.Cmd {
	it := m.selected()
	if it == nil {
		return nil
	}
	// Registering happens on the intra, which knows whether the project is
	// open to you yet.
	return m.openURL(api.ProjectPageURL(it.ref.Slug))
}

// actionSubmit runs `ft submit [options]` in the linked folder; it shows
// the push plan and asks for confirmation in the terminal.
func (m *model) actionSubmit(options []string) tea.Cmd {
	it := m.selected()
	if it == nil {
		return nil
	}
	if m.link == nil || m.link.ProjectID != it.ref.ID {
		return m.setFlash(true, "submit pushes the linked folder: run ft from the %s folder, or :link it here", it.ref.Slug)
	}
	return m.execFT(m.linkDir, it.ref.ID, "submit", append([]string{"submit"}, options...)...)
}

func (m *model) actionCorrect() tea.Cmd {
	it := m.selected()
	if it == nil {
		return nil
	}
	if it.status == api.NotRegistered {
		return m.setFlash(true, "Not registered to %s: register on the intra (:open)", it.ref.Slug)
	}
	if platform := api.SlotsPlatform(m.slotsURL, m.me); platform != "" {
		return m.openURL(platform)
	}
	st := m.statuses[it.ref.ID]
	if st == nil {
		return tea.Batch(m.setFlash(false, "Loading %s…", it.ref.Slug), m.loadStatus(it.ref, false))
	}
	if st.Team == nil {
		return m.setFlash(true, "No team yet for %s", it.ref.Slug)
	}
	return m.openURL(api.SlotsPageURL(it.ref.Slug, st.Team.ID))
}

func (m *model) openURL(u string) tea.Cmd {
	if err := browser.Open(u); err != nil {
		return m.setFlash(true, "could not open a browser: %v", err)
	}
	return m.setFlash(false, "Opened %s", u)
}

// execFT suspends the TUI and runs `ft args...` in the terminal, so its
// prompts (confirmation, ssh) work, then waits for Enter.
func (m *model) execFT(dir string, projectID int, label string, args ...string) tea.Cmd {
	if m.exe == "" {
		return m.setFlash(true, "cannot locate the ft binary")
	}
	script := `"$0" "$@"; s=$?; printf '\n\033[2m[Enter: back to ft]\033[0m '; read _; exit $s`
	c := exec.Command("sh", append([]string{"-c", script, m.exe}, args...)...)
	c.Dir = dir
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execDoneMsg{label: label, projectID: projectID, err: err}
	})
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return min(max(v, lo), hi)
}
