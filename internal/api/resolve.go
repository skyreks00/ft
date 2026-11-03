package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/swilmus/ft/internal/fuzzy"
)

// DefaultCursusID is 42cursus, used to search projects when the user has no
// cursus.
const DefaultCursusID = 21

// ResolveProject finds the project a user means by query: an exact match
// among their own projects, then an exact slug or id, then a name search in
// their main cursus, then a fuzzy match over the whole cursus (cached for a
// day). Ambiguous queries return an error listing suggestions; the user's
// own projects come first.
func (c *Client) ResolveProject(ctx context.Context, me *User, query string) (*ProjectRef, error) {
	var refs []ProjectRef
	for _, pu := range me.ProjectsUsers {
		refs = append(refs, pu.Project)
	}
	if m, ok := fuzzy.Pick(fuzzy.Rank(query, items(refs))); ok && m.Score >= fuzzy.ExactShort {
		return &refs[m.Index], nil
	}

	for _, slug := range []string{query, "42cursus-" + query} {
		p, err := c.Project(ctx, slug)
		if err == nil {
			return &ProjectRef{ID: p.ID, Name: p.Name, Slug: p.Slug}, nil
		}
		// 403 means the slug exists but is hidden from students.
		if !IsNotFound(err) && !IsForbidden(err) {
			return nil, err
		}
		if _, err := strconv.Atoi(query); err == nil {
			break // an id has no prefixed form
		}
	}

	cursusID := DefaultCursusID
	if cu := me.MainCursus(); cu != nil {
		cursusID = cu.Cursus.ID
	}
	found, err := c.SearchProjects(ctx, cursusID, query)
	if err != nil {
		return nil, err
	}
	refs = mergeRefs(refs, found)
	matches := fuzzy.Rank(query, items(refs))
	if m, ok := fuzzy.Pick(matches); ok {
		return &refs[m.Index], nil
	}

	all, err := c.CursusProjects(ctx, cursusID)
	if err != nil {
		return nil, err
	}
	refs = mergeRefs(refs, all)
	matches = fuzzy.Rank(query, items(refs))
	if m, ok := fuzzy.Pick(matches); ok {
		return &refs[m.Index], nil
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no project matches %q", query)
	}
	var sugg []string
	for _, m := range matches[:min(5, len(matches))] {
		sugg = append(sugg, "  "+m.Slug)
	}
	return nil, fmt.Errorf("%q is ambiguous, did you mean:\n%s", query, strings.Join(sugg, "\n"))
}

func items(refs []ProjectRef) []fuzzy.Item {
	out := make([]fuzzy.Item, len(refs))
	for i, r := range refs {
		out[i] = fuzzy.Item{Slug: r.Slug, Name: r.Name}
	}
	return out
}

func mergeRefs(a, b []ProjectRef) []ProjectRef {
	seen := map[int]bool{}
	for _, r := range a {
		seen[r.ID] = true
	}
	for _, r := range b {
		if !seen[r.ID] {
			a = append(a, r)
			seen[r.ID] = true
		}
	}
	return a
}
