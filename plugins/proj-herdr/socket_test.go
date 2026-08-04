package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSocketPath(t *testing.T) {
	tests := []struct {
		name string
		env  herdrEnv
		want string
	}{
		{
			name: "default session under HOME",
			env:  herdrEnv{Home: "/home/g"},
			want: "/home/g/.config/herdr/herdr.sock",
		},
		{
			name: "XDG_CONFIG_HOME wins over HOME",
			env:  herdrEnv{Home: "/home/g", ConfigHome: "/xdg"},
			want: "/xdg/herdr/herdr.sock",
		},
		{
			name: "HERDR_SOCKET_PATH wins over the config dir",
			env:  herdrEnv{Home: "/home/g", SocketPath: "/run/custom.sock"},
			want: "/run/custom.sock",
		},
		{
			// herdr resolves an explicit session before consulting the env
			// override, so a named session is never answered with the default
			// session's socket.
			name: "explicit session wins over HERDR_SOCKET_PATH",
			env:  herdrEnv{Home: "/home/g", SocketPath: "/run/custom.sock", Session: "work"},
			want: "/home/g/.config/herdr/sessions/work/herdr.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := socketPath(tt.env)
			if err != nil {
				t.Fatalf("socketPath() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("socketPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSocketPathWithoutHomeFails(t *testing.T) {
	if _, err := socketPath(herdrEnv{}); err == nil {
		t.Error("socketPath() with no HOME and no XDG_CONFIG_HOME = nil error, want error")
	}
}

// fakeAPIServer accepts one connection, records the request and replies with
// reply. It stands in for the herdr server's unix socket.
func fakeAPIServer(t *testing.T, reply string) (path string, requests chan []byte) {
	t.Helper()

	// macOS caps unix socket paths near 104 bytes and t.TempDir() spends most of
	// that budget on the test name, so the directory is made directly.
	dir, err := os.MkdirTemp("", "p")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path = filepath.Join(dir, "s")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	requests = make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		requests <- buf[:n]
		_, _ = conn.Write([]byte(reply + "\n"))
	}()
	return path, requests
}

func TestAPIClientCallSendsRequestAndDecodesReply(t *testing.T) {
	path, requests := fakeAPIServer(t, `{"id":"x","result":{"type":"agent_view","active":true}}`)

	client := newAPIClient(slog.New(slog.DiscardHandler), path)
	out, err := client.Call(context.Background(), "agent.view.set", map[string]string{"source": "s"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	var sent struct {
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
		ID     string            `json:"id"`
	}
	if err := json.Unmarshal(<-requests, &sent); err != nil {
		t.Fatalf("decoding what we sent: %v", err)
	}
	if sent.Method != "agent.view.set" {
		t.Errorf("method = %q, want %q", sent.Method, "agent.view.set")
	}
	if want := map[string]string{"source": "s"}; !reflect.DeepEqual(sent.Params, want) {
		t.Errorf("params = %v, want %v", sent.Params, want)
	}
	if sent.ID == "" {
		t.Error("id is empty; herdr correlates replies by id")
	}

	var got struct {
		Result struct {
			Active bool `json:"active"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decoding the reply: %v", err)
	}
	if !got.Result.Active {
		t.Error("reply was not returned to the caller intact")
	}
}

// A herdr error arrives in the same envelope as a success and merely omits
// `result`, so the client has to reject it rather than hand back a zero value.
func TestAPIClientCallRejectsErrorEnvelope(t *testing.T) {
	path, _ := fakeAPIServer(t, `{"id":"x","error":{"code":"bad_request","message":"nope"}}`)

	client := newAPIClient(slog.New(slog.DiscardHandler), path)
	_, err := client.Call(context.Background(), "agent.view.set", nil)
	if err == nil {
		t.Fatal("Call() on an error envelope = nil error, want error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to carry herdr's message", err)
	}
}

func TestAPIClientCallFailsWhenSocketIsAbsent(t *testing.T) {
	client := newAPIClient(slog.New(slog.DiscardHandler), filepath.Join(t.TempDir(), "missing"))
	if _, err := client.Call(context.Background(), "agent.view.set", nil); err == nil {
		t.Error("Call() with no socket = nil error, want error")
	}
}
