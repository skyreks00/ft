package api

import (
	"context"
	"time"
)

// TTL for profile data; location changes often, so keep it short.
const meTTL = 2 * time.Minute

// User is a /v2/me or /v2/users/:id response (fields used by ft only).
type User struct {
	ID              int          `json:"id"`
	Login           string       `json:"login"`
	Email           string       `json:"email"`
	DisplayName     string       `json:"displayname"`
	URL             string       `json:"url"`
	Location        *string      `json:"location"`
	Wallet          int          `json:"wallet"`
	CorrectionPoint int          `json:"correction_point"`
	PoolMonth       string       `json:"pool_month"`
	PoolYear        string       `json:"pool_year"`
	Staff           bool         `json:"staff?"`
	CursusUsers     []CursusUser `json:"cursus_users"`
	Campus          []Campus     `json:"campus"`
	CampusUsers     []CampusUser `json:"campus_users"`
	// Only returned by /v2/me and /v2/users/:id.
	ProjectsUsers []ProjectsUser `json:"projects_users"`
}

// CursusUser is a user's enrolment in a cursus.
type CursusUser struct {
	ID           int        `json:"id"`
	Level        float64    `json:"level"`
	Grade        *string    `json:"grade"`
	BeginAt      *time.Time `json:"begin_at"`
	EndAt        *time.Time `json:"end_at"`
	BlackholedAt *time.Time `json:"blackholed_at"`
	Cursus       Cursus     `json:"cursus"`
}

// Cursus is a 42 cursus.
type Cursus struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Campus is a 42 campus.
type Campus struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	TimeZone string `json:"time_zone"`
}

// CampusUser links a user to a campus.
type CampusUser struct {
	CampusID  int  `json:"campus_id"`
	IsPrimary bool `json:"is_primary"`
}

// MainCursus returns the 42cursus enrolment if any, else the one with the
// highest level. It returns nil when the user has no cursus.
func (u *User) MainCursus() *CursusUser {
	var best *CursusUser
	for i := range u.CursusUsers {
		cu := &u.CursusUsers[i]
		if cu.Cursus.Slug == "42cursus" {
			return cu
		}
		if best == nil || cu.Level > best.Level {
			best = cu
		}
	}
	return best
}

// PrimaryCampus returns the user's primary campus, or nil.
func (u *User) PrimaryCampus() *Campus {
	primary := 0
	for _, cu := range u.CampusUsers {
		if cu.IsPrimary {
			primary = cu.CampusID
		}
	}
	for i := range u.Campus {
		if u.Campus[i].ID == primary || (primary == 0 && i == 0) {
			return &u.Campus[i]
		}
	}
	return nil
}

// ProjectsUser returns the user's registration to a project, or nil. The
// embedded form has no teams; use Client.ProjectsUser for those.
func (u *User) ProjectsUser(projectID int) *ProjectsUser {
	for i := range u.ProjectsUsers {
		if u.ProjectsUsers[i].Project.ID == projectID {
			return &u.ProjectsUsers[i]
		}
	}
	return nil
}

// Me returns the authenticated user.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var u User
	if err := c.Get(ctx, "/v2/me", nil, meTTL, &u); err != nil {
		return nil, err
	}
	return &u, nil
}
