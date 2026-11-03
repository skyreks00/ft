package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/swilmus/ft/internal/fuzzy"
)

// Project definitions rarely change; per-user progress does.
const (
	projectTTL      = 24 * time.Hour
	projectsUserTTL = time.Minute
	scaleTeamTTL    = 2 * time.Minute
)

// ProjectRef is the short project form embedded in other resources.
type ProjectRef struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	ParentID *int   `json:"parent_id"`
}

// Project is a /v2/projects/:id response.
type Project struct {
	ID         int              `json:"id"`
	Name       string           `json:"name"`
	Slug       string           `json:"slug"`
	Exam       bool             `json:"exam"`
	Difficulty int              `json:"difficulty"` // XP
	Parent     *ProjectRef      `json:"parent"`
	Campus     []Campus         `json:"campus"`
	Sessions   []ProjectSession `json:"project_sessions"`

	// Set by Curriculum from ft's holy graph table, when it has one.
	Circle *int     `json:"circle,omitempty"`
	Tracks []string `json:"tracks,omitempty"`
}

// ProjectSession holds the campus/cursus specific settings of a project.
type ProjectSession struct {
	ID              int     `json:"id"`
	CampusID        *int    `json:"campus_id"`
	CursusID        *int    `json:"cursus_id"`
	Solo            *bool   `json:"solo"`
	EstimateTime    *string `json:"estimate_time"`
	IsSubscriptable bool    `json:"is_subscriptable"`
	MaxPeople       *int    `json:"max_people"`
	Scales          []Scale `json:"scales"`
}

// Scale is an evaluation grid; CorrectionNumber is how many peer
// evaluations a team needs.
type Scale struct {
	ID               int  `json:"id"`
	CorrectionNumber int  `json:"correction_number"`
	IsPrimary        bool `json:"is_primary"`
}

// ProjectsUser is a user's registration to a project.
type ProjectsUser struct {
	ID            int        `json:"id"`
	Status        string     `json:"status"`
	FinalMark     *int       `json:"final_mark"`
	Validated     *bool      `json:"validated?"`
	CurrentTeamID *int       `json:"current_team_id"`
	Occurrence    int        `json:"occurrence"`
	Marked        bool       `json:"marked"`
	MarkedAt      *time.Time `json:"marked_at"`
	RetriableAt   *time.Time `json:"retriable_at"`
	CursusIDs     []int      `json:"cursus_ids"`
	Project       ProjectRef `json:"project"`
	Teams         []Team     `json:"teams"`
}

// Team is a project team, as embedded in /v2/projects_users/:id
// (/v2/teams/:id is forbidden for students).
type Team struct {
	ID               int        `json:"id"`
	Name             string     `json:"name"`
	Status           string     `json:"status"`
	FinalMark        *int       `json:"final_mark"`
	Validated        *bool      `json:"validated?"`
	Locked           bool       `json:"locked?"`
	Closed           bool       `json:"closed?"`
	RepoURL          string     `json:"repo_url"`
	ProjectID        int        `json:"project_id"`
	ProjectSessionID int        `json:"project_session_id"`
	TerminatingAt    *time.Time `json:"terminating_at"`
	CreatedAt        time.Time  `json:"created_at"`
	Users            []TeamUser `json:"users"`
}

// TeamUser is a team member.
type TeamUser struct {
	ID     int    `json:"id"`
	Login  string `json:"login"`
	Leader bool   `json:"leader"`
}

// CurrentTeam returns the team matching CurrentTeamID, else the most
// recently created one, else nil.
func (pu *ProjectsUser) CurrentTeam() *Team {
	var latest *Team
	for i := range pu.Teams {
		t := &pu.Teams[i]
		if pu.CurrentTeamID != nil && t.ID == *pu.CurrentTeamID {
			return t
		}
		if latest == nil || t.CreatedAt.After(latest.CreatedAt) {
			latest = t
		}
	}
	return latest
}

// Session returns the project session with the given id, or nil.
func (p *Project) Session(id int) *ProjectSession {
	for i := range p.Sessions {
		if p.Sessions[i].ID == id {
			return &p.Sessions[i]
		}
	}
	return nil
}

// RequiredEvaluations returns the primary scale's correction number, or 0.
func (s *ProjectSession) RequiredEvaluations() int {
	for _, sc := range s.Scales {
		if sc.IsPrimary {
			return sc.CorrectionNumber
		}
	}
	if len(s.Scales) > 0 {
		return s.Scales[0].CorrectionNumber
	}
	return 0
}

// ScaleTeam is an evaluation (one corrector, one corrected team).
type ScaleTeam struct {
	ID         int        `json:"id"`
	BeginAt    *time.Time `json:"begin_at"`
	FilledAt   *time.Time `json:"filled_at"`
	FinalMark  *int       `json:"final_mark"`
	Comment    *string    `json:"comment"`  // the evaluator's
	Feedback   *string    `json:"feedback"` // the evaluated team's, about the evaluator
	Corrector  Corrector  `json:"corrector"`
	Correcteds Correcteds `json:"correcteds"`
	Flag       *Flag      `json:"flag"`
	Team       *Team      `json:"team"`
}

// Done reports whether the evaluation has been filled in.
func (s *ScaleTeam) Done() bool { return s.FilledAt != nil }

// Flag is the outcome flag of an evaluation (ok, cheat, ...).
type Flag struct {
	Name     string `json:"name"`
	Positive bool   `json:"positive"`
}

// Corrector is the evaluator: an object, or a string such as "invisible"
// when the intra hides who it is.
type Corrector struct {
	ID    int    `json:"id,omitempty"`
	Login string `json:"login,omitempty"`
}

// UnmarshalJSON accepts both the object and the string forms.
func (c *Corrector) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*c = Corrector{}
		return nil
	}
	type plain Corrector
	return json.Unmarshal(b, (*plain)(c))
}

// Project fetches a project by id or slug.
func (c *Client) Project(ctx context.Context, idOrSlug string) (*Project, error) {
	var p Project
	if err := c.Get(ctx, "/v2/projects/"+url.PathEscape(idOrSlug), nil, projectTTL, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SearchProjects searches a cursus' projects by name.
func (c *Client) SearchProjects(ctx context.Context, cursusID int, term string) ([]ProjectRef, error) {
	q := url.Values{"search[name]": {term}, "page[size]": {"30"}}
	var ps []ProjectRef
	if err := c.Get(ctx, "/v2/cursus/"+strconv.Itoa(cursusID)+"/projects", q, projectTTL, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

// CursusProjects lists every project of a cursus (several pages; cached
// for a day, and shared with Curriculum).
func (c *Client) CursusProjects(ctx context.Context, cursusID int) ([]ProjectRef, error) {
	return GetAll[ProjectRef](ctx, c, "/v2/cursus/"+strconv.Itoa(cursusID)+"/projects", nil, projectTTL)
}

// curriculumSkip are slug prefixes of variants or retired projects that
// duplicate the current ones.
var curriculumSkip = []string{"42next-", "old-"}

// Curriculum lists the projects a campus offers in a cursus. With a holy
// graph (see Graph; quests picks the curriculum variant) it returns the
// graph's projects by circle; without, it guesses: projects with a session
// there, by XP, leaving out exams, sub-projects, retired variants and 0 XP
// entries.
func (c *Client) Curriculum(ctx context.Context, cursusID, campusID int, quests []string) ([]Project, error) {
	all, err := GetAll[Project](ctx, c, "/v2/cursus/"+strconv.Itoa(cursusID)+"/projects", nil, projectTTL)
	if err != nil {
		return nil, err
	}
	if t := Graph(campusID, cursusID, quests); t != nil {
		return byCircle(all, t), nil
	}
	// Some projects exist twice (e.g. call-me-maybe and
	// 42cursus-call-me-maybe); keep the one with a session of this campus.
	byName := map[string]int{}
	var out []Project
	for _, p := range all {
		if p.Parent != nil || p.Exam || p.Difficulty <= 0 || hasAnyPrefix(p.Slug, curriculumSkip) {
			continue
		}
		if !p.atCampus(campusID) || p.SessionFor(campusID, cursusID) == nil {
			continue
		}
		name := fuzzy.ShortSlug(p.Slug)
		if i, dup := byName[name]; dup {
			if p.campusSession(campusID) && !out[i].campusSession(campusID) {
				out[i] = p
			}
			continue
		}
		byName[name] = len(out)
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Difficulty != out[j].Difficulty {
			return out[i].Difficulty < out[j].Difficulty
		}
		return out[i].Slug < out[j].Slug
	})
	return out, nil
}

// optionalModules matches the modules of the optional piscines (Python,
// data science, Unity, mobile, Django, Symfony, RoR, OCaml, deep learning,
// cybersecurity). The API does not link them to their piscine, so this
// goes by name.
var optionalModules = regexp.MustCompile(`^(python|data-science|unity|mobile|django|symfony|ror|ocaml|deep-learning|cybersecurity|piscine-object)-`)

// OptionalModule reports whether p looks like a module of an optional
// piscine rather than a project of the main path (heuristic, by slug).
func (p *Project) OptionalModule() bool {
	return optionalModules.MatchString(p.Slug)
}

// campusSession reports whether p has a session specific to campusID.
// byCircle keeps the graph's projects, ordered by circle, then (after the
// common core) by track, then XP.
func byCircle(all []Project, t *CircleTable) []Project {
	var out []Project
	for _, p := range all {
		info, ok := t.Lookup(p.Slug)
		if !ok || info.Primary != "" {
			continue // not on the graph, or an older slug of a project
		}
		circle := info.Circle
		p.Circle, p.Tracks = &circle, info.Tracks
		out = append(out, p)
	}
	firstTrack := func(p *Project) string {
		if len(p.Tracks) == 0 {
			return "\uffff" // untracked last
		}
		return p.Tracks[0]
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := &out[i], &out[j]
		if *a.Circle != *b.Circle {
			return *a.Circle < *b.Circle
		}
		if *a.Circle == AfterCore && firstTrack(a) != firstTrack(b) {
			return firstTrack(a) < firstTrack(b)
		}
		if a.Exam != b.Exam {
			return b.Exam // a circle's exam comes after its projects
		}
		if a.Difficulty != b.Difficulty {
			return a.Difficulty < b.Difficulty
		}
		return a.Slug < b.Slug
	})
	return out
}

func (p *Project) campusSession(campusID int) bool {
	for _, s := range p.Sessions {
		if s.CampusID != nil && *s.CampusID == campusID {
			return true
		}
	}
	return false
}

func (p *Project) atCampus(campusID int) bool {
	for _, c := range p.Campus {
		if c.ID == campusID {
			return true
		}
	}
	return false
}

// SessionFor returns the session that applies to a campus and cursus
// (a session without campus or cursus applies to all), or nil.
func (p *Project) SessionFor(campusID, cursusID int) *ProjectSession {
	var generic *ProjectSession
	for i := range p.Sessions {
		s := &p.Sessions[i]
		campusOK := s.CampusID == nil || *s.CampusID == campusID
		cursusOK := s.CursusID == nil || *s.CursusID == cursusID
		if !campusOK || !cursusOK {
			continue
		}
		if s.CampusID != nil {
			return s
		}
		generic = s
	}
	return generic
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// QuestSlugs lists the slugs of a user's quests (e.g. "42next", which picks
// the curriculum variant of the holy graph).
func (c *Client) QuestSlugs(ctx context.Context, login string) ([]string, error) {
	var qus []struct {
		Quest struct {
			Slug string `json:"slug"`
		} `json:"quest"`
	}
	if err := c.Get(ctx, "/v2/users/"+url.PathEscape(login)+"/quests_users", url.Values{"page[size]": {"100"}}, projectTTL, &qus); err != nil {
		return nil, err
	}
	slugs := make([]string, 0, len(qus))
	for _, q := range qus {
		slugs = append(slugs, q.Quest.Slug)
	}
	return slugs, nil
}

// ProjectsUser fetches a registration with its teams.
func (c *Client) ProjectsUser(ctx context.Context, id int) (*ProjectsUser, error) {
	var pu ProjectsUser
	if err := c.Get(ctx, "/v2/projects_users/"+strconv.Itoa(id), nil, projectsUserTTL, &pu); err != nil {
		return nil, err
	}
	return &pu, nil
}

// Register subscribes a user to a project (POST /v2/projects_users). The
// intra may refuse it for students; callers fall back to the web page.
func (c *Client) Register(ctx context.Context, projectID, userID int) (*ProjectsUser, error) {
	body := map[string]any{"projects_user": map[string]int{"project_id": projectID, "user_id": userID}}
	var pu ProjectsUser
	if err := c.Send(ctx, http.MethodPost, "/v2/projects_users", body, &pu); err != nil {
		return nil, err
	}
	c.Forget("/v2/me", nil)
	return &pu, nil
}

// Slot is an evaluation slot. The evaluator is hidden ("invisible") for
// other users' slots.
type Slot struct {
	ID        int       `json:"id"`
	BeginAt   time.Time `json:"begin_at"`
	EndAt     time.Time `json:"end_at"`
	ScaleTeam *struct {
		ID int `json:"id"`
	} `json:"scale_team"`
}

// ProjectSlots lists upcoming open slots for a project, soonest first. The
// API does not filter them by campus.
func (c *Client) ProjectSlots(ctx context.Context, projectID int) ([]Slot, error) {
	q := url.Values{"filter[future]": {"true"}, "sort": {"begin_at"}}
	return GetAll[Slot](ctx, c, "/v2/projects/"+strconv.Itoa(projectID)+"/slots", q, time.Minute)
}

// TeamEvaluations lists the evaluations of a team.
func (c *Client) TeamEvaluations(ctx context.Context, teamID int) ([]ScaleTeam, error) {
	q := url.Values{"filter[team_id]": {strconv.Itoa(teamID)}}
	return GetAll[ScaleTeam](ctx, c, "/v2/scale_teams", q, scaleTeamTTL)
}
