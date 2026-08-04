package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gfanton/projects"
	"github.com/peterbourgon/ff/v4"
)

func newWorkspaceCommand(logger *slog.Logger, projectsCfg *projects.Config, projectsLogger projects.Logger) *ff.Command {
	return &ff.Command{
		Name:      "workspace",
		Usage:     "proj-herdr workspace <subcommand>",
		ShortHelp: "Manage herdr workspaces for projects",
		LongHelp: `Manage herdr workspaces for projects.

Commands:
  pick              Choose a project interactively and open it
  open <project>    Focus the project's workspace, creating it when absent
  list              List proj-managed workspaces`,
		Subcommands: []*ff.Command{
			newWorkspacePickCommand(logger, projectsCfg.RootDir),
			newWorkspaceOpenCommand(logger, projectsCfg, projectsLogger),
			newWorkspaceListCommand(logger),
		},
		Exec: func(ctx context.Context, args []string) error {
			return ff.ErrHelp
		},
	}
}

func newWorkspaceOpenCommand(logger *slog.Logger, projectsCfg *projects.Config, projectsLogger projects.Logger) *ff.Command {
	create := true
	fs := ff.NewFlagSet("workspace open")
	fs.BoolVar(&create, 0, "create", "create the workspace when it does not exist")

	return &ff.Command{
		Name:      "open",
		Usage:     "proj-herdr workspace open [flags] <project>",
		ShortHelp: "Focus a project's herdr workspace",
		LongHelp: `Focus the herdr workspace for the specified project.

An open workspace carrying the project's label is focused rather than
duplicated. Pass --create=false to refuse opening a workspace that does not
already exist.`,
		Flags: fs,
		Exec: func(ctx context.Context, args []string) error {
			if len(args) < 1 {
				return errors.New("project name is required")
			}
			return runWorkspaceOpen(ctx, logger, projectsCfg, projectsLogger, args[0], create)
		},
	}
}

func newWorkspaceListCommand(logger *slog.Logger) *ff.Command {
	return &ff.Command{
		Name:      "list",
		Usage:     "proj-herdr workspace list",
		ShortHelp: "List proj-managed herdr workspaces",
		LongHelp:  `List the herdr workspaces that proj-herdr opened, as org/name.`,
		Exec: func(ctx context.Context, args []string) error {
			return runWorkspaceList(ctx, logger)
		},
	}
}

func runWorkspaceOpen(ctx context.Context, logger *slog.Logger, projectsCfg *projects.Config, projectsLogger projects.Logger, projectName string, create bool) error {
	projectSvc := projects.NewProjectService(projectsCfg, projectsLogger)

	project, err := projectSvc.ParseProject(projectName)
	if err != nil {
		return fmt.Errorf("invalid project name: %w", err)
	}

	svc := NewHerdrService(logger, newExecRunnerFromEnv(logger))
	ns := scanNamespaces(logger, projectsCfg.RootDir)
	label := workspaceLabel(ns.short(project.Organisation), project.Name)

	ws, err := svc.EnsureWorkspace(ctx, label, project.Path, create)
	if err != nil {
		return err
	}

	logger.Info("workspace open", "project", project.String(), "workspace_id", ws.ID, "label", ws.Label)
	return nil
}

func runWorkspaceList(ctx context.Context, logger *slog.Logger) error {
	svc := NewHerdrService(logger, newExecRunnerFromEnv(logger))

	list, err := svc.workspaces(ctx)
	if err != nil {
		return err
	}

	for _, ws := range list {
		if name := projectFromWorkspaceLabel(ws.Label); name != "" {
			fmt.Println(name)
		}
	}
	return nil
}
