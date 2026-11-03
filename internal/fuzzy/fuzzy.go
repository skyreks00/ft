// Package fuzzy ranks project slugs and names against a user query.
package fuzzy

import (
	"sort"
	"strings"
)

// Scores, from best to worst.
const (
	Exact       = 100
	ExactShort  = 95 // slug without its cursus prefix, e.g. "libft"
	Prefix      = 70
	Initials    = 60 // "gnl" for get_next_line
	Substring   = 50
	Subsequence = 30
)

// prefixes stripped from slugs so that "libft" matches "42cursus-libft".
var prefixes = []string{"42cursus-", "42next-", "c-piscine-"}

// Item is something to match: a slug and a display name.
type Item struct {
	Slug string
	Name string
}

// Match is a ranked item.
type Match struct {
	Item
	Index int // position in the input slice
	Score int
}

// Rank returns the items that match query, best first. Ties keep the input
// order, so callers list the items they prefer first.
func Rank(query string, items []Item) []Match {
	q := normalize(query)
	if q == "" {
		return nil
	}
	var out []Match
	for i, it := range items {
		s := score(q, it)
		if s > 0 {
			out = append(out, Match{Item: it, Index: i, Score: s})
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].Score > out[b].Score
	})
	return out
}

// Pick returns the single unambiguous match: the best one when it is at
// least a prefix match and strictly better than the runner-up, or the only
// match scoring at least Substring.
func Pick(matches []Match) (Match, bool) {
	if len(matches) == 0 || matches[0].Score < Substring {
		return Match{}, false
	}
	best, next := matches[0].Score, 0
	if len(matches) > 1 {
		next = matches[1].Score
	}
	if (best >= Prefix && next < best) || next < Substring {
		return matches[0], true
	}
	return Match{}, false
}

// ShortSlug strips a known cursus prefix from a slug.
func ShortSlug(slug string) string {
	for _, p := range prefixes {
		if s, ok := strings.CutPrefix(slug, p); ok {
			return s
		}
	}
	return slug
}

func score(q string, it Item) int {
	slug := normalize(it.Slug)
	short := normalize(ShortSlug(it.Slug))
	name := normalize(it.Name)
	switch {
	case q == slug:
		return Exact
	case q == short || q == name:
		return ExactShort
	}
	best := 0
	for _, s := range []string{short, name, slug} {
		switch {
		case s == "":
		case strings.HasPrefix(s, q):
			best = max(best, Prefix)
		case q == initials(s):
			best = max(best, Initials)
		case strings.Contains(s, q):
			best = max(best, Substring)
		case isSubsequence(q, s):
			best = max(best, Subsequence)
		}
	}
	return best
}

// initials returns the first letter of each dash-separated word.
func initials(s string) string {
	var b strings.Builder
	for w := range strings.SplitSeq(s, "-") {
		if w != "" {
			b.WriteByte(w[0])
		}
	}
	return b.String()
}

// normalize lowercases and unifies separators: "Get_Next_Line" and
// "get-next-line" compare equal.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("_", "-", " ", "-").Replace(s)
}

func isSubsequence(q, s string) bool {
	i := 0
	for j := 0; j < len(s) && i < len(q); j++ {
		if s[j] == q[i] {
			i++
		}
	}
	return i == len(q)
}
