package api

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"time"
)

const logtimeTTL = 5 * time.Minute

// Locations lists a user's campus sessions that began between from and to,
// oldest first.
func (c *Client) Locations(ctx context.Context, login string, from, to time.Time) ([]Location, error) {
	q := url.Values{
		"range[begin_at]": {from.UTC().Format(time.RFC3339) + "," + to.UTC().Format(time.RFC3339)},
		"sort":            {"begin_at"},
	}
	return GetAll[Location](ctx, c, "/v2/users/"+url.PathEscape(login)+"/locations", q, logtimeTTL)
}

// DayKey is the local calendar day of t, as used by Logtime.
func DayKey(t time.Time) string { return t.Local().Format("2006-01-02") }

// Logtime sums the time spent on campus per local day, between from and to.
// Sessions are split at midnight; a session still open counts until now.
func Logtime(locs []Location, from, to, now time.Time) map[string]time.Duration {
	days := map[string]time.Duration{}
	for _, l := range locs {
		begin, end := l.BeginAt, now
		if l.EndAt != nil {
			end = *l.EndAt
		}
		begin, end = later(begin, from), earlier(end, to)
		for begin.Before(end) {
			y, m, d := begin.Local().Date()
			midnight := time.Date(y, m, d+1, 0, 0, 0, 0, time.Local)
			stop := earlier(end, midnight)
			days[DayKey(begin)] += stop.Sub(begin)
			begin = stop
		}
	}
	return days
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// PastEvaluations lists a user's last evaluations on both sides, newest
// first, with the evaluators' comments and the feedback given back.
func (c *Client) PastEvaluations(ctx context.Context, login string, n int) (*Evaluations, error) {
	base := "/v2/users/" + url.PathEscape(login) + "/scale_teams/"
	q := url.Values{
		"range[begin_at]": {"2000-01-01T00:00:00Z," + time.Now().UTC().Format(time.RFC3339)},
		"sort":            {"-begin_at"},
		"page[size]":      {strconv.Itoa(n)},
	}
	var e Evaluations
	if err := c.Get(ctx, base+"as_corrector", q, logtimeTTL, &e.AsCorrector); err != nil {
		return nil, err
	}
	if err := c.Get(ctx, base+"as_corrected", q, logtimeTTL, &e.AsCorrected); err != nil {
		return nil, err
	}
	for _, list := range []*[]ScaleTeam{&e.AsCorrector, &e.AsCorrected} {
		sort.SliceStable(*list, func(i, j int) bool {
			return timeOrZero((*list)[i].BeginAt).After(timeOrZero((*list)[j].BeginAt))
		})
	}
	return &e, nil
}
