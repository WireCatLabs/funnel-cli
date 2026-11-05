package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"tailscale.com/envknob"
	"tailscale.com/logtail"
)

var version = "0.0.0-dev"

const usage = `wirecat-funnel: publish local MCP servers through Tailscale Funnel, with no administrator rights.

  wirecat-funnel start [--hostname <name>] [--json]
  wirecat-funnel stop [--json]
  wirecat-funnel status [--json]
  wirecat-funnel route add <path> <port> [--json]
  wirecat-funnel route remove <path> [--json]
  wirecat-funnel route list [--json]
  wirecat-funnel run [--hostname <name>] [--ephemeral] [--verbose]
  wirecat-funnel --version

State: $WIRECAT_FUNNEL_STATE_DIR, else ~/.local/share/wirecat-funnel (%LOCALAPPDATA%\wirecat-funnel\Data on Windows).
Tailscale's log upload is always off.
`

const defaultHostname = "wirecat"

func main() {
	disableTailscaleLogs()
	os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr))
}

// NEED-76: tsnet builds its log uploader regardless; this knob makes its transport send nothing
// (logpolicy.TransportOptions.New), and Disable drops every entry before it is buffered.
func disableTailscaleLogs() {
	envknob.SetNoLogsNoSupport()
	logtail.Disable()
}

func cli(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	dir, err := stateDir(os.Getenv, runtime.GOOS)
	if err != nil {
		fmt.Fprintln(stderr, "wirecat-funnel:", err)
		return 1
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	hostname := fs.String("hostname", defaultHostname, "the node's name in the tailnet")
	ephemeral := fs.Bool("ephemeral", false, "in-memory state; the node leaves the tailnet when it stops")
	funnelOnly := fs.Bool("funnel-only", false, "answer only connections that arrive through Funnel")
	verbose := fs.Bool("verbose", false, "print Tailscale's own log lines to stderr")
	if err := fs.Parse(interleave(args[1:])); err != nil {
		return 2
	}
	rest := fs.Args()
	out := func(v any, line string) int {
		if *asJSON {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(v)
		} else {
			fmt.Fprintln(stdout, line)
		}
		return 0
	}
	failed := func(err error) int {
		if *asJSON {
			_ = json.NewEncoder(stdout).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(stderr, "wirecat-funnel:", err)
		}
		return 1
	}
	ctx := context.Background()

	switch args[0] {
	case "run":
		err := run(runOptions{dir: dir, hostname: *hostname, ephemeral: *ephemeral, funnelOnly: *funnelOnly, verbose: *verbose})
		if err != nil {
			return failed(err)
		}
		return 0
	case "status":
		st := currentStatus(ctx, dir)
		return out(st, describe(st))
	case "start":
		st, err := start(ctx, dir, *hostname)
		if err != nil {
			return failed(err)
		}
		return out(st, describe(st))
	case "stop":
		st, err := stop(ctx, dir)
		if err != nil {
			return failed(err)
		}
		return out(st, describe(st))
	case "route":
		routes, err := route(dir, rest)
		if err != nil {
			return failed(err)
		}
		lines := ""
		for _, r := range routes {
			lines += fmt.Sprintf("%s -> 127.0.0.1:%d\n", r.Path, r.Port)
		}
		if lines == "" {
			lines = "no routes\n"
		}
		return out(map[string][]Route{"routes": routes}, lines[:len(lines)-1])
	}
	fmt.Fprint(stderr, usage)
	return 2
}

// interleave lets flags come after positional arguments (`route add /tg 8765 --json`), which flag stops at.
func interleave(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case len(a) > 1 && a[0] == '-':
			flags = append(flags, a)
			if (a == "--hostname" || a == "-hostname") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		default:
			positional = append(positional, a)
		}
	}
	return append(append(flags, "--"), positional...)
}

func route(dir string, args []string) ([]Route, error) {
	file := filepath.Join(dir, "routes.json")
	if len(args) == 0 {
		return nil, errors.New("route add <path> <port> | route remove <path> | route list")
	}
	if args[0] != "list" {
		if err := ensureDir(dir); err != nil {
			return nil, err
		}
	}
	switch {
	case args[0] == "list" && len(args) == 1:
		return readRoutes(file)
	case args[0] == "add" && len(args) == 3:
		port, err := strconv.Atoi(args[2])
		if err != nil {
			return nil, fmt.Errorf("port %q is not a number", args[2])
		}
		return addRoute(file, Route{Path: args[1], Port: port})
	case args[0] == "remove" && len(args) == 2:
		return removeRoute(file, args[1])
	}
	return nil, errors.New("route add <path> <port> | route remove <path> | route list")
}

func currentStatus(ctx context.Context, dir string) Status {
	if st, err := liveStatus(ctx, dir); err == nil {
		return st
	}
	routes, err := readRoutes(filepath.Join(dir, "routes.json"))
	if err != nil {
		routes = []Route{}
	}
	return Status{State: stateStopped, Routes: routes, Dir: dir, Version: version}
}

func start(ctx context.Context, dir, hostname string) (Status, error) {
	if st, err := liveStatus(ctx, dir); err == nil {
		return st, nil
	}
	if err := ensureDir(dir); err != nil {
		return Status{}, err
	}
	self, err := os.Executable()
	if err != nil {
		return Status{}, err
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "run.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Status{}, err
	}
	defer logFile.Close()
	cmd := exec.Command(self, "run", "--hostname", hostname)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return Status{}, err
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return Status{}, fmt.Errorf("the helper stopped at once; see %s", filepath.Join(dir, "run.log"))
		case <-time.After(250 * time.Millisecond):
		}
		if st, err := liveStatus(ctx, dir); err == nil && st.State != stateStarting {
			return st, nil
		}
	}
	return currentStatus(ctx, dir), nil
}

func stop(ctx context.Context, dir string) (Status, error) {
	resp, err := callControl(ctx, dir, http.MethodPost, "/stop")
	if errors.Is(err, errNotRunning) {
		return currentStatus(ctx, dir), nil
	}
	if err != nil {
		return Status{}, err
	}
	resp.Body.Close()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := liveStatus(ctx, dir); err != nil {
			return currentStatus(ctx, dir), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return Status{}, errors.New("the helper did not stop within 15 s")
}

func describe(st Status) string {
	switch st.State {
	case stateRunning:
		return "running: " + val(st.URL)
	case stateNeedsLogin:
		return "sign in to Tailscale: " + val(st.ActionURL)
	case stateNeedsApproval:
		return "approve this device in the Tailscale admin console: " + val(st.ActionURL)
	case stateNeedsFunnel:
		return "allow Funnel for this device: " + val(st.ActionURL)
	case stateError:
		return "error: " + val(st.Error)
	}
	return st.State
}
