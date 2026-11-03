package tui

import (
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/skyreks00/ft/internal/format"
	"github.com/skyreks00/ft/internal/fuzzy"
)

// The command palette suggests completions in a bar above the command line
// (like nvim's wildmenu): command names, then their arguments. Tab and
// shift+Tab cycle through them.

type candidate struct {
	value string
	desc  string
}

// commandHelp lists the commands by full name; short forms (palette.go)
// work when typed but are not suggested.
var commandHelp = []candidate{
	{"link", "clone & link a project"},
	{"open", "intra page"},
	{"submit", "push the linked folder"},
	{"correct", "book an evaluation"},
	{"slot", "add <start> <end> | rm [id]"},
	{"event", "events; join | leave [id]"},
	{"eval", "upcoming and recent evaluations"},
	{"logtime", "[login]  time spent on campus"},
	{"user", "<login>"},
	{"refresh", "reload everything"},
	{"help", "keys and commands"},
	{"quit", ""},
}

// commandOptions are the options a command passes on to the ft command it
// runs, suggested after its arguments or when a word starts with "-".
var commandOptions = map[string][]candidate{
	"link":   {{"--here", "link the current folder instead of cloning"}},
	"submit": {{"--dry-run", "show what would be pushed"}, {"--no-norm", "skip norminette"}, {"--branch", "<name>  remote branch"}},
}

// optionsFor lists a command's options not given yet, or nil when the last
// one given expects a value (--branch).
func optionsFor(cmd string, fields []string) []candidate {
	if n := len(fields); n > 0 && fields[n-1] == "--branch" {
		return nil
	}
	var out []candidate
	for _, o := range commandOptions[cmd] {
		if !slices.Contains(fields, o.value) {
			out = append(out, o)
		}
	}
	return out
}

// suggest returns the completions of the last word of input; base is the
// input before that word. needEvents is set when event ids are wanted but
// not loaded yet.
func (m model) suggest(input string) (base string, out []candidate, needEvents bool) {
	i := strings.LastIndex(input, " ")
	base, word := input[:i+1], strings.ToLower(input[i+1:])
	fields := strings.Fields(base)
	if len(fields) > 0 {
		fields[0] = canonical(fields[0])
	}
	if len(fields) > 1 {
		fields[1] = canonicalSub(fields[0], fields[1])
	}

	var all []candidate
	switch {
	case len(fields) == 0:
		all = commandHelp
	case strings.HasPrefix(word, "-"):
		all = optionsFor(fields[0], fields)
	case fields[0] == "link" && len(fields) >= 2, fields[0] == "submit":
		all = optionsFor(fields[0], fields)
	case fields[0] == "link" && len(fields) == 1:
		for _, it := range m.items {
			short := fuzzy.ShortSlug(it.ref.Slug)
			all = append(all, candidate{short, format.Status(it.status)})
		}
	case isEvent(fields[0]) && len(fields) == 1:
		all = []candidate{{"join", "[id]"}, {"leave", "[id]"}}
	case isEvent(fields[0]) && len(fields) == 2 && (fields[1] == "join" || fields[1] == "leave"):
		if m.events == nil {
			return base, nil, true
		}
		leave := fields[1] == "leave"
		for _, e := range m.events {
			if m.joined[e.ID] == leave {
				all = append(all, candidate{strconv.Itoa(e.ID), e.Name + ", " + e.BeginAt.Local().Format("Mon 02 15:04")})
			}
		}
	case fields[0] == "eval" && len(fields) == 1:
		all = []candidate{{"clone", "[id]  the repository of the team you evaluate"}}
	case isSlot(fields[0]) && len(fields) == 1:
		all = []candidate{{"add", "<start> <end>, e.g. tomorrow 14:00 +2h"}, {"rm", "[id]"}}
	case isSlot(fields[0]) && len(fields) == 2 && fields[1] == "rm":
		for _, r := range m.slotRanges() {
			if !r.Booked {
				all = append(all, candidate{strconv.Itoa(r.ID()), slotLabel(r.BeginAt, r.EndAt)})
			}
		}
	}
	for _, c := range all {
		if strings.HasPrefix(strings.ToLower(c.value), word) {
			out = append(out, c)
		}
	}
	return base, out, false
}

func isEvent(s string) bool { return s == "event" || s == "events" }
func isSlot(s string) bool  { return s == "slot" || s == "slots" }

// refreshWild recomputes the suggestions for the current input, loading
// events the first time their ids are wanted.
func (m *model) refreshWild() tea.Cmd {
	base, cands, needEvents := m.suggest(m.input.Value())
	m.wildBase, m.wild, m.wildIdx = base, cands, -1
	if needEvents && !m.eventsRequested {
		m.eventsRequested = true
		m.loading++
		return m.loadEvents(false)
	}
	return nil
}

// cycleWild moves through the suggestions and puts the chosen one in the
// input. A lone suggestion is accepted with a trailing space, so the next
// Tab completes its arguments.
func (m *model) cycleWild(step int) tea.Cmd {
	n := len(m.wild)
	if n == 0 {
		return nil
	}
	if n == 1 {
		m.input.SetValue(m.wildBase + m.wild[0].value + " ")
		m.input.CursorEnd()
		return m.refreshWild()
	}
	m.wildIdx = ((m.wildIdx+step)%n + n) % n
	m.input.SetValue(m.wildBase + m.wild[m.wildIdx].value)
	m.input.CursorEnd()
	return nil
}

func (m model) wildVisible() bool {
	return m.mode == modeCommand && len(m.wild) > 0
}

// wildLine renders the suggestions on one line, scrolled so the selected
// one is visible.
func (m model) wildLine() string {
	parts := make([]string, len(m.wild))
	for i, c := range m.wild {
		text := " " + c.value
		if c.desc != "" {
			text += " " + sDim.Render(c.desc)
		}
		text += " "
		if i == m.wildIdx {
			text = sSelected.Render(ansi.Strip(text))
		}
		parts[i] = text
	}
	start := 0
	for start < m.wildIdx && width(parts[start:m.wildIdx+1]) > m.width {
		start++
	}
	line := strings.Join(parts[start:], sDim.Render("│"))
	if start > 0 {
		line = sDim.Render("‹") + line
	}
	return fit(line, m.width)
}

func width(parts []string) int {
	w := 0
	for _, p := range parts {
		w += ansi.StringWidth(p) + 1
	}
	return w
}
