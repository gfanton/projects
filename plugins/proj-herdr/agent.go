package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/peterbourgon/ff/v4"
)

// newAgentCommand exposes the agents-panel helpers.
func newAgentCommand(logger *slog.Logger) *ff.Command {
	return &ff.Command{
		Name:      "agent",
		Usage:     "proj-herdr agent <subcommand>",
		ShortHelp: "Control the herdr agents panel",
		Flags:     ff.NewFlagSet("proj-herdr agent"),
		Exec: func(ctx context.Context, args []string) error {
			return ff.ErrHelp
		},
		Subcommands: []*ff.Command{
			newAgentSortCommand(logger),
		},
	}
}

func newAgentSortCommand(logger *slog.Logger) *ff.Command {
	var mode string
	fs := ff.NewFlagSet("proj-herdr agent sort")
	fs.StringVar(&mode, 0, "mode", "", "apply an ordering directly instead of cycling: priority, recent or attention")

	return &ff.Command{
		Name:      "sort",
		Usage:     "proj-herdr agent sort [--mode <ordering>]",
		ShortHelp: "Cycle how the herdr agents panel is ordered",
		LongHelp: `Step the herdr agents panel through three orderings.

  priority   herdr's own ui.agent_panel_sort, restored by clearing our view
  recent     most recently active first
  attention  wants-you-first, most recently active within each group

Only the first is expressible in config.toml. The other two are held in the
running herdr server rather than on disk, so they do not survive a server
restart; pressing the key again reinstalls one.

The panel header shows which ordering is active.`,
		Flags: fs,
		Exec: func(ctx context.Context, args []string) error {
			socket, err := socketPath(herdrEnvFromOS())
			if err != nil {
				return err
			}

			statePath, err := modeStatePath(herdrEnvFromOS())
			if err != nil {
				return err
			}

			service := NewAgentViewService(
				logger,
				newAPIClient(logger, socket),
				newFileModeStore(statePath),
			)

			if mode != "" {
				applied := parseAgentSortMode(mode)
				if err := service.Apply(ctx, applied); err != nil {
					return err
				}
				fmt.Println(applied)
				return nil
			}

			applied, err := service.Cycle(ctx)
			if err != nil {
				return err
			}
			fmt.Println(applied)
			return nil
		},
	}
}
