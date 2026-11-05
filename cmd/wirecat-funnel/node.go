package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/store/mem"
	"tailscale.com/tsnet"
)

type runOptions struct {
	dir        string
	hostname   string
	ephemeral  bool
	funnelOnly bool
	verbose    bool
}

type liveState struct {
	mu     sync.Mutex
	status Status
	table  *routeTable
}

func (s *liveState) get() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Routes = s.table.current()
	return st
}

func (s *liveState) set(state string, edit func(*Status)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.State = state
	s.status.ActionURL, s.status.Error = nil, nil
	if edit != nil {
		edit(&s.status)
	}
}

const loggingRequired = "This tailnet requires Tailscale logging, which WireCat keeps off."

func run(opts runOptions) error {
	if err := ensureDir(opts.dir); err != nil {
		return err
	}
	if st, err := liveStatus(context.Background(), opts.dir); err == nil {
		return fmt.Errorf("already running (pid %d, %s)", val(st.PID), st.State)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	live := &liveState{
		table:  &routeTable{file: filepath.Join(opts.dir, "routes.json")},
		status: Status{State: stateStarting, PID: ptr(os.Getpid()), Dir: opts.dir, Version: version},
	}
	closeControl, err := serveControl(opts.dir, live.get, cancel)
	if err != nil {
		return err
	}
	defer closeControl()

	backendLog := func(string, ...any) {}
	if opts.verbose {
		backendLog = log.Printf
	}
	srv := &tsnet.Server{
		Dir:       opts.dir,
		Hostname:  opts.hostname,
		Ephemeral: opts.ephemeral,
		Logf:      backendLog,
		UserLogf:  func(string, ...any) {},
	}
	if opts.ephemeral {
		srv.Store = new(mem.Store)
	}
	defer srv.Close()

	if err := srv.Start(); err != nil {
		return fail(live, err)
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return fail(live, err)
	}
	upCtx, stopWatch := context.WithCancel(ctx)
	go watchLogin(upCtx, srv, live)
	status, err := srv.Up(upCtx)
	stopWatch()
	if ctx.Err() != nil {
		return shutdown(srv, opts)
	}
	if err != nil {
		return fail(live, err)
	}
	if len(status.CertDomains) == 0 {
		return fail(live, errors.New("HTTPS is off in this tailnet. Turn it on at https://login.tailscale.com/admin/dns"))
	}
	publicURL := "https://" + status.CertDomains[0]

	var funnelOpts []tsnet.FunnelOption
	if opts.funnelOnly {
		funnelOpts = append(funnelOpts, tsnet.FunnelOnly())
	}
	var ln net.Listener
	for {
		ln, err = srv.ListenFunnel("tcp", ":443", funnelOpts...)
		if err == nil {
			break
		}
		answer, qerr := lc.QueryFeature(ctx, "funnel")
		if qerr != nil || answer.URL == "" {
			live.set(stateError, func(s *Status) { s.Error = ptr(err.Error()) })
		} else {
			live.set(stateNeedsFunnel, func(s *Status) { s.ActionURL = ptr(answer.URL) })
		}
		select {
		case <-ctx.Done():
			return shutdown(srv, opts)
		case <-time.After(5 * time.Second):
		}
	}
	live.set(stateRunning, func(s *Status) { s.URL = ptr(publicURL) })

	web := &http.Server{
		Handler:           newProxy(live.table.current),
		ReadHeaderTimeout: 30 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = web.Serve(ln) }()
	<-ctx.Done()
	shutdownCtx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	_ = web.Shutdown(shutdownCtx)
	return shutdown(srv, opts)
}

// An ephemeral node logs out on the way down, which removes it from the tailnet at once instead of 30-60 min later.
func shutdown(srv *tsnet.Server, opts runOptions) error {
	if !opts.ephemeral {
		return nil
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = lc.Logout(ctx)
	return nil
}

func fail(live *liveState, err error) error {
	live.set(stateError, func(s *Status) { s.Error = ptr(err.Error()) })
	return err
}

// watchLogin publishes the login link while tsnet's Up waits; Up itself only returns once the node runs.
func watchLogin(ctx context.Context, srv *tsnet.Server, live *liveState) {
	lc, err := srv.LocalClient()
	if err != nil {
		return
	}
	for {
		if st, err := lc.StatusWithoutPeers(ctx); err == nil {
			switch {
			case requiresLogging(st.Health):
				live.set(stateError, func(s *Status) { s.Error = ptr(loggingRequired) })
			case st.BackendState == ipn.NeedsLogin.String() && st.AuthURL != "":
				live.set(stateNeedsLogin, func(s *Status) { s.ActionURL = ptr(st.AuthURL) })
			case st.BackendState == ipn.NeedsMachineAuth.String():
				live.set(stateNeedsApproval, func(s *Status) {
					s.ActionURL = ptr("https://login.tailscale.com/admin/machines")
				})
			case st.BackendState == ipn.Running.String():
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func requiresLogging(health []string) bool {
	for _, h := range health {
		if strings.Contains(h, "requires logging") {
			return true
		}
	}
	return false
}
