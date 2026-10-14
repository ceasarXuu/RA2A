package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ceasarXuu/RA2A/internal/ochost"
	"github.com/ceasarXuu/RA2A/internal/operator"
)

func TestOpenCodeV2IsNotRegisteredOrAttached(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executable fixture")
	}
	home := isolatedOperatorHome(t)
	binary := filepath.Join(home, "opencode-v2")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\necho 'opencode v2.0.28'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RA2A_DISABLE_OPENCODE", "false")
	t.Setenv("RA2A_OPENCODE_BINARY", binary)
	if err := operator.Save(operator.Config{NodeID: "node", Name: "node", PIN: "A1B2C3", OpenCode: binary}); err != nil {
		t.Fatal(err)
	}
	adapter, err := startOpencodeAdapter(context.Background(), "node", io.Discard)
	if adapter != nil || !errors.Is(err, ochost.ErrUnsupportedVersion) {
		t.Fatalf("v2 adapter must remain unavailable: %v, %v", adapter, err)
	}
	if err := runOpencodeAttach(context.Background(), nil, io.Discard); !errors.Is(err, ochost.ErrUnsupportedVersion) {
		t.Fatalf("explicit attach must reject v2: %v", err)
	}
}
