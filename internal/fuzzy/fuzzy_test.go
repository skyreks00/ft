package fuzzy

import "testing"

var projects = []Item{
	{"42cursus-libft", "Libft"},
	{"libftpp", "libftpp"},
	{"libasm", "libasm"},
	{"42cursus-get_next_line", "get_next_line"},
	{"42cursus-ft_printf", "ft_printf"},
	{"c-piscine-c-00", "C Piscine C 00"},
	{"c-piscine-c-01", "C Piscine C 01"},
}

func TestRankAndPick(t *testing.T) {
	tests := []struct {
		query    string
		wantPick string // "" means ambiguous
		wantTop  string
	}{
		{"42cursus-libft", "42cursus-libft", "42cursus-libft"},
		{"libft", "42cursus-libft", "42cursus-libft"},
		{"LIBFT", "42cursus-libft", "42cursus-libft"},
		{"get-next-line", "42cursus-get_next_line", "42cursus-get_next_line"},
		{"printf", "42cursus-ft_printf", "42cursus-ft_printf"},
		{"get", "42cursus-get_next_line", "42cursus-get_next_line"},
		{"lib", "", "42cursus-libft"},
		{"c-0", "", "c-piscine-c-00"},
		{"gnl", "42cursus-get_next_line", "42cursus-get_next_line"},
		{"fp", "42cursus-ft_printf", "42cursus-ft_printf"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			m := Rank(tt.query, projects)
			if len(m) == 0 {
				t.Fatal("no matches")
			}
			if m[0].Slug != tt.wantTop {
				t.Errorf("top = %s (%d), want %s", m[0].Slug, m[0].Score, tt.wantTop)
			}
			got, ok := Pick(m)
			if tt.wantPick == "" {
				if ok {
					t.Errorf("Pick = %s, want ambiguous", got.Slug)
				}
			} else if !ok || got.Slug != tt.wantPick {
				t.Errorf("Pick = %s %v, want %s", got.Slug, ok, tt.wantPick)
			}
		})
	}
}

func TestRankNoMatch(t *testing.T) {
	if m := Rank("zzz", projects); len(m) != 0 {
		t.Fatalf("got %v", m)
	}
	if m := Rank("  ", projects); m != nil {
		t.Fatalf("empty query matched %v", m)
	}
}

func TestShortSlug(t *testing.T) {
	if s := ShortSlug("42cursus-libft"); s != "libft" {
		t.Fatal(s)
	}
	if s := ShortSlug("libasm"); s != "libasm" {
		t.Fatal(s)
	}
}
