package api

import (
	"context"
	"slices"
	"strconv"
	"time"
)

// ProjectStatus is everything `ft status` and the TUI show for a project.
type ProjectStatus struct {
	Project             ProjectRef  `json:"project"`
	Status              string      `json:"status"` // "not_registered" when there is no registration
	FinalMark           *int        `json:"final_mark"`
	Validated           *bool       `json:"validated"`
	RetriableAt         *time.Time  `json:"retriable_at,omitempty"`
	BlackholedAt        *time.Time  `json:"blackholed_at,omitempty"`
	Team                *Team       `json:"team,omitempty"`
	RequiredEvaluations int         `json:"required_evaluations"`
	Evaluations         []ScaleTeam `json:"evaluations"`
}

// NotRegistered is ProjectStatus.Status for a project the user has not
// registered to.
const NotRegistered = "not_registered"

// Registered reports whether the user is registered to the project.
func (s *ProjectStatus) Registered() bool { return s.Status != NotRegistered }

// EvaluationsDone counts filled-in evaluations.
func (s *ProjectStatus) EvaluationsDone() int {
	n := 0
	for i := range s.Evaluations {
		if s.Evaluations[i].Done() {
			n++
		}
	}
	return n
}

// ProjectStatus gathers a project's registration, current team, required
// evaluation count and evaluations (at most 4 requests, all cached).
func (c *Client) ProjectStatus(ctx context.Context, me *User, ref ProjectRef) (*ProjectStatus, error) {
	s := &ProjectStatus{Project: ref, Status: NotRegistered, Evaluations: []ScaleTeam{}}
	embedded := me.ProjectsUser(ref.ID)
	if embedded == nil {
		return s, nil
	}
	pu, err := c.ProjectsUser(ctx, embedded.ID)
	if err != nil {
		return nil, err
	}
	s.Project = pu.Project
	s.Status = pu.Status
	s.FinalMark = pu.FinalMark
	s.Validated = pu.Validated
	if pu.RetriableAt != nil && pu.RetriableAt.After(time.Now()) {
		s.RetriableAt = pu.RetriableAt
	}
	if cu := me.MainCursus(); cu != nil && slices.Contains(pu.CursusIDs, cu.Cursus.ID) {
		s.BlackholedAt = cu.BlackholedAt
	}

	s.Team = pu.CurrentTeam()
	if s.Team == nil {
		return s, nil
	}
	proj, err := c.Project(ctx, strconv.Itoa(pu.Project.ID))
	if err != nil {
		return nil, err
	}
	if sess := proj.Session(s.Team.ProjectSessionID); sess != nil {
		s.RequiredEvaluations = sess.RequiredEvaluations()
	}
	evals, err := c.TeamEvaluations(ctx, s.Team.ID)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(evals, func(x, y ScaleTeam) int {
		return timeOrZero(x.BeginAt).Compare(timeOrZero(y.BeginAt))
	})
	s.Evaluations = evals
	return s, nil
}

func timeOrZero(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
