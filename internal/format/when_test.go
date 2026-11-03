package format

import (
	"testing"
	"time"
)

func TestParseRange(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	now := time.Date(2026, 11, 2, 19, 50, 0, 0, loc) // a Monday evening
	const layout = "2006-01-02 15:04"
	tests := []struct {
		start, end         string
		wantBegin, wantEnd string
	}{
		{"21:00", "23:00", "2026-11-02 21:00", "2026-11-02 23:00"},
		{"14:00", "16:00", "2026-11-03 14:00", "2026-11-03 16:00"}, // past today: tomorrow
		{"14h", "16h30", "2026-11-03 14:00", "2026-11-03 16:30"},
		{"tomorrow 9:00", "+2h", "2026-11-03 09:00", "2026-11-03 11:00"},
		{"demain 9:00", "+90m", "2026-11-03 09:00", "2026-11-03 10:30"},
		{"wed 10:00", "12:00", "2026-11-04 10:00", "2026-11-04 12:00"},
		{"mon 10:00", "11:00", "2026-11-09 10:00", "2026-11-09 11:00"}, // next Monday
		{"2026-11-20 22:00", "01:00", "2026-11-20 22:00", "2026-11-21 01:00"},
		{"20/11 10:00", "2026-11-20 12:00", "2026-11-20 10:00", "2026-11-20 12:00"},
		{"2026-11-20T10:00", "+1h", "2026-11-20 10:00", "2026-11-20 11:00"},
	}
	for _, tt := range tests {
		t.Run(tt.start+"→"+tt.end, func(t *testing.T) {
			b, e, err := ParseRange(tt.start, tt.end, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := b.Format(layout); got != tt.wantBegin {
				t.Errorf("begin = %s, want %s", got, tt.wantBegin)
			}
			if got := e.Format(layout); got != tt.wantEnd {
				t.Errorf("end = %s, want %s", got, tt.wantEnd)
			}
		})
	}
}

func TestParseRangeErrors(t *testing.T) {
	now := time.Date(2026, 11, 2, 19, 50, 0, 0, time.UTC)
	for _, c := range [][2]string{
		{"21:10", "22:00"},       // not on a quarter hour
		{"21:00", "+0m"},         // empty
		{"21:00", "+abc"},        // bad duration
		{"noon", "14:00"},        // bad clock
		{"someday 10:00", "+1h"}, // bad day
		{"2026-11-20 12:00", "2026-11-20 11:00"},
	} {
		if _, _, err := ParseRange(c[0], c[1], now); err == nil {
			t.Errorf("ParseRange(%q, %q): expected error", c[0], c[1])
		}
	}
}

func TestHoursAndWeekStart(t *testing.T) {
	if got := Hours(5*time.Hour + 12*time.Minute + 40*time.Second); got != "5h13" {
		t.Errorf("Hours = %s", got)
	}
	tue := time.Date(2026, 11, 3, 15, 0, 0, 0, time.Local)
	if got := WeekStart(tue); !got.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.Local)) {
		t.Errorf("WeekStart(Tue) = %v", got)
	}
	sun := time.Date(2026, 11, 8, 23, 0, 0, 0, time.Local)
	if got := WeekStart(sun); got.Day() != 2 {
		t.Errorf("WeekStart(Sun) = %v", got)
	}
	if got := Wrap("one two three four five", 10, 2); len(got) != 2 || got[0] != "one two" || got[1] != "three four …" {
		t.Errorf("Wrap = %q", got)
	}
}
