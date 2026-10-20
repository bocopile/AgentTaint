//go:build !linux

package runner

import (
	"context"
	"errors"
	"github.com/bocopile/AgentTaint/internal/policy"
	"io"
)

func runObservedPlatform(context.Context, string, []string, io.Reader, io.Writer, io.Writer, string, *policy.Snapshot, *TargetIdentity) (int, error) {
	return 1, errors.New("run --observe requires a supported Linux host; target was not started")
}

func runBlockedPlatform(context.Context, string, []string, io.Reader, io.Writer, io.Writer, string, *policy.Snapshot, *TargetIdentity) (int, error) {
	return 1, errors.New("run --block requires a supported Linux BPF-LSM host; target was not started")
}
