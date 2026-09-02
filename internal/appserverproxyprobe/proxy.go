package appserverproxyprobe

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/gorilla/websocket"
)

func Serve(ctx context.Context, listenSocket, upstreamSocket string, emit func(Event)) error {
	listener, err := net.Listen("unix", listenSocket)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveConnection(ctx, writer, request, upstreamSocket, emit)
	})}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func serveConnection(
	ctx context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	upstreamSocket string,
	emit func(Event),
) {
	inbound, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).
		Upgrade(writer, request, nil)
	if err != nil {
		return
	}
	defer inbound.Close()

	dialer := websocket.Dialer{NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", upstreamSocket)
	}}
	upstream, response, err := dialer.DialContext(ctx, "ws://localhost/", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return
	}
	defer upstream.Close()

	observer := NewObserver()
	done := make(chan struct{}, 2)
	observeClient := func(payload []byte) *Event {
		if event := observer.ObserveClient(payload); event != nil {
			return event
		}
		return observer.ObserveClientShape(payload)
	}
	go relayMessages(inbound, upstream, observeClient, emit, done)
	go relayMessages(upstream, inbound, observer.ObserveServer, emit, done)
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func relayMessages(
	source, destination *websocket.Conn,
	observe func([]byte) *Event,
	emit func(Event),
	done chan<- struct{},
) {
	defer func() { done <- struct{}{} }()
	for {
		messageType, payload, err := source.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage {
			if event := observe(payload); event != nil && emit != nil {
				emit(*event)
			}
		}
		if err := destination.WriteMessage(messageType, payload); err != nil {
			return
		}
	}
}
