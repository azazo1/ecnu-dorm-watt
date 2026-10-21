package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/azazo1/ecnu-dorm-watt/internal/client/upstream"
	"github.com/azazo1/ecnu-dorm-watt/internal/config"
	"github.com/azazo1/ecnu-dorm-watt/internal/protocol"
)

func newTestApp(stdin string) (*App, *bytes.Buffer, *bytes.Buffer) {
	app := NewApp()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	app.stdin = strings.NewReader(stdin)
	app.stdout = out
	app.stderr = errOut
	return app, out, errOut
}

func TestConfigSetTokenWrites0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.toml")
	app, out, errOut := newTestApp("dw_1234567890abcdef\n")

	code := app.Execute([]string{"--config", path, "client", "config", "set-token"})
	if code != ExitOK {
		t.Fatalf("exit code = %d, stderr = %s", code, errOut.String())
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("permission = %04o, want 0600", perm)
	}
	cfg, _, err := config.LoadClient(path, false)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Token != "dw_1234567890abcdef" {
		t.Fatalf("token = %q", cfg.Server.Token)
	}
	if !strings.Contains(out.String(), "0600") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestConfigShowJSONHidesToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.toml")
	app, _, errOut := newTestApp("dw_1234567890abcdef\n")
	if code := app.Execute([]string{"--config", path, "client", "config", "set-token"}); code != ExitOK {
		t.Fatalf("set-token exit = %d: %s", code, errOut.String())
	}

	app2, out2, errOut2 := newTestApp("")
	code := app2.Execute([]string{"--config", path, "--json", "client", "config", "show"})
	if code != ExitOK {
		t.Fatalf("show exit = %d: %s", code, errOut2.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out2.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v, raw = %s", err, out2.String())
	}
	if payload["token_set"] != true {
		t.Fatalf("token_set = %v", payload["token_set"])
	}
	if payload["token_prefix"] != "dw_1234" {
		t.Fatalf("token_prefix = %v", payload["token_prefix"])
	}
	if strings.Contains(out2.String(), "1234567890abcdef") {
		t.Fatalf("完整 token 出现在输出中: %s", out2.String())
	}
}

func TestInvalidTokenRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.toml")
	app, _, _ := newTestApp("bad_token\n")
	if code := app.Execute([]string{"--config", path, "client", "config", "set-token"}); code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestQuitWithoutDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	app, _, _ := newTestApp("")
	if code := app.Execute([]string{"client", "quit"}); code != ExitDaemon {
		t.Fatalf("exit = %d, want %d", code, ExitDaemon)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	app, _, _ := newTestApp("")
	if code := app.Execute([]string{"client", "status", "--not-a-flag"}); code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"general", errors.New("boom"), ExitGeneral},
		{"usage", usageErrorf("bad args"), ExitUsage},
		{"auth", &upstream.APIError{Status: 401}, ExitAuth},
		{"forbidden", &upstream.APIError{Status: 403}, ExitAuth},
		{"network", &upstream.TransportError{Op: "GET", Err: errors.New("refused")}, ExitNetwork},
		{"room", &upstream.APIError{Status: 409, Problem: protocolProblem("room_not_configured")}, ExitNotConfigured},
		{"session", &upstream.APIError{Status: 502, Problem: protocolProblem("ecnu_not_logged_in")}, ExitNotConfigured},
		{"ecnu", &upstream.APIError{Status: 502, Problem: protocolProblem("ecnu_upstream_error")}, ExitNetwork},
		{"no_degree", &upstream.APIError{Status: 502, Problem: protocolProblem("ecnu_no_degree")}, ExitNetwork},
		{"server", &upstream.APIError{Status: 500, Problem: protocolProblem("storage_error")}, ExitServer},
		{"daemon", exitErr(ExitDaemon, errors.New("not running")), ExitDaemon},
		{"canceled", context.Canceled, ExitInterrupted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyError(tc.err); got != tc.want {
				t.Fatalf("classifyError = %d, want %d", got, tc.want)
			}
		})
	}
}

func protocolProblem(code string) protocol.Problem {
	return protocol.Problem{Type: "about:blank", Status: 409, Code: code}
}

func TestVersionJSON(t *testing.T) {
	app, out, _ := newTestApp("")
	if code := app.Execute([]string{"--json", "version"}); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var payload map[string]string
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload["version"] == "" {
		t.Fatalf("missing version: %s", out.String())
	}
}
