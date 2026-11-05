package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The running process answers `status` and `stop` on 127.0.0.1 behind a token in a 600 file. This gives a clean stop
// on Windows, which has no signal for it, and never sends a signal to a PID that may by now be another program.
type controlInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func controlFile(dir string) string { return filepath.Join(dir, "control.json") }

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func serveControl(dir string, status func() Status, stop func()) (func(), error) {
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	authorized := func(r *http.Request) bool {
		got := []byte(r.Header.Get("Authorization"))
		return subtle.ConstantTimeCompare(got, []byte("Bearer "+token)) == 1
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status())
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go stop()
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	info := controlInfo{Port: ln.Addr().(*net.TCPAddr).Port, Token: token, PID: os.Getpid()}
	data, _ := json.Marshal(info)
	if err := writeFileAtomic(controlFile(dir), data); err != nil {
		srv.Close()
		return nil, err
	}
	return func() {
		srv.Close()
		if current, err := readControl(dir); err == nil && current.Token == token {
			os.Remove(controlFile(dir))
		}
	}, nil
}

func readControl(dir string) (controlInfo, error) {
	var info controlInfo
	data, err := os.ReadFile(controlFile(dir))
	if err != nil {
		return info, err
	}
	return info, json.Unmarshal(data, &info)
}

var errNotRunning = errors.New("not running")

func callControl(ctx context.Context, dir, method, path string) (*http.Response, error) {
	info, err := readControl(dir)
	if err != nil {
		return nil, errNotRunning
	}
	req, err := http.NewRequestWithContext(ctx, method, fmt.Sprintf("http://127.0.0.1:%d%s", info.Port, path), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errNotRunning
	}
	return resp, nil
}

func liveStatus(ctx context.Context, dir string) (Status, error) {
	resp, err := callControl(ctx, dir, http.MethodGet, "/status")
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Status{}, errNotRunning
	}
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return Status{}, err
	}
	return st, nil
}
