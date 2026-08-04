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
// The two directories are separate because a workspace outlives the result
// that opened it: rooting it at a branch checkout because a branch happened to
// be picked first would persist for every later open, since those match on the
// label alone.
type target struct {
	WorkspaceLabel string
	WorkspaceDir   string
	TabLabel       string
	TabDir         string
}

func targetOf(r *query.Result, paths pathResolver, ns namespaces) target {
	t := target{
		WorkspaceLabel: workspaceLabel(ns.short(r.Project.Organisation), r.Project.Name),
		WorkspaceDir:   r.Project.Path,
	}

	if r.Workspace != "" {
		t.TabLabel = r.Workspace
		t.TabDir = paths.WorkspacePath(*r.Project, r.Workspace)
	}
	return t
}
