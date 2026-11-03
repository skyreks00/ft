// Package format holds display helpers shared by the CLI and the TUI.
package format

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Status turns "waiting_for_correction" into "waiting for correction".
func Status(s string) string {
	return strings.ReplaceAll(s, "_", " ")
}

// DateWithDays formats t as "2027-01-04 (62 days)".
func DateWithDays(t time.Time) string {
	days := int(math.Floor(time.Until(t).Hours() / 24))
	switch {
	case days < 0:
		return fmt.Sprintf("%s (%d days ago)", t.Local().Format("2006-01-02"), -days)
	case days == 1:
		return fmt.Sprintf("%s (1 day)", t.Local().Format("2006-01-02"))
	default:
		return fmt.Sprintf("%s (%d days)", t.Local().Format("2006-01-02"), days)
	}
}

// TildePath shortens the home directory to ~.
func TildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home); ok && (rest == "" || rest[0] == filepath.Separator) {
		return "~" + rest
	}
	return p
}

// Hours formats a duration as "5h12" (or "0h00").
func Hours(d time.Duration) string {
	m := int(d.Round(time.Minute).Minutes())
	return fmt.Sprintf("%dh%02d", m/60, m%60)
}

// WeekStart is the Monday 00:00 (local) of t's week.
func WeekStart(t time.Time) time.Time {
	t = t.Local()
	offset := (int(t.Weekday()) + 6) % 7 // Monday = 0
	y, m, d := t.Date()
	return time.Date(y, m, d-offset, 0, 0, 0, 0, time.Local)
}

// Wrap splits text into lines of at most w runes (n lines at most, the
// last ending with "…" when cut).
func Wrap(text string, w, n int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		if line != "" && len([]rune(line))+1+len([]rune(word)) > w {
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
