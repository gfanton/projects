package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"
)

// apiTimeout bounds a single socket round trip. herdr answers from memory, so
// anything slower means the server is wedged rather than busy.
const apiTimeout = 3 * time.Second

// herdrEnv is the part of the environment that decides which herdr server to
// talk to. It is a struct rather than direct os.Getenv calls so the resolution
// rules can be exercised without mutating process state.
type herdrEnv struct {
	ConfigHome string // XDG_CONFIG_HOME
	StateHome  string // XDG_STATE_HOME
	Home       string // HOME
	SocketPath string // HERDR_SOCKET_PATH
	Session    string // HERDR_SESSION
}

func herdrEnvFromOS() herdrEnv {
	return herdrEnv{
		ConfigHome: os.Getenv("XDG_CONFIG_HOME"),
		StateHome:  os.Getenv("XDG_STATE_HOME"),
		Home:       os.Getenv("HOME"),
		SocketPath: os.Getenv("HERDR_SOCKET_PATH"),
		Session:    os.Getenv("HERDR_SESSION"),
	}
}

// socketPath mirrors herdr's own resolution (session.rs): a named session is
// answered from its own directory before the environment override is consulted,
// so asking for one never yields the default session's socket.
//
// Plugin commands are spawned by the server, which does not export
// HERDR_SOCKET_PATH, so the config-dir fallback is the path that actually runs
// in production — the override only applies when a pane invokes us directly.
func socketPath(env herdrEnv) (string, error) {
	if env.Session != "" {
		dir, err := configDir(env)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "sessions", env.Session, "herdr.sock"), nil
	}

	if env.SocketPath != "" {
		return env.SocketPath, nil
	}

	dir, err := configDir(env)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "herdr.sock"), nil
}

func configDir(env herdrEnv) (string, error) {
	if env.ConfigHome != "" {
		return filepath.Join(env.ConfigHome, "herdr"), nil
	}
	if env.Home != "" {
		return filepath.Join(env.Home, ".config", "herdr"), nil
	}
	return "", errors.New("cannot locate the herdr config directory: neither XDG_CONFIG_HOME nor HOME is set")
}

// ---- Socket API

// apiClient speaks herdr's newline-delimited JSON socket protocol. It exists
// alongside the CLI runner because a handful of methods — agent.view.set among
// them — have no CLI surface.
type apiClient struct {
	logger *slog.Logger
	path   string
}

func newAPIClient(logger *slog.Logger, path string) *apiClient {
	return &apiClient{logger: logger, path: path}
}

// Call sends one request and returns the raw reply line. herdr answers every
// request with a single JSON object terminated by a newline.
func (c *apiClient) Call(ctx context.Context, method string, params any) ([]byte, error) {
	request := map[string]any{
		"id":     "gfanton.proj:" + method,
		"method": method,
	}
	if params != nil {
		request["params"] = params
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", method, err)
	}

	c.logger.Debug("calling herdr api", "socket", c.path, "method", method)

	dialer := net.Dialer{Timeout: apiTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("connect to herdr at %s: %w", c.path, err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		err = conn.SetDeadline(deadline)
	} else {
		err = conn.SetDeadline(time.Now().Add(apiTimeout))
	}
	if err != nil {
		return nil, fmt.Errorf("set deadline for %s: %w", method, err)
	}

	if _, err := conn.Write(append(payload, '\n')); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	// The reply is one line; bufio stops at the newline rather than waiting for
	// the server to close a connection it keeps open.
	out, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read %s reply: %w", method, err)
	}

	// Shares the error-envelope check with the CLI path: a failure arrives in
	// the same shape as a success and merely omits `result`.
	var discard json.RawMessage
	if err := decodeReply(out, &discard); err != nil {
		return nil, err
	}
	return out, nil
}
