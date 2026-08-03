package main

import "strings"

// workspacePrefix marks a workspace as proj-managed so listings can tell them
// apart from ones opened by hand. It is deliberately one character: the label
// is the sidebar's widest column, and the prefix earns none of that space.
const workspacePrefix = "p/"

// workspaceLabel names the workspace for a project as p/<ns>/<name>, where <ns>
// is an organisation already abbreviated by namespaces.short. Abbreviating in
// the caller keeps this a pure join, and lets the abbreviation depend on the
// whole set of organisations rather than one name in isolation.
//
// The project name keeps its dots, unlike proj-tmux: tmux reserves them in
// target names so that backend rewrites them to dashes, but herdr has no such
// rule and rewriting would show a project under a name it does not have.
func workspaceLabel(namespace, name string) string {
	return workspacePrefix + namespace + "/" + name
}

// projectFromWorkspaceLabel reports the <ns>/<name> a workspace was opened for,
// or "" when the workspace is not proj-managed. The namespace is the shortened
// one: workspaceLabel discards the rest, so the original cannot be recovered.
func projectFromWorkspaceLabel(label string) string {
	rest, ok := strings.CutPrefix(label, workspacePrefix)
	if !ok {
		return ""
	}

	org, name, ok := strings.Cut(rest, "/")
	if !ok || org == "" || name == "" {
		return ""
	}
	return org + "/" + name
}
