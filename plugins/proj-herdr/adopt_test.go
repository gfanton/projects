package main

import (
	"context"
	"slices"
	"testing"
)

// One proj-managed workspace under a stale abbreviation, and one opened by hand.
const staleWorkspaceListJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
	`{"focused":false,"label":"p/a/gno-infra","workspace_id":"w9"},` +
	`{"focused":false,"label":"scratch","workspace_id":"w4"}]}}`

// Shaped from a real `herdr pane list`: panes carry cwd and workspace_id, which
// is the only exact link between a directory and a workspace.
const paneListJSON = `{"id":"cli:pane:list","result":{"panes":[` +
	`{"cwd":"/root/aeddi/gno-infra","foreground_cwd":"/root/aeddi/gno-infra","pane_id":"w9:p1","workspace_id":"w9"},` +
	`{"cwd":"/root/elsewhere","foreground_cwd":"/root/elsewhere","pane_id":"w4:p1","workspace_id":"w4"}]}}`

func TestEnsureWorkspaceAdoptsStaleLabelAtSameDir(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{
		[]byte(staleWorkspaceListJSON), // no p/ae/gno-infra
		[]byte(paneListJSON),           // w9 lives at the project dir
		[]byte(`{"result":{}}`),        // rename
		[]byte(`{"result":{}}`),        // focus
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "p/ae/gno-infra", "/root/aeddi/gno-infra", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w9" {
		t.Errorf("ID = %q, want %q — the existing workspace, not a new one", ws.ID, "w9")
	}

	wantRename := []string{"workspace", "rename", "w9", "p/ae/gno-infra"}
	if len(runner.calls) < 3 || !slices.Equal(runner.calls[2], wantRename) {
		t.Fatalf("calls = %v, want a rename %v", runner.calls, wantRename)
	}
	for _, c := range runner.calls {
		if len(c) > 1 && c[0] == "workspace" && c[1] == "create" {
			t.Errorf("created a duplicate workspace: %v", runner.calls)
		}
	}
}

func TestEnsureWorkspaceLeavesUnmanagedWorkspacesAlone(t *testing.T) {
	// The only workspace at this directory was opened by hand, so it must not be
	// renamed out from under the user.
	runner := &stubRunner{replies: [][]byte{
		[]byte(staleWorkspaceListJSON),
		[]byte(paneListJSON),
		[]byte(`{"result":{}}`), // create
		[]byte(`{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
			`{"focused":true,"label":"p/e/notes","workspace_id":"w12"}]}}`),
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "p/e/notes", "/root/elsewhere", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w12" {
		t.Errorf("ID = %q, want the newly created %q", ws.ID, "w12")
	}

	for _, c := range runner.calls {
		if len(c) > 1 && c[0] == "workspace" && c[1] == "rename" {
			t.Errorf("renamed a workspace it does not manage: %v", runner.calls)
		}
	}
}
