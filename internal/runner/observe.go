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

func RunObserved(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, identity *TargetIdentity) (int, error) {
	return RunObservedWithPolicy(ctx, resolver, bin, args, stdin, stdout, stderr, auditPath, nil, identity)
}

// RunObservedWithPolicy runs one immutable policy snapshot for the whole run.
func RunObservedWithPolicy(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	if len(args) == 0 {
		return 1, errors.New("missing command")
	}
	found, err := resolver.Resolve(args[0], bin)
	if err != nil {
		return 1, fmt.Errorf("resolve command: %w", err)
	}
	return runObservedPlatform(ctx, found.Path, args, stdin, stdout, stderr, auditPath, snapshot, identity)
}

// RunBlockedWithPolicy starts one command only after the validated policy
// subset, private run scope, and required kernel hooks are ready.
func RunBlockedWithPolicy(ctx context.Context, resolver env.Resolver, bin string, args []string, stdin io.Reader, stdout, stderr io.Writer, auditPath string, snapshot *policy.Snapshot, identity *TargetIdentity) (int, error) {
	if len(args) == 0 {
		return 1, errors.New("missing command")
	}
	if snapshot == nil {
		return 1, errors.New("block mode requires a policy")
	}
	found, err := resolver.Resolve(args[0], bin)
	if err != nil {
		return 1, fmt.Errorf("resolve command: %w", err)
	}
	return runBlockedPlatform(ctx, found.Path, args, stdin, stdout, stderr, auditPath, snapshot, identity)
}
