package main

import (
	"fmt"
	"os"

	"github.com/ceasarXuu/RA2A/internal/mcpcontext"
)

func main() {
	if err := mcpcontext.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
