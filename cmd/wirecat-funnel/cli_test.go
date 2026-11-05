package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestStateDirFollowsCliBaseLayout(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	cases := []struct {
		env  map[string]string
		goos string
		want string
	}{
		{map[string]string{"HOME": "/h"}, "linux", "/h/.local/share/wirecat-funnel"},
		{map[string]string{"HOME": "/h", "XDG_DATA_HOME": "/d"}, "darwin", "/d/wirecat-funnel"},
		{map[string]string{"HOME": "/h", "WIRECAT_FUNNEL_STATE_DIR": "/o"}, "linux", "/o"},
		{map[string]string{"LOCALAPPDATA": "/l"}, "windows", "/l/wirecat-funnel/Data"},
	}
	for _, c := range cases {
		got, err := stateDir(env(c.env), c.goos)
		if err != nil || filepathToSlash(got) != c.want {
			t.Errorf("%v %s: %s %v, want %s", c.env, c.goos, got, err, c.want)
		}
	}
}

func filepathToSlash(p string) string {
	return string(bytes.ReplaceAll([]byte(p), []byte(`\`), []byte("/")))
}

func TestStatusWhenStoppedHasEveryKeyAndTheRoutes(t *testing.T) {
	t.Setenv("WIRECAT_FUNNEL_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	if code := cli([]string{"route", "add", "/tg", "8765", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("route add: %d %s %s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := cli([]string{"status", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("status: %d %s", code, errOut.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"state", "pid", "url", "action_url", "error", "routes", "dir", "version"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing %s in %s", key, out.String())
		}
	}
	if string(raw["state"]) != `"stopped"` || string(raw["pid"]) != "null" {
		t.Errorf("state %s pid %s", raw["state"], raw["pid"])
	}
	if string(raw["routes"]) == "null" || !bytes.Contains(raw["routes"], []byte(`"/tg"`)) {
		t.Errorf("routes %s", raw["routes"])
	}
}

func TestBadInputIsAJSONErrorAndExitOne(t *testing.T) {
	t.Setenv("WIRECAT_FUNNEL_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	if code := cli([]string{"route", "add", "/a/../b", "80", "--json"}, &out, &errOut); code != 1 {
		t.Fatalf("exit %d", code)
	}
	var e map[string]string
	if err := json.Unmarshal(out.Bytes(), &e); err != nil || e["error"] == "" {
		t.Fatalf("%s", out.String())
	}
}

func TestStopWithNothingRunningIsNotAnError(t *testing.T) {
	t.Setenv("WIRECAT_FUNNEL_STATE_DIR", t.TempDir())
	var out, errOut bytes.Buffer
	if code := cli([]string{"stop", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d %s", code, errOut.String())
	}
}

func TestControlAnswersOnlyWithTheToken(t *testing.T) {
	dir := t.TempDir()
	stopped := make(chan struct{})
	closeControl, err := serveControl(dir, func() Status { return Status{State: stateRunning, Routes: []Route{}} },
		func() { close(stopped) })
	if err != nil {
		t.Fatal(err)
	}
	defer closeControl()
	st, err := liveStatus(t.Context(), dir)
	if err != nil || st.State != stateRunning {
		t.Fatalf("%v %v", st, err)
	}
	info, _ := readControl(dir)
	info.Token = "wrong"
	data, _ := json.Marshal(info)
	_ = writeFileAtomic(controlFile(dir), data)
	if _, err := liveStatus(t.Context(), dir); err == nil {
		t.Fatal("a wrong token was answered")
	}
}
