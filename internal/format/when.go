package format

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// slotStep is the intra's slot granularity.
const slotStep = 15 * time.Minute

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
	"dim": time.Sunday, "lun": time.Monday, "mar": time.Tuesday, "mer": time.Wednesday,
	"jeu": time.Thursday, "ven": time.Friday, "sam": time.Saturday,
}

// ParseRange parses a slot range in local time:
//
//	start: "14:00" (today, or tomorrow if already past), "tomorrow 14:00",
//	       "mon 14:00", "2026-11-03 14:00", "03/11 14:00"
//	end:   "16:00" (after start), "+2h", "+90m", or a full date like start
//
// Both ends must fall on a quarter hour.
func ParseRange(start, end string, now time.Time) (time.Time, time.Time, error) {
	b, err := parseStart(start, now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	var e time.Time
	if d, ok := strings.CutPrefix(strings.TrimSpace(end), "+"); ok {
		dur, err := time.ParseDuration(d)
		if err != nil || dur <= 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid duration %q (try +2h or +90m)", end)
		}
		e = b.Add(dur)
	} else if hm, err := clock(end); err == nil {
		e = at(b, hm)
		if !e.After(b) {
			e = e.AddDate(0, 0, 1)
		}
	} else if e, err = parseStart(end, b); err != nil {
		return time.Time{}, time.Time{}, err
	}

	if !e.After(b) {
		return time.Time{}, time.Time{}, errors.New("the end must be after the start")
	}
	for _, t := range []time.Time{b, e} {
		if t.Sub(t.Truncate(slotStep)) != 0 {
			return time.Time{}, time.Time{}, fmt.Errorf("%s is not on a quarter hour (:00, :15, :30, :45)", t.Format("15:04"))
		}
	}
	return b, e, nil
}

// parseStart parses a day (optional) and a clock time.
func parseStart(s string, now time.Time) (time.Time, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	switch len(f) {
	case 1:
		hm, err := clock(f[0])
		if err != nil {
			if t, err2 := time.ParseInLocation("2006-01-02T15:04", strings.ToUpper(f[0]), now.Location()); err2 == nil {
				return t, nil
			}
			return time.Time{}, err
		}
		t := at(now, hm)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, nil
	case 2:
		hm, err := clock(f[1])
		if err != nil {
			return time.Time{}, err
		}
		day, err := parseDay(f[0], now)
		if err != nil {
			return time.Time{}, err
		}
		return at(day, hm), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (try 14:00 or \"tomorrow 14:00\")", s)
}

func parseDay(s string, now time.Time) (time.Time, error) {
	switch s {
	case "today", "auj", "aujourd'hui":
		return now, nil
	case "tomorrow", "demain":
		return now.AddDate(0, 0, 1), nil
	}
	if wd, ok := weekdays[s[:min(3, len(s))]]; ok && len(s) >= 3 {
		days := (int(wd) - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		return now.AddDate(0, 0, days), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, now.Location()); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("02/01", s, now.Location()); err == nil {
		t = t.AddDate(now.Year(), 0, 0)
		if t.Before(now.Truncate(24 * time.Hour)) {
			t = t.AddDate(1, 0, 0)
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid day %q (try today, tomorrow, mon, 2026-11-03 or 03/11)", s)
}

type hourMin struct{ h, m int }

func clock(s string) (hourMin, error) {
	s = strings.ReplaceAll(strings.ToLower(s), "h", ":")
	if strings.HasSuffix(s, ":") {
		s += "00"
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return hourMin{}, fmt.Errorf("invalid time %q (try 14:00)", s)
	}
	return hourMin{t.Hour(), t.Minute()}, nil
}

func at(day time.Time, hm hourMin) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hm.h, hm.m, 0, 0, day.Location())
}
