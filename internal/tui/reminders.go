package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/swilmus/ft/internal/api"
	"github.com/swilmus/ft/internal/notify"
)

// Reminders: while the TUI runs, evaluations (given or received) and the
// events the user joined are announced remindBefore their start, in the
// status bar and as a desktop notification. Their data is refreshed every
// reminderReload; the check itself makes no request.

const (
	reminderTick   = 30 * time.Second
	reminderReload = 10 * time.Minute
)

type (
	remindTickMsg   struct{}
	remindReloadMsg struct{}
	myEventsMsg     struct {
		events []api.Event
		err    error
	}
)

func scheduleRemindTick() tea.Cmd {
	return tea.Tick(reminderTick, func(time.Time) tea.Msg { return remindTickMsg{} })
}

func scheduleRemindReload() tea.Cmd {
	return tea.Tick(reminderReload, func(time.Time) tea.Msg { return remindReloadMsg{} })
}

// startReminders loads what reminders need and starts their timers.
func (m *model) startReminders() tea.Cmd {
	if m.remindBefore <= 0 {
		return nil
	}
	m.loading += 2
	return tea.Batch(m.loadEvals(false), m.loadMyEvents(false), scheduleRemindTick(), scheduleRemindReload())
}

func (m model) loadMyEvents(fresh bool) tea.Cmd {
	ctx, c, me := m.reqCtx(fresh), m.c, m.me
	return func() tea.Msg {
		evs, err := c.MyEvents(ctx, me.ID)
		return myEventsMsg{evs, err}
	}
}

// reminder is something starting soon.
type reminder struct {
	key   string
	at    time.Time
	title string
	body  string
}

// dueReminders lists what starts within remindBefore and was not announced.
func (m model) dueReminders(now time.Time) []reminder {
	var out []reminder
	add := func(key string, at *time.Time, title, body string) {
		if at == nil || m.reminded[key] || at.Before(now) || at.Sub(now) > m.remindBefore {
			return
		}
		out = append(out, reminder{key, *at, title, body})
	}
	if m.evals != nil {
		for _, e := range m.evals.AsCorrector {
			who := "(hidden)"
			if len(e.Correcteds) > 0 {
				var ls []string
				for _, u := range e.Correcteds {
					ls = append(ls, u.Login)
				}
				who = strings.Join(ls, ", ")
			}
			add(fmt.Sprintf("eval:%d", e.ID), e.BeginAt, "You evaluate "+m.evalNames[e.ProjectID()], "with "+who)
		}
		for _, e := range m.evals.AsCorrected {
			who := e.Corrector.Login
			if who == "" {
				who = "(hidden)"
			}
			add(fmt.Sprintf("eval:%d", e.ID), e.BeginAt, "You are evaluated on "+m.evalNames[e.ProjectID()], "by "+who)
		}
	}
	for _, e := range m.myEvents {
		at := e.BeginAt
		add(fmt.Sprintf("event:%d", e.ID), &at, e.Name, e.Location)
	}
	return out
}

// checkReminders announces what is due.
func (m *model) checkReminders(now time.Time) tea.Cmd {
	due := m.dueReminders(now)
	if len(due) == 0 {
		return nil
	}
	for _, r := range due {
		m.reminded[r.key] = true
		mins := int(r.at.Sub(now).Round(time.Minute).Minutes())
		title := fmt.Sprintf("In %d min: %s", mins, r.title)
		if m.notifyDesktop {
			_ = notify.Send(title, r.body)
		}
		m.flash, m.flashErr = title+" "+r.body, false
	}
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(2*time.Minute, func(time.Time) tea.Msg { return flashClearMsg{seq} })
}
