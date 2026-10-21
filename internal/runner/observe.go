package runner

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/bocopile/AgentTaint/internal/env"
	"github.com/bocopile/AgentTaint/internal/policy"
)

// RunObserved starts a command only after its audit output and observer are
// ready. It preserves the target's three standard streams and exit status.
type TargetIdentity struct {
	UID uint32
	GID uint32
}

// PolicyMode selects how a validated policy decision is acted on. Kill is a
// post-event response and does not undo the operation that produced the event.
type PolicyMode string

const (
	ModeMonitor PolicyMode = "monitor"
	ModeNotify  PolicyMode = "notify"
	ModeBlock   PolicyMode = "block"
	ModeKill    PolicyMode = "kill"
)

// ModeStatus distinguishes the requested mode, successfully started mode,
// action taken, target result, and audit coverage.
type ModeStatus struct {
	RequestedMode PolicyMode
	ActiveMode    PolicyMode
	ActualAction  string
	Result        string
	AuditCoverage string
	Notifications uint64
}

func RunObserved(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, identity *TargetIdentity) (int, error) {
	return RunObservedWithPolicy(ctx, resolver, bin, args, stdin, stdout, stderr, auditPath, nil, identity)
}

// RunObservedWithPolicy runs one immutable policy snapshot for the whole run.
func RunObservedWithPolicy(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	code, _, err := RunWithPolicyMode(ctx, resolver, bin, args, stdin, stdout, stderr, auditPath, snapshot, identity, ModeMonitor)
	return code, err
}

// RunWithPolicyMode runs one command using a single explicit policy mode.
func RunWithPolicyMode(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity, mode PolicyMode) (int, ModeStatus, error) {
	status := ModeStatus{
		RequestedMode: mode,
		ActiveMode:    "none",
		ActualAction:  "none",
		Result:        "not_started",
		AuditCoverage: "unknown",
	}
	switch mode {
	case ModeMonitor, ModeNotify, ModeBlock, ModeKill:
	default:
		return 2, status, fmt.Errorf("unsupported policy mode %q", mode)
	}
	if mode != ModeMonitor && snapshot == nil {
		return 1, status, fmt.Errorf("%s mode requires a policy", mode)
	}
	if len(args) == 0 {
		return 1, status, errors.New("missing command")
	}
	found, err := resolver.Resolve(args[0], bin)
	if err != nil {
		return 1, status, fmt.Errorf("resolve command: %w", err)
	}
	code, err := runWithModePlatform(ctx, found.Path, args, stdin, stdout, stderr, auditPath, snapshot, identity, mode, &status)
	return code, status, err
}

// RunBlockedWithPolicy starts one command only after the validated policy
// subset, private run scope, and required kernel hooks are ready.
func RunBlockedWithPolicy(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	code, _, err := RunWithPolicyMode(ctx, resolver, bin, args, stdin, stdout, stderr, auditPath, snapshot, identity, ModeBlock)
	return code, err
}
