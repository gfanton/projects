package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// agentViewSource scopes the view herdr stores. It matches the plugin id in
// herdr-plugin.toml so clearing removes our view and leaves any other source's
// alone.
const agentViewSource = "gfanton.proj"

// ---- Modes

// agentSortMode is one ordering in the cycle a keybinding steps through.
type agentSortMode int

const (
	// agentSortPriority defers to herdr's own ui.agent_panel_sort by removing
	// our view rather than installing one that imitates it.
	agentSortPriority agentSortMode = iota
	agentSortRecent
	agentSortAttention
)

func (m agentSortMode) String() string {
	switch m {
	case agentSortRecent:
		return "recent"
	case agentSortAttention:
		return "attention"
	default:
		return "priority"
	}
}

// parseAgentSortMode reads a persisted mode, falling back to priority for
// anything unrecognised so a stale or corrupt state file cannot wedge the key.
func parseAgentSortMode(name string) agentSortMode {
	switch strings.TrimSpace(name) {
	case "recent":
		return agentSortRecent
	case "attention":
		return agentSortAttention
	default:
		return agentSortPriority
	}
}

func nextAgentSortMode(m agentSortMode) agentSortMode {
	switch m {
	case agentSortPriority:
		return agentSortRecent
	case agentSortRecent:
		return agentSortAttention
	default:
		return agentSortPriority
	}
}

// ---- Wire types

// agentViewSort is one ordering key of herdr's agent.view.set payload.
type agentViewSort struct {
	Field string `json:"field"`
	Order string `json:"order,omitempty"`
}

// agentViewSetParams is herdr's agent.view.set payload.
type agentViewSetParams struct {
	Source string          `json:"source"`
	Label  string          `json:"label,omitempty"`
	Sort   []agentViewSort `json:"sort,omitempty"`
}

// agentViewClearParams is herdr's agent.view.clear payload.
type agentViewClearParams struct {
	Source string `json:"source"`
}

// sortKeys describes an ordering to herdr. A nil result means the mode is
// expressed by clearing the view instead of setting one.
//
// state_change_seq is a counter herdr bumps on every agent state change, so
// descending is last-activity-first. attention is herdr's own urgency rank —
// blocked, then finished-but-unseen, then working — and descending puts the
// agents wanting something from you at the top.
func (m agentSortMode) sortKeys() []agentViewSort {
	switch m {
	case agentSortRecent:
		return []agentViewSort{{Field: "state_change_seq", Order: "desc"}}
	case agentSortAttention:
		return []agentViewSort{
			{Field: "attention", Order: "desc"},
			{Field: "state_change_seq", Order: "desc"},
		}
	default:
		return nil
	}
}

// ---- Service

// apiCaller sends a request to herdr's socket API.
type apiCaller interface {
	Call(ctx context.Context, method string, params any) ([]byte, error)
}

// modeStore remembers which ordering was applied last. herdr exposes no way to
// read the active view back — there is no agent.view.get and the snapshot omits
// it — so the cycle has to track its own position.
type modeStore interface {
	Load() (agentSortMode, error)
	Save(agentSortMode) error
}

// AgentViewService installs the ordering of herdr's agents panel.
//
// The panel's built-in toggle offers only grouped-by-workspace and
// attention-then-workspace, and the view installed here lives in the running
// server rather than in config.toml, so it does not survive a server restart.
// Reapplying is a keypress; the cycle returns to the config-level ordering
// after one lap.
type AgentViewService struct {
	logger *slog.Logger
	caller apiCaller
	store  modeStore
}

func NewAgentViewService(logger *slog.Logger, caller apiCaller, store modeStore) *AgentViewService {
	return &AgentViewService{logger: logger, caller: caller, store: store}
}

// Apply installs one ordering.
func (s *AgentViewService) Apply(ctx context.Context, mode agentSortMode) error {
	method, params := "agent.view.set", any(agentViewSetParams{
		Source: agentViewSource,
		Label:  mode.String(),
		Sort:   mode.sortKeys(),
	})
	if mode.sortKeys() == nil {
		method, params = "agent.view.clear", agentViewClearParams{Source: agentViewSource}
	}

	s.logger.Debug("applying agent view", "mode", mode.String(), "method", method)

	if _, err := s.caller.Call(ctx, method, params); err != nil {
		return fmt.Errorf("apply agent sort %q: %w", mode, err)
	}
	return nil
}

// Cycle advances to the next ordering, applies it, and remembers it. The stored
// mode only moves once herdr has accepted the new view, so a failed call leaves
// the cycle where it was rather than silently skipping an ordering.
func (s *AgentViewService) Cycle(ctx context.Context) (agentSortMode, error) {
	current, err := s.store.Load()
	if err != nil {
		return current, err
	}

	next := nextAgentSortMode(current)
	if err := s.Apply(ctx, next); err != nil {
		return current, err
	}
	if err := s.store.Save(next); err != nil {
		return next, err
	}
	return next, nil
}

// ---- Mode store

// fileModeStore keeps the cycle position in a single-line file.
type fileModeStore struct {
	path string
}

func newFileModeStore(path string) *fileModeStore {
	return &fileModeStore{path: path}
}

// modeStatePath resolves the XDG state location for the cycle position. State
// rather than config because it is a transient UI position, not a setting.
func modeStatePath(env herdrEnv) (string, error) {
	if env.StateHome != "" {
		return filepath.Join(env.StateHome, "proj-herdr", "agent-sort"), nil
	}
	if env.Home != "" {
		return filepath.Join(env.Home, ".local", "state", "proj-herdr", "agent-sort"), nil
	}
	return "", fmt.Errorf("cannot locate a state directory: neither XDG_STATE_HOME nor HOME is set")
}

// Load reports the last applied mode. A missing file is the un-cycled state,
// not a failure.
func (s *fileModeStore) Load() (agentSortMode, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return agentSortPriority, nil
		}
		return agentSortPriority, fmt.Errorf("read agent sort state: %w", err)
	}
	return parseAgentSortMode(string(raw)), nil
}

func (s *fileModeStore) Save(mode agentSortMode) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create agent sort state directory: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(mode.String()+"\n"), 0o644); err != nil {
		return fmt.Errorf("write agent sort state: %w", err)
	}
	return nil
}
