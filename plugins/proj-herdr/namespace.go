package main

import (
	"io/fs"
	"log/slog"
	"slices"

	"github.com/gfanton/projects/internal/project"
)

// namespaces maps an organisation to the shortest prefix that identifies it
// among the organisations present on disk.
type namespaces map[string]string

// short abbreviates an organisation, falling back to the full name for one that
// was not on disk when the set was built — a project cloned since then still
// gets a usable, if longer, label.
func (n namespaces) short(organisation string) string {
	if s, ok := n[organisation]; ok {
		return s
	}
	return organisation
}

// shortestUniquePrefixes gives each organisation the fewest leading runes that
// no other organisation shares. An organisation that is itself a prefix of
// another cannot be shortened at all and keeps its full name.
func shortestUniquePrefixes(orgs []string) namespaces {
	out := make(namespaces, len(orgs))

	for _, org := range orgs {
		runes := []rune(org)
		out[org] = org

		for k := 1; k <= len(runes); k++ {
			prefix := string(runes[:k])
			if !sharedBy(orgs, org, prefix) {
				out[org] = prefix
				break
			}
		}
	}
	return out
}

// sharedBy reports whether an organisation other than org also starts with
// prefix.
func sharedBy(orgs []string, org, prefix string) bool {
	return slices.ContainsFunc(orgs, func(other string) bool {
		if other == org || len([]rune(other)) < len([]rune(prefix)) {
			return false
		}
		return string([]rune(other)[:len([]rune(prefix))]) == prefix
	})
}

// scanNamespaces walks the project root and abbreviates every organisation it
// finds. Labels therefore depend on what is checked out: cloning a project
// under a new, similarly named organisation lengthens the abbreviation, and
// workspaces opened under the old one stop matching until reopened.
func scanNamespaces(logger *slog.Logger, rootDir string) namespaces {
	var orgs []string

	err := project.Walk(rootDir, func(_ fs.DirEntry, p *project.Project) error {
		if p != nil && !slices.Contains(orgs, p.Organisation) {
			orgs = append(orgs, p.Organisation)
		}
		return nil
	})
	if err != nil {
		// A partial scan still labels correctly for what it saw, and unknown
		// organisations fall back to their full name.
		logger.Warn("scanning namespaces", "root", rootDir, "error", err)
	}

	return shortestUniquePrefixes(orgs)
}
