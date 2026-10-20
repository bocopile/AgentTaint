package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
	"github.com/bocopile/AgentTaint/internal/runner"
)

const usage = "usage: agenttaint doctor [--json]\n       agenttaint run [(--observe|--block) --policy <path> --audit-file <path> [--run-as <uid>:<gid>]] [--bin <path>] -- <command> [args...]"

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
		auditPath := ""
		policyPath := ""
		var targetIdentity *runner.TargetIdentity
		observe := false
		block := false
		rest := args[1:]
		for len(rest) > 0 && rest[0] != "--" {
			option := rest[0]
			rest = rest[1:]
			switch {
			case option == "--observe":
				if observe {
					return invalid("--observe may be specified only once")
				}
				observe = true
			case option == "--block":
				if block {
					return invalid("--block may be specified only once")
				}
				block = true
			case option == "--audit-file":
				if auditPath != "" || len(rest) == 0 || rest[0] == "--" || rest[0] == "" {
					return invalid("--audit-file requires one literal path")
				}
				auditPath, rest = rest[0], rest[1:]
			case option == "--policy":
				if policyPath != "" || len(rest) == 0 || rest[0] == "--" || rest[0] == "" {
					return invalid("--policy requires one literal path")
				}
				policyPath, rest = rest[0], rest[1:]
			case option == "--run-as":
				if targetIdentity != nil || len(rest) == 0 || rest[0] == "--" || rest[0] == "" {
					return invalid("--run-as requires one UID:GID pair")
				}
				var err error
				targetIdentity, err = parseTargetIdentity(rest[0])
				if err != nil {
					return invalid(err.Error())
				}
				rest = rest[1:]
			case option == "--bin":
				if bin != "" || len(rest) == 0 || rest[0] == "--" || rest[0] == "" {
					return invalid("--bin requires one literal path")
				}
				bin, rest = rest[0], rest[1:]
			case strings.HasPrefix(option, "--bin="):
				if bin != "" || strings.TrimPrefix(option, "--bin=") == "" {
					return invalid("--bin requires one literal path")
				}
				bin = strings.TrimPrefix(option, "--bin=")
			default:
				return invalid("unknown run option: " + option)
			}
		}
		if len(rest) < 2 || rest[0] != "--" || len(rest[1:]) == 0 {
			return invalid("run requires -- followed by a command")
		}
		if observe && block {
			return invalid("--observe and --block are mutually exclusive")
		}
		if (observe || block) && auditPath == "" {
			return invalid("--observe and --block require --audit-file PATH")
		}
		if (observe || block) && policyPath == "" {
			return invalid("--observe and --block require --policy PATH")
		}
		if !observe && !block && auditPath != "" {
			return invalid("--audit-file requires --observe or --block")
		}
		if !observe && !block && policyPath != "" {
			return invalid("--policy requires --observe or --block")
		}
		if !observe && !block && targetIdentity != nil {
			return invalid("--run-as requires --observe or --block")
		}
		var code int
		var err error
		if observe || block {
			snapshot, loadErr := policy.Load(policyPath)
			if loadErr != nil {
				fmt.Fprintln(stderr, "agenttaint: load policy:", loadErr)
				return 1
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			if block {
				code, err = runner.RunBlockedWithPolicy(ctx, resolver, bin, rest[1:], stdin, stdout, stderr, auditPath, snapshot, targetIdentity)
			} else {
				code, err = runner.RunObservedWithPolicy(ctx, resolver, bin, rest[1:], stdin, stdout, stderr, auditPath, snapshot, targetIdentity)
			}
		} else {
			code, err = runner.Run(resolver, bin, rest[1:], stdin, stdout, stderr)
		}
		if err != nil {
			fmt.Fprintln(stderr, "agenttaint:", err)
		}
		return code
	default:
		return invalid("unknown subcommand: " + args[0])
	}
}

func parseTargetIdentity(value string) (*runner.TargetIdentity, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("--run-as requires numeric UID:GID")
	}
	uid, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("--run-as UID must be an unsigned 32-bit integer")
	}
	gid, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("--run-as GID must be an unsigned 32-bit integer")
	}
	return &runner.TargetIdentity{UID: uint32(uid), GID: uint32(gid)}, nil
}
