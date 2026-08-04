package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// One proj-managed workspace under a stale abbreviation, and one opened by hand.
const staleWorkspaceListJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
	`{"focused":false,"label":"p/a/gno-infra","workspace_id":"w9"},` +
	`{"focused":false,"label":"scratch","workspace_id":"w4"}]}}`

// Shaped from a real `herdr pane list`: panes carry cwd and workspace_id, which
// is the only exact link between a directory and a workspace.
// foreground_cwd deliberately differs from cwd so a regression swapping the two
// cannot pass unnoticed. Adoption reads cwd, which tracks the shell: verified by
// creating a workspace, running cd in its pane, and watching herdr report the
// new directory. A workspace whose panes have all moved away is therefore not
// adoptable — acceptable, because by then nothing in it is at the project.
const paneListJSON = `{"id":"cli:pane:list","result":{"panes":[` +
	`{"cwd":"/root/aeddi/gno-infra","foreground_cwd":"/root/aeddi/gno-infra/cmd","pane_id":"w9:p1","workspace_id":"w9"},` +
	`{"cwd":"/root/elsewhere","foreground_cwd":"/tmp","pane_id":"w4:p1","workspace_id":"w4"}]}}`

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

// An unmanaged pane listed before the managed one at the same directory must
// not hide the managed workspace: taking only the first pane at that path would
// skip adoption and open a duplicate.
const sharedDirPaneListJSON = `{"id":"cli:pane:list","result":{"panes":[` +
	`{"cwd":"/root/aeddi/gno-infra","workspace_id":"w4","pane_id":"w4:p1"},` +
	`{"cwd":"/root/aeddi/gno-infra","workspace_id":"w9","pane_id":"w9:p1"}]}}`

func TestEnsureWorkspaceAdoptsPastAnUnmanagedPaneAtTheSameDir(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{
		[]byte(staleWorkspaceListJSON), // w9 managed, w4 "scratch"
		[]byte(sharedDirPaneListJSON),  // unmanaged w4 listed first
		[]byte(`{"result":{}}`),        // rename
		[]byte(`{"result":{}}`),        // focus
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "p/ae/gno-infra", "/root/aeddi/gno-infra", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w9" {
		t.Errorf("ID = %q, want %q — the managed workspace, not a new one", ws.ID, "w9")
	}

	for _, c := range runner.calls {
		if len(c) > 1 && c[0] == "workspace" && c[1] == "create" {
			t.Errorf("created a duplicate instead of adopting: %v", runner.calls)
		}
		if len(c) > 2 && c[0] == "workspace" && c[1] == "rename" && c[2] != "w9" {
			t.Errorf("renamed the wrong workspace: %v", c)
		}
	}
}

// Adoption must survive a directory that differs only by trailing slash or an
// unclean segment, which herdr and the project engine can spell differently.
func TestEnsureWorkspaceAdoptsAcrossUncleanPaths(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{
		[]byte(staleWorkspaceListJSON),
		[]byte(paneListJSON), // pane cwd is /root/aeddi/gno-infra
		[]byte(`{"result":{}}`),
		[]byte(`{"result":{}}`),
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "p/ae/gno-infra", "/root/aeddi/gno-infra/", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w9" {
		t.Errorf("ID = %q, want %q", ws.ID, "w9")
	}
}

// Captured verbatim from `herdr workspace get wZZZ` on herdr 0.7.5: errors come
// back in the same envelope, with no result member. Decoding one into the
// result type yields an empty list, which is indistinguishable from "nothing
// open" — and would send EnsureWorkspace off to create a duplicate.
const errorEnvelopeJSON = `{"error":{"code":"workspace_not_found","message":"workspace wZZZ not found"},"id":"cli:workspace:list"}`

func TestWorkspacesRejectsAnErrorEnvelope(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{[]byte(errorEnvelopeJSON)}}
	svc := NewHerdrService(testLogger(), runner)

	_, _, err := svc.WorkspaceByLabel(context.Background(), "p/gf/nixpkgs")
	if err == nil {
		t.Fatal("WorkspaceByLabel() error = nil, want the herdr error surfaced")
	}
	if !strings.Contains(err.Error(), "workspace_not_found") {
		t.Errorf("error = %v, want it to carry herdr's code", err)
	}
}

func TestEnsureWorkspaceDoesNotCreateOnAnErrorEnvelope(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{[]byte(errorEnvelopeJSON)}}
	svc := NewHerdrService(testLogger(), runner)

	if _, err := svc.EnsureWorkspace(context.Background(), "p/gf/nixpkgs", "/root/gfanton/nixpkgs", true); err == nil {
		t.Fatal("EnsureWorkspace() error = nil, want the herdr error surfaced")
	}
	for _, c := range runner.calls {
		if len(c) > 1 && c[0] == "workspace" && c[1] == "create" {
			t.Errorf("created a workspace despite herdr erroring: %v", runner.calls)
		}
	}
}

// A transport failure must propagate, not read as an empty herdr.
func TestWorkspacesPropagatesRunnerFailure(t *testing.T) {
	runner := &stubRunner{err: errors.New("herdr exited 1")}
	svc := NewHerdrService(testLogger(), runner)

	if _, _, err := svc.WorkspaceByLabel(context.Background(), "anything"); err == nil {
		t.Fatal("WorkspaceByLabel() error = nil, want the runner failure")
	}
}

// A pane's cwd is mutable shell state, not project identity: cd-ing a pane of
// one project into another project's directory must not let that workspace be
// renamed and repurposed. Only a workspace naming the SAME project is stale.
const hijackWorkspaceListJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
	`{"focused":false,"label":"p/gf/goforge","workspace_id":"wC"}]}}`

const hijackPaneListJSON = `{"id":"cli:pane:list","result":{"panes":[` +
	`{"cwd":"/root/gnolang/gno","workspace_id":"wC","pane_id":"wC:p4"}]}}`

func TestEnsureWorkspaceDoesNotAdoptAnotherProjectsWorkspace(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{
		[]byte(hijackWorkspaceListJSON), // no p/gnol/gno
		[]byte(hijackPaneListJSON),      // a goforge pane has cd'd into gno
		[]byte(`{"result":{}}`),         // create
		[]byte(`{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
			`{"focused":true,"label":"p/gnol/gno","workspace_id":"wE"}]}}`),
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "p/gnol/gno", "/root/gnolang/gno", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "wE" {
		t.Errorf("ID = %q, want the newly created %q", ws.ID, "wE")
	}

	for _, c := range runner.calls {
		if len(c) > 1 && c[0] == "workspace" && c[1] == "rename" {
			t.Errorf("renamed another project's workspace: %v", c)
		}
	}
}
