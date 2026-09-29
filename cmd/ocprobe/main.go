// Command ocprobe exercises the OpenCode adapter against a real OpenCode
// server, so the delivery contract can be verified outside unit tests.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ceasarXuu/RA2A/internal/agentbridge"
	"github.com/ceasarXuu/RA2A/internal/opencode"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:4096", "opencode server base URL")
	session := flag.String("session", "", "opencode session id")
	text := flag.String("text", "", "message text")
	flag.Parse()
	if *session == "" || *text == "" {
		fmt.Fprintln(os.Stderr, "ocprobe requires -session and -text")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	adapter := opencode.New("probe-node", opencode.Config{BaseURL: *base, Stderr: os.Stderr,
		CallTimeout: 30 * time.Second, IdleWait: 90 * time.Second}, os.Stderr)
	defer adapter.Close()
	adapter.Watch(ctx)
	endpoints, err := adapter.ListEndpoints(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list:", err)
		os.Exit(1)
	}
	fmt.Printf("endpoints=%d\n", len(endpoints))
	result := adapter.Deliver(ctx,
		agentbridge.Address{NodeID: "probe-node", EndpointID: *session},
		agentbridge.MessageEnvelope{ID: "probe-1", ProtocolVersion: agentbridge.ProtocolVersion,
			SourceAddress: "ra2a://probe-node/probe", TargetAddress: "ra2a://probe-node/" + *session,
			Text: *text, CreatedAt: time.Now().UTC()})
	fmt.Printf("code=%s class=%s turn=%s detail=%q\n",
		result.Code, result.NativeErrorClass, result.TurnID, result.Detail)
	if !result.Delivered() {
		os.Exit(1)
	}
}
