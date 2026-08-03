package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/gfanton/projects/internal/project"
	"github.com/gfanton/projects/internal/query"
)

// Captured verbatim from `herdr workspace list` on herdr 0.7.5. Two workspaces
// share the label "gno-mcp", which is why lookup resolves to the first match.
const workspaceListJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[` +
	`{"active_tab_id":"w6:t1","agent_status":"unknown","focused":true,"label":"gno-mcp","number":1,"pane_count":2,"tab_count":1,"workspace_id":"w6"},` +
	`{"active_tab_id":"w7:t2","agent_status":"unknown","focused":false,"label":"nixpkgs","number":2,"pane_count":1,"tab_count":1,"workspace_id":"w7"},` +
	`{"active_tab_id":"w8:t1","agent_status":"unknown","focused":false,"label":"gno-mcp","number":3,"pane_count":1,"tab_count":1,"workspace_id":"w8"}]}}`

const emptyWorkspaceListJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[]}}`

// stubRunner returns canned output instead of executing the herdr binary, and
// records the arguments of every call. Replies are consumed in order; the last
// one is reused once exhausted.
type stubRunner struct {
	replies [][]byte
	err     error
	calls   [][]string
}

func (r *stubRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	r.calls = append(r.calls, args)
	if r.err != nil {
		return nil, r.err
	}
	i := min(len(r.calls)-1, len(r.replies)-1)
	return r.replies[i], nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWorkspaceByLabel(t *testing.T) {
	tests := []struct {
		name      string
		label     string
		wantID    string
		wantFound bool
	}{
		{"unique label", "nixpkgs", "w7", true},
		{"duplicate label resolves to first", "gno-mcp", "w6", true},
		{"missing label", "absent", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &stubRunner{replies: [][]byte{[]byte(workspaceListJSON)}}
			svc := NewHerdrService(testLogger(), runner)

			ws, found, err := svc.WorkspaceByLabel(context.Background(), tt.label)
			if err != nil {
				t.Fatalf("WorkspaceByLabel() error = %v", err)
			}
			if found != tt.wantFound {
				t.Fatalf("WorkspaceByLabel() found = %v, want %v", found, tt.wantFound)
			}
			if ws.ID != tt.wantID {
				t.Errorf("WorkspaceByLabel() ID = %q, want %q", ws.ID, tt.wantID)
			}
		})
	}
}

func TestEnsureWorkspaceFocusesExisting(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{[]byte(workspaceListJSON)}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "nixpkgs", "/root/gfanton/nixpkgs", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w7" {
		t.Errorf("EnsureWorkspace() ID = %q, want %q", ws.ID, "w7")
	}

	wantCalls := [][]string{
		{"workspace", "list"},
		{"workspace", "focus", "w7"},
	}
	if !slices.EqualFunc(runner.calls, wantCalls, slices.Equal) {
		t.Errorf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestEnsureWorkspaceCreatesWhenMissing(t *testing.T) {
	// list (empty) -> pane list (nothing to adopt) -> create -> list (present)
	runner := &stubRunner{replies: [][]byte{
		[]byte(emptyWorkspaceListJSON),
		[]byte(`{"id":"cli:pane:list","result":{"panes":[]}}`),
		[]byte(`{"id":"cli:workspace:create","result":{}}`),
		[]byte(workspaceListJSON),
	}}
	svc := NewHerdrService(testLogger(), runner)

	ws, err := svc.EnsureWorkspace(context.Background(), "nixpkgs", "/root/gfanton/nixpkgs", true)
	if err != nil {
		t.Fatalf("EnsureWorkspace() error = %v", err)
	}
	if ws.ID != "w7" {
		t.Errorf("EnsureWorkspace() ID = %q, want %q", ws.ID, "w7")
	}

	wantCreate := []string{
		"workspace", "create",
		"--cwd", "/root/gfanton/nixpkgs",
		"--label", "nixpkgs",
		"--focus",
	}
	if len(runner.calls) < 3 || !slices.Equal(runner.calls[2], wantCreate) {
		t.Errorf("create call = %v, want %v", runner.calls, wantCreate)
	}
}

func TestEnsureWorkspaceWithoutAutoCreate(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{[]byte(emptyWorkspaceListJSON)}}
	svc := NewHerdrService(testLogger(), runner)

	_, err := svc.EnsureWorkspace(context.Background(), "nixpkgs", "/root/gfanton/nixpkgs", false)
	if !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("EnsureWorkspace() error = %v, want ErrWorkspaceNotFound", err)
	}
	if len(runner.calls) != 1 {
		t.Errorf("calls = %v, want only the list call", runner.calls)
	}
}

// Captured verbatim from `herdr tab list --workspace w7` on herdr 0.7.5.
const tabListJSON = `{"id":"cli:tab:list","result":{"tabs":[` +
	`{"agent_status":"unknown","focused":false,"label":"1","number":2,"pane_count":1,"tab_id":"w7:t2","workspace_id":"w7"}` +
	`],"type":"tab_list"}}`

const emptyTabListJSON = `{"id":"cli:tab:list","result":{"tabs":[],"type":"tab_list"}}`

func TestEnsureTabFocusesExisting(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{[]byte(tabListJSON)}}
	svc := NewHerdrService(testLogger(), runner)

	tab, err := svc.EnsureTab(context.Background(), "w7", "1", "/root/gfanton/nixpkgs")
	if err != nil {
		t.Fatalf("EnsureTab() error = %v", err)
	}
	if tab.ID != "w7:t2" {
		t.Errorf("EnsureTab() ID = %q, want %q", tab.ID, "w7:t2")
	}

	wantCalls := [][]string{
		{"tab", "list", "--workspace", "w7"},
		{"tab", "focus", "w7:t2"},
	}
	if !slices.EqualFunc(runner.calls, wantCalls, slices.Equal) {
		t.Errorf("calls = %v, want %v", runner.calls, wantCalls)
	}
}

func TestEnsureTabCreatesWhenMissing(t *testing.T) {
	runner := &stubRunner{replies: [][]byte{
		[]byte(emptyTabListJSON),
		[]byte(`{"id":"cli:tab:create","result":{}}`),
		[]byte(tabListJSON),
	}}
	svc := NewHerdrService(testLogger(), runner)

	tab, err := svc.EnsureTab(context.Background(), "w7", "1", "/root/gfanton/nixpkgs")
	if err != nil {
		t.Fatalf("EnsureTab() error = %v", err)
	}
	if tab.ID != "w7:t2" {
		t.Errorf("EnsureTab() ID = %q, want %q", tab.ID, "w7:t2")
	}

	wantCreate := []string{
		"tab", "create",
		"--workspace", "w7",
		"--cwd", "/root/gfanton/nixpkgs",
		"--label", "1",
		"--focus",
	}
	if len(runner.calls) < 2 || !slices.Equal(runner.calls[1], wantCreate) {
		t.Errorf("create call = %v, want %v", runner.calls, wantCreate)
	}
}

func TestHerdrArgs(t *testing.T) {
	tests := []struct {
		name    string
		session string
		args    []string
		want    []string
	}{
		{
			name: "default session passes args through",
			args: []string{"workspace", "list"},
			want: []string{"workspace", "list"},
		},
		{
			name:    "named session is a global flag before the subcommand",
			session: "work",
			args:    []string{"workspace", "list"},
			want:    []string{"--session", "work", "workspace", "list"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := herdrArgs(tt.session, tt.args); !slices.Equal(got, tt.want) {
				t.Errorf("herdrArgs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkspaceLabel(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		project   string
		want      string
	}{
		{
			name:      "joins the abbreviated namespace",
			namespace: "gf",
			project:   "nixpkgs",
			want:      "p/gf/nixpkgs",
		},
		{
			name:      "a namespace unique at one letter stays one letter",
			namespace: "a",
			project:   "gno-infra",
			want:      "p/a/gno-infra",
		},
		{
			name:      "project name keeps its dots",
			namespace: "gno",
			project:   "gno.me",
			want:      "p/gno/gno.me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceLabel(tt.namespace, tt.project); got != tt.want {
				t.Errorf("workspaceLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectFromWorkspaceLabel(t *testing.T) {
	tests := []struct {
		name  string
		label string
		want  string
	}{
		{"managed workspace", "p/gf/nixpkgs", "gf/nixpkgs"},
		{"unmanaged workspace", "nixpkgs", ""},
		{"prefix only", "p/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := projectFromWorkspaceLabel(tt.label); got != tt.want {
				t.Errorf("projectFromWorkspaceLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

// stubPaths resolves branch checkouts without touching the filesystem.
type stubPaths struct{}

func (stubPaths) WorkspacePath(proj project.Project, branch string) string {
	return "/root/.workspace/" + proj.Name + "/" + branch
}

func TestTargetOf(t *testing.T) {
	proj := &project.Project{Organisation: "gfanton", Name: "nixpkgs", Path: "/root/gfanton/nixpkgs"}

	tests := []struct {
		name   string
		result query.Result
		want   target
	}{
		{
			name:   "project opens a workspace at the repo",
			result: query.Result{Project: proj},
			want: target{
				WorkspaceLabel: "p/gf/nixpkgs",
				Dir:            "/root/gfanton/nixpkgs",
			},
		},
		{
			name:   "branch adds a tab at the branch checkout",
			result: query.Result{Project: proj, Workspace: "feat/auth"},
			want: target{
				WorkspaceLabel: "p/gf/nixpkgs",
				TabLabel:       "feat/auth",
				Dir:            "/root/.workspace/nixpkgs/feat/auth",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetOf(&tt.result, stubPaths{}, namespaces{"gfanton": "gf"})
			if got != tt.want {
				t.Errorf("targetOf() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
