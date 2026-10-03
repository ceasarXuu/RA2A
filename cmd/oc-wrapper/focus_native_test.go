package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ceasarXuu/RA2A/internal/ocsession"
)

// This opt-in runs real attach clients with pipes, including on Windows. The
// controller navigates native TUI routes; it never sends a model prompt.
func TestNativeOpenCodeFocusTracksClientRoutes(t *testing.T) {
	binary := os.Getenv("OPENCODE_TEST_INTEGRATION_BINARY")
	if binary == "" {
		t.Skip("set OPENCODE_TEST_INTEGRATION_BINARY to test native TUI focus")
	}
	home := t.TempDir()
	env := focusNativeEnvironment(t, home)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var serverLog synchronizedLog
	server := exec.CommandContext(ctx, binary, "serve", "--pure", "--port", fmt.Sprint(port), "--hostname", "127.0.0.1")
	server.Env, server.Dir, server.Stdout, server.Stderr = env, home, &serverLog, &serverLog
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = server.Wait() })
	client := &http.Client{Timeout: time.Second}
	waitFocusNative(t, func() bool {
		r, e := client.Get(baseURL + "/session")
		if e != nil {
			return false
		}
		_ = r.Body.Close()
		return r.StatusCode == 200
	}, func() string { return serverLog.String() })
	create := func() string {
		r, e := client.Post(baseURL+"/session", "application/json", strings.NewReader("{}"))
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		var value struct {
			ID string `json:"id"`
		}
		if e = json.NewDecoder(r.Body).Decode(&value); e != nil || value.ID == "" {
			t.Fatalf("create session: %v %+v", e, value)
		}
		return value.ID
	}
	a, b := create(), create()
	start := func(name, session string) (string, func()) {
		dir := filepath.Join(home, name)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		control := filepath.Join(dir, "control.json")
		controlJSON, _ := json.Marshal(control)
		controller := fmt.Sprintf(`import { readFileSync } from "node:fs";
export default {id:"ra2a-native-focus-controller",tui:async(api)=>{
let last="";const timer=setInterval(()=>{try{const value=readFileSync(%s,"utf8");if(value===last)return;last=value;const route=JSON.parse(value);api.route.navigate(route.name,route.params);}catch{}},50);
api.lifecycle.onDispose(()=>clearInterval(timer));}};`, controlJSON)
		plugin := filepath.Join(dir, "controller.mjs")
		if err := os.WriteFile(plugin, []byte(controller), 0600); err != nil {
			t.Fatal(err)
		}
		override := filepath.Join(dir, "original.json")
		data, _ := json.Marshal(map[string]any{"plugin": []string{plugin}})
		if err := os.WriteFile(override, data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("OPENCODE_TUI_CONFIG", override)
		config, release, err := ocsession.PrepareFocus(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(release)
		command := exec.CommandContext(ctx, binary, "attach", baseURL, "--dir", home, "--session", session)
		command.Env = append(env, "OPENCODE_TUI_CONFIG="+config)
		command.Dir = home
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		var output synchronizedLog
		command.Stdout, command.Stderr = &output, &output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = input.Close()
			if command.Process != nil {
				_ = command.Process.Kill()
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("native attach did not exit")
			}
		}
		t.Cleanup(stop)
		waitFocusNative(t, func() bool { return ocsession.Current(dir) == session }, func() string { return output.String() })
		return dir, stop
	}
	dirA, stopA := start("client-a", a)
	dirB, stopB := start("client-b", b)
	navigate := func(dir, name, session string) {
		value := map[string]any{"name": name}
		if session != "" {
			value["params"] = map[string]string{"sessionID": session}
		}
		data, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, "control.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	assertFocus := func(dir, want string) {
		waitFocusNative(t, func() bool { return ocsession.Current(dir) == want }, func() string { return fmt.Sprintf("focus=%q active=%v", ocsession.Current(dir), ocsession.Active(dir)) })
		active := ocsession.Active(dir)
		if want == "" && len(active) != 0 {
			t.Fatalf("unfocused client published sessions: %v", active)
		}
		if want != "" && (len(active) != 1 || !active[want]) {
			t.Fatalf("client focus mismatch: %v want %s", active, want)
		}
	}
	navigate(dirA, "session", b)
	assertFocus(dirA, b)
	assertFocus(dirB, b)
	navigate(dirA, "home", "")
	assertFocus(dirA, "")
	assertFocus(dirB, b)
	navigate(dirA, "session", a)
	assertFocus(dirA, a)
	assertFocus(dirB, b)
	stopA()
	stopB()
	// Abrupt attach termination bypasses plugin disposal. The wrapper is still
	// alive here, so TTL alone must revoke execution ownership.
	waitFocusNative(t, func() bool { return len(ocsession.Active(dirA)) == 0 && len(ocsession.Active(dirB)) == 0 }, func() string {
		return fmt.Sprintf("leases retained: %v %v", ocsession.Active(dirA), ocsession.Active(dirB))
	})
}

func focusNativeEnvironment(t *testing.T, home string) []string {
	t.Helper()
	var env []string
	for _, entry := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if strings.HasPrefix(key, "RA2A_") || strings.HasPrefix(key, "OPENCODE_") || strings.HasPrefix(key, "CODEX_") || strings.Contains(key, "API_KEY") || strings.Contains(key, "TOKEN") || strings.Contains(key, "PROXY") {
			continue
		}
		skip := false
		for _, name := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "TEMP", "TMP", "TMPDIR"} {
			if key == name {
				skip = true
			}
		}
		if !skip {
			env = append(env, entry)
		}
	}
	for _, name := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "CODEX_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "TEMP", "TMP", "TMPDIR"} {
		path := filepath.Join(home, strings.ToLower(name))
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		env = append(env, name+"="+path)
	}
	return append(env, "TERM=xterm-256color", "CI=true", "OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_PROJECT_CONFIG=true", "OPENCODE_CONFIG_CONTENT={\"autoupdate\":false,\"share\":\"disabled\",\"plugin\":[]}")
}

func waitFocusNative(t *testing.T, ready func() bool, details func() string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("native focus condition timed out: %s", details())
}
