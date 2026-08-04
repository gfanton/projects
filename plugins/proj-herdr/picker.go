package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfanton/projects/internal/query"
)

// searchFunc ranks projects for a query. Matching stays in the project's own
// query engine; the picker only drives it and renders what comes back.
type searchFunc func(q string) ([]*query.Result, error)

// visibleRows caps the rendered list. herdr popups are short, and the manifest
// asks for a 20 row popup, which leaves room for the prompt and a footer.
const visibleRows = 12

type pickerModel struct {
	input    textinput.Model
	search   searchFunc
	results  []*query.Result
	cursor   int
	offset   int
	chosen   *query.Result
	quitting bool
	err      error
}

func newPickerModel(search searchFunc, initial []*query.Result) pickerModel {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "project or project/branch"
	in.Focus()

	return pickerModel{input: in, search: search, results: initial}
}

func (m pickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.quitting = true
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.results) > 0 {
				m.chosen = m.results[m.cursor]
			}
			m.quitting = true
			return m, tea.Quit

		case tea.KeyUp, tea.KeyCtrlP:
			m.moveCursor(-1)
			return m, nil

		case tea.KeyDown, tea.KeyCtrlN:
			m.moveCursor(1)
			return m, nil
		}
	}

	// Everything else goes to the text input, including the cursor's own blink
	// ticks: swallowing those stops the blink after the first one. A message
	// that changes the query recomputes the ranking and returns the highlight
	// to the best match.
	before := m.input.Value()

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	if m.input.Value() != before {
		results, err := m.search(m.input.Value())
		if err != nil {
			m.err = err
		} else {
			m.err = nil
			m.results = results
		}
		m.cursor = 0
		m.offset = 0
	}
	return m, cmd
}

// moveCursor clamps to the result bounds and keeps the highlight inside the
// rendered window.
func (m *pickerModel) moveCursor(delta int) {
	if len(m.results) == 0 {
		m.cursor = 0
		return
	}

	m.cursor = min(max(m.cursor+delta, 0), len(m.results)-1)

	switch {
	case m.cursor < m.offset:
		m.offset = m.cursor
	case m.cursor >= m.offset+visibleRows:
		m.offset = m.cursor - visibleRows + 1
	}
}

func (m pickerModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n\n")

	// The failed search keeps the previous ranking, so the list is still drawn
	// under the message: hiding it would leave Enter selecting a row the user
	// can no longer see.
	if m.err != nil {
		fmt.Fprintf(&b, "  search failed: %v\n", m.err)
	}

	if len(m.results) == 0 {
		b.WriteString("  no matching project\n")
		return b.String()
	}

	end := min(m.offset+visibleRows, len(m.results))
	for i := m.offset; i < end; i++ {
		marker := "  "
		if i == m.cursor {
			marker = "> "
		}
		b.WriteString(marker + resultLabel(m.results[i]) + "\n")
	}

	if len(m.results) > visibleRows {
		fmt.Fprintf(&b, "\n  %d/%d\n", m.cursor+1, len(m.results))
	}
	return b.String()
}

// resultLabel renders a result the way the query engine names it: org/name for
// a project, org/name@branch for one of its branch checkouts.
func resultLabel(r *query.Result) string {
	name := r.Project.Organisation + "/" + r.Project.Name
	if r.Workspace != "" {
		return name + "@" + r.Workspace
	}
	return name
}
