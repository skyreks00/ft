package api

import (
	"embed"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The public API has no holy graph. ft ships it in circles/*.json, taken
// from the intra's graph page (see circles/README.md):
//
//   - <campus>-<cursus>.json: a campus' graph for a cursus (project_data.json:
//     circles from the projects' rules, tracks from the graph's layers);
//   - <cursus>-<quest>.json: the core circles of a curriculum variant that
//     the page draws itself (hack.list), such as 42Next.
//
// Students in a variant's quest get its core circles, with the campus
// graph's projects after the common core.

//go:embed circles/*.json
var circleFiles embed.FS

// AfterCore is the circle of the projects after the common core.
const AfterCore = 7

// CircleInfo places a project on the holy graph.
type CircleInfo struct {
	Circle  int      // 0–6 for the common core, AfterCore after it
	Tracks  []string // specialization layers, after the common core
	Order   int      // position in the table, for a stable order
	Primary string   // set on an older slug that counts for this project
	Choice  []string // projects to choose one from (e.g. agent-smith, tree_nity)
	Planet  []string // the modules of a planet (e.g. Python modules)
}

// CircleTable is the holy graph of one campus, cursus and variant.
type CircleTable struct {
	Generated string
	Variant   string // "" for the campus graph, else the quest (e.g. "42next")
	bySlug    map[string]CircleInfo
	alts      map[string][]string // primary slug → older slugs that count
}

type campusFile struct {
	Generated string   `json:"generated"`
	CampusID  int      `json:"campus_id"`
	CursusID  int      `json:"cursus_id"`
	Tracks    []string `json:"tracks"`
	// [project id, slug, circle, [track indexes]]
	Projects [][]json.RawMessage `json:"projects"`
}

type variantFile struct {
	Generated string `json:"generated"`
	CursusID  int    `json:"cursus_id"`
	Quest     string `json:"quest"`
	Circles   [][]struct {
		Kind        string   `json:"kind"` // solo, exam, big, planet, linked
		Slug        string   `json:"slug"`
		Replacement string   `json:"replacement"`
		Slugs       []string `json:"slugs"`
	} `json:"circles"`
}

var (
	circlesOnce   sync.Once
	campusGraphs  map[string]*CircleTable // "campus/cursus"
	variantGraphs map[string]*CircleTable // "cursus/quest", core only
	mergedGraphs  sync.Map                // "campus/cursus/quest"
	circlesErrors []error
)

// Graph returns the holy graph for a campus and cursus, in the variant of
// the user's quests (QuestSlugs), or nil when ft has none.
func Graph(campusID, cursusID int, quests []string) *CircleTable {
	circlesOnce.Do(loadCircles)
	base := campusGraphs[key(campusID, cursusID)]
	for _, q := range quests {
		core := variantGraphs[strconv.Itoa(cursusID)+"/"+q]
		if core == nil {
			continue
		}
		k := key(campusID, cursusID) + "/" + q
		if t, ok := mergedGraphs.Load(k); ok {
			return t.(*CircleTable)
		}
		t := core.withAfterCore(base)
		mergedGraphs.Store(k, t)
		return t
	}
	return base
}

func key(campusID, cursusID int) string {
	return strconv.Itoa(campusID) + "/" + strconv.Itoa(cursusID)
}

func loadCircles() {
	campusGraphs, variantGraphs = map[string]*CircleTable{}, map[string]*CircleTable{}
	entries, _ := circleFiles.ReadDir("circles")
	for _, e := range entries {
		b, err := circleFiles.ReadFile("circles/" + e.Name())
		if err != nil {
			continue
		}
		var probe struct {
			Quest    string `json:"quest"`
			CampusID int    `json:"campus_id"`
			CursusID int    `json:"cursus_id"`
		}
		if err := json.Unmarshal(b, &probe); err != nil {
			circlesErrors = append(circlesErrors, err)
			continue
		}
		if probe.Quest != "" {
			t, err := parseVariant(b)
			if err != nil {
				circlesErrors = append(circlesErrors, err)
				continue
			}
			variantGraphs[strconv.Itoa(probe.CursusID)+"/"+probe.Quest] = t
			continue
		}
		t, err := parseCampus(b)
		if err != nil {
			circlesErrors = append(circlesErrors, err)
			continue
		}
		campusGraphs[key(probe.CampusID, probe.CursusID)] = t
	}
}

func newTable(generated, variant string) *CircleTable {
	return &CircleTable{Generated: generated, Variant: variant, bySlug: map[string]CircleInfo{}, alts: map[string][]string{}}
}

func parseCampus(b []byte) (*CircleTable, error) {
	var f campusFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	t := newTable(f.Generated, "")
	for i, row := range f.Projects {
		var slug string
		var circle int
		var tracks []int
		if len(row) < 3 || json.Unmarshal(row[1], &slug) != nil || json.Unmarshal(row[2], &circle) != nil {
			continue
		}
		if len(row) > 3 {
			_ = json.Unmarshal(row[3], &tracks)
		}
		info := CircleInfo{Circle: circle, Order: i}
		for _, ti := range tracks {
			if ti >= 0 && ti < len(f.Tracks) {
				info.Tracks = append(info.Tracks, f.Tracks[ti])
			}
		}
		t.bySlug[slug] = info
	}
	return t, nil
}

func parseVariant(b []byte) (*CircleTable, error) {
	var f variantFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	t := newTable(f.Generated, f.Quest)
	order := 0
	add := func(slug string, info CircleInfo) {
		info.Order = order
		order++
		t.bySlug[slug] = info
	}
	for circle, entries := range f.Circles {
		for _, e := range entries {
			switch e.Kind {
			case "planet":
				for _, s := range e.Slugs {
					add(s, CircleInfo{Circle: circle, Planet: e.Slugs})
				}
			case "linked":
				for _, s := range e.Slugs {
					add(s, CircleInfo{Circle: circle, Choice: e.Slugs})
				}
			default:
				add(e.Slug, CircleInfo{Circle: circle})
				if e.Replacement != "" {
					add(e.Replacement, CircleInfo{Circle: circle, Primary: e.Slug})
					t.alts[e.Slug] = append(t.alts[e.Slug], e.Replacement)
				}
			}
		}
	}
	return t, nil
}

// withAfterCore copies a variant's core circles and adds the campus
// graph's projects after the common core.
func (t *CircleTable) withAfterCore(base *CircleTable) *CircleTable {
	out := newTable(t.Generated, t.Variant)
	for s, info := range t.bySlug {
		out.bySlug[s] = info
	}
	for s, alts := range t.alts {
		out.alts[s] = alts
	}
	if base == nil {
		return out
	}
	for s, info := range base.bySlug {
		if _, taken := out.bySlug[s]; !taken && info.Circle == AfterCore {
			info.Order += 1 << 16 // after the core entries
			out.bySlug[s] = info
		}
	}
	return out
}

// Lookup returns where a project sits on the graph, by slug.
func (t *CircleTable) Lookup(slug string) (CircleInfo, bool) {
	if t == nil {
		return CircleInfo{}, false
	}
	info, ok := t.bySlug[slug]
	return info, ok
}

// InCircle lists the projects of a circle (not their older slugs), in
// table order.
func (t *CircleTable) InCircle(circle int) []string {
	var slugs []string
	for s, info := range t.bySlug {
		if info.Circle == circle && info.Primary == "" {
			slugs = append(slugs, s)
		}
	}
	sort.Slice(slugs, func(i, j int) bool { return t.bySlug[slugs[i]].Order < t.bySlug[slugs[j]].Order })
	return slugs
}

// Progress counts what a circle requires and what the user validated: a
// project counts once (also through an older slug), a choice once for any
// of its projects, each module of a planet once.
func (t *CircleTable) Progress(circle int, validated func(slug string) bool) (done, total int) {
	seenChoice := map[string]bool{}
	for _, s := range t.InCircle(circle) {
		info := t.bySlug[s]
		ok := validated(s) || slices.ContainsFunc(t.alts[s], validated)
		if len(info.Choice) > 0 {
			k := strings.Join(info.Choice, "|")
			if seenChoice[k] {
				continue
			}
			seenChoice[k] = true
			ok = slices.ContainsFunc(info.Choice, validated)
		}
		total++
		if ok {
			done++
		}
	}
	return done, total
}

// Current is the first circle not complete (AfterCore once the common core
// is done).
func (t *CircleTable) Current(validated func(slug string) bool) int {
	for c := 0; c < AfterCore; c++ {
		if done, total := t.Progress(c, validated); done < total {
			return c
		}
	}
	return AfterCore
}

// CircleName is "Circle 2", or "After the common core".
func CircleName(circle int) string {
	if circle >= AfterCore {
		return "After the common core"
	}
	return "Circle " + strconv.Itoa(circle)
}
