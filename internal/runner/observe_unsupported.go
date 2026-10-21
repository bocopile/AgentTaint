//go:build !linux

package runner

import (
	"context"
	"errors"
	"io"

	"github.com/bocopile/AgentTaint/internal/policy"
)

func runWithModePlatform(_ context.Context, _ string, _ []string, _ io.Reader, _ io.Writer, _ io.Writer, _ string, _ *policy.Snapshot, _ *TargetIdentity, mode PolicyMode, status *ModeStatus) (int, error) {
	if status != nil {
		status.Result = "not_started"
		status.ActiveMode = "none"
		status.ActualAction = "none"
	}
	if mode == ModeBlock || mode == ModeKill {
		return 1, errors.New("requested enforcement mode requires a supported Linux host; target was not started")
	}
	return 1, errors.New("requested observation mode requires a supported Linux host; target was not started")
}
