package main

import (
	"maps"
	"testing"
)

func TestShortestUniquePrefixes(t *testing.T) {
	tests := []struct {
		name string
		orgs []string
		want map[string]string
	}{
		{
			name: "single namespace shortens to one",
			orgs: []string{"solo"},
			want: map[string]string{"solo": "s"},
		},
		{
			name: "distinct first letters all shorten to one",
			orgs: []string{"aeddi", "mvm-sh", "NewTendermint"},
			want: map[string]string{"aeddi": "a", "mvm-sh": "m", "NewTendermint": "N"},
		},
		{
			name: "shared prefixes grow only as far as needed",
			orgs: []string{"aeddi", "gfanton", "gnolang", "gnoverse", "gnoveser"},
			want: map[string]string{
				"aeddi":    "a",
				"gfanton":  "gf",
				"gnolang":  "gnol",
				"gnoverse": "gnover",
				"gnoveser": "gnoves",
			},
		},
		{
			name: "a namespace that is a prefix of another keeps its full name",
			orgs: []string{"gno", "gnolang"},
			want: map[string]string{"gno": "gno", "gnolang": "gnol"},
		},
		{
			name: "counts runes, not bytes",
			orgs: []string{"école", "zebra"},
			want: map[string]string{"école": "é", "zebra": "z"},
		},
		{
			name: "no namespaces",
			orgs: nil,
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortestUniquePrefixes(tt.orgs)
			if !maps.Equal(got, tt.want) {
				t.Errorf("shortestUniquePrefixes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNamespacesShortFallsBackToTheFullName(t *testing.T) {
	ns := namespaces(shortestUniquePrefixes([]string{"aeddi"}))

	if got := ns.short("aeddi"); got != "a" {
		t.Errorf("short(known) = %q, want %q", got, "a")
	}
	// A project cloned since the set was computed still gets a usable label.
	if got := ns.short("unseen"); got != "unseen" {
		t.Errorf("short(unknown) = %q, want %q", got, "unseen")
	}
}
