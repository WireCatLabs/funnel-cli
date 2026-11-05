package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type Route struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

type routesFile struct {
	Routes []Route `json:"routes"`
}

func validateRoute(r Route) error {
	if err := validatePath(r.Path); err != nil {
		return err
	}
	if r.Port < 1 || r.Port > 65535 {
		return fmt.Errorf("port %d is not between 1 and 65535", r.Port)
	}
	return nil
}

func validatePath(p string) error {
	if !strings.HasPrefix(p, "/") {
		return fmt.Errorf("path %q must start with /", p)
	}
	if strings.ContainsAny(p, "?#\\ \t\r\n%") || strings.Contains(p, "..") {
		return fmt.Errorf("path %q may not contain spaces, %%, \\, ?, # or ..", p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("path %q is not in its plain form (%s)", p, path.Clean(p))
	}
	return nil
}

// match picks the longest route equal to the path or a prefix of it at a "/" boundary, as tailscale serve does.
func match(routes []Route, p string) (Route, bool) {
	var best Route
	found := false
	for _, r := range routes {
		ok := p == r.Path || r.Path == "/" || strings.HasPrefix(p, r.Path+"/")
		if ok && (!found || len(r.Path) > len(best.Path)) {
			best, found = r, true
		}
	}
	return best, found
}

func readRoutes(file string) ([]Route, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return []Route{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f routesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	valid := make([]Route, 0, len(f.Routes))
	for _, r := range f.Routes {
		if validateRoute(r) == nil {
			valid = append(valid, r)
		}
	}
	return valid, nil
}

func writeRoutes(file string, routes []Route) error {
	sort.Slice(routes, func(i, j int) bool { return routes[i].Path < routes[j].Path })
	data, err := json.MarshalIndent(routesFile{Routes: routes}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(file, append(data, '\n'))
}

func addRoute(file string, r Route) ([]Route, error) {
	if err := validateRoute(r); err != nil {
		return nil, err
	}
	routes, err := readRoutes(file)
	if err != nil {
		return nil, err
	}
	kept := routes[:0]
	for _, old := range routes {
		if old.Path != r.Path {
			kept = append(kept, old)
		}
	}
	kept = append(kept, r)
	return kept, writeRoutes(file, kept)
}

func removeRoute(file, p string) ([]Route, error) {
	if err := validatePath(p); err != nil {
		return nil, err
	}
	routes, err := readRoutes(file)
	if err != nil {
		return nil, err
	}
	kept := routes[:0]
	for _, old := range routes {
		if old.Path != p {
			kept = append(kept, old)
		}
	}
	return kept, writeRoutes(file, kept)
}

// routeTable re-reads routes.json when it changes, so `route add` reaches a running process without a restart.
type routeTable struct {
	file    string
	mu      sync.Mutex
	modTime time.Time
	size    int64
	loaded  bool
	routes  []Route
}

func (t *routeTable) current() []Route {
	t.mu.Lock()
	defer t.mu.Unlock()
	info, err := os.Stat(t.file)
	if err != nil {
		t.routes, t.loaded = []Route{}, false
		return t.routes
	}
	if t.loaded && info.ModTime().Equal(t.modTime) && info.Size() == t.size {
		return t.routes
	}
	routes, err := readRoutes(t.file)
	if err != nil {
		return t.routes
	}
	t.routes, t.modTime, t.size, t.loaded = routes, info.ModTime(), info.Size(), true
	return t.routes
}
