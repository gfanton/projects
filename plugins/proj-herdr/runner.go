package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
)

// herdrArgs prepends the global session selector, which herdr expects before
// the subcommand. An empty session targets the default session.
func herdrArgs(session string, args []string) []string {
	if session == "" {
		return args
	}

	out := make([]string, 0, len(args)+2)
	out = append(out, "--session", session)
	return append(out, args...)
}

// execRunner runs the herdr binary. herdr writes its JSON reply to stdout and
// diagnostics to stderr, so only stdout is returned.
type execRunner struct {
	logger  *slog.Logger
	bin     string
	session string
}

func newExecRunner(logger *slog.Logger, bin, session string) *execRunner {
	return &execRunner{logger: logger, bin: bin, session: session}
}

func (r *execRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	full := herdrArgs(r.session, args)
	r.logger.Debug("running herdr", "bin", r.bin, "args", full)

	out, err := exec.CommandContext(ctx, r.bin, full...).Output()
	if err != nil {
		// Surface herdr's own message; Output() captures stderr on ExitError.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("herdr %v: %w: %s", full, err, bytes.TrimSpace(exitErr.Stderr))
		}
		return nil, fmt.Errorf("herdr %v: %w", full, err)
	}
	return out, nil
}
