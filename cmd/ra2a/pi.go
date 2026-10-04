package main

import (
	"context"
	"os"
	"os/exec"

	"github.com/ceasarXuu/RA2A/internal/operator"
	"github.com/ceasarXuu/RA2A/internal/pi"
)

func runPi(ctx context.Context, args []string) error {
	executable := "pi"
	if config, err := operator.Load(); err == nil && config.Pi != "" {
		executable = config.Pi
	}
	if _, err := pi.Install(); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	return command.Run()
}

func unregisterPi() error { return pi.Uninstall() }
