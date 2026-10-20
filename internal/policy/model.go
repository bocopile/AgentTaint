package policy

import (
	"net/netip"

	"github.com/bocopile/AgentTaint/internal/core"
)

type Action string

const (
	Allow Action = "allow"
	Audit Action = "audit"
	Deny  Action = "deny"
)

type Decision struct {
	Action Action
	Rule   string
}

type source struct {
	path  string
	label core.Label
}

type rule struct {
	name     string
	label    core.Label
	prefixes []netip.Prefix
	ports    map[uint16]struct{}
	action   Action
}

// Snapshot is an immutable, validated policy loaded once for a run.
type Snapshot struct {
	digest  string
	sources []source
	rules   []rule
}

// KernelRule is one bounded IPv4/port match projected from a validated rule.
// It contains only deny matches: allow/audit have no effect on kernel blocking
// because deny has higher precedence in Snapshot.Evaluate.
type KernelRule struct {
	Address   [4]byte
	PrefixLen uint8
	Port      uint16
	AnyPort   bool
}

// KernelProjection is the deliberately small policy subset supported by the
// synchronous Linux enforcement backend.
type KernelProjection struct {
	Sources []string
	Denies  []KernelRule
}

func (s *Snapshot) Digest() string {
	if s == nil {
		return ""
	}
	return s.digest
}

// SourcePaths returns a copy of the resolved policy source paths.
func (s *Snapshot) SourcePaths() []string {
	if s == nil {
		return nil
	}
	paths := make([]string, len(s.sources))
	for i, source := range s.sources {
		paths[i] = source.path
	}
	return paths
}
