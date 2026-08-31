package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	piondtls "github.com/pion/dtls/v3"
	coapdtls "github.com/plgd-dev/go-coap/v3/dtls"
	"github.com/plgd-dev/go-coap/v3/message"
	"github.com/plgd-dev/go-coap/v3/message/codes"
	"github.com/plgd-dev/go-coap/v3/mux"
	coapnet "github.com/plgd-dev/go-coap/v3/net"
	"github.com/plgd-dev/go-coap/v3/options"
)

func coapRoundTrip(ctx context.Context, key, payload []byte) ([]byte, error) {
	serverConfig := coapnet.NewDTLSServerOptions(
		piondtls.WithPSK(func([]byte) ([]byte, error) { return key, nil }),
		piondtls.WithPSKIdentityHint([]byte("ra2a-server")),
		piondtls.WithCipherSuites(piondtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		piondtls.WithExtendedMasterSecret(piondtls.RequireExtendedMasterSecret),
	)
	listener, err := coapnet.NewDTLSListener("udp4", "127.0.0.1:0", serverConfig)
	if err != nil {
		return nil, fmt.Errorf("listen CoAP DTLS: %w", err)
	}

	handlerErrors := make(chan error, 1)
	router := mux.NewRouter()
	if err := router.Handle("/v1/messages", mux.HandlerFunc(func(w mux.ResponseWriter, request *mux.Message) {
		body, readErr := request.ReadBody()
		if readErr == nil {
			readErr = w.SetResponse(codes.Content, message.AppOctets, bytes.NewReader(body))
		}
		if readErr != nil {
			select {
			case handlerErrors <- readErr:
			default:
			}
		}
	})); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("register CoAP handler: %w", err)
	}
	server := coapdtls.NewServer(options.WithMux(router))
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		_ = listener.Close()
		<-serveResult
	}()

	clientConfig := coapdtls.NewDTLSClientOptions(
		piondtls.WithPSK(func([]byte) ([]byte, error) { return key, nil }),
		piondtls.WithPSKIdentityHint([]byte("ra2a-client")),
		piondtls.WithCipherSuites(piondtls.TLS_PSK_WITH_AES_128_GCM_SHA256),
		piondtls.WithExtendedMasterSecret(piondtls.RequireExtendedMasterSecret),
	)
	client, err := coapdtls.Dial(listener.Addr().String(), clientConfig)
	if err != nil {
		return nil, fmt.Errorf("dial CoAP DTLS: %w", err)
	}
	defer client.Close()
	response, err := client.Post(ctx, "/v1/messages", message.AppOctets, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("post CoAP message: %w", err)
	}
	defer client.ReleaseMessage(response)
	result, err := io.ReadAll(response.Body())
	if err != nil {
		return nil, fmt.Errorf("read CoAP response: %w", err)
	}
	select {
	case err := <-handlerErrors:
		return nil, fmt.Errorf("handle CoAP message: %w", err)
	default:
	}
	if response.Code() != codes.Content {
		return nil, errors.New("unexpected CoAP response code")
	}
	return result, nil
}
