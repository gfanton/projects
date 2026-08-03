package main

import (
	"github.com/gfanton/projects/internal/project"
	"github.com/gfanton/projects/internal/query"
)

// pathResolver resolves the checkout directory backing a project branch.
type pathResolver interface {
	WorkspacePath(proj project.Project, branch string) string
}

// target is where a search result lands in herdr. A project result opens the
// repository as a workspace; a branch result reuses that workspace and adds a
// tab for the branch checkout, so every branch of a project shares one entry in
// the sidebar instead of competing for top level space.
type target struct {
	WorkspaceLabel string
	TabLabel       string
	Dir            string
}

func targetOf(r *query.Result, paths pathResolver, ns namespaces) target {
	t := target{
		WorkspaceLabel: workspaceLabel(ns.short(r.Project.Organisation), r.Project.Name),
		Dir:            r.Project.Path,
	}

	if r.Workspace != "" {
		t.TabLabel = r.Workspace
		t.Dir = paths.WorkspacePath(*r.Project, r.Workspace)
	}
	return t
}
