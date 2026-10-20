//go:build !linux || (!386 && !amd64 && !arm && !arm64 && !loong64 && !mips64le && !mipsle && !ppc64le && !riscv64)

package enforce

import (
	"errors"

	"github.com/bocopile/AgentTaint/internal/policy"
)

type Enforcer struct{}

func New(policy.KernelProjection, uint64) (*Enforcer, error) {
	return nil, errors.New("kernel enforcement is available only on supported little-endian Linux architectures")
}

func (*Enforcer) Close() error { return nil }
