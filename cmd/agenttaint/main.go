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

const usage = "usage: agenttaint doctor [--json]\n       agenttaint run [--mode monitor|notify|block|kill --policy <path> --audit-file <path> [--run-as <uid>:<gid>]] [--bin <path>] -- <command> [args...]\n       --observe and --block remain aliases for --mode monitor and --mode block"

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
		var mode runner.PolicyMode
		modeSet := false
		rest := args[1:]
		for len(rest) > 0 && rest[0] != "--" {
			option := rest[0]
			rest = rest[1:]
			switch {
			case option == "--observe":
				if modeSet {
					return invalid("specify only one of --mode, --observe, or --block")
				}
				mode, modeSet = runner.ModeMonitor, true
			case option == "--block":
				if modeSet {
					return invalid("specify only one of --mode, --observe, or --block")
				}
				mode, modeSet = runner.ModeBlock, true
			case option == "--mode":
				if modeSet {
					return invalid("specify only one of --mode, --observe, or --block")
				}
				if len(rest) == 0 || rest[0] == "--" || rest[0] == "" {
					return invalid("--mode requires one of monitor, notify, block, or kill")
				}
				mode, modeSet = runner.PolicyMode(rest[0]), true
				rest = rest[1:]
			case strings.HasPrefix(option, "--mode="):
				if modeSet {
					return invalid("specify only one of --mode, --observe, or --block")
				}
				if strings.TrimPrefix(option, "--mode=") == "" {
					return invalid("--mode requires one of monitor, notify, block, or kill")
				}
				mode, modeSet = runner.PolicyMode(strings.TrimPrefix(option, "--mode=")), true
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
		if modeSet {
			switch mode {
			case runner.ModeMonitor, runner.ModeNotify, runner.ModeBlock, runner.ModeKill:
			default:
				return invalid("--mode must be monitor, notify, block, or kill")
			}
		} else if auditPath != "" || policyPath != "" {
			// A policy-backed run defaults to audit-only monitoring. Plain `run`
			// without policy/audit options keeps its existing pass-through behavior.
			mode = runner.ModeMonitor
		}
		if mode != "" && auditPath == "" {
			return invalid("policy modes require --audit-file PATH")
		}
		if mode != "" && policyPath == "" {
			return invalid("policy modes require --policy PATH")
		}
		if mode == "" && targetIdentity != nil {
			return invalid("--run-as requires an explicit policy mode")
		}
		var code int
		var err error
		if mode != "" {
			snapshot, loadErr := policy.Load(policyPath)
			if loadErr != nil {
				fmt.Fprintln(stderr, "agenttaint: load policy:", loadErr)
				fmt.Fprintf(stderr, "agenttaint: mode requested=%s active=none actual_action=none result=policy_load_failed audit_coverage=unknown notifications=0\n", mode)
				return 1
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			var status runner.ModeStatus
			code, status, err = runner.RunWithPolicyMode(ctx, resolver, bin, rest[1:], stdin, stdout, stderr, auditPath, snapshot, targetIdentity, mode)
			fmt.Fprintf(stderr, "agenttaint: mode requested=%s active=%s actual_action=%s result=%s audit_coverage=%s notifications=%d\n", status.RequestedMode, status.ActiveMode, status.ActualAction, status.Result, status.AuditCoverage, status.Notifications)
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
