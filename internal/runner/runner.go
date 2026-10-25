// Package runner launches one explicitly selected command without protection.
package runner

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/bocopile/AgentTaint/internal/env"
)

// Run preserves argv, streams, the inherited environment, and normal exit codes.
// A target terminated by signal N yields 128+N, the POSIX shell convention.
// It is not a sandbox, sensor, or process-tree supervisor.
func Run(resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 1, errors.New("missing command")
	}
	found, err := resolver.Resolve(args[0], bin)
	if err != nil {
		return 1, fmt.Errorf("resolve command: %w", err)
	}
	fmt.Fprintln(stderr, env.RunNotice)
	if runtime.GOOS != "linux" {
		fmt.Fprintln(stderr, "Linux sensor unsupported on this host; use a Linux VM for future Linux phases.")
	}
	fmt.Fprintf(stderr, "agenttaint: resolved %q via %s\n", found.Path, found.Source)
	// Cmd.Path and Cmd.Args are independent, preserving the requested argv[0].
	// https://pkg.go.dev/os/exec#Cmd
	cmd := &exec.Cmd{Path: found.Path, Args: append([]string(nil), args...), Stdin: stdin, Stdout: stdout, Stderr: stderr}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			if exit.ExitCode() >= 0 {
				return exit.ExitCode(), nil
			}
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				fmt.Fprintf(stderr, "agenttaint: target terminated by signal: %v\n", status.Signal())
				return 128 + int(status.Signal()), nil
			}
		}
		return 1, fmt.Errorf("execute command: %w", err)
	}
	return 0, nil
}
