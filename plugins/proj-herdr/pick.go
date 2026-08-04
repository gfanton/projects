package main

import (
	"context"
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gfanton/projects/internal/query"
	"github.com/gfanton/projects/internal/workspace"
	"github.com/peterbourgon/ff/v4"
)

// pickLimit bounds the ranked list. The popup shows a dozen rows, and typing
// narrows faster than scrolling does.
const pickLimit = 50

func newWorkspacePickCommand(logger *slog.Logger, rootDir string) *ff.Command {
	return &ff.Command{
		Name:      "pick",
		Usage:     "proj-herdr workspace pick",
		ShortHelp: "Choose a project and open its herdr workspace",
		LongHelp: `Open an interactive picker over the local projects.

Selecting a project focuses its workspace, creating it when absent. Selecting a
branch focuses that project's workspace and opens a tab on the branch checkout.

Intended to run as a herdr popup pane, which supplies the modal surface.`,
		Exec: func(ctx context.Context, args []string) error {
			return runWorkspacePick(ctx, logger, rootDir)
		},
	}
}

func runWorkspacePick(ctx context.Context, logger *slog.Logger, rootDir string) error {
	querySvc := query.NewService(logger, rootDir)

	search := func(q string) ([]*query.Result, error) {
		return querySvc.Search(ctx, query.Options{Query: q, Limit: pickLimit})
	}

	initial, err := search("")
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	// No alt screen: herdr's popup is already a modal surface that restores the
	// pane underneath when the command exits.
	final, err := tea.NewProgram(newPickerModel(search, initial), tea.WithContext(ctx)).Run()
	if err != nil {
		return fmt.Errorf("run picker: %w", err)
	}

	model, ok := final.(pickerModel)
	if !ok {
		return fmt.Errorf("picker returned %T", final)
	}
	if model.chosen == nil {
		logger.Debug("picker cancelled")
		return nil
	}

	return openTarget(ctx, logger, rootDir, model.chosen)
}

// openTarget realises a chosen result in herdr: the project's workspace, plus a
// tab when the result names a branch.
func openTarget(ctx context.Context, logger *slog.Logger, rootDir string, result *query.Result) error {
	svc := NewHerdrService(logger, newExecRunnerFromEnv(logger))
	t := targetOf(result, workspace.NewService(logger, rootDir), scanNamespaces(logger, rootDir))

	ws, err := svc.EnsureWorkspace(ctx, t.WorkspaceLabel, t.WorkspaceDir, true)
	if err != nil {
		return err
	}

	if t.TabLabel == "" {
		logger.Debug("workspace open", "workspace_id", ws.ID, "label", ws.Label)
		return nil
	}

	tab, err := svc.EnsureTab(ctx, ws.ID, t.TabLabel, t.TabDir)
	if err != nil {
		return err
	}

	logger.Debug("workspace open", "workspace_id", ws.ID, "tab_id", tab.ID, "branch", t.TabLabel)
	return nil
}
