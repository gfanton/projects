package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfanton/projects/internal/project"
	"github.com/gfanton/projects/internal/query"
)

func testResults() []*query.Result {
	return []*query.Result{
		{Project: &project.Project{Organisation: "gfanton", Name: "nixpkgs", Path: "/root/gfanton/nixpkgs"}},
		{Project: &project.Project{Organisation: "gnolang", Name: "gno", Path: "/root/gnolang/gno"}},
		{Project: &project.Project{Organisation: "gfanton", Name: "shard", Path: "/root/gfanton/shard"}},
	}
}

// staticSearch ignores the query and always returns the same results, so cursor
// behaviour can be tested without the filesystem.
func staticSearch(results []*query.Result) searchFunc {
	return func(string) ([]*query.Result, error) { return results, nil }
}

func newTestPicker() pickerModel {
	results := testResults()
	return newPickerModel(staticSearch(results), results)
}

func sendKey(m pickerModel, k tea.KeyMsg) pickerModel {
	next, _ := m.Update(k)
	return next.(pickerModel)
}

func TestPickerCursorMovesAndClamps(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyMsg
		want int
	}{
		{"starts at first", nil, 0},
		{"down moves", []tea.KeyMsg{{Type: tea.KeyDown}}, 1},
		{"ctrl+n moves", []tea.KeyMsg{{Type: tea.KeyCtrlN}}, 1},
		{"up from first stays", []tea.KeyMsg{{Type: tea.KeyUp}}, 0},
		{"down past last clamps", []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyDown}, {Type: tea.KeyDown}}, 2},
		{"down then up returns", []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyUp}}, 0},
		{"ctrl+p moves back", []tea.KeyMsg{{Type: tea.KeyDown}, {Type: tea.KeyCtrlP}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestPicker()
			for _, k := range tt.keys {
				m = sendKey(m, k)
			}
			if m.cursor != tt.want {
				t.Errorf("cursor = %d, want %d", m.cursor, tt.want)
			}
		})
	}
}

func TestPickerEnterChoosesHighlighted(t *testing.T) {
	m := newTestPicker()
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.chosen == nil {
		t.Fatal("chosen = nil, want the highlighted result")
	}
	if got := m.chosen.Project.Name; got != "gno" {
		t.Errorf("chosen = %q, want %q", got, "gno")
	}
}

func TestPickerEscCancels(t *testing.T) {
	m := newTestPicker()
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.chosen != nil {
		t.Errorf("chosen = %v, want nil after cancel", m.chosen)
	}
	if !m.quitting {
		t.Error("quitting = false, want true after cancel")
	}
}

func TestPickerEnterOnEmptyResultsChoosesNothing(t *testing.T) {
	m := newPickerModel(staticSearch(nil), nil)
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.chosen != nil {
		t.Errorf("chosen = %v, want nil when there is nothing to choose", m.chosen)
	}
}

func TestPickerTypingResetsCursorToFirstMatch(t *testing.T) {
	m := newTestPicker()
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after the query changed", m.cursor)
	}
}

func manyResults(n int) []*query.Result {
	out := make([]*query.Result, 0, n)
	for i := range n {
		out = append(out, &query.Result{
			Project: &project.Project{Organisation: "org", Name: fmt.Sprintf("repo%02d", i)},
		})
	}
	return out
}

// The window has to follow the cursor: with more results than rows, scrolling
// past the bottom must move offset, or the highlighted row leaves the screen
// and the user selects something they cannot see.
func TestPickerWindowFollowsCursorPastTheBottom(t *testing.T) {
	results := manyResults(30)
	m := newPickerModel(staticSearch(results), results)

	for range visibleRows + 7 {
		m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}

	if m.cursor != visibleRows+7 {
		t.Fatalf("cursor = %d, want %d", m.cursor, visibleRows+7)
	}
	if want := m.cursor - visibleRows + 1; m.offset != want {
		t.Errorf("offset = %d, want %d", m.offset, want)
	}
	if m.cursor < m.offset || m.cursor >= m.offset+visibleRows {
		t.Errorf("cursor %d outside window [%d,%d)", m.cursor, m.offset, m.offset+visibleRows)
	}

	view := m.View()
	if !strings.Contains(view, "> org/repo19") {
		t.Errorf("View() does not show the highlighted row:\n%s", view)
	}
	if strings.Contains(view, "org/repo00") {
		t.Errorf("View() still shows the first row after scrolling:\n%s", view)
	}
}

// Scrolling back up has to pull the window with it.
func TestPickerWindowFollowsCursorBackUp(t *testing.T) {
	results := manyResults(30)
	m := newPickerModel(staticSearch(results), results)

	for range 20 {
		m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for range 18 {
		m = sendKey(m, tea.KeyMsg{Type: tea.KeyUp})
	}

	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want 2", m.cursor)
	}
	if m.offset > m.cursor {
		t.Errorf("offset %d is below cursor %d — window did not follow back up", m.offset, m.cursor)
	}
}

// A branch result must be distinguishable from its project in the list.
func TestResultLabel(t *testing.T) {
	proj := &project.Project{Organisation: "gfanton", Name: "nixpkgs"}

	if got := resultLabel(&query.Result{Project: proj}); got != "gfanton/nixpkgs" {
		t.Errorf("resultLabel(project) = %q, want %q", got, "gfanton/nixpkgs")
	}
	if got := resultLabel(&query.Result{Project: proj, Workspace: "feat/auth"}); got != "gfanton/nixpkgs@feat/auth" {
		t.Errorf("resultLabel(branch) = %q, want %q", got, "gfanton/nixpkgs@feat/auth")
	}
}

// A failed search keeps the last good ranking rather than blanking the list.
func TestPickerKeepsResultsWhenSearchFails(t *testing.T) {
	results := testResults()
	m := newPickerModel(func(string) ([]*query.Result, error) {
		return nil, errors.New("walk failed")
	}, results)

	m = sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})

	if m.err == nil {
		t.Error("err = nil, want the search failure recorded")
	}
	if len(m.results) != len(results) {
		t.Errorf("results = %d, want the previous %d kept", len(m.results), len(results))
	}
}
