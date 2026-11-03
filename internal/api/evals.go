package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

const (
	userTTL     = 5 * time.Minute
	locationTTL = 2 * time.Minute
)

// Correcteds is the list of evaluated users, which the intra hides (as a
// string) before the evaluation.
type Correcteds []TeamUser

// UnmarshalJSON accepts both the list and the string forms.
func (c *Correcteds) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*c = nil
		return nil
	}
	var users []TeamUser
	if err := json.Unmarshal(b, &users); err != nil {
		return err
	}
	*c = users
	return nil
}

// Evaluations are a user's upcoming evaluations, on both sides.
type Evaluations struct {
	AsCorrector []ScaleTeam `json:"as_corrector"` // evaluations to give
	AsCorrected []ScaleTeam `json:"as_corrected"` // evaluations to receive
}

// UpcomingEvaluations lists a user's future evaluations. (/v2/me/scale_teams
// returns nothing for students, so this goes through /v2/users/:login.)
func (c *Client) UpcomingEvaluations(ctx context.Context, login string) (*Evaluations, error) {
	base := "/v2/users/" + url.PathEscape(login) + "/scale_teams/"
	q := url.Values{"filter[future]": {"true"}, "sort": {"begin_at"}}
	var e Evaluations
	var err error
	if e.AsCorrector, err = GetAll[ScaleTeam](ctx, c, base+"as_corrector", q, time.Minute); err != nil {
		return nil, err
	}
	if e.AsCorrected, err = GetAll[ScaleTeam](ctx, c, base+"as_corrected", q, time.Minute); err != nil {
		return nil, err
	}
	return &e, nil
}

// CorrectorEvaluations lists the evaluations a user gives that begin
// between from and to, soonest first (with the evaluated team and its
// repository).
func (c *Client) CorrectorEvaluations(ctx context.Context, login string, from, to time.Time) ([]ScaleTeam, error) {
	q := url.Values{
		"range[begin_at]": {from.UTC().Format(time.RFC3339) + "," + to.UTC().Format(time.RFC3339)},
		"sort":            {"begin_at"},
	}
	return GetAll[ScaleTeam](ctx, c, "/v2/users/"+url.PathEscape(login)+"/scale_teams/as_corrector", q, time.Minute)
}

// ProjectSlugs maps the evaluations' project ids to slugs, from the profile
// first, then from the (day-cached) project endpoint.
func (c *Client) ProjectSlugs(ctx context.Context, me *User, ev *Evaluations) map[int]string {
	names := map[int]string{0: "?"}
	for _, pu := range me.ProjectsUsers {
		names[pu.Project.ID] = pu.Project.Slug
	}
	for _, list := range [][]ScaleTeam{ev.AsCorrector, ev.AsCorrected} {
		for i := range list {
			id := list[i].ProjectID()
			if _, ok := names[id]; ok {
				continue
			}
			names[id] = strconv.Itoa(id)
			if p, err := c.Project(ctx, strconv.Itoa(id)); err == nil {
				names[id] = p.Slug
			}
		}
	}
	return names
}

// ProjectID is the evaluated team's project, or 0.
func (s *ScaleTeam) ProjectID() int {
	if s.Team == nil {
		return 0
	}
	return s.Team.ProjectID
}

// User fetches a user's profile by login.
func (c *Client) User(ctx context.Context, login string) (*User, error) {
	var u User
	if err := c.Get(ctx, "/v2/users/"+url.PathEscape(login), nil, userTTL, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Location is a session on a campus computer. EndAt is nil while the user
// is logged in.
type Location struct {
	ID       int        `json:"id"`
	Host     string     `json:"host"`
	BeginAt  time.Time  `json:"begin_at"`
	EndAt    *time.Time `json:"end_at"`
	CampusID int        `json:"campus_id"`
}

// LastLocation returns the user's most recent session, or nil.
func (c *Client) LastLocation(ctx context.Context, login string) (*Location, error) {
	q := url.Values{"page[size]": {"1"}}
	var locs []Location
	if err := c.Get(ctx, "/v2/users/"+url.PathEscape(login)+"/locations", q, locationTTL, &locs); err != nil {
		return nil, err
	}
	if len(locs) == 0 {
		return nil, nil
	}
	return &locs[0], nil
}
