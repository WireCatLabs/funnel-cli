package main

import (
	"path/filepath"
	"testing"
)

func TestMatchPicksTheLongestRouteAtASlashBoundary(t *testing.T) {
	routes := []Route{
		{Path: "/tg", Port: 1},
		{Path: "/.well-known/oauth-authorization-server/tg", Port: 2},
		{Path: "/tgx", Port: 3},
	}
	cases := map[string]int{
		"/tg":     1,
		"/tg/mcp": 1,
		"/tgx/a":  3,
		"/.well-known/oauth-authorization-server/tg":  2,
		"/.well-known/oauth-authorization-server/tgy": 0,
		"/t": 0,
	}
	for p, want := range cases {
		got, ok := match(routes, p)
		if want == 0 && ok {
			t.Errorf("%s matched %v, want none", p, got)
		}
		if want != 0 && got.Port != want {
			t.Errorf("%s went to %d, want %d", p, got.Port, want)
		}
	}
}

func TestRootRouteCatchesWhatNothingElseDoes(t *testing.T) {
	routes := []Route{{Path: "/", Port: 1}, {Path: "/tg", Port: 2}}
	if r, _ := match(routes, "/anything"); r.Port != 1 {
		t.Fatalf("got %d", r.Port)
	}
	if r, _ := match(routes, "/tg/mcp"); r.Port != 2 {
		t.Fatalf("got %d", r.Port)
	}
}

func TestValidatePathRefusesAnythingButAPlainPath(t *testing.T) {
	for _, bad := range []string{"", "tg", "/tg/", "/a/../b", "/a?b", "/a#b", "/a b", "/a%2e", `/a\b`, "//a"} {
		if validatePath(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, good := range []string{"/", "/tg", "/.well-known/oauth-protected-resource/tg"} {
		if err := validatePath(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
}

func TestAddReplacesThePortOfAnExistingPathAndRemoveDropsIt(t *testing.T) {
	file := filepath.Join(t.TempDir(), "routes.json")
	if _, err := addRoute(file, Route{Path: "/tg", Port: 8765}); err != nil {
		t.Fatal(err)
	}
	if _, err := addRoute(file, Route{Path: "/max", Port: 8766}); err != nil {
		t.Fatal(err)
	}
	routes, err := addRoute(file, Route{Path: "/tg", Port: 9000})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0] != (Route{"/max", 8766}) || routes[1] != (Route{"/tg", 9000}) {
		t.Fatalf("routes %v", routes)
	}
	routes, _ = removeRoute(file, "/max")
	if len(routes) != 1 {
		t.Fatalf("routes %v", routes)
	}
	if _, err := addRoute(file, Route{Path: "/tg", Port: 70000}); err == nil {
		t.Fatal("port 70000 accepted")
	}
}

func TestRouteTableSeesAChangeWithoutARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "routes.json")
	table := &routeTable{file: file}
	if len(table.current()) != 0 {
		t.Fatal("routes before the file exists")
	}
	_, _ = addRoute(file, Route{Path: "/tg", Port: 1})
	if got := table.current(); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	_, _ = addRoute(file, Route{Path: "/max", Port: 2})
	if got := table.current(); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}
