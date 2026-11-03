package api

import (
	"testing"
	"time"
)

func TestLogtime(t *testing.T) {
	loc := time.Local
	at := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }
	ptr := func(t time.Time) *time.Time { return &t }
	locs := []Location{
		{BeginAt: at(2, 9, 0), EndAt: ptr(at(2, 12, 30))}, // 3h30 on the 2nd
		{BeginAt: at(2, 14, 0), EndAt: ptr(at(2, 15, 0))}, // +1h
		{BeginAt: at(2, 22, 0), EndAt: ptr(at(3, 1, 15))}, // 2h on the 2nd, 1h15 on the 3rd
		{BeginAt: at(3, 10, 0)},                           // still open: until now (11:00)
		{BeginAt: at(1, 23, 0), EndAt: ptr(at(2, 0, 30))}, // 30 min on the 2nd, the rest before `from`
	}
	from, to, now := at(2, 0, 0), at(4, 0, 0), at(3, 11, 0)
	got := Logtime(locs, from, to, now)
	want := map[string]time.Duration{
		"2026-11-02": 7 * time.Hour,
		"2026-11-03": 2*time.Hour + 15*time.Minute,
	}
	for day, d := range want {
		if got[day] != d {
			t.Errorf("%s = %v, want %v", day, got[day], d)
		}
	}
	if len(got) != len(want) {
		t.Errorf("days = %v", got)
	}
}
