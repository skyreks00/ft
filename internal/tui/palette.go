package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// aliases maps short and alternative command names to the canonical ones
// (vim style: :e for :event, :sl a for :slot add…).
var aliases = map[string]string{
	"q": "quit", "q!": "quit", "qa": "quit", "wq": "quit", "exit": "quit",
	"r": "refresh", "o": "open", "s": "submit", "c": "correct", "h": "help",
	"l": "link", "e": "event", "events": "event", "ev": "eval", "evals": "eval",
	"u": "user", "sl": "slot", "slots": "slot", "lt": "logtime",
}

// subAliases are the short forms of subcommands, per command.
var subAliases = map[string]map[string]string{
	"event": {"j": "join", "l": "leave"},
	"slot":  {"a": "add", "r": "rm", "del": "rm", "delete": "rm"},
}

// canonical resolves an alias; unknown names are returned as is.
func canonical(name string) string {
	if c, ok := aliases[name]; ok {
		return c
	}
	return name
}

// canonicalSub resolves a subcommand alias of a (canonical) command.
func canonicalSub(cmd, sub string) string {
	if c, ok := subAliases[cmd][sub]; ok {
		return c
	}
	return sub
}

func (m model) runCommand(line string) (tea.Model, tea.Cmd) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return m, nil
	}
	name, args := canonical(f[0]), f[1:]
	if len(args) > 0 {
		args[0] = canonicalSub(name, args[0])
	}
	switch name {
	case "quit":
		return m, tea.Quit
	case "refresh":
		return m, m.refresh()
	case "open":
		return m, m.actionOpen()
	case "submit":
		return m, m.actionSubmit(args)
	case "correct":
		return m, m.actionCorrect()
	case "help":
		m.help = true
		return m, nil
	case "slot":
		return m.slotCommand(args)
	case "event":
		return m.eventCommand(args)
	case "eval":
		if len(args) > 0 && (args[0] == "clone" || args[0] == "cl") {
			// Cloning may ask ssh questions: run it in the terminal.
			return m, m.execFT("", 0, "eval clone", append([]string{"eval", "clone"}, args[1:]...)...)
		}
		return m.showView(viewEvals)
	case "logtime":
		login := ""
		if len(args) > 0 {
			login = args[0]
		} else if m.me != nil {
			login = m.me.Login
		}
		if login == "" {
			return m, nil
		}
		m.logtime, m.logtimeErr, m.logtimeLogin = nil, nil, login
		m.loading++
		next, _ := m.showView(viewLogtime)
		m = next.(model)
		return m, m.loadLogtime(login)
	case "user":
		if len(args) != 1 {
			return m, m.setFlash(true, "usage: :user <login>")
		}
		m.user, m.userErr = nil, nil
		m.loading++
		next, _ := m.showView(viewUser)
		m = next.(model)
		return m, m.loadUser(args[0])
	case "link":
		if len(args) == 0 {
			return m, m.setFlash(true, "usage: :link <project>")
		}
		// Cloning may need ssh prompts: run `ft link` in the terminal.
		return m, m.execFT("", 0, "link", append([]string{"link"}, args...)...)
	}
	return m, m.setFlash(true, "unknown command :%s (try :help)", f[0])
}
