package codexhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type websocketStream struct {
	connection  *websocket.Conn
	readBuffer  bytes.Reader
	writeMu     sync.Mutex
	writeBuffer []byte
}

func DialUnixWebSocket(ctx context.Context, socketPath string) (io.ReadWriteCloser, error) {
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	connection, response, err := dialer.DialContext(ctx, "ws://localhost/", http.Header{})
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	return &websocketStream{connection: connection}, nil
}

func (stream *websocketStream) Read(destination []byte) (int, error) {
	for stream.readBuffer.Len() == 0 {
		messageType, payload, err := stream.connection.ReadMessage()
		if err != nil {
			return 0, err
		}
		if messageType != websocket.TextMessage {
			continue
		}
		payload = append(payload, '\n')
		stream.readBuffer.Reset(payload)
	}
	return stream.readBuffer.Read(destination)
}

func (stream *websocketStream) Write(payload []byte) (int, error) {
	stream.writeMu.Lock()
	defer stream.writeMu.Unlock()
	stream.writeBuffer = append(stream.writeBuffer, payload...)
	for {
		lineEnd := bytes.IndexByte(stream.writeBuffer, '\n')
		if lineEnd < 0 {
			return len(payload), nil
		}
		line := bytes.TrimSpace(stream.writeBuffer[:lineEnd])
		stream.writeBuffer = stream.writeBuffer[lineEnd+1:]
		if len(line) == 0 {
			continue
		}
		if err := stream.connection.WriteMessage(websocket.TextMessage, line); err != nil {
			return 0, err
		}
	}
}

func (stream *websocketStream) Close() error {
	if stream.connection == nil {
		return errors.New("websocket is not connected")
	}
	return stream.connection.Close()
}
