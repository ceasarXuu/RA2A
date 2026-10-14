//go:build linux

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/codexowner"
	"github.com/ceasarXuu/RA2A/internal/codexstdio"
	"golang.org/x/sys/unix"
)

var appThreadID = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

type appPeer struct{}
type AppHost struct {
	Bridge                                     *codexstdio.Bridge
	PID, PeerPID                               int
	Start, PeerStart, Home, Executable, NodeID string
}

func AppProcessStart(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	end := strings.LastIndexByte(string(data), ')')
	if err != nil || end < 0 {
		return ""
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return ""
	}
	return fields[19]
}

func (h AppHost) thread(ctx context.Context, id string) (bool, bool) {
	if !appThreadID.MatchString(id) {
		return false, false
	}
	owner, err := codexowner.ReadWriter(ctx, filepath.Join(h.Home, "thread-writer-locks", id+".lock"))
	if err != nil || owner == nil || owner.PID != h.PID || owner.Executable != h.Executable || strconv.FormatUint(owner.StartTicks, 10) != h.Start {
		return false, false
	}
	raw, err := h.Bridge.Call(ctx, "thread/read", map[string]any{"threadId": id})
	var value struct {
		Thread struct {
			CanAcceptDirectInput bool
			Status               struct{ Type string }
		}
	}
	if err != nil || json.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return value.Thread.CanAcceptDirectInput, value.Thread.Status.Type == "active"
}

func (h AppHost) endpoints(ctx context.Context) []agentbridge.Endpoint {
	raw, err := h.Bridge.Call(ctx, "thread/loaded/list", map[string]any{})
	var value struct {
		Data       []string
		NextCursor *string
	}
	if err != nil || json.Unmarshal(raw, &value) != nil || value.NextCursor != nil {
		return nil
	}
	var result []agentbridge.Endpoint
	for _, id := range value.Data {
		ok, busy := h.thread(ctx, id)
		if !ok {
			continue
		}
		status := agentbridge.EndpointReady
		if busy {
			status = agentbridge.EndpointBusy
		}
		result = append(result, agentbridge.Endpoint{ID: id, NativeSessionID: id, Title: id,
			Agent: agentbridge.AgentCodexApp, Status: status, Address: agentbridge.Address{NodeID: h.NodeID, EndpointID: id},
			Capabilities: []agentbridge.Capability{agentbridge.CapabilityReceiveText, agentbridge.CapabilityReplyAddress, agentbridge.CapabilitySteerActiveTurn}})
	}
	return result
}

func (h AppHost) deliver(ctx context.Context, id, text string) agentbridge.DeliveryResult {
	ok, busy := h.thread(ctx, id)
	if !ok {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound, NativeErrorClass: "app_writer_unavailable"}
	}
	method := "turn/start"
	params := map[string]any{"threadId": id, "input": []map[string]any{{"type": "text", "text": text, "textElements": []any{}}}}
	expected := ""
	if busy {
		raw, err := h.Bridge.Call(ctx, "thread/turns/list", map[string]any{"threadId": id})
		var turns struct{ Data []struct{ ID, Status string } }
		if err != nil || json.Unmarshal(raw, &turns) != nil || len(turns.Data) == 0 || turns.Data[0].Status != "inProgress" {
			return agentbridge.DeliveryResult{Code: agentbridge.ResultBusy}
		}
		method, expected = "turn/steer", turns.Data[0].ID
		params["expectedTurnId"] = expected
	}
	if ok, _ = h.thread(ctx, id); !ok {
		return agentbridge.DeliveryResult{Code: agentbridge.ResultNotFound}
	}
	raw, err := h.Bridge.Call(ctx, method, params)
	var receipt struct {
		Turn struct {
			ID    string
			Error json.RawMessage
		}
		TurnID string
	}
	if err != nil || json.Unmarshal(raw, &receipt) != nil {
		return unknown("app_receipt_unknown")
	}
	id = receipt.Turn.ID
	if method == "turn/steer" {
		id = receipt.TurnID
		if id != expected {
			id = ""
		}
	}
	if id == "" || (len(receipt.Turn.Error) > 0 && string(receipt.Turn.Error) != "null") {
		return unknown("app_receipt_unknown")
	}
	return agentbridge.Delivered(id)
}

// Only the protected worker's peer identity authorizes requests, not socket visibility.
func (h AppHost) Serve(ctx context.Context, listener *net.UnixListener) error {
	if h.PeerPID <= 0 || h.PeerStart == "" || AppProcessStart(h.PeerPID) != h.PeerStart {
		return fmt.Errorf("app worker identity unavailable")
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
		raw, err := conn.(*net.UnixConn).SyscallConn()
		allowed := false
		if err == nil {
			_ = raw.Control(func(fd uintptr) {
				peer, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
				allowed = e == nil && int(peer.Pid) == h.PeerPID
			})
		}
		return context.WithValue(ctx, appPeer{}, allowed)
	}, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(appPeer{}) != true || AppProcessStart(h.PeerPID) != h.PeerStart {
			http.Error(w, "worker required", http.StatusForbidden)
			return
		}
		call, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		var result any
		switch {
		case (r.Method == http.MethodGet || r.Method == http.MethodPost) && r.URL.Path == "/endpoints":
			result = h.endpoints(call)
		case r.Method == http.MethodPost && r.URL.Path == "/deliver":
			var input struct{ ID, Text string }
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input) != nil {
				http.Error(w, "invalid input", 400)
				return
			}
			result = h.deliver(call, input.ID, input.Text)
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	})}
	go func() { <-ctx.Done(); _ = server.Close() }()
	return server.Serve(listener)
}

func ListenApp(directory string) (*net.UnixListener, error) {
	info, err := os.Lstat(directory)
	if err != nil || !filepath.IsAbs(directory) || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("private app socket directory required")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return nil, fmt.Errorf("private app directory owner required")
	}
	path := filepath.Join(directory, strconv.Itoa(os.Getpid())+".sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err == nil {
		err = os.Chmod(path, 0600)
		if err != nil {
			_ = listener.Close()
		}
	}
	return listener, err
}
