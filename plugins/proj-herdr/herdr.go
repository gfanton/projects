package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
)

// ErrWorkspaceNotFound reports that no workspace carries the requested label
// and the caller declined to create one.
var ErrWorkspaceNotFound = errors.New("workspace not found")

// ErrTabNotFound reports that a tab is absent from a workspace even after herdr
// was asked to create it.
var ErrTabNotFound = errors.New("tab not found")

// commandRunner executes a herdr CLI invocation and returns its stdout. herdr
// answers every command with JSON, so callers decode rather than parse text.
type commandRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

// errorReply is herdr's failure shape. It arrives in the same envelope as a
// success and simply omits `result`, so decoding one straight into a result
// type yields an empty list rather than an error — a herdr that is failing
// would read as a herdr with nothing open.
type errorReply struct {
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeReply rejects an error envelope before unmarshalling into v.
func decodeReply(out []byte, v any) error {
	var reply errorReply
	if err := json.Unmarshal(out, &reply); err == nil && reply.Error != nil {
		return fmt.Errorf("herdr %s: %s", reply.Error.Code, reply.Error.Message)
	}
	return json.Unmarshal(out, v)
}

// ---- Workspaces

// Workspace is a herdr workspace as reported by `herdr workspace list`.
type Workspace struct {
	ID      string
	Label   string
	Focused bool
}

type workspaceListResponse struct {
	Result struct {
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
			Focused     bool   `json:"focused"`
		} `json:"workspaces"`
	} `json:"result"`
}

// HerdrService drives herdr through its CLI.
type HerdrService struct {
	logger *slog.Logger
	runner commandRunner
}

func NewHerdrService(logger *slog.Logger, runner commandRunner) *HerdrService {
	return &HerdrService{logger: logger, runner: runner}
}

func (s *HerdrService) workspaces(ctx context.Context) ([]Workspace, error) {
	out, err := s.runner.Run(ctx, "workspace", "list")
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}

	var resp workspaceListResponse
	if err := decodeReply(out, &resp); err != nil {
		return nil, fmt.Errorf("decode workspace list: %w", err)
	}

	list := make([]Workspace, 0, len(resp.Result.Workspaces))
	for _, w := range resp.Result.Workspaces {
		list = append(list, Workspace{ID: w.WorkspaceID, Label: w.Label, Focused: w.Focused})
	}
	return list, nil
}

// WorkspaceByLabel resolves a label to a workspace. herdr addresses workspaces
// by id and does not enforce unique labels, so the first match wins.
func (s *HerdrService) WorkspaceByLabel(ctx context.Context, label string) (Workspace, bool, error) {
	list, err := s.workspaces(ctx)
	if err != nil {
		return Workspace{}, false, err
	}

	i := slices.IndexFunc(list, func(w Workspace) bool { return w.Label == label })
	if i < 0 {
		return Workspace{}, false, nil
	}
	return list[i], true, nil
}

// EnsureWorkspace focuses the workspace carrying label, creating it at dir
// first when absent. Passing create=false is the `auto_session = off` case: an
// absent workspace is reported rather than opened.
//
// The freshly created workspace is resolved with a second lookup because herdr
// assigns the id, and only `workspace list` has a contract we rely on.
func (s *HerdrService) EnsureWorkspace(ctx context.Context, label, dir string, create bool) (Workspace, error) {
	list, err := s.workspaces(ctx)
	if err != nil {
		return Workspace{}, err
	}

	i := slices.IndexFunc(list, func(w Workspace) bool { return w.Label == label })
	ws, found := Workspace{}, i >= 0
	if found {
		ws = list[i]
	}

	if !found {
		if !create {
			return Workspace{}, fmt.Errorf("%q: %w", label, ErrWorkspaceNotFound)
		}

		// The abbreviation a label carries depends on which organisations are
		// checked out, so cloning a new one can leave open workspaces under a
		// name this build no longer produces. Adopt such a workspace instead of
		// opening a second one onto the same directory.
		if stale, ok, err := s.managedWorkspaceAt(ctx, list, dir); err != nil {
			return Workspace{}, err
		} else if ok {
			if err := s.renameWorkspace(ctx, stale.ID, label); err != nil {
				return Workspace{}, err
			}
			if err := s.focusWorkspace(ctx, stale.ID); err != nil {
				return Workspace{}, err
			}
			stale.Label = label
			return stale, nil
		}

		if err := s.createWorkspace(ctx, label, dir); err != nil {
			return Workspace{}, err
		}

		ws, found, err = s.WorkspaceByLabel(ctx, label)
		if err != nil {
			return Workspace{}, err
		}
		if !found {
			return Workspace{}, fmt.Errorf("%q after create: %w", label, ErrWorkspaceNotFound)
		}
		// create --focus already focused it.
		return ws, nil
	}

	if err := s.focusWorkspace(ctx, ws.ID); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

type pane struct {
	WorkspaceID string `json:"workspace_id"`
	Cwd         string `json:"cwd"`
}

type paneListResponse struct {
	Result struct {
		Panes []pane `json:"panes"`
	} `json:"result"`
}

// managedWorkspaceAt finds a proj-managed workspace already open on dir.
//
// Workspaces expose no directory of their own, so the link runs through panes,
// which do. Matching on the directory rather than on the label is what keeps
// adoption safe: abbreviations are ambiguous by construction — "gno" prefixes
// both gnolang and gnoverse — while a path identifies exactly one project.
//
// Workspaces this plugin did not open are never adopted, so a hand-made
// workspace that happens to sit on the same directory is left alone.
func (s *HerdrService) managedWorkspaceAt(ctx context.Context, list []Workspace, dir string) (Workspace, bool, error) {
	out, err := s.runner.Run(ctx, "pane", "list")
	if err != nil {
		return Workspace{}, false, fmt.Errorf("list panes: %w", err)
	}

	var panes paneListResponse
	if err := decodeReply(out, &panes); err != nil {
		return Workspace{}, false, fmt.Errorf("decode pane list: %w", err)
	}

	// Every pane at the directory is considered, not just the first: a workspace
	// opened by hand can sit on the same path, and stopping at it would skip the
	// managed workspace this exists to find. Paths are cleaned because herdr and
	// the project engine can spell the same directory differently.
	want := filepath.Clean(dir)

	for _, p := range panes.Result.Panes {
		if filepath.Clean(p.Cwd) != want {
			continue
		}

		i := slices.IndexFunc(list, func(w Workspace) bool {
			return w.ID == p.WorkspaceID && strings.HasPrefix(w.Label, workspacePrefix)
		})
		if i >= 0 {
			return list[i], true, nil
		}
	}
	return Workspace{}, false, nil
}

func (s *HerdrService) renameWorkspace(ctx context.Context, id, label string) error {
	s.logger.Info("relabelling workspace", "workspace_id", id, "label", label)

	if _, err := s.runner.Run(ctx, "workspace", "rename", id, label); err != nil {
		return fmt.Errorf("rename workspace %s: %w", id, err)
	}
	return nil
}

// ---- Tabs

// Tab is a herdr tab as reported by `herdr tab list`.
type Tab struct {
	ID      string
	Label   string
	Focused bool
}

type tabListResponse struct {
	Result struct {
		Tabs []struct {
			TabID   string `json:"tab_id"`
			Label   string `json:"label"`
			Focused bool   `json:"focused"`
		} `json:"tabs"`
	} `json:"result"`
}

func (s *HerdrService) tabs(ctx context.Context, workspaceID string) ([]Tab, error) {
	out, err := s.runner.Run(ctx, "tab", "list", "--workspace", workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list tabs of %s: %w", workspaceID, err)
	}

	var resp tabListResponse
	if err := decodeReply(out, &resp); err != nil {
		return nil, fmt.Errorf("decode tab list: %w", err)
	}

	list := make([]Tab, 0, len(resp.Result.Tabs))
	for _, t := range resp.Result.Tabs {
		list = append(list, Tab{ID: t.TabID, Label: t.Label, Focused: t.Focused})
	}
	return list, nil
}

// TabByLabel resolves a label to a tab within a workspace. As with workspaces,
// labels are not unique and the first match wins.
func (s *HerdrService) TabByLabel(ctx context.Context, workspaceID, label string) (Tab, bool, error) {
	list, err := s.tabs(ctx, workspaceID)
	if err != nil {
		return Tab{}, false, err
	}

	i := slices.IndexFunc(list, func(t Tab) bool { return t.Label == label })
	if i < 0 {
		return Tab{}, false, nil
	}
	return list[i], true, nil
}

// EnsureTab focuses the tab carrying label inside workspaceID, creating it at
// dir when absent. Unlike workspaces there is no opt-out: a window the caller
// asked for is a window it wants.
func (s *HerdrService) EnsureTab(ctx context.Context, workspaceID, label, dir string) (Tab, error) {
	tab, found, err := s.TabByLabel(ctx, workspaceID, label)
	if err != nil {
		return Tab{}, err
	}

	if !found {
		if err := s.createTab(ctx, workspaceID, label, dir); err != nil {
			return Tab{}, err
		}

		tab, found, err = s.TabByLabel(ctx, workspaceID, label)
		if err != nil {
			return Tab{}, err
		}
		if !found {
			return Tab{}, fmt.Errorf("tab %q in %s after create: %w", label, workspaceID, ErrTabNotFound)
		}
		// create --focus already focused it.
		return tab, nil
	}

	if err := s.focusTab(ctx, tab.ID); err != nil {
		return Tab{}, err
	}
	return tab, nil
}

func (s *HerdrService) createTab(ctx context.Context, workspaceID, label, dir string) error {
	s.logger.Debug("creating herdr tab", "workspace_id", workspaceID, "label", label, "dir", dir)

	_, err := s.runner.Run(ctx, "tab", "create",
		"--workspace", workspaceID, "--cwd", dir, "--label", label, "--focus")
	if err != nil {
		return fmt.Errorf("create tab %q in %s: %w", label, workspaceID, err)
	}
	return nil
}

func (s *HerdrService) focusTab(ctx context.Context, id string) error {
	s.logger.Debug("focusing herdr tab", "tab_id", id)

	if _, err := s.runner.Run(ctx, "tab", "focus", id); err != nil {
		return fmt.Errorf("focus tab %s: %w", id, err)
	}
	return nil
}

// ---- Commands

func (s *HerdrService) createWorkspace(ctx context.Context, label, dir string) error {
	s.logger.Debug("creating herdr workspace", "label", label, "dir", dir)

	if _, err := s.runner.Run(ctx, "workspace", "create", "--cwd", dir, "--label", label, "--focus"); err != nil {
		return fmt.Errorf("create workspace %q: %w", label, err)
	}
	return nil
}

func (s *HerdrService) focusWorkspace(ctx context.Context, id string) error {
	s.logger.Debug("focusing herdr workspace", "workspace_id", id)

	if _, err := s.runner.Run(ctx, "workspace", "focus", id); err != nil {
		return fmt.Errorf("focus workspace %s: %w", id, err)
	}
	return nil
}
