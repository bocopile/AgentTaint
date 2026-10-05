package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/runner"
)

const usage = "usage: agenttaint doctor [--json]\n       agenttaint run [--bin <path>] -- <command> [args...]"

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, env.NewResolver())) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, resolver env.Resolver) int {
	invalid := func(message string) int { fmt.Fprintln(stderr, message); fmt.Fprintln(stderr, usage); return 2 }
	if len(args) == 0 {
		return invalid("missing subcommand")
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprintln(stdout, usage)
		return 0
	case "doctor":
		asJSON := len(args) == 2 && args[1] == "--json"
		if len(args) != 1 && !asJSON {
			return invalid("doctor accepts only --json")
		}
		fmt.Fprintln(stderr, env.Notice)
		if err := env.Diagnose(resolver).Write(stdout, asJSON); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "run":
		bin := ""
		rest := args[1:]
		if len(rest) > 0 && rest[0] == "--bin" {
			if len(rest) < 2 || rest[1] == "" || rest[1] == "--" {
				return invalid("--bin requires a literal path")
			}
			bin, rest = rest[1], rest[2:]
		} else if len(rest) > 0 && strings.HasPrefix(rest[0], "--bin=") {
			bin, rest = strings.TrimPrefix(rest[0], "--bin="), rest[1:]
			if bin == "" {
				return invalid("--bin requires a literal path")
			}
		}
		if len(rest) < 2 || rest[0] != "--" {
			return invalid("run requires -- followed by a command")
		}
		code, err := runner.Run(resolver, bin, rest[1:], stdin, stdout, stderr)
		if err != nil {
			fmt.Fprintln(stderr, "agenttaint:", err)
		}
		return code
	default:
		return invalid("unknown subcommand: " + args[0])
	}
}
