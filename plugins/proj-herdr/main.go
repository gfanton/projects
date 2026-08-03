package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/gfanton/projects"
	"github.com/gfanton/projects/internal/config"
	"github.com/peterbourgon/ff/v4"
	"github.com/peterbourgon/ff/v4/ffhelp"
)

// ---- Version Variables (injected at build time)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "unknown"
)

// newExecRunnerFromEnv builds a runner from the variables herdr exports into
// plugin actions and custom commands, falling back to the binary on PATH.
func newExecRunnerFromEnv(logger *slog.Logger) *execRunner {
	bin := cmp.Or(os.Getenv("HERDR_BIN_PATH"), "herdr")
	return newExecRunner(logger, bin, os.Getenv("HERDR_SESSION"))
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.NewConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to create config: %v\n", err)
		os.Exit(1)
	}

	if err := cfg.Load(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := cfg.Logger()

	projectsCfg := &projects.Config{
		ConfigFile: cfg.ConfigFile,
		Debug:      cfg.Debug,
		RootDir:    cfg.RootDir,
		RootUser:   cfg.RootUser,
	}
	projectsLogger := projects.NewSlogAdapter(logger)

	rootFlags := ff.NewFlagSet("proj-herdr")
	rootFlags.BoolVar(&cfg.Debug, 0, "debug", "enable debug logging")
	rootFlags.StringVar(&cfg.RootDir, 0, "root", cfg.RootDir, "root directory for projects")
	rootFlags.StringVar(&cfg.RootUser, 0, "user", cfg.RootUser, "default user for projects")
	rootFlags.StringVar(&cfg.ConfigFile, 0, "config", cfg.ConfigFile, "configuration file path")

	root := &ff.Command{
		Name:      "proj-herdr",
		Usage:     "proj-herdr [flags] <subcommand>",
		ShortHelp: "Herdr integration for proj - workspace and tab management",
		LongHelp: `proj-herdr provides herdr workspace and tab management for proj.

This binary is designed to be called from herdr keybindings and plugin actions.
It maps projects onto herdr workspaces, reusing an open workspace when one
already carries the project's label.

Use 'proj-herdr <subcommand> -h' for more information about a specific command.`,
		Flags: rootFlags,
		Exec: func(ctx context.Context, args []string) error {
			return ff.ErrHelp
		},
		Subcommands: []*ff.Command{
			newWorkspaceCommand(logger, projectsCfg, projectsLogger),
			newVersionCommand(),
		},
	}

	if err := root.ParseAndRun(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, ff.ErrHelp) {
			fmt.Fprint(os.Stdout, ffhelp.Command(root))
			os.Exit(0)
		}
		logger.Error("command failed", "error", err)
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newVersionCommand() *ff.Command {
	var verbose bool
	fs := ff.NewFlagSet("proj-herdr version")
	fs.BoolVar(&verbose, 'v', "verbose", "show verbose version information")

	return &ff.Command{
		Name:      "version",
		Usage:     "proj-herdr version [-v]",
		ShortHelp: "Show version information",
		Flags:     fs,
		Exec: func(ctx context.Context, args []string) error {
			if verbose {
				fmt.Printf("proj-herdr version %s\n", version)
				fmt.Printf("  commit: %s\n", commit)
				fmt.Printf("  built at: %s\n", date)
				fmt.Printf("  built by: %s\n", builtBy)
				fmt.Printf("  go version: %s\n", runtime.Version())
				fmt.Printf("  platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			} else {
				fmt.Println(version)
			}
			return nil
		},
	}
}
