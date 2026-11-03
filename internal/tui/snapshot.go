package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Snapshot renders a single frame with live data and no terminal, for
// screenshots and debugging. Requests run synchronously.
// view is "", "upcoming", "events", "eval", "user:<login>" or
// "palette:<input>".
func Snapshot(ctx context.Context, o Options, width, height int, view string) (string, error) {
	m := newModel(ctx, o)
	step := func(msg tea.Msg) {
		next, _ := m.Update(msg)
		m = next.(model)
	}
	step(tea.WindowSizeMsg{Width: width, Height: height})
	step(m.loadMe(false)())
	if m.me == nil {
		return "", errors.New(m.flash)
	}
	if m.curriculumLoading {
		step(fetchCurriculum(ctx, m.c, m.me, m.quests))
	}
	step(m.loadSlots(false)())
	if it := m.selected(); it != nil {
		if cmd := m.loadStatus(it.ref, false); cmd != nil {
			step(cmd())
		}
	}
	switch {
	case view == "events":
		next, cmd := m.showView(viewEvents)
		m = next.(model)
		step(cmd())
	case view == "eval":
		next, cmd := m.showView(viewEvals)
		m = next.(model)
		step(cmd())
	case strings.HasPrefix(view, "user:"):
		next, cmd := m.runCommand("user " + strings.TrimPrefix(view, "user:"))
		m = next.(model)
		step(cmd())
	case view == "upcoming":
		for i, r := range m.shown {
			if r.header() && r.group == upcomingGroup {
				m.cursor = i
			}
		}
		if cmd := m.toggleGroup(upcomingGroup); cmd != nil {
			step(cmd())
		}
		m.cursor++ // the first upcoming project
	case view == "logtime":
		next, cmd := m.runCommand("logtime")
		m = next.(model)
		step(cmd())
	case strings.HasPrefix(view, "palette:"):
		next, cmd := m.startInput(modeCommand, strings.TrimPrefix(view, "palette:"))
		m = next.(model)
		_ = cmd // the cursor blink; event ids would load here if asked for
		if m.wildIdx = -1; len(m.wild) > 1 {
			m.wildIdx = 0
		}
	case view != "":
		return "", fmt.Errorf("unknown view %q", view)
	}
	return m.View(), nil
}
