package ochost

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrUnsupportedVersion = errors.New("OpenCode v2 or later is not supported by RA2A yet; run native opencode without --ra2a")

var versionPattern = regexp.MustCompile(`^(?:opencode\s+)?v?([0-9]+)\.`)

// CheckCompatibility detects the known incompatible major version. Unrecognized
// output retains the existing v1 behavior; transport readiness is checked separately.
func CheckCompatibility(ctx context.Context, executable string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := serverCommand(executable, "--version")
	command = exec.CommandContext(ctx, command.Path, command.Args[1:]...)
	command.WaitDelay = time.Second
	// serverCommand also handles Windows command shims.
	output, err := command.Output()
	if err != nil {
		return nil
	}
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(string(output)))
	if len(match) == 2 {
		major, _ := strconv.Atoi(match[1])
		if major >= 2 {
			return fmt.Errorf("%w (detected %s)", ErrUnsupportedVersion, strings.TrimSpace(string(output)))
		}
	}
	return nil
}
