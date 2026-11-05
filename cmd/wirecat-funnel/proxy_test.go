package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func backend(t *testing.T, h http.HandlerFunc) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestProxyKeepsThePathAndThePublicHostLikeTailscaleServe(t *testing.T) {
	seen := make(chan *http.Request, 1)
	port := backend(t, func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		fmt.Fprint(w, "ok")
	})
	front := httptest.NewServer(newProxy(func() []Route { return []Route{{Path: "/tg", Port: port}} }))
	defer front.Close()

	req, _ := http.NewRequest(http.MethodPost, front.URL+"/tg/mcp?x=1", strings.NewReader("{}"))
	req.Host = "wirecat.tail1234.ts.net"
	req.Header.Set("Tailscale-User-Login", "someone@example.com")
	req.Header.Set("Tailscale-Funnel-Request", "?1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	r := <-seen
	if r.URL.Path != "/tg/mcp" || r.URL.RawQuery != "x=1" {
		t.Errorf("path %s?%s", r.URL.Path, r.URL.RawQuery)
	}
	if r.Host != "wirecat.tail1234.ts.net" {
		t.Errorf("host %s", r.Host)
	}
	if r.Header.Get("X-Forwarded-Host") != "wirecat.tail1234.ts.net" || r.Header.Get("X-Forwarded-Proto") != "https" {
		t.Errorf("forwarded %v", r.Header)
	}
	if r.Header.Get("X-Forwarded-For") != "127.0.0.1" {
		t.Errorf("for %q", r.Header.Get("X-Forwarded-For"))
	}
	if r.Header.Get("Tailscale-User-Login") != "" || r.Header.Get("Tailscale-Funnel-Request") != "" {
		t.Errorf("a Tailscale-* header got through: %v", r.Header)
	}
}

func TestProxyAnswers404WithoutARouteAnd502WhenTheServerIsDown(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	front := httptest.NewServer(newProxy(func() []Route { return []Route{{Path: "/tg", Port: closed}} }))
	defer front.Close()
	for path, want := range map[string]int{"/max/mcp": 404, "/tg/mcp": 502} {
		resp, err := http.Get(front.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: %d, want %d", path, resp.StatusCode, want)
		}
	}
}

func TestProxyStreamsAnEventStreamAsItComes(t *testing.T) {
	release := make(chan struct{})
	port := backend(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: second\n\n")
	})
	defer close(release)
	front := httptest.NewServer(newProxy(func() []Route { return []Route{{Path: "/tg", Port: port}} }))
	defer front.Close()

	resp, err := http.Get(front.URL + "/tg/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(resp.Body).ReadString('\n')
		line <- l
	}()
	select {
	case l := <-line:
		if l != "data: first\n" {
			t.Fatalf("got %q", l)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first event was held back until the stream ended")
	}
}
