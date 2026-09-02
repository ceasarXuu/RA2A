package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ceasarXuu/RA2A/internal/appserverproxyprobe"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("appserver-proxy-probe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listenSocket := flags.String("listen-socket", "", "Unix socket exposed to the observed client")
	upstreamSocket := flags.String("upstream-socket", "", "Unix socket of the isolated App Server")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *listenSocket == "" || *upstreamSocket == "" {
		return errors.New("--listen-socket and --upstream-socket are required")
	}

	var outputMu sync.Mutex
	return appserverproxyprobe.Serve(ctx, *listenSocket, *upstreamSocket, func(event appserverproxyprobe.Event) {
		outputMu.Lock()
		defer outputMu.Unlock()
		_ = json.NewEncoder(output).Encode(event)
	})
}
