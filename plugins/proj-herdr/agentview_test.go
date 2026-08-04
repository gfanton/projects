package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ---- Modes

func TestAgentSortModeRoundTrips(t *testing.T) {
	for _, mode := range []agentSortMode{agentSortPriority, agentSortRecent, agentSortAttention} {
		if got := parseAgentSortMode(mode.String()); got != mode {
			t.Errorf("parseAgentSortMode(%q) = %v, want %v", mode, got, mode)
		}
	}
}

// A state file written by a newer build, or corrupted, must not wedge the key.
func TestParseAgentSortModeFallsBackToPriority(t *testing.T) {
	for _, name := range []string{"nonsense", ""} {
		if got := parseAgentSortMode(name); got != agentSortPriority {
			t.Errorf("parseAgentSortMode(%q) = %v, want priority", name, got)
		}
	}
}

func TestNextAgentSortModeCycles(t *testing.T) {
	tests := []struct {
		from agentSortMode
		want agentSortMode
	}{
		{agentSortPriority, agentSortRecent},
		{agentSortRecent, agentSortAttention},
		{agentSortAttention, agentSortPriority},
	}

	for _, tt := range tests {
		if got := nextAgentSortMode(tt.from); got != tt.want {
			t.Errorf("nextAgentSortMode(%v) = %v, want %v", tt.from, got, tt.want)
		}
	}
}

// ---- Applying

type recordingCaller struct {
	method string
	params any
	err    error
}

func (c *recordingCaller) Call(_ context.Context, method string, params any) ([]byte, error) {
	c.method, c.params = method, params
	if c.err != nil {
		return nil, c.err
	}
	return []byte(`{"id":"x","result":{"type":"agent_view","active":true}}`), nil
}

// paramsJSON compares against the wire shape, which is what herdr validates,
// rather than against the Go struct.
func paramsJSON(t *testing.T, params any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshalling params: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshalling params: %v", err)
	}
	return out
}

func TestApply(t *testing.T) {
	tests := []struct {
		name       string
		mode       agentSortMode
		wantMethod string
		wantParams map[string]any
	}{
		{
			// priority is herdr's own config-level ordering, so it is expressed
			// by removing our view rather than by imitating it.
			name:       "priority clears the view",
			mode:       agentSortPriority,
			wantMethod: "agent.view.clear",
			wantParams: map[string]any{"source": agentViewSource},
		},
		{
			name:       "recent sorts by last state change descending",
			mode:       agentSortRecent,
			wantMethod: "agent.view.set",
			wantParams: map[string]any{
				"source": agentViewSource,
				"label":  "recent",
				"sort": []any{
					map[string]any{"field": "state_change_seq", "order": "desc"},
				},
			},
		},
		{
			// The ordering config cannot express: the built-in priority sort
			// tie-breaks by workspace order, this one by recency.
			name:       "attention sorts by attention then recency",
			mode:       agentSortAttention,
			wantMethod: "agent.view.set",
			wantParams: map[string]any{
				"source": agentViewSource,
				"label":  "attention",
				"sort": []any{
					map[string]any{"field": "attention", "order": "desc"},
					map[string]any{"field": "state_change_seq", "order": "desc"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caller := &recordingCaller{}
			service := NewAgentViewService(slog.New(slog.DiscardHandler), caller, nil)

			if err := service.Apply(context.Background(), tt.mode); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if caller.method != tt.wantMethod {
				t.Errorf("method = %q, want %q", caller.method, tt.wantMethod)
			}
			if got := paramsJSON(t, caller.params); !reflect.DeepEqual(got, tt.wantParams) {
				t.Errorf("params = %v, want %v", got, tt.wantParams)
			}
		})
	}
}

func TestApplyPropagatesCallFailure(t *testing.T) {
	caller := &recordingCaller{err: errors.New("socket down")}
	service := NewAgentViewService(slog.New(slog.DiscardHandler), caller, nil)

	err := service.Apply(context.Background(), agentSortRecent)
	if err == nil {
		t.Fatal("Apply() with a failing caller = nil error, want error")
	}
	if !strings.Contains(err.Error(), "socket down") {
		t.Errorf("error = %v, want it to carry the cause", err)
	}
}

// The source scopes the view so clearing removes ours and nobody else's; it has
// to match the plugin id in herdr-plugin.toml.
func TestAgentViewSourceMatchesPluginID(t *testing.T) {
	if agentViewSource != "gfanton.proj" {
		t.Errorf("agentViewSource = %q, want the plugin id %q", agentViewSource, "gfanton.proj")
	}
}

// ---- Cycling

func TestCycleAdvancesAppliesAndPersists(t *testing.T) {
	store := newFileModeStore(filepath.Join(t.TempDir(), "agent-sort"))
	caller := &recordingCaller{}
	service := NewAgentViewService(slog.New(slog.DiscardHandler), caller, store)

	// No state yet, so the cycle starts from priority.
	wants := []struct {
		mode   agentSortMode
		method string
	}{
		{agentSortRecent, "agent.view.set"},
		{agentSortAttention, "agent.view.set"},
		{agentSortPriority, "agent.view.clear"},
	}

	for i, want := range wants {
		got, err := service.Cycle(context.Background())
		if err != nil {
			t.Fatalf("Cycle() %d error = %v", i, err)
		}
		if got != want.mode {
			t.Errorf("Cycle() %d = %v, want %v", i, got, want.mode)
		}
		if caller.method != want.method {
			t.Errorf("Cycle() %d method = %q, want %q", i, caller.method, want.method)
		}
	}
}

// A failed apply must not advance the stored mode, or the next press would skip
// the ordering that never took effect.
func TestCycleDoesNotPersistWhenApplyFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-sort")
	service := NewAgentViewService(
		slog.New(slog.DiscardHandler),
		&recordingCaller{err: errors.New("socket down")},
		newFileModeStore(path),
	)

	if _, err := service.Cycle(context.Background()); err == nil {
		t.Fatal("Cycle() with a failing caller = nil error, want error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file exists after a failed apply (stat err = %v)", err)
	}
}

// ---- Mode store

func TestFileModeStoreRoundTrips(t *testing.T) {
	store := newFileModeStore(filepath.Join(t.TempDir(), "nested", "agent-sort"))

	if err := store.Save(agentSortAttention); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != agentSortAttention {
		t.Errorf("Load() = %v, want %v", got, agentSortAttention)
	}
}

func TestFileModeStoreLoadsPriorityWhenMissing(t *testing.T) {
	store := newFileModeStore(filepath.Join(t.TempDir(), "absent"))

	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() on a missing file error = %v, want nil", err)
	}
	if got != agentSortPriority {
		t.Errorf("Load() = %v, want priority", got)
	}
}

func TestModeStatePath(t *testing.T) {
	tests := []struct {
		name string
		env  herdrEnv
		want string
	}{
		{
			name: "XDG_STATE_HOME wins",
			env:  herdrEnv{Home: "/home/g", StateHome: "/xdg-state"},
			want: "/xdg-state/proj-herdr/agent-sort",
		},
		{
			name: "falls back under HOME",
			env:  herdrEnv{Home: "/home/g"},
			want: "/home/g/.local/state/proj-herdr/agent-sort",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := modeStatePath(tt.env)
			if err != nil {
				t.Fatalf("modeStatePath() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("modeStatePath() = %q, want %q", got, tt.want)
			}
		})
	}
}
