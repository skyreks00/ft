package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colors adapt to light and dark terminals.
var (
	cAccent   = lipgloss.AdaptiveColor{Light: "#00807F", Dark: "#00BABC"}
	cDim      = lipgloss.AdaptiveColor{Light: "#6E7781", Dark: "#7D8590"}
	cBorder   = lipgloss.AdaptiveColor{Light: "#D0D7DE", Dark: "#30363D"}
	cGreen    = lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"}
	cRed      = lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}
	cYellow   = lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"}
	cBlue     = lipgloss.AdaptiveColor{Light: "#0969DA", Dark: "#58A6FF"}
	cSelected = lipgloss.AdaptiveColor{Light: "#DDF4FF", Dark: "#1C2D3A"}
	cBadgeFg  = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#0D1117"}
)

var (
	sDim      = lipgloss.NewStyle().Foreground(cDim)
	sAccent   = lipgloss.NewStyle().Foreground(cAccent)
	sBold     = lipgloss.NewStyle().Bold(true)
	sErr      = lipgloss.NewStyle().Foreground(cRed)
	sOK       = lipgloss.NewStyle().Foreground(cGreen)
	sWarn     = lipgloss.NewStyle().Foreground(cYellow)
	sInfo     = lipgloss.NewStyle().Foreground(cBlue)
	sKey      = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sSelected = lipgloss.NewStyle().Background(cSelected).Bold(true)
	sBadge    = lipgloss.NewStyle().Foreground(cBadgeFg).Bold(true).Padding(0, 1)
)

var modeColors = map[mode]lipgloss.AdaptiveColor{
	modeNormal:  cAccent,
	modeCommand: cYellow,
	modeSearch:  cBlue,
	modeConfirm: cRed,
}

// box draws a rounded panel of exactly w×h cells, with the title set in the
// top border (lazygit style). Lines are truncated or padded to fit.
func box(title string, lines []string, w, h int, focused bool) string {
	if w < 4 || h < 2 {
		return ""
	}
	bc, tc := cBorder, cDim
	if focused {
		bc, tc = cAccent, cAccent
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	inner := w - 2

	t := ""
	if title != "" {
		t = " " + ansi.Truncate(title, max(0, inner-3), "…") + " "
	}
	var b strings.Builder
	b.WriteString(bs.Render("╭─"))
	b.WriteString(lipgloss.NewStyle().Foreground(tc).Bold(focused).Render(t))
	b.WriteString(bs.Render(strings.Repeat("─", max(0, inner-1-ansi.StringWidth(t))) + "╮"))
	b.WriteByte('\n')
	for i := range h - 2 {
		line := ""
		if i < len(lines) {
			line = fit(lines[i], inner)
		} else {
			line = strings.Repeat(" ", inner)
		}
		b.WriteString(bs.Render("│") + line + bs.Render("│") + "\n")
	}
	b.WriteString(bs.Render("╰" + strings.Repeat("─", inner) + "╯"))
	return b.String()
}

// fit truncates or pads s (which may contain ANSI codes) to exactly w cells.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// spread puts left and right on one line of width w.
func spread(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}
